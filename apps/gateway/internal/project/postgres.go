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

func (p *Postgres) SaveFlowState(ctx context.Context, state FlowState) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO scm_flow_states (
		nonce_hash, tenant_id, user_id, provider, flow_type, return_to,
		public_origin, registration_id, expires_at, created_at
	) VALUES ($1,$2,$3,'github',$4,$5,$6,NULLIF($7,'')::uuid,$8,$9)`,
		state.NonceHash, state.TenantID, state.UserID, state.FlowType, state.ReturnTo,
		state.PublicOrigin, state.RegistrationID, state.ExpiresAt, state.CreatedAt)
	return err
}

func (p *Postgres) ConsumeFlowState(
	ctx context.Context,
	id Identity,
	nonceHash string,
	flowType string,
	now time.Time,
) (FlowState, error) {
	var state FlowState
	err := p.pool.QueryRow(ctx, `DELETE FROM scm_flow_states
		WHERE nonce_hash=$1 AND tenant_id=$2 AND user_id=$3 AND flow_type=$4
			AND expires_at >= $5
		RETURNING nonce_hash, tenant_id, user_id, provider, flow_type, return_to,
			public_origin, COALESCE(registration_id::text,''), expires_at, created_at`,
		nonceHash, id.TenantID, id.UserID, flowType, now).Scan(
		&state.NonceHash, &state.TenantID, &state.UserID, &state.Provider,
		&state.FlowType, &state.ReturnTo, &state.PublicOrigin, &state.RegistrationID,
		&state.ExpiresAt, &state.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return FlowState{}, ErrInvalidArgument
	}
	if err != nil {
		return FlowState{}, err
	}
	// User-triggered, bounded cleanup; there is deliberately no state poller.
	_, _ = p.pool.Exec(ctx, `DELETE FROM scm_flow_states WHERE expires_at < $1`, now.Add(-time.Hour))
	return state, nil
}

const registrationColumns = `id::text, tenant_id, user_id, provider, app_id,
	app_slug, client_id, client_secret_ciphertext, private_key_ciphertext,
	owner_external_id, owner_login, public_origin, status, version, created_at, updated_at`

func scanAppRegistration(row pgx.Row) (AppRegistration, error) {
	var registration AppRegistration
	err := row.Scan(&registration.ID, &registration.TenantID, &registration.UserID,
		&registration.Provider, &registration.AppID, &registration.AppSlug,
		&registration.ClientID, &registration.ClientSecretCiphertext,
		&registration.PrivateKeyCiphertext, &registration.OwnerExternalID,
		&registration.OwnerLogin, &registration.PublicOrigin, &registration.Status, &registration.Version,
		&registration.CreatedAt, &registration.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AppRegistration{}, ErrNotFound
	}
	return registration, err
}

func (p *Postgres) GetAppRegistration(ctx context.Context, id Identity) (AppRegistration, error) {
	return scanAppRegistration(p.pool.QueryRow(ctx, `SELECT `+registrationColumns+`
		FROM scm_app_registrations
		WHERE tenant_id=$1 AND user_id=$2 AND provider='github'`, id.TenantID, id.UserID))
}

func (p *Postgres) UpsertAppRegistration(ctx context.Context, value AppRegistration) (AppRegistration, error) {
	const query = `INSERT INTO scm_app_registrations (
		id, tenant_id, user_id, provider, app_id, app_slug, client_id,
		client_secret_ciphertext, private_key_ciphertext, owner_external_id,
		owner_login, public_origin, status, version, created_at, updated_at
	) VALUES ($1::uuid,$2,$3,'github',$4,$5,$6,$7,$8,$9,$10,$11,$12,1,$13,$14)
	ON CONFLICT (tenant_id, user_id, provider) DO UPDATE SET
		app_id=EXCLUDED.app_id, app_slug=EXCLUDED.app_slug,
		client_id=EXCLUDED.client_id,
		client_secret_ciphertext=EXCLUDED.client_secret_ciphertext,
		private_key_ciphertext=EXCLUDED.private_key_ciphertext,
		owner_external_id=EXCLUDED.owner_external_id,
		owner_login=EXCLUDED.owner_login, public_origin=EXCLUDED.public_origin,
		status=EXCLUDED.status,
		version=scm_app_registrations.version+1, updated_at=EXCLUDED.updated_at
	RETURNING ` + registrationColumns
	registration, err := scanAppRegistration(p.pool.QueryRow(ctx, query,
		value.ID, value.TenantID, value.UserID, value.AppID, value.AppSlug,
		value.ClientID, value.ClientSecretCiphertext, value.PrivateKeyCiphertext,
		value.OwnerExternalID, value.OwnerLogin, value.PublicOrigin, value.Status, value.CreatedAt,
		value.UpdatedAt))
	if postgresCode(err, "23505") {
		return AppRegistration{}, ErrConflict
	}
	return registration, err
}

func (p *Postgres) DeleteAppRegistration(ctx context.Context, id Identity) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM scm_app_registrations
		WHERE tenant_id=$1 AND user_id=$2 AND provider='github'`, id.TenantID, id.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

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
	status, created_at, updated_at, COALESCE(registration_id::text,'')`

func scanConnection(row pgx.Row) (Connection, error) {
	var c Connection
	err := row.Scan(&c.ID, &c.TenantID, &c.UserID, &c.Provider, &c.ExternalUserID,
		&c.ExternalLogin, &c.InstallationID, &c.AccessTokenCiphertext,
		&c.AccessTokenExpiresAt, &c.RefreshTokenCiphertext, &c.RefreshTokenExpiresAt,
		&c.Status, &c.CreatedAt, &c.UpdatedAt, &c.RegistrationID)
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
		, registration_id
	) VALUES ($1::uuid,$2,$3,'github',$4,$5,NULLIF($6,0),$7,$8,$9,$10,$11,$12,$13,NULLIF($14,'')::uuid)
	ON CONFLICT (tenant_id, user_id, provider) DO UPDATE SET
		external_user_id=EXCLUDED.external_user_id,
		external_login=EXCLUDED.external_login,
		installation_id=EXCLUDED.installation_id,
		access_token_ciphertext=EXCLUDED.access_token_ciphertext,
		access_token_expires_at=EXCLUDED.access_token_expires_at,
		refresh_token_ciphertext=EXCLUDED.refresh_token_ciphertext,
		refresh_token_expires_at=EXCLUDED.refresh_token_expires_at,
		registration_id=EXCLUDED.registration_id,
		status=EXCLUDED.status,
		updated_at=EXCLUDED.updated_at
	RETURNING ` + connectionColumns
	result, err := scanConnection(p.pool.QueryRow(ctx, q, c.ID, c.TenantID, c.UserID,
		c.ExternalUserID, c.ExternalLogin, c.InstallationID, c.AccessTokenCiphertext,
		c.AccessTokenExpiresAt, c.RefreshTokenCiphertext, c.RefreshTokenExpiresAt,
		c.Status, c.CreatedAt, c.UpdatedAt, c.RegistrationID))
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

