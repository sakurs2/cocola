package project

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct{ pool *pgxpool.Pool }

var _ Store = (*Postgres)(nil)

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

func (p *Postgres) SaveOAuthState(ctx context.Context, id Identity, nonceHash string, expiresAt, now time.Time) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO scm_oauth_states
		(nonce_hash, tenant_id, user_id, expires_at, created_at) VALUES ($1,$2,$3,$4,$5)`,
		nonceHash, id.TenantID, id.UserID, expiresAt, now)
	return err
}

func (p *Postgres) ConsumeOAuthState(ctx context.Context, id Identity, nonceHash string, now time.Time) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM scm_oauth_states
		WHERE nonce_hash=$1 AND tenant_id=$2 AND user_id=$3 AND expires_at >= $4`,
		nonceHash, id.TenantID, id.UserID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidArgument
	}
	// Opportunistic cleanup is bounded to this user-triggered operation; no poller.
	_, _ = p.pool.Exec(ctx, `DELETE FROM scm_oauth_states WHERE expires_at < $1`, now.Add(-time.Hour))
	return nil
}

const connectionColumns = `id::text, tenant_id, user_id, provider, external_user_id,
	external_login, COALESCE(installation_id, 0), access_token_ciphertext,
	access_token_expires_at, refresh_token_ciphertext, refresh_token_expires_at,
	status, created_at, updated_at`

