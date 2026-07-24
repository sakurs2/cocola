package chatrun

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
)

type Postgres struct{ pool *pgxpool.Pool }

func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() { p.pool.Close() }

// RuntimeSetting reads a dynamic execution setting at the start of a new Run.
// Admin API remains the only writer and validator of system_settings.
func (p *Postgres) RuntimeSetting(ctx context.Context, key string) (json.RawMessage, error) {
	var value json.RawMessage
	err := p.pool.QueryRow(ctx, `SELECT value_json FROM system_settings WHERE key=$1`, key).Scan(&value)
	return value, err
}

const runColumns = `trace_id, root_span_id, conversation_id, conversation_title,
	user_id, source, model_route_id, model_alias, client_request_id, interaction_mode,
	COALESCE(plan_id::text, ''), status, started_at,
	completed_at, last_activity_at, error_code`

func scanRun(row pgx.Row) (Run, error) {
	var run Run
	if err := row.Scan(
		&run.ID, &run.RootSpanID, &run.ConversationID, &run.ConversationTitle,
		&run.UserID, &run.Source, &run.ModelRouteID, &run.ModelAlias, &run.ClientRequestID,
		&run.InteractionMode, &run.PlanID, &run.Status,
		&run.StartedAt, &run.CompletedAt, &run.LastActivityAt, &run.ErrorCode,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Run{}, ErrNotFound
		}
		return Run{}, err
	}
	return run, nil
}

func (p *Postgres) findRequest(ctx context.Context, userID, conversationID, requestID string) (Run, error) {
	return scanRun(p.pool.QueryRow(ctx, `SELECT `+runColumns+` FROM conversation_runs
		WHERE user_id=$1 AND conversation_id=$2 AND client_request_id=$3`,
		userID, conversationID, requestID))
}

func (p *Postgres) GetRequest(
	ctx context.Context,
	conversationID, userID, clientRequestID string,
) (Run, error) {
	return p.findRequest(ctx, userID, conversationID, clientRequestID)
}