const approvalColumns = `id::text, tenant_id, user_id, conversation_id, run_id,
	project_id::text, repository_id, command_digest, command_category, command_label,
	permissions_json, status, expires_at, decided_at, created_at, updated_at`

func scanApproval(row pgx.Row) (Approval, error) {
	var value Approval
	var permissions []byte
	err := row.Scan(&value.ID, &value.TenantID, &value.UserID, &value.ConversationID,
		&value.RunID, &value.ProjectID, &value.RepositoryID, &value.CommandDigest,
		&value.CommandCategory, &value.CommandLabel, &permissions, &value.Status,
		&value.ExpiresAt, &value.DecidedAt, &value.CreatedAt, &value.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Approval{}, ErrNotFound
	}
	if err != nil {
		return Approval{}, err
	}
	if err := json.Unmarshal(permissions, &value.Permissions); err != nil {
		return Approval{}, err
	}
	return value, nil
}

func (p *Postgres) GetOrCreateApproval(ctx context.Context, value Approval) (Approval, error) {
	permissions, err := json.Marshal(value.Permissions)
	if err != nil {
		return Approval{}, err
	}
	const query = `INSERT INTO scm_approvals (
		id, tenant_id, user_id, conversation_id, run_id, project_id, repository_id,
		command_digest, command_category, command_label, permissions_json, status,
		expires_at, created_at, updated_at
	) VALUES ($1::uuid,$2,$3,$4,$5,$6::uuid,$7,$8,$9,$10,$11::jsonb,'pending',$12,$13,$14)
	ON CONFLICT (run_id, command_digest) DO UPDATE SET updated_at=scm_approvals.updated_at
	RETURNING ` + approvalColumns
	return scanApproval(p.pool.QueryRow(ctx, query, value.ID, value.TenantID, value.UserID,
		value.ConversationID, value.RunID, value.ProjectID, value.RepositoryID,
		value.CommandDigest, value.CommandCategory, value.CommandLabel, permissions,
		value.ExpiresAt, value.CreatedAt, value.UpdatedAt))
}

