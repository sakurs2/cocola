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
	user_id, source, model_route_id, model_alias, client_request_id, status, started_at,
	completed_at, last_activity_at, error_code`

func scanRun(row pgx.Row) (Run, error) {
	var run Run
	if err := row.Scan(
		&run.ID, &run.RootSpanID, &run.ConversationID, &run.ConversationTitle,
		&run.UserID, &run.Source, &run.ModelRouteID, &run.ModelAlias, &run.ClientRequestID, &run.Status,
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

func (p *Postgres) Start(ctx context.Context, in StartInput) (StartResult, error) {
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
		var projectBaseRef string
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
				&projectBaseRef, &projectRuntime, &projectStatus, &projectProvider,
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
					(conversation_id, project_id, base_ref, branch_name, created_at, updated_at)
					VALUES ($1,$2::uuid,$3,$4,$5,$5)`, effective.ID, effective.ProjectID,
					projectBaseRef, branchName, effective.CreatedAt)
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
		user_email, source, model_route_id, model_alias, client_request_id, status, started_at,
		last_activity_at, detail_status)
		VALUES ($1,$2,$3,$4,$5,$5,$6,$7,$8,$9,'running',$10,$10,'available')`,
		in.Run.ID, in.Run.RootSpanID, in.Run.ConversationID, in.Run.ConversationTitle,
		in.Run.UserID, in.Run.Source, in.Run.ModelRouteID, in.Run.ModelAlias, in.Run.ClientRequestID,
		in.Run.StartedAt)
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

func (p *Postgres) Finalize(ctx context.Context, in FinalizeInput) (Run, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Run{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	run, err := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM conversation_runs
		WHERE trace_id=$1 AND user_id=$2 FOR UPDATE`, in.RunID, in.UserID))
	if err != nil {
		return Run{}, err
	}
	if IsTerminal(run.Status) {
		return run, tx.Commit(ctx)
	}
	if in.AssistantMessage != nil {
		if err := upsertMessage(ctx, tx, *in.AssistantMessage); err != nil {
			return Run{}, err
		}
	}
	now := in.CompletedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	_, err = tx.Exec(ctx, `UPDATE conversation_runs SET status=$2, error_code=$3,
		completed_at=$4, last_activity_at=$4, duration_ms=GREATEST(0, EXTRACT(EPOCH FROM ($4-started_at))*1000)::bigint,
		updated_at=now() WHERE trace_id=$1 AND status='running'`,
		in.RunID, in.Status, in.ErrorCode, now)
	if err != nil {
		return Run{}, err
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
		return Run{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Run{}, err
	}
	run.Status = in.Status
	run.ErrorCode = in.ErrorCode
	run.CompletedAt = &now
	run.LastActivityAt = now
	return run, nil
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