func (p *Postgres) Start(ctx context.Context, in StartInput) (StartResult, error) {
	in.Run.InteractionMode = normalizeInteractionMode(in.Run.InteractionMode)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return StartResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	effective, err := scanConversation(tx.QueryRow(ctx, `SELECT id, user_id, tenant_id,
		title, chat_type, COALESCE(folder_id, ''), COALESCE(project_id::text, ''), hidden, runtime_id, created_at, updated_at
		FROM conversations WHERE id=$1 FOR UPDATE`, in.Conversation.ID))
	createdConversation := false
	if errors.Is(err, convo.ErrNotFound) {
		effective = in.Conversation
		if effective.ChatType == "" {
			effective.ChatType = "chat"
		}
		var projectDefaultBranch string
		var projectProvider string
		var projectPrimaryConversationID string
		if effective.ProjectID != "" {
			if effective.FolderID != "" || effective.ChatType != "chat" {
				return StartResult{}, ErrProjectNotFound
			}
			var projectRuntime, projectStatus string
			projectErr := tx.QueryRow(ctx, `SELECT default_branch, runtime_id, status,
				repository_provider, COALESCE(primary_conversation_id, '') FROM projects
				WHERE id=$1::uuid AND tenant_id=$2 AND owner_user_id=$3 FOR UPDATE`,
				effective.ProjectID, effective.TenantID, effective.UserID).Scan(
				&projectDefaultBranch, &projectRuntime, &projectStatus, &projectProvider,
				&projectPrimaryConversationID)
			if projectErr == pgx.ErrNoRows {
				return StartResult{}, ErrProjectNotFound
			}
			if projectErr != nil {
				return StartResult{}, projectErr
			}
			if projectStatus != "ready" {
				return StartResult{}, ErrProjectNotReady
			}
			if projectProvider == "local" && projectPrimaryConversationID != "" &&
				projectPrimaryConversationID != effective.ID {
				return StartResult{}, ErrProjectSingleTask
			}
			if effective.RuntimeID == "" {
				effective.RuntimeID = projectRuntime
			}
		}
		if effective.RuntimeID == "" {
			effective.RuntimeID = convo.DefaultRuntimeID
		}
		if effective.FolderID != "" {
			var folderID string
			folderErr := tx.QueryRow(ctx, `SELECT id FROM conversation_folders
				WHERE id=$1 AND user_id=$2 FOR SHARE`, effective.FolderID, effective.UserID).Scan(&folderID)
			if folderErr == pgx.ErrNoRows {
				return StartResult{}, ErrFolderNotFound
			}
			if folderErr != nil {
				return StartResult{}, folderErr
			}
		}
		tag, insertErr := tx.Exec(ctx, `INSERT INTO conversations
			(id, user_id, tenant_id, title, chat_type, folder_id, project_id, hidden, runtime_id, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,'')::uuid,$8,$9,$10,$11)
			ON CONFLICT (id) DO NOTHING`, effective.ID, effective.UserID,
			effective.TenantID, effective.Title, effective.ChatType, effective.FolderID, effective.ProjectID,
			effective.Hidden, effective.RuntimeID, effective.CreatedAt, effective.UpdatedAt)
		if insertErr != nil {
			return StartResult{}, insertErr
		}
		if tag.RowsAffected() == 1 {
			createdConversation = true
			if effective.ProjectID != "" {
				projectBaseRef := strings.TrimSpace(in.ProjectBaseRef)
				if projectBaseRef == "" {
					projectBaseRef = projectDefaultBranch
				}
				branchName := taskBranch(effective.ID)
				if projectProvider == "local" {
					branchName = "main"
					tag, insertErr = tx.Exec(ctx, `UPDATE projects SET primary_conversation_id=$2
						WHERE id=$1::uuid AND (primary_conversation_id IS NULL OR primary_conversation_id=$2)`,
						effective.ProjectID, effective.ID)
					if insertErr != nil {
						return StartResult{}, insertErr
					}
					if tag.RowsAffected() != 1 {
						return StartResult{}, ErrProjectSingleTask
					}
				}
				_, insertErr = tx.Exec(ctx, `INSERT INTO project_workspaces
					(conversation_id, project_id, base_ref, base_sha, branch_name, created_at, updated_at)
					VALUES ($1,$2::uuid,$3,$4,$5,$6,$6)`, effective.ID, effective.ProjectID,
					projectBaseRef, strings.TrimSpace(in.ProjectBaseSHA), branchName, effective.CreatedAt)
				if insertErr != nil {
					return StartResult{}, insertErr
				}
			}
		} else {
			effective, err = scanConversation(tx.QueryRow(ctx, `SELECT id, user_id, tenant_id,
				title, chat_type, COALESCE(folder_id, ''), COALESCE(project_id::text, ''), hidden, runtime_id, created_at, updated_at
				FROM conversations WHERE id=$1 FOR UPDATE`, in.Conversation.ID))
			if err != nil {
				return StartResult{}, err
			}
		}
	} else if err != nil {
		return StartResult{}, err
	}
	if !createdConversation {
		if effective.UserID != in.Conversation.UserID {
			return StartResult{}, ErrNotFound
		}
		if in.Conversation.RuntimeID != "" && in.Conversation.RuntimeID != effective.RuntimeID {
			return StartResult{}, ErrRuntimeMismatch
		}
		if in.Run.ClientRequestID != "" {
			run, requestErr := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM conversation_runs
				WHERE user_id=$1 AND conversation_id=$2 AND client_request_id=$3`,
				in.Run.UserID, in.Run.ConversationID, in.Run.ClientRequestID))
			if requestErr == nil {
				return StartResult{Run: run, Conversation: effective}, nil
			}
			if !errors.Is(requestErr, ErrNotFound) {
				return StartResult{}, requestErr
			}
		}
		if in.Conversation.FolderID != "" && in.Conversation.FolderID != effective.FolderID {
			return StartResult{}, ErrFolderMismatch
		}
		if in.Conversation.ProjectID != "" && in.Conversation.ProjectID != effective.ProjectID {
			return StartResult{}, ErrProjectMismatch
		}
		effective.UpdatedAt = in.Conversation.UpdatedAt
		if _, err = tx.Exec(ctx, `UPDATE conversations SET updated_at=$2 WHERE id=$1`,
			effective.ID, effective.UpdatedAt); err != nil {
			return StartResult{}, err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO conversation_runs (
		trace_id, root_span_id, conversation_id, conversation_title, user_id,
		user_email, source, model_route_id, model_alias, client_request_id, interaction_mode,
		status, started_at,
		last_activity_at, detail_status)
		VALUES ($1,$2,$3,$4,$5,$5,$6,$7,$8,$9,$10,'running',$11,$11,'available')`,
		in.Run.ID, in.Run.RootSpanID, in.Run.ConversationID, in.Run.ConversationTitle,
		in.Run.UserID, in.Run.Source, in.Run.ModelRouteID, in.Run.ModelAlias, in.Run.ClientRequestID,
		normalizeInteractionMode(in.Run.InteractionMode), in.Run.StartedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			_ = tx.Rollback(ctx)
			if in.Run.ClientRequestID != "" {
				if run, findErr := p.findRequest(ctx, in.Run.UserID, in.Run.ConversationID, in.Run.ClientRequestID); findErr == nil {
					return StartResult{Run: run, Conversation: effective}, nil
				}
			}
			if run, activeErr := p.Active(ctx, in.Run.ConversationID, in.Run.UserID); activeErr == nil {
				return StartResult{Run: run, Conversation: effective}, ErrConflict
			}
		}
		return StartResult{}, err
	}
	parts, metadata, err := marshalMessage(in.UserMessage)
	if err != nil {
		return StartResult{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO messages
		(id, conversation_id, role, parts_json, metadata_json, created_at)
		VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (id) DO NOTHING`,
		in.UserMessage.ID, in.UserMessage.ConversationID, in.UserMessage.Role,
		parts, metadata, in.UserMessage.CreatedAt)
	if err != nil {
		return StartResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return StartResult{}, err
	}
	return StartResult{Run: in.Run, Conversation: effective, Created: true}, nil
}

func scanConversation(row pgx.Row) (convo.Conversation, error) {
	var conversation convo.Conversation
	if err := row.Scan(&conversation.ID, &conversation.UserID, &conversation.TenantID,
		&conversation.Title, &conversation.ChatType, &conversation.FolderID, &conversation.ProjectID, &conversation.Hidden, &conversation.RuntimeID,
		&conversation.CreatedAt, &conversation.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return convo.Conversation{}, convo.ErrNotFound
		}
		return convo.Conversation{}, err
	}
	return conversation, nil
}

func taskBranch(conversationID string) string {
	short := strings.ReplaceAll(strings.TrimSpace(conversationID), "-", "")
	if len(short) > 12 {
		short = short[:12]
	}
	if short == "" {
		short = "workspace"
	}
	return "cocola/task-" + short
}

func (p *Postgres) GetOwned(ctx context.Context, runID, userID string) (Run, error) {
	return scanRun(p.pool.QueryRow(ctx, `SELECT `+runColumns+` FROM conversation_runs
		WHERE trace_id=$1 AND user_id=$2`, runID, userID))
}

func (p *Postgres) Active(ctx context.Context, conversationID, userID string) (Run, error) {
	return scanRun(p.pool.QueryRow(ctx, `SELECT `+runColumns+` FROM conversation_runs
		WHERE conversation_id=$1 AND user_id=$2 AND status='running'
		ORDER BY started_at DESC LIMIT 1`, conversationID, userID))
}

func marshalMessage(message convo.Message) ([]byte, []byte, error) {
	parts, err := json.Marshal(message.Parts)
	if err != nil {
		return nil, nil, err
	}
	metadata := message.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	meta, err := json.Marshal(metadata)
	return parts, meta, err
}

func upsertMessage(ctx context.Context, tx pgx.Tx, message convo.Message) error {
	parts, metadata, err := marshalMessage(message)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO messages
		(id, conversation_id, role, parts_json, metadata_json, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (id) DO UPDATE SET
			parts_json=EXCLUDED.parts_json, metadata_json=EXCLUDED.metadata_json,
			created_at=EXCLUDED.created_at`,
		message.ID, message.ConversationID, message.Role, parts, metadata, message.CreatedAt)
	return err
}

const planColumns = `p.id::text, p.conversation_id, p.version, p.status,
	p.source_run_id, p.runtime_id, p.model_route_id, p.model_alias,
	p.content_markdown, p.workspace_revision, p.approved_by, p.approved_at,
	p.created_at, p.updated_at`

func scanPlan(row pgx.Row) (Plan, error) {
	var plan Plan
	if err := row.Scan(
		&plan.ID, &plan.ConversationID, &plan.Version, &plan.Status,
		&plan.SourceRunID, &plan.RuntimeID, &plan.ModelRouteID, &plan.ModelAlias,
		&plan.ContentMarkdown, &plan.WorkspaceRevision, &plan.ApprovedBy, &plan.ApprovedAt,
		&plan.CreatedAt, &plan.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Plan{}, ErrNotFound
		}
		return Plan{}, err
	}
	return plan, nil
}