func (p *Postgres) GetApproval(ctx context.Context, approvalID string) (Approval, error) {
	return scanApproval(p.pool.QueryRow(ctx, `SELECT `+approvalColumns+`
		FROM scm_approvals WHERE id=$1::uuid`, approvalID))
}

func (p *Postgres) DecideApproval(
	ctx context.Context,
	id Identity,
	approvalID string,
	decision string,
	now time.Time,
) (Approval, error) {
	if decision != "approved" && decision != "denied" {
		return Approval{}, ErrInvalidArgument
	}
	value, err := scanApproval(p.pool.QueryRow(ctx, `UPDATE scm_approvals SET
		status=$5, decided_at=$6, updated_at=$6
		WHERE id=$1::uuid AND tenant_id=$2 AND user_id=$3 AND status='pending' AND expires_at >= $4
		RETURNING `+approvalColumns, approvalID, id.TenantID, id.UserID, now, decision, now))
	if errors.Is(err, ErrNotFound) {
		current, getErr := p.GetApproval(ctx, approvalID)
		if getErr != nil || current.TenantID != id.TenantID || current.UserID != id.UserID {
			return Approval{}, ErrNotFound
		}
		if current.Status == decision {
			return current, nil
		}
		return Approval{}, ErrConflict
	}
	return value, err
}

func (p *Postgres) ClaimApproval(
	ctx context.Context,
	id Identity,
	approvalID string,
	now time.Time,
) (Approval, error) {
	return scanApproval(p.pool.QueryRow(ctx, `UPDATE scm_approvals SET
		status='consumed', updated_at=$5
		WHERE id=$1::uuid AND tenant_id=$2 AND user_id=$3
			AND status='approved' AND expires_at >= $4
		RETURNING `+approvalColumns, approvalID, id.TenantID, id.UserID, now, now))
}

func (p *Postgres) ExpireApproval(
	ctx context.Context,
	id Identity,
	approvalID string,
	now time.Time,
) (Approval, error) {
	value, err := scanApproval(p.pool.QueryRow(ctx, `UPDATE scm_approvals SET
		status='expired', updated_at=$5
		WHERE id=$1::uuid AND tenant_id=$2 AND user_id=$3
			AND status IN ('pending','approved') AND expires_at < $4
		RETURNING `+approvalColumns, approvalID, id.TenantID, id.UserID, now, now))
	if errors.Is(err, ErrNotFound) {
		current, getErr := p.GetApproval(ctx, approvalID)
		if getErr != nil || current.TenantID != id.TenantID || current.UserID != id.UserID {
			return Approval{}, ErrNotFound
		}
		return current, nil
	}
	return value, err
}

func (p *Postgres) SaveBrokerRun(ctx context.Context, value BrokerRun) error {
	var runID string
	err := p.pool.QueryRow(ctx, `INSERT INTO scm_broker_runs (
		run_id, tenant_id, user_id, conversation_id, project_id, repository_id,
		registration_id, expires_at, created_at
	) VALUES ($1,$2,$3,$4,$5::uuid,$6,$7::uuid,$8,$9)
	ON CONFLICT (run_id) DO UPDATE SET expires_at=GREATEST(scm_broker_runs.expires_at, EXCLUDED.expires_at)
	WHERE scm_broker_runs.tenant_id=EXCLUDED.tenant_id
		AND scm_broker_runs.user_id=EXCLUDED.user_id
		AND scm_broker_runs.conversation_id=EXCLUDED.conversation_id
		AND scm_broker_runs.project_id=EXCLUDED.project_id
		AND scm_broker_runs.repository_id=EXCLUDED.repository_id
		AND scm_broker_runs.registration_id=EXCLUDED.registration_id
		AND scm_broker_runs.revoked_at IS NULL
	RETURNING run_id`, value.RunID, value.TenantID, value.UserID, value.ConversationID,
		value.ProjectID, value.RepositoryID, value.RegistrationID, value.ExpiresAt,
		value.CreatedAt).Scan(&runID)
	if errors.Is(err, pgx.ErrNoRows) || postgresCode(err, "23505") {
		return ErrConflict
	}
	return err
}