func scanConnection(row pgx.Row) (Connection, error) {
	var c Connection
	err := row.Scan(&c.ID, &c.TenantID, &c.UserID, &c.Provider, &c.ExternalUserID,
		&c.ExternalLogin, &c.InstallationID, &c.AccessTokenCiphertext,
		&c.AccessTokenExpiresAt, &c.RefreshTokenCiphertext, &c.RefreshTokenExpiresAt,
		&c.Status, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	return c, err
}

func (p *Postgres) GetConnection(ctx context.Context, id Identity) (Connection, error) {
	return scanConnection(p.pool.QueryRow(ctx, `SELECT `+connectionColumns+`
		FROM scm_connections WHERE tenant_id=$1 AND user_id=$2 AND provider='github'`, id.TenantID, id.UserID))
}

func (p *Postgres) UpsertConnection(ctx context.Context, c Connection) (Connection, error) {
	const q = `INSERT INTO scm_connections (
		id, tenant_id, user_id, provider, external_user_id, external_login, installation_id,
		access_token_ciphertext, access_token_expires_at, refresh_token_ciphertext,
		refresh_token_expires_at, status, created_at, updated_at
	) VALUES ($1::uuid,$2,$3,'github',$4,$5,NULLIF($6,0),$7,$8,$9,$10,$11,$12,$13)
	ON CONFLICT (tenant_id, user_id, provider) DO UPDATE SET
		external_user_id=EXCLUDED.external_user_id,
		external_login=EXCLUDED.external_login,
		installation_id=EXCLUDED.installation_id,
		access_token_ciphertext=EXCLUDED.access_token_ciphertext,
		access_token_expires_at=EXCLUDED.access_token_expires_at,
		refresh_token_ciphertext=EXCLUDED.refresh_token_ciphertext,
		refresh_token_expires_at=EXCLUDED.refresh_token_expires_at,
		status=EXCLUDED.status,
		updated_at=EXCLUDED.updated_at
	RETURNING ` + connectionColumns
	result, err := scanConnection(p.pool.QueryRow(ctx, q, c.ID, c.TenantID, c.UserID,
		c.ExternalUserID, c.ExternalLogin, c.InstallationID, c.AccessTokenCiphertext,
		c.AccessTokenExpiresAt, c.RefreshTokenCiphertext, c.RefreshTokenExpiresAt,
		c.Status, c.CreatedAt, c.UpdatedAt))
	if postgresCode(err, "23505") {
		return Connection{}, ErrConflict
	}
	return result, err
}

// RefreshConnection serializes an expiring GitHub user-token refresh across
// Gateway replicas. GitHub refresh tokens rotate, so a process-local mutex is
// insufficient: only the holder of this database row lock may call upstream.
func (p *Postgres) RefreshConnection(
	ctx context.Context,
	id Identity,
	refresh func(Connection) (Connection, bool, error),
) (Connection, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Connection{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	c, err := scanConnection(tx.QueryRow(ctx, `SELECT `+connectionColumns+`
		FROM scm_connections WHERE tenant_id=$1 AND user_id=$2 AND provider='github' FOR UPDATE`,
		id.TenantID, id.UserID))
	if err != nil {
		return Connection{}, err
	}
	updated, changed, err := refresh(c)
	if err != nil {
		return Connection{}, err
	}
	if changed {
		updated, err = scanConnection(tx.QueryRow(ctx, `UPDATE scm_connections SET
			access_token_ciphertext=$4, access_token_expires_at=$5,
			refresh_token_ciphertext=$6, refresh_token_expires_at=$7,
			status=$8, updated_at=$9
			WHERE tenant_id=$1 AND user_id=$2 AND provider='github' AND id=$3::uuid
			RETURNING `+connectionColumns, id.TenantID, id.UserID, c.ID,
			updated.AccessTokenCiphertext, updated.AccessTokenExpiresAt,
			updated.RefreshTokenCiphertext, updated.RefreshTokenExpiresAt,
			updated.Status, updated.UpdatedAt))
		if err != nil {
			return Connection{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Connection{}, err
	}
	return updated, nil
}

func (p *Postgres) DeleteConnection(ctx context.Context, id Identity) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM scm_connections
		WHERE tenant_id=$1 AND user_id=$2 AND provider='github'`, id.TenantID, id.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const projectColumns = `id::text, tenant_id, owner_user_id, name, description, runtime_id,
	repository_mode, repository_provider, COALESCE(repository_external_id, 0), repository_owner,
	repository_name, repository_html_url, COALESCE(installation_id, 0), default_branch,
	visibility, repository_size_kb, repository_has_lfs, repository_has_submodule,
	status, provision_error_code, provision_request_id,
	provision_started_at, version, created_at, updated_at, archived_at`

const joinedProjectColumns = `projects.id::text, projects.tenant_id, projects.owner_user_id,
	projects.name, projects.description, projects.runtime_id, projects.repository_mode,
	projects.repository_provider, COALESCE(projects.repository_external_id, 0),
	projects.repository_owner, projects.repository_name, projects.repository_html_url,
	COALESCE(projects.installation_id, 0), projects.default_branch, projects.visibility,
	projects.repository_size_kb, projects.repository_has_lfs, projects.repository_has_submodule,
	projects.status, projects.provision_error_code,
	projects.provision_request_id, projects.provision_started_at, projects.version,
	projects.created_at, projects.updated_at, projects.archived_at`

func scanProject(row pgx.Row) (Project, error) {
	var v Project
	err := row.Scan(&v.ID, &v.TenantID, &v.OwnerUserID, &v.Name, &v.Description,
		&v.RuntimeID, &v.RepositoryMode, &v.RepositoryProvider, &v.RepositoryExternalID,
		&v.RepositoryOwner, &v.RepositoryName, &v.RepositoryHTMLURL, &v.InstallationID,
		&v.DefaultBranch, &v.Visibility, &v.RepositorySizeKB, &v.RepositoryHasLFS,
		&v.RepositoryHasSubmodule, &v.Status,
		&v.ProvisionErrorCode, &v.ProvisionRequestID, &v.ProvisionStartedAt, &v.Version,
		&v.CreatedAt, &v.UpdatedAt, &v.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return v, err
}

func scanProjectRows(rows pgx.Rows) ([]Project, error) {
	out := make([]Project, 0)
	for rows.Next() {
		v, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (p *Postgres) ListProjects(ctx context.Context, id Identity) ([]Project, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+projectColumns+` FROM projects
		WHERE tenant_id=$1 AND owner_user_id=$2 AND status <> 'archived'
		ORDER BY updated_at DESC, id DESC`, id.TenantID, id.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProjectRows(rows)
}

func (p *Postgres) GetProject(ctx context.Context, id Identity, projectID string) (Project, error) {
	return scanProject(p.pool.QueryRow(ctx, `SELECT `+projectColumns+` FROM projects
		WHERE id=$1::uuid AND tenant_id=$2 AND owner_user_id=$3`, projectID, id.TenantID, id.UserID))
}

func (p *Postgres) GetProjectByRequest(ctx context.Context, id Identity, requestID string) (Project, error) {
	return scanProject(p.pool.QueryRow(ctx, `SELECT `+projectColumns+` FROM projects
		WHERE tenant_id=$1 AND owner_user_id=$2 AND provision_request_id=$3`, id.TenantID, id.UserID, requestID))
}

func (p *Postgres) CreateProject(ctx context.Context, v Project) (Project, error) {
	const q = `INSERT INTO projects (
		id, tenant_id, owner_user_id, name, description, runtime_id, repository_mode,
		repository_provider, repository_external_id, repository_name, visibility, status, provision_request_id,
		provision_started_at, version, created_at, updated_at
	) VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,'github',NULLIF($8,0),$9,$10,'provisioning',$11,$12,1,$13,$14)
	RETURNING ` + projectColumns
	result, err := scanProject(p.pool.QueryRow(ctx, q, v.ID, v.TenantID, v.OwnerUserID,
		v.Name, v.Description, v.RuntimeID, v.RepositoryMode, v.RepositoryExternalID,
		v.RepositoryName, v.Visibility, v.ProvisionRequestID, v.ProvisionStartedAt, v.CreatedAt, v.UpdatedAt))
	if postgresCode(err, "23505") {
		return Project{}, ErrConflict
	}
	return result, err
}

func (p *Postgres) CompleteProject(ctx context.Context, id Identity, projectID string, repo Repository, installationID int64, now time.Time) (Project, error) {
	visibility := repo.Visibility
	if visibility == "" {
		if repo.Private {
			visibility = "private"
		} else {
			visibility = "public"
		}
	}
	const q = `UPDATE projects SET
		repository_external_id=$4, repository_owner=$5, repository_name=$6,
		repository_html_url=$7, installation_id=$8, default_branch=$9,
		visibility=$10, repository_size_kb=$11, repository_has_lfs=$12,
		repository_has_submodule=$13, status='ready', provision_error_code='',
		version=version+1, updated_at=$14
	WHERE id=$1::uuid AND tenant_id=$2 AND owner_user_id=$3 AND status IN ('provisioning','failed')
	RETURNING ` + projectColumns
	result, err := scanProject(p.pool.QueryRow(ctx, q, projectID, id.TenantID, id.UserID,
		repo.ID, repo.Owner, repo.Name, repo.HTMLURL, installationID, repo.DefaultBranch,
		visibility, repo.SizeKB, repo.HasLFS, repo.HasSubmodule, now))
	if postgresCode(err, "23505") {
		return Project{}, ErrConflict
	}
	return result, err
}

func (p *Postgres) FailProject(ctx context.Context, id Identity, projectID, code string, now time.Time) (Project, error) {
	return scanProject(p.pool.QueryRow(ctx, `UPDATE projects SET status='failed',
		provision_error_code=$4, version=version+1, updated_at=$5
		WHERE id=$1::uuid AND tenant_id=$2 AND owner_user_id=$3 AND status <> 'archived'
		RETURNING `+projectColumns, projectID, id.TenantID, id.UserID, code, now))
}

func (p *Postgres) UpdateProject(ctx context.Context, id Identity, projectID string, expected int64, name, description, runtimeID string, now time.Time) (Project, error) {
	v, err := scanProject(p.pool.QueryRow(ctx, `UPDATE projects SET name=$5, description=$6,
		runtime_id=$7, version=version+1, updated_at=$8
		WHERE id=$1::uuid AND tenant_id=$2 AND owner_user_id=$3 AND version=$4 AND status <> 'archived'
		RETURNING `+projectColumns, projectID, id.TenantID, id.UserID, expected, name, description, runtimeID, now))
	if errors.Is(err, ErrNotFound) {
		if _, getErr := p.GetProject(ctx, id, projectID); getErr == nil {
			return Project{}, ErrVersionConflict
		}
	}
	if postgresCode(err, "23505") {
		return Project{}, ErrConflict
	}
	return v, err
}

func (p *Postgres) ArchiveProject(ctx context.Context, id Identity, projectID string, expected int64, now time.Time) (Project, error) {
	v, err := scanProject(p.pool.QueryRow(ctx, `UPDATE projects SET status='archived',
		archived_at=$5, version=version+1, updated_at=$5
		WHERE id=$1::uuid AND tenant_id=$2 AND owner_user_id=$3 AND version=$4 AND status <> 'archived'
		RETURNING `+projectColumns, projectID, id.TenantID, id.UserID, expected, now))
	if errors.Is(err, ErrNotFound) {
		if _, getErr := p.GetProject(ctx, id, projectID); getErr == nil {
			return Project{}, ErrVersionConflict
		}
	}
	return v, err
}

func (p *Postgres) ListTasks(ctx context.Context, id Identity, projectID string) ([]Task, error) {
	if _, err := p.GetProject(ctx, id, projectID); err != nil {
		return nil, err
	}
	const q = `SELECT c.id, c.title, c.runtime_id, c.created_at, c.updated_at,
		w.conversation_id, w.project_id::text, w.base_ref, w.base_sha, w.branch_name,
		w.head_sha, w.bootstrap_status, w.bootstrap_error_code, w.git_snapshot_json,
		w.git_snapshot_at, w.created_at, w.updated_at
	FROM conversations c JOIN project_workspaces w ON w.conversation_id=c.id
	WHERE c.project_id=$1::uuid AND c.user_id=$2 ORDER BY c.updated_at DESC, c.id DESC`
	rows, err := p.pool.Query(ctx, q, projectID, id.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Task, 0)
	for rows.Next() {
		var task Task
		if err := rows.Scan(&task.ID, &task.Title, &task.RuntimeID, &task.CreatedAt, &task.UpdatedAt,
			&task.Workspace.ConversationID, &task.Workspace.ProjectID, &task.Workspace.BaseRef,
			&task.Workspace.BaseSHA, &task.Workspace.BranchName, &task.Workspace.HeadSHA,
			&task.Workspace.BootstrapStatus, &task.Workspace.BootstrapErrorCode,
			&task.Workspace.GitSnapshotRaw, &task.Workspace.GitSnapshotAt,
			&task.Workspace.CreatedAt, &task.Workspace.UpdatedAt); err != nil {
			return nil, err
		}
		decodeSnapshot(&task.Workspace)
		out = append(out, task)
	}
	return out, rows.Err()
}

func scanWorkspaceProject(row pgx.Row) (Workspace, Project, error) {
	var w Workspace
	var v Project
	err := row.Scan(&w.ConversationID, &w.ProjectID, &w.BaseRef, &w.BaseSHA,
		&w.BranchName, &w.HeadSHA, &w.BootstrapStatus, &w.BootstrapErrorCode,
		&w.GitSnapshotRaw, &w.GitSnapshotAt, &w.CreatedAt, &w.UpdatedAt,
		&v.ID, &v.TenantID, &v.OwnerUserID, &v.Name, &v.Description,
		&v.RuntimeID, &v.RepositoryMode, &v.RepositoryProvider, &v.RepositoryExternalID,
		&v.RepositoryOwner, &v.RepositoryName, &v.RepositoryHTMLURL, &v.InstallationID,
		&v.DefaultBranch, &v.Visibility, &v.RepositorySizeKB, &v.RepositoryHasLFS,
		&v.RepositoryHasSubmodule, &v.Status,
		&v.ProvisionErrorCode, &v.ProvisionRequestID, &v.ProvisionStartedAt,
		&v.Version, &v.CreatedAt, &v.UpdatedAt, &v.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Workspace{}, Project{}, ErrNotFound
	}
	if err == nil {
		decodeSnapshot(&w)
	}
	return w, v, err
}

func (p *Postgres) GetWorkspace(ctx context.Context, id Identity, conversationID string) (Workspace, Project, error) {
	const q = `SELECT w.conversation_id, w.project_id::text, w.base_ref, w.base_sha,
		w.branch_name, w.head_sha, w.bootstrap_status, w.bootstrap_error_code,
		w.git_snapshot_json, w.git_snapshot_at, w.created_at, w.updated_at, ` + joinedProjectColumns + `
	FROM project_workspaces w
	JOIN projects ON projects.id=w.project_id
	JOIN conversations c ON c.id=w.conversation_id
	WHERE w.conversation_id=$1 AND c.user_id=$2 AND projects.tenant_id=$3 AND projects.owner_user_id=$2`
	return scanWorkspaceProject(p.pool.QueryRow(ctx, q, conversationID, id.UserID, id.TenantID))
}

func (p *Postgres) LockBaseSHA(ctx context.Context, id Identity, conversationID, sha string, now time.Time) (Workspace, error) {
	const q = `UPDATE project_workspaces w SET base_sha=$4, updated_at=$5
	FROM conversations c, projects p
	WHERE w.conversation_id=$1 AND c.id=w.conversation_id AND p.id=w.project_id
		AND c.user_id=$2 AND p.tenant_id=$3 AND p.owner_user_id=$2
		AND (w.base_sha='' OR w.base_sha=$4)
	RETURNING w.conversation_id, w.project_id::text, w.base_ref, w.base_sha,
		w.branch_name, w.head_sha, w.bootstrap_status, w.bootstrap_error_code,
		w.git_snapshot_json, w.git_snapshot_at, w.created_at, w.updated_at`
	var w Workspace
	err := p.pool.QueryRow(ctx, q, conversationID, id.UserID, id.TenantID, sha, now).Scan(
		&w.ConversationID, &w.ProjectID, &w.BaseRef, &w.BaseSHA, &w.BranchName, &w.HeadSHA,
		&w.BootstrapStatus, &w.BootstrapErrorCode, &w.GitSnapshotRaw, &w.GitSnapshotAt,
		&w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Workspace{}, ErrConflict
	}
	decodeSnapshot(&w)
	return w, err
}

func (p *Postgres) SaveSnapshot(ctx context.Context, id Identity, conversationID string, snapshot GitSnapshot, headSHA, bootstrapStatus string, now time.Time) error {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `UPDATE project_workspaces w SET
		head_sha=$4, bootstrap_status=$5, bootstrap_error_code='', git_snapshot_json=$6,
		git_snapshot_at=$7, updated_at=$7
	FROM conversations c, projects p
	WHERE w.conversation_id=$1 AND c.id=w.conversation_id AND p.id=w.project_id
		AND c.user_id=$2 AND p.tenant_id=$3 AND p.owner_user_id=$2`,
		conversationID, id.UserID, id.TenantID, headSHA, bootstrapStatus, raw, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) MarkBootstrapFailed(ctx context.Context, id Identity, conversationID, code string, now time.Time) error {
	tag, err := p.pool.Exec(ctx, `UPDATE project_workspaces w SET
		bootstrap_status='failed', bootstrap_error_code=$4, updated_at=$5
	FROM conversations c, projects p
	WHERE w.conversation_id=$1 AND c.id=w.conversation_id AND p.id=w.project_id
		AND c.user_id=$2 AND p.tenant_id=$3 AND p.owner_user_id=$2`,
		conversationID, id.UserID, id.TenantID, code, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func decodeSnapshot(w *Workspace) {
	if len(w.GitSnapshotRaw) > 0 {
		_ = json.Unmarshal(w.GitSnapshotRaw, &w.GitSnapshot)
	}
	if w.GitSnapshotAt != nil && w.GitSnapshot.CapturedAt.IsZero() {
		w.GitSnapshot.CapturedAt = *w.GitSnapshotAt
	}
}

func postgresCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