func planPart(plan Plan) convo.Part {
	return convo.Part{
		Type: convo.PartPlan, PlanID: plan.ID, PlanVersion: plan.Version,
		Status: plan.Status, PlanContentMarkdown: plan.ContentMarkdown,
	}
}

func updatePlanMessageStatus(ctx context.Context, tx pgx.Tx, plan Plan) error {
	messageID := plan.SourceRunID + "-assistant"
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT parts_json FROM messages WHERE id=$1 FOR UPDATE`,
		messageID).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	var parts []convo.Part
	if err := json.Unmarshal(raw, &parts); err != nil {
		return err
	}
	changed := false
	for index := range parts {
		if parts[index].Type == convo.PartPlan && parts[index].PlanID == plan.ID {
			parts[index].Status = plan.Status
			changed = true
		}
	}
	if !changed {
		return nil
	}
	encoded, err := json.Marshal(parts)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE messages SET parts_json=$2 WHERE id=$1`, messageID, encoded)
	return err
}

func (p *Postgres) GetPlan(ctx context.Context, conversationID, planID, userID string) (Plan, error) {
	return scanPlan(p.pool.QueryRow(ctx, `SELECT `+planColumns+`
		FROM conversation_plans p JOIN conversations c ON c.id=p.conversation_id
		WHERE p.conversation_id=$1 AND p.id=$2::uuid AND c.user_id=$3`,
		conversationID, planID, userID))
}