func (p *Postgres) BrokerRunActive(
	ctx context.Context,
	claims BrokerCredentialClaims,
	now time.Time,
) (bool, error) {
	var active bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM scm_broker_runs
		WHERE run_id=$1 AND tenant_id=$2 AND user_id=$3 AND conversation_id=$4
			AND project_id=$5::uuid AND repository_id=$6 AND registration_id=$7::uuid
			AND revoked_at IS NULL AND expires_at >= $8
	)`, claims.RunID, claims.TenantID, claims.UserID, claims.ConversationID,
		claims.ProjectID, claims.RepositoryID, claims.RegistrationID, now).Scan(&active)
	return active, err
}

func (p *Postgres) RevokeBrokerRun(
	ctx context.Context,
	id Identity,
	runID string,
	now time.Time,
) error {
	tag, err := p.pool.Exec(ctx, `UPDATE scm_broker_runs
		SET revoked_at=COALESCE(revoked_at,$4)
		WHERE run_id=$1 AND tenant_id=$2 AND user_id=$3`,
		runID, id.TenantID, id.UserID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) SaveTokenLease(ctx context.Context, value TokenLease) error {
	permissions, err := json.Marshal(value.Permissions)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `INSERT INTO scm_token_leases (
		id, approval_id, tenant_id, user_id, conversation_id, run_id, project_id,
		repository_id, command_category, permissions_json, token_ciphertext, expires_at, created_at
	) VALUES ($1::uuid,NULLIF($2,'')::uuid,$3,$4,$5,$6,$7::uuid,$8,$9,$10::jsonb,$11,$12,$13)`,
		value.ID, value.ApprovalID, value.TenantID, value.UserID, value.ConversationID,
		value.RunID, value.ProjectID, value.RepositoryID, value.CommandCategory,
		permissions, value.TokenCiphertext, value.ExpiresAt, value.CreatedAt)
	return err
}

func (p *Postgres) GetTokenLease(
	ctx context.Context,
	id Identity,
	runID string,
	leaseID string,
) (TokenLease, error) {
	return scanTokenLease(p.pool.QueryRow(ctx, `SELECT id::text, approval_id::text, tenant_id, user_id, conversation_id,
		run_id, project_id::text, repository_id, command_category, permissions_json,
		token_ciphertext, expires_at, revoked_at, created_at
		FROM scm_token_leases
		WHERE id=$1::uuid AND tenant_id=$2 AND user_id=$3 AND run_id=$4 AND revoked_at IS NULL`,
		leaseID, id.TenantID, id.UserID, runID))
}

func (p *Postgres) ListActiveTokenLeasesForRun(
	ctx context.Context,
	id Identity,
	runID string,
	now time.Time,
) ([]TokenLease, error) {
	return p.listTokenLeases(ctx, `SELECT id::text, approval_id::text, tenant_id, user_id, conversation_id,
		run_id, project_id::text, repository_id, command_category, permissions_json,
		token_ciphertext, expires_at, revoked_at, created_at
		FROM scm_token_leases
		WHERE tenant_id=$1 AND user_id=$2 AND run_id=$3 AND revoked_at IS NULL AND expires_at > $4`,
		id.TenantID, id.UserID, runID, now)
}

func (p *Postgres) ListActiveTokenLeasesForProject(
	ctx context.Context,
	id Identity,
	projectID string,
	now time.Time,
) ([]TokenLease, error) {
	return p.listTokenLeases(ctx, `SELECT id::text, approval_id::text, tenant_id, user_id, conversation_id,
		run_id, project_id::text, repository_id, command_category, permissions_json,
		token_ciphertext, expires_at, revoked_at, created_at
		FROM scm_token_leases
		WHERE tenant_id=$1 AND user_id=$2 AND project_id=$3::uuid AND revoked_at IS NULL AND expires_at > $4`,
		id.TenantID, id.UserID, projectID, now)
}

func (p *Postgres) ListActiveTokenLeasesForUser(
	ctx context.Context,
	id Identity,
	now time.Time,
) ([]TokenLease, error) {
	return p.listTokenLeases(ctx, `SELECT id::text, approval_id::text, tenant_id, user_id, conversation_id,
		run_id, project_id::text, repository_id, command_category, permissions_json,
		token_ciphertext, expires_at, revoked_at, created_at
		FROM scm_token_leases
		WHERE tenant_id=$1 AND user_id=$2 AND revoked_at IS NULL AND expires_at > $3`,
		id.TenantID, id.UserID, now)
}

func (p *Postgres) MarkTokenLeaseRevoked(
	ctx context.Context,
	id Identity,
	runID string,
	leaseID string,
	now time.Time,
) error {
	tag, err := p.pool.Exec(ctx, `UPDATE scm_token_leases SET revoked_at=$5
		WHERE id=$1::uuid AND tenant_id=$2 AND user_id=$3 AND run_id=$4 AND revoked_at IS NULL`,
		leaseID, id.TenantID, id.UserID, runID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type tokenLeaseScanner interface {
	Scan(...any) error
}

func scanTokenLease(row tokenLeaseScanner) (TokenLease, error) {
	var value TokenLease
	var approvalID *string
	var permissions []byte
	err := row.Scan(
		&value.ID, &approvalID, &value.TenantID, &value.UserID, &value.ConversationID,
		&value.RunID, &value.ProjectID, &value.RepositoryID, &value.CommandCategory,
		&permissions, &value.TokenCiphertext, &value.ExpiresAt, &value.RevokedAt, &value.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TokenLease{}, ErrNotFound
	}
	if err != nil {
		return TokenLease{}, err
	}
	if approvalID != nil {
		value.ApprovalID = *approvalID
	}
	if err := json.Unmarshal(permissions, &value.Permissions); err != nil {
		return TokenLease{}, err
	}
	return value, nil
}

func (p *Postgres) listTokenLeases(ctx context.Context, query string, args ...any) ([]TokenLease, error) {
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]TokenLease, 0)
	for rows.Next() {
		value, scanErr := scanTokenLease(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (p *Postgres) SaveAuditEvent(ctx context.Context, value AuditEvent) error {
	permissions, err := json.Marshal(value.Permissions)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `INSERT INTO scm_audit_events (
		tenant_id, user_id, project_id, repository_id, run_id, command_category,
		permissions_json, result, duration_ms, created_at
	) VALUES ($1,$2,$3::uuid,$4,$5,$6,$7::jsonb,$8,$9,$10)`,
		value.TenantID, value.UserID, value.ProjectID, value.RepositoryID, value.RunID,
		value.CommandCategory, permissions, value.Result, value.DurationMS, value.CreatedAt)
	return err
}

const projectColumns = `id::text, tenant_id, owner_user_id, name, description, runtime_id,
	repository_mode, repository_provider, COALESCE(repository_external_id, 0), repository_owner,
	repository_name, repository_html_url, COALESCE(installation_id, 0), default_branch,
	visibility, repository_size_kb, repository_has_lfs, repository_has_submodule,
	status, provision_error_code, provision_request_id,
	provision_started_at, version, created_at, updated_at, archived_at,
	COALESCE(primary_conversation_id, ''), github_publish_status`

const joinedProjectColumns = `projects.id::text, projects.tenant_id, projects.owner_user_id,
	projects.name, projects.description, projects.runtime_id, projects.repository_mode,
	projects.repository_provider, COALESCE(projects.repository_external_id, 0),
	projects.repository_owner, projects.repository_name, projects.repository_html_url,
	COALESCE(projects.installation_id, 0), projects.default_branch, projects.visibility,
	projects.repository_size_kb, projects.repository_has_lfs, projects.repository_has_submodule,
	projects.status, projects.provision_error_code,
	projects.provision_request_id, projects.provision_started_at, projects.version,
	projects.created_at, projects.updated_at, projects.archived_at,
	COALESCE(projects.primary_conversation_id, ''), projects.github_publish_status`

func scanProject(row pgx.Row) (Project, error) {
	var v Project
	err := row.Scan(&v.ID, &v.TenantID, &v.OwnerUserID, &v.Name, &v.Description,
		&v.RuntimeID, &v.RepositoryMode, &v.RepositoryProvider, &v.RepositoryExternalID,
		&v.RepositoryOwner, &v.RepositoryName, &v.RepositoryHTMLURL, &v.InstallationID,
		&v.DefaultBranch, &v.Visibility, &v.RepositorySizeKB, &v.RepositoryHasLFS,
		&v.RepositoryHasSubmodule, &v.Status,
		&v.ProvisionErrorCode, &v.ProvisionRequestID, &v.ProvisionStartedAt, &v.Version,
		&v.CreatedAt, &v.UpdatedAt, &v.ArchivedAt, &v.PrimaryConversationID,
		&v.GitHubPublishStatus)
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
		repository_provider, repository_external_id, repository_name, default_branch, visibility, status, provision_request_id,
		provision_started_at, version, created_at, updated_at
	) VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,0),$10,$11,$12,$13,$14,$15,1,$16,$17)
	RETURNING ` + projectColumns
	result, err := scanProject(p.pool.QueryRow(ctx, q, v.ID, v.TenantID, v.OwnerUserID,
		v.Name, v.Description, v.RuntimeID, v.RepositoryMode, v.RepositoryProvider,
		v.RepositoryExternalID, v.RepositoryName, v.DefaultBranch, v.Visibility, v.Status,
		v.ProvisionRequestID, v.ProvisionStartedAt, v.CreatedAt, v.UpdatedAt))
	if postgresCode(err, "23505") {
		return Project{}, ErrConflict
	}
	return result, err
}