func (p *Postgres) StartPlanExecution(
	ctx context.Context,
	in PlanExecutionInput,
) (PlanExecutionResult, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return PlanExecutionResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	conversation, err := scanConversation(tx.QueryRow(ctx, `SELECT id, user_id, tenant_id,
		title, chat_type, COALESCE(folder_id, ''), COALESCE(project_id::text, ''), hidden,
		runtime_id, created_at, updated_at FROM conversations
		WHERE id=$1 AND user_id=$2 FOR UPDATE`, in.ConversationID, in.UserID))
	if err != nil {
		return PlanExecutionResult{}, ErrNotFound
	}
	if in.Run.ClientRequestID != "" {
		existing, requestErr := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+`
			FROM conversation_runs WHERE user_id=$1 AND conversation_id=$2
			AND client_request_id=$3`, in.UserID, in.ConversationID, in.Run.ClientRequestID))
		if requestErr == nil {
			plan, planErr := scanPlan(tx.QueryRow(ctx, `SELECT `+planColumns+`
				FROM conversation_plans p WHERE p.id=$1::uuid`, in.PlanID))
			if planErr != nil {
				return PlanExecutionResult{}, planErr
			}
			if err := tx.Commit(ctx); err != nil {
				return PlanExecutionResult{}, err
			}
			return PlanExecutionResult{
				Run: existing, Conversation: conversation, Plan: plan,
			}, nil
		}
		if !errors.Is(requestErr, ErrNotFound) {
			return PlanExecutionResult{}, requestErr
		}
	}
	plan, err := scanPlan(tx.QueryRow(ctx, `SELECT `+planColumns+`
		FROM conversation_plans p WHERE p.id=$1::uuid AND p.conversation_id=$2 FOR UPDATE`,
		in.PlanID, in.ConversationID))
	if err != nil {
		return PlanExecutionResult{}, err
	}
	if plan.Version != in.ExpectedVersion {
		return PlanExecutionResult{}, ErrPlanNotCurrent
	}
	var latestVersion int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0)
		FROM conversation_plans WHERE conversation_id=$1`, in.ConversationID).Scan(&latestVersion); err != nil {
		return PlanExecutionResult{}, err
	}
	if latestVersion != plan.Version {
		return PlanExecutionResult{}, ErrPlanNotCurrent
	}
	if plan.Status != PlanStatusReady && plan.Status != PlanStatusStopped {
		return PlanExecutionResult{}, ErrPlanState
	}
	var modelAvailable bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM llm_model_routes r
		JOIN llm_providers p ON p.id=r.provider_id
		WHERE r.id=$1 AND r.protocol='anthropic-messages'
			AND r.enabled=TRUE AND r.visible=TRUE AND p.enabled=TRUE
	)`, plan.ModelRouteID).Scan(&modelAvailable); err != nil {
		return PlanExecutionResult{}, err
	}
	if !modelAvailable {
		return PlanExecutionResult{}, ErrPlanModelUnavailable
	}
	if _, activeErr := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+`
		FROM conversation_runs WHERE conversation_id=$1 AND status='running'
		ORDER BY started_at DESC LIMIT 1`, in.ConversationID)); activeErr == nil {
		return PlanExecutionResult{}, ErrConflict
	} else if !errors.Is(activeErr, ErrNotFound) {
		return PlanExecutionResult{}, activeErr
	}

	run := in.Run
	run.ConversationID = in.ConversationID
	run.UserID = in.UserID
	run.InteractionMode = InteractionModeExecute
	run.PlanID = plan.ID
	_, err = tx.Exec(ctx, `INSERT INTO conversation_runs (
		trace_id, root_span_id, conversation_id, conversation_title, user_id,
		user_email, source, model_route_id, model_alias, client_request_id,
		interaction_mode, plan_id, status, started_at, last_activity_at, detail_status)
		VALUES ($1,$2,$3,$4,$5,$5,$6,$7,$8,$9,'execute',$10::uuid,'running',$11,$11,'available')`,
		run.ID, run.RootSpanID, run.ConversationID, run.ConversationTitle, run.UserID,
		run.Source, run.ModelRouteID, run.ModelAlias, run.ClientRequestID, run.PlanID, run.StartedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return PlanExecutionResult{}, ErrConflict
		}
		return PlanExecutionResult{}, err
	}
	now := in.ApprovedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if plan.Status == PlanStatusReady {
		plan.ApprovedBy = in.UserID
		plan.ApprovedAt = &now
	}
	plan.Status = PlanStatusExecuting
	plan.UpdatedAt = now
	_, err = tx.Exec(ctx, `UPDATE conversation_plans SET status='executing',
		approved_by=CASE WHEN approved_by='' THEN $2 ELSE approved_by END,
		approved_at=COALESCE(approved_at, $3), updated_at=$3 WHERE id=$1::uuid`,
		plan.ID, in.UserID, now)
	if err != nil {
		return PlanExecutionResult{}, err
	}
	if err := updatePlanMessageStatus(ctx, tx, plan); err != nil {
		return PlanExecutionResult{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE conversations SET updated_at=$3
		WHERE id=$1 AND user_id=$2`, conversation.ID, in.UserID, now)
	if err != nil {
		return PlanExecutionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PlanExecutionResult{}, err
	}
	return PlanExecutionResult{
		Run: run, Conversation: conversation, Plan: plan, Created: true,
	}, nil
}

func (p *Postgres) CancelPlan(
	ctx context.Context,
	conversationID, planID, userID string,
	expectedVersion int,
	now time.Time,
) (Plan, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Plan{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	plan, err := scanPlan(tx.QueryRow(ctx, `SELECT `+planColumns+`
		FROM conversation_plans p JOIN conversations c ON c.id=p.conversation_id
		WHERE p.id=$1::uuid AND p.conversation_id=$2 AND c.user_id=$3 FOR UPDATE`,
		planID, conversationID, userID))
	if err != nil {
		return Plan{}, err
	}
	if plan.Version != expectedVersion {
		return Plan{}, ErrPlanNotCurrent
	}
	if plan.Status != PlanStatusReady && plan.Status != PlanStatusStopped {
		return Plan{}, ErrPlanState
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	plan.Status = PlanStatusCancelled
	plan.UpdatedAt = now
	if _, err := tx.Exec(ctx, `UPDATE conversation_plans SET status='cancelled',
		updated_at=$2 WHERE id=$1::uuid`, plan.ID, now); err != nil {
		return Plan{}, err
	}
	if err := updatePlanMessageStatus(ctx, tx, plan); err != nil {
		return Plan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (p *Postgres) SaveDraft(ctx context.Context, runID, userID string, message convo.Message) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var conversationID string
	err = tx.QueryRow(ctx, `SELECT conversation_id FROM conversation_runs
		WHERE trace_id=$1 AND user_id=$2 AND status='running' FOR UPDATE`, runID, userID).Scan(&conversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if message.ConversationID != conversationID {
		return ErrNotFound
	}
	if err := upsertMessage(ctx, tx, message); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE conversation_runs SET last_activity_at=now(), updated_at=now()
		WHERE trace_id=$1`, runID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) Finalize(ctx context.Context, in FinalizeInput) (FinalizeResult, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return FinalizeResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	run, err := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM conversation_runs
		WHERE trace_id=$1 AND user_id=$2 FOR UPDATE`, in.RunID, in.UserID))
	if err != nil {
		return FinalizeResult{}, err
	}
	if IsTerminal(run.Status) {
		if err := tx.Commit(ctx); err != nil {
			return FinalizeResult{}, err
		}
		return FinalizeResult{Run: run}, nil
	}
	now := in.CompletedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var createdPlan *Plan
	var supersededPlanID string
	if in.PlanCandidate != nil && run.InteractionMode == InteractionModePlan &&
		in.Status == StatusSuccess {
		candidate := in.PlanCandidate
		if candidate.ID == "" || candidate.ContentMarkdown == "" ||
			len(candidate.ContentMarkdown) > 128<<10 {
			return FinalizeResult{}, ErrPlanState
		}
		err = tx.QueryRow(ctx, `UPDATE conversation_plans SET status='superseded',
			updated_at=$2 WHERE conversation_id=$1
			AND status IN ('ready','stopped') RETURNING id::text`,
			run.ConversationID, now).Scan(&supersededPlanID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return FinalizeResult{}, err
		}
		if supersededPlanID != "" {
			superseded, findErr := scanPlan(tx.QueryRow(ctx, `SELECT `+planColumns+`
				FROM conversation_plans p WHERE p.id=$1::uuid`, supersededPlanID))
			if findErr != nil {
				return FinalizeResult{}, findErr
			}
			if err := updatePlanMessageStatus(ctx, tx, superseded); err != nil {
				return FinalizeResult{}, err
			}
		}
		var version int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0)+1
			FROM conversation_plans WHERE conversation_id=$1`,
			run.ConversationID).Scan(&version); err != nil {
			return FinalizeResult{}, err
		}
		plan := Plan{
			ID: candidate.ID, ConversationID: run.ConversationID, Version: version,
			Status: PlanStatusReady, SourceRunID: run.ID, RuntimeID: candidate.RuntimeID,
			ModelRouteID: candidate.ModelRouteID, ModelAlias: candidate.ModelAlias,
			ContentMarkdown:   candidate.ContentMarkdown,
			WorkspaceRevision: candidate.WorkspaceRevision, CreatedAt: now, UpdatedAt: now,
		}
		_, err = tx.Exec(ctx, `INSERT INTO conversation_plans (
			id, conversation_id, version, status, source_run_id, runtime_id,
			model_route_id, model_alias, content_markdown, workspace_revision,
			created_at, updated_at)
			VALUES ($1::uuid,$2,$3,'ready',$4,$5,$6,$7,$8,$9,$10,$10)`,
			plan.ID, plan.ConversationID, plan.Version, plan.SourceRunID, plan.RuntimeID,
			plan.ModelRouteID, plan.ModelAlias, plan.ContentMarkdown,
			plan.WorkspaceRevision, now)
		if err != nil {
			return FinalizeResult{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE conversation_runs SET plan_id=$2::uuid
			WHERE trace_id=$1`, run.ID, plan.ID); err != nil {
			return FinalizeResult{}, err
		}
		run.PlanID = plan.ID
		if in.AssistantMessage == nil {
			in.AssistantMessage = &convo.Message{
				ID: run.ID + "-assistant", ConversationID: run.ConversationID,
				Role: "assistant", CreatedAt: now,
			}
		}
		in.AssistantMessage.Parts = append(in.AssistantMessage.Parts, planPart(plan))
		createdPlan = &plan
	}
	if in.AssistantMessage != nil {
		if err := upsertMessage(ctx, tx, *in.AssistantMessage); err != nil {
			return FinalizeResult{}, err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE conversation_runs SET status=$2, error_code=$3,
		completed_at=$4, last_activity_at=$4, duration_ms=GREATEST(0, EXTRACT(EPOCH FROM ($4-started_at))*1000)::bigint,
		updated_at=now() WHERE trace_id=$1 AND status='running'`,
		in.RunID, in.Status, in.ErrorCode, now)
	if err != nil {
		return FinalizeResult{}, err
	}
	var executionPlan *Plan
	if run.PlanID != "" && run.InteractionMode == InteractionModeExecute {
		plan, planErr := scanPlan(tx.QueryRow(ctx, `SELECT `+planColumns+`
			FROM conversation_plans p WHERE p.id=$1::uuid FOR UPDATE`, run.PlanID))
		if planErr != nil {
			return FinalizeResult{}, planErr
		}
		switch in.Status {
		case StatusSuccess:
			plan.Status = PlanStatusCompleted
		case StatusCancelled, StatusInterrupted:
			plan.Status = PlanStatusStopped
		default:
			plan.Status = PlanStatusFailed
		}
		plan.UpdatedAt = now
		if _, err := tx.Exec(ctx, `UPDATE conversation_plans SET status=$2,
			updated_at=$3 WHERE id=$1::uuid`, plan.ID, plan.Status, now); err != nil {
			return FinalizeResult{}, err
		}
		if err := updatePlanMessageStatus(ctx, tx, plan); err != nil {
			return FinalizeResult{}, err
		}
		executionPlan = &plan
	}
	if in.Reveal {
		_, err = tx.Exec(ctx, `UPDATE conversations SET hidden=FALSE,
			title=CASE WHEN $3 <> '' THEN $3 ELSE title END, updated_at=$4
			WHERE id=$1 AND user_id=$2`, run.ConversationID, run.UserID, in.ConversationTitle, now)
	} else {
		_, err = tx.Exec(ctx, `UPDATE conversations SET updated_at=$3 WHERE id=$1 AND user_id=$2`,
			run.ConversationID, run.UserID, now)
	}
	if err != nil {
		return FinalizeResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return FinalizeResult{}, err
	}
	run.Status = in.Status
	run.ErrorCode = in.ErrorCode
	run.CompletedAt = &now
	run.LastActivityAt = now
	if createdPlan == nil {
		createdPlan = executionPlan
	}
	return FinalizeResult{
		Run: run, Plan: createdPlan, SupersededPlanID: supersededPlanID,
	}, nil
}

func (p *Postgres) InterruptRunning(ctx context.Context, now time.Time) (int64, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `UPDATE messages AS m SET metadata_json =
		COALESCE(m.metadata_json, '{}'::jsonb) || '{"partial":false,"interrupted":true}'::jsonb
		FROM conversation_runs AS r
		WHERE r.status='running' AND m.id=r.trace_id || '-assistant'`)
	if err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `UPDATE conversation_plans p SET status='stopped', updated_at=$1
		FROM conversation_runs r WHERE r.status='running' AND r.plan_id=p.id
		AND r.interaction_mode='execute' RETURNING `+planColumns, now)
	if err != nil {
		return 0, err
	}
	var stoppedPlans []Plan
	for rows.Next() {
		plan, scanErr := scanPlan(rows)
		if scanErr != nil {
			rows.Close()
			return 0, scanErr
		}
		stoppedPlans = append(stoppedPlans, plan)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, plan := range stoppedPlans {
		if err := updatePlanMessageStatus(ctx, tx, plan); err != nil {
			return 0, err
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE conversation_runs SET status='interrupted',
		error_code='GATEWAY_RESTARTED', completed_at=$1, last_activity_at=$1,
		duration_ms=GREATEST(0, EXTRACT(EPOCH FROM ($1-started_at))*1000)::bigint,
		updated_at=now() WHERE status='running'`, now)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