func (p *Postgres) RefreshProjectProvisionAttempt(
	ctx context.Context,
	id Identity,
	projectID string,
	now time.Time,
) (Project, error) {
	return scanProject(p.pool.QueryRow(ctx, `UPDATE projects SET
		provision_started_at=$4, status='provisioning', provision_error_code='',
		version=version+1, updated_at=$4
		WHERE id=$1::uuid AND tenant_id=$2 AND owner_user_id=$3
			AND status IN ('provisioning','failed')
			AND repository_provider='github' AND repository_mode='create'
		RETURNING `+projectColumns, projectID, id.TenantID, id.UserID, now))
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
		github_publish_status='published', version=version+1, updated_at=$14
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

func (p *Postgres) BeginLocalProjectPublishIntent(
	ctx context.Context,
	id Identity,
	projectID string,
	expected int64,
	repositoryName string,
	visibility string,
	now time.Time,
) (Project, error) {
	const query = `UPDATE projects SET
		repository_name=$5, visibility=$6, github_publish_status='pending',
		provision_started_at=$7, version=version+1, updated_at=$7
	WHERE id=$1::uuid AND tenant_id=$2 AND owner_user_id=$3 AND version=$4
		AND repository_provider='local' AND repository_mode='empty'
		AND repository_external_id IS NULL AND github_publish_status='unpublished'
		AND status='ready'
	RETURNING ` + projectColumns
	value, err := scanProject(p.pool.QueryRow(ctx, query,
		projectID, id.TenantID, id.UserID, expected, repositoryName, visibility, now))
	if errors.Is(err, ErrNotFound) {
		if existing, getErr := p.GetProject(ctx, id, projectID); getErr == nil {
			if existing.Version != expected {
				return Project{}, ErrVersionConflict
			}
			return Project{}, ErrConflict
		}
	}
	if postgresCode(err, "23505") {
		return Project{}, ErrConflict
	}
	return value, err
}

func (p *Postgres) BindLocalProjectPublishRepository(
	ctx context.Context,
	id Identity,
	projectID string,
	expected int64,
	repo Repository,
	installationID int64,
	now time.Time,
) (Project, error) {
	visibility := repo.Visibility
	if visibility == "" {
		if repo.Private {
			visibility = "private"
		} else {
			visibility = "public"
		}
	}
	const query = `UPDATE projects SET
		repository_external_id=$5, repository_owner=$6, repository_html_url=$7,
		installation_id=$8, visibility=$9, repository_size_kb=$10,
		version=version+1, updated_at=$11
	WHERE id=$1::uuid AND tenant_id=$2 AND owner_user_id=$3 AND version=$4
		AND repository_provider='local' AND repository_mode='empty'
		AND repository_external_id IS NULL AND github_publish_status='pending'
		AND LOWER(repository_name)=LOWER($12) AND status='ready'
	RETURNING ` + projectColumns
	value, err := scanProject(p.pool.QueryRow(ctx, query,
		projectID, id.TenantID, id.UserID, expected, repo.ID, repo.Owner,
		repo.HTMLURL, installationID, visibility, repo.SizeKB, now, repo.Name))
	if errors.Is(err, ErrNotFound) {
		if existing, getErr := p.GetProject(ctx, id, projectID); getErr == nil {
			if existing.Version != expected {
				return Project{}, ErrVersionConflict
			}
			return Project{}, ErrConflict
		}
	}
	if postgresCode(err, "23505") {
		return Project{}, ErrConflict
	}
	return value, err
}

func (p *Postgres) CancelLocalProjectPublishIntent(
	ctx context.Context,
	id Identity,
	projectID string,
	expected int64,
	now time.Time,
) (Project, error) {
	value, err := scanProject(p.pool.QueryRow(ctx, `UPDATE projects SET
		repository_name='', visibility='private', github_publish_status='unpublished',
		version=version+1, updated_at=$5
		WHERE id=$1::uuid AND tenant_id=$2 AND owner_user_id=$3 AND version=$4
			AND repository_provider='local' AND repository_external_id IS NULL
			AND github_publish_status='pending' AND status='ready'
		RETURNING `+projectColumns, projectID, id.TenantID, id.UserID, expected, now))
	if errors.Is(err, ErrNotFound) {
		if existing, getErr := p.GetProject(ctx, id, projectID); getErr == nil {
			if existing.Version != expected {
				return Project{}, ErrVersionConflict
			}
			return Project{}, ErrConflict
		}
	}
	return value, err
}

func (p *Postgres) RebindProjectInstallation(
	ctx context.Context,
	id Identity,
	projectID string,
	repositoryID int64,
	installationID int64,
	now time.Time,
) (Project, error) {
	return scanProject(p.pool.QueryRow(ctx, `UPDATE projects SET
		installation_id=$5, version=version+1, updated_at=$6
		WHERE id=$1::uuid AND tenant_id=$2 AND owner_user_id=$3
			AND repository_external_id=$4 AND status='ready'
			AND (repository_provider='github' OR
				(repository_provider='local' AND github_publish_status IN ('pending','published')))
		RETURNING `+projectColumns, projectID, id.TenantID, id.UserID,
		repositoryID, installationID, now))
}

func (p *Postgres) CompleteLocalProjectPublish(
	ctx context.Context,
	id Identity,
	projectID string,
	expected int64,
	now time.Time,
) (Project, error) {
	value, err := scanProject(p.pool.QueryRow(ctx, `UPDATE projects SET
		github_publish_status='published', version=version+1, updated_at=$5
		WHERE id=$1::uuid AND tenant_id=$2 AND owner_user_id=$3 AND version=$4
			AND repository_provider='local' AND github_publish_status='pending'
		RETURNING `+projectColumns, projectID, id.TenantID, id.UserID, expected, now))
	if errors.Is(err, ErrNotFound) {
		if existing, getErr := p.GetProject(ctx, id, projectID); getErr == nil {
			if existing.Version != expected {
				return Project{}, ErrVersionConflict
			}
			return Project{}, ErrConflict
		}
	}
	return value, err
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
		&v.Version, &v.CreatedAt, &v.UpdatedAt, &v.ArchivedAt, &v.PrimaryConversationID,
		&v.GitHubPublishStatus)
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
		head_sha=$4, base_sha=CASE WHEN base_sha='' THEN $8 ELSE base_sha END,
		bootstrap_status=$5, bootstrap_error_code='', git_snapshot_json=$6,
		git_snapshot_at=$7, updated_at=$7
	FROM conversations c, projects p
	WHERE w.conversation_id=$1 AND c.id=w.conversation_id AND p.id=w.project_id
		AND c.user_id=$2 AND p.tenant_id=$3 AND p.owner_user_id=$2`,
		conversationID, id.UserID, id.TenantID, headSHA, bootstrapStatus, raw, now, snapshot.BaseSHA)
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
