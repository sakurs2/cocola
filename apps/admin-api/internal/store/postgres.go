package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres is the durable Store backend (M7). It implements the exact same
// store.Store contract as Memory, so the composition root swaps it in by env
// (COCOLA_PG_DSN set) with no service/handler change. Schema is owned by the
// goose migrations in the db module (single source of truth); this type only
// reads/writes rows and never declares DDL.
//
// All slices returned are freshly built so callers cannot mutate shared state,
// matching Memory's value semantics. NotFound/Conflict map to the package
// sentinels so handlers behave identically regardless of backend.
type Postgres struct {
	pool *pgxpool.Pool
}

var _ Store = (*Postgres)(nil)

// NewPostgres connects a pool to dsn and verifies connectivity. The caller owns
// the lifecycle and must call Close. Schema migration is a separate concern
// (see migrate.go) and is expected to have run before queries are issued.
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

// Close releases the connection pool.
func (p *Postgres) Close() { p.pool.Close() }

// isUniqueViolation reports whether err is a Postgres unique-constraint error
// (SQLSTATE 23505), which we surface as ErrConflict.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	return false
}

// nullableTime converts a zero time.Time to NULL on write. expires_at and
// revoked_at use NULL for "unset"; everything else stores the value as-is.
func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// ---- Auth users ----

const authUserCols = `id, username_normalized, email_normalized, name, tenant_id, role, enabled, password_hash, created_at, updated_at, last_login_at, created_by, updated_by, password_updated_at, deleted_at, deleted_by`
const authUserColsU = `u.id, u.username_normalized, u.email_normalized, u.name, u.tenant_id, u.role, u.enabled, u.password_hash, u.created_at, u.updated_at, u.last_login_at, u.created_by, u.updated_by, u.password_updated_at, u.deleted_at, u.deleted_by`

func scanAuthUser(row pgx.Row) (AuthUser, error) {
	var u AuthUser
	var lastLogin, passwordUpdated, deletedAt *time.Time
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.Name, &u.TenantID, &u.Role, &u.Enabled,
		&u.PasswordHash, &u.CreatedAt, &u.UpdatedAt, &lastLogin, &u.CreatedBy, &u.UpdatedBy, &passwordUpdated, &deletedAt, &u.DeletedBy)
	if err != nil {
		return AuthUser{}, err
	}
	if lastLogin != nil {
		u.LastLoginAt = *lastLogin
	}
	if passwordUpdated != nil {
		u.PasswordUpdated = *passwordUpdated
	}
	if deletedAt != nil {
		u.DeletedAt = *deletedAt
	}
	return u, nil
}

func (p *Postgres) CreateAuthUser(ctx context.Context, u AuthUser) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	const q = `INSERT INTO auth_users (` + authUserCols + `)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`
	_, err = tx.Exec(ctx, q,
		u.ID, u.Username, u.Email, u.Name, u.TenantID, u.Role, u.Enabled, u.PasswordHash,
		u.CreatedAt, u.UpdatedAt, nullableTime(u.LastLoginAt), u.CreatedBy, u.UpdatedBy, nullableTime(u.PasswordUpdated), nullableTime(u.DeletedAt), u.DeletedBy)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if err := insertAuthUserIdentifiers(ctx, tx, u); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) GetAuthUser(ctx context.Context, id string) (AuthUser, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+authUserCols+` FROM auth_users WHERE id=$1`, id)
	u, err := scanAuthUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthUser{}, ErrNotFound
	}
	return u, err
}

func (p *Postgres) GetAuthUserByEmail(ctx context.Context, email string) (AuthUser, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+authUserCols+` FROM auth_users WHERE email_normalized=$1`, email)
	u, err := scanAuthUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthUser{}, ErrNotFound
	}
	return u, err
}

func (p *Postgres) GetAuthUserByIdentifier(ctx context.Context, identifier string) (AuthUser, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+authUserColsU+`
		FROM auth_users u
		JOIN auth_user_identifiers i ON i.user_id = u.id
		WHERE i.value_normalized=$1`, identifier)
	u, err := scanAuthUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthUser{}, ErrNotFound
	}
	return u, err
}

func (p *Postgres) ListAuthUsers(ctx context.Context) ([]AuthUser, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+authUserCols+` FROM auth_users WHERE deleted_at IS NULL ORDER BY email_normalized`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AuthUser, 0)
	for rows.Next() {
		u, err := scanAuthUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (p *Postgres) UpdateAuthUser(ctx context.Context, u AuthUser) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	const q = `UPDATE auth_users
		SET username_normalized=$2, email_normalized=$3, name=$4, tenant_id=$5, role=$6, enabled=$7,
		    password_hash=$8, created_at=$9, updated_at=$10, last_login_at=$11,
		    created_by=$12, updated_by=$13, password_updated_at=$14, deleted_at=$15, deleted_by=$16
		WHERE id=$1`
	ct, err := tx.Exec(ctx, q,
		u.ID, u.Username, u.Email, u.Name, u.TenantID, u.Role, u.Enabled, u.PasswordHash,
		u.CreatedAt, u.UpdatedAt, nullableTime(u.LastLoginAt), u.CreatedBy, u.UpdatedBy, nullableTime(u.PasswordUpdated), nullableTime(u.DeletedAt), u.DeletedBy)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM auth_user_identifiers WHERE user_id=$1 AND kind IN ('username','email')`, u.ID); err != nil {
		return err
	}
	if err := insertAuthUserIdentifiers(ctx, tx, u); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) DeleteAuthUser(ctx context.Context, id, actor string, at time.Time) error {
	ct, err := p.pool.Exec(ctx, `UPDATE auth_users
		SET enabled=FALSE, deleted_at=$2, deleted_by=$3, updated_at=$2, updated_by=$3
		WHERE id=$1 AND deleted_at IS NULL`, id, at, actor)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func insertAuthUserIdentifiers(ctx context.Context, tx pgx.Tx, u AuthUser) error {
	const q = `INSERT INTO auth_user_identifiers
		(id, user_id, kind, value_normalized, display_value, verified, is_primary, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`
	for _, ident := range authUserIdentifiersFor(u) {
		_, err := tx.Exec(ctx, q,
			ident.ID, ident.UserID, ident.Kind, ident.Value, ident.DisplayValue,
			ident.Verified, ident.Primary, ident.CreatedAt, ident.UpdatedAt)
		if isUniqueViolation(err) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Postgres) TouchAuthUserLogin(ctx context.Context, id string, at time.Time) error {
	ct, err := p.pool.Exec(ctx, `UPDATE auth_users SET last_login_at=$2 WHERE id=$1`, id, at)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- Tokens ----

func (p *Postgres) CreateToken(ctx context.Context, r TokenRecord) error {
	const q = `INSERT INTO token_records
		(id, user_id, tenant_id, issuer, issued_at, expires_at, revoked, revoked_at, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`
	_, err := p.pool.Exec(ctx, q,
		r.ID, r.UserID, r.TenantID, r.Issuer, r.IssuedAt,
		nullableTime(r.ExpiresAt), r.Revoked, nullableTime(r.RevokedAt), r.CreatedBy)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// scanToken reads one row into a TokenRecord. NULL expires_at/revoked_at become
// the zero time.Time so the JSON shape matches Memory's.
func scanToken(row pgx.Row) (TokenRecord, error) {
	var r TokenRecord
	var expires, revoked *time.Time
	err := row.Scan(&r.ID, &r.UserID, &r.TenantID, &r.Issuer, &r.IssuedAt,
		&expires, &r.Revoked, &revoked, &r.CreatedBy)
	if err != nil {
		return TokenRecord{}, err
	}
	if expires != nil {
		r.ExpiresAt = *expires
	}
	if revoked != nil {
		r.RevokedAt = *revoked
	}
	return r, nil
}

const tokenCols = `id, user_id, tenant_id, issuer, issued_at, expires_at, revoked, revoked_at, created_by`

func (p *Postgres) GetToken(ctx context.Context, id string) (TokenRecord, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+tokenCols+` FROM token_records WHERE id=$1`, id)
	r, err := scanToken(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TokenRecord{}, ErrNotFound
	}
	return r, err
}

func (p *Postgres) ListTokens(ctx context.Context, userID string) ([]TokenRecord, error) {
	q := `SELECT ` + tokenCols + ` FROM token_records`
	args := []any{}
	if userID != "" {
		q += ` WHERE user_id=$1`
		args = append(args, userID)
	}
	q += ` ORDER BY issued_at DESC`
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TokenRecord, 0)
	for rows.Next() {
		r, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *Postgres) RevokeToken(ctx context.Context, id string, at time.Time) error {
	ct, err := p.pool.Exec(ctx,
		`UPDATE token_records SET revoked=TRUE, revoked_at=$2 WHERE id=$1`, id, at)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) IsRevoked(ctx context.Context, id string) (bool, error) {
	var revoked bool
	err := p.pool.QueryRow(ctx, `SELECT revoked FROM token_records WHERE id=$1`, id).Scan(&revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	return revoked, err
}

// ---- Quota overrides ----

func (p *Postgres) SetQuota(ctx context.Context, q QuotaOverride) error {
	const sql = `INSERT INTO quota_overrides (scope, subject, limit_tokens, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (scope, subject) DO UPDATE
		SET limit_tokens=EXCLUDED.limit_tokens, updated_at=EXCLUDED.updated_at, updated_by=EXCLUDED.updated_by`
	_, err := p.pool.Exec(ctx, sql, q.Scope, q.Subject, q.Limit, q.UpdatedAt, q.UpdatedBy)
	return err
}

func (p *Postgres) GetQuota(ctx context.Context, scope, subject string) (QuotaOverride, error) {
	var q QuotaOverride
	err := p.pool.QueryRow(ctx,
		`SELECT scope, subject, limit_tokens, updated_at, updated_by FROM quota_overrides WHERE scope=$1 AND subject=$2`,
		scope, subject).Scan(&q.Scope, &q.Subject, &q.Limit, &q.UpdatedAt, &q.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return QuotaOverride{}, ErrNotFound
	}
	return q, err
}

func (p *Postgres) ListQuotas(ctx context.Context) ([]QuotaOverride, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT scope, subject, limit_tokens, updated_at, updated_by FROM quota_overrides ORDER BY scope, subject`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]QuotaOverride, 0)
	for rows.Next() {
		var q QuotaOverride
		if err := rows.Scan(&q.Scope, &q.Subject, &q.Limit, &q.UpdatedAt, &q.UpdatedBy); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (p *Postgres) DeleteQuota(ctx context.Context, scope, subject string) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM quota_overrides WHERE scope=$1 AND subject=$2`, scope, subject)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- System settings ----

func scanSystemSetting(row pgx.Row) (SystemSetting, error) {
	var setting SystemSetting
	err := row.Scan(&setting.Key, &setting.ValueJSON, &setting.Version, &setting.UpdatedAt, &setting.UpdatedBy)
	return setting, err
}

func (p *Postgres) GetSystemSetting(ctx context.Context, key string) (SystemSetting, error) {
	row := p.pool.QueryRow(ctx, `SELECT key, value_json, version, updated_at, updated_by FROM system_settings WHERE key=$1`, key)
	setting, err := scanSystemSetting(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return SystemSetting{}, ErrNotFound
	}
	return setting, err
}

func (p *Postgres) ListSystemSettings(ctx context.Context) ([]SystemSetting, error) {
	rows, err := p.pool.Query(ctx, `SELECT key, value_json, version, updated_at, updated_by FROM system_settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SystemSetting, 0)
	for rows.Next() {
		setting, err := scanSystemSetting(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, setting)
	}
	return out, rows.Err()
}

func (p *Postgres) SetSystemSetting(ctx context.Context, setting SystemSetting, expectedVersion int64) (SystemSetting, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return SystemSetting{}, err
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `SELECT key, value_json, version, updated_at, updated_by FROM system_settings WHERE key=$1 FOR UPDATE`, setting.Key)
	current, err := scanSystemSetting(row)
	if errors.Is(err, pgx.ErrNoRows) {
		if expectedVersion > 0 {
			return SystemSetting{}, ErrConflict
		}
		setting.Version = 1
		if _, err := tx.Exec(ctx, `INSERT INTO system_settings (key, value_json, version, updated_at, updated_by)
			VALUES ($1,$2,$3,$4,$5)`, setting.Key, setting.ValueJSON, setting.Version, setting.UpdatedAt, setting.UpdatedBy); err != nil {
			return SystemSetting{}, err
		}
		return setting, tx.Commit(ctx)
	}
	if err != nil {
		return SystemSetting{}, err
	}
	if expectedVersion >= 0 && expectedVersion != current.Version {
		return SystemSetting{}, ErrConflict
	}
	setting.Version = current.Version + 1
	if _, err := tx.Exec(ctx, `UPDATE system_settings SET value_json=$2, version=$3, updated_at=$4, updated_by=$5 WHERE key=$1`,
		setting.Key, setting.ValueJSON, setting.Version, setting.UpdatedAt, setting.UpdatedBy); err != nil {
		return SystemSetting{}, err
	}
	return setting, tx.Commit(ctx)
}

func (p *Postgres) DeleteSystemSetting(ctx context.Context, key string, expectedVersion int64) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `SELECT version FROM system_settings WHERE key=$1 FOR UPDATE`, key)
	var version int64
	if err := row.Scan(&version); errors.Is(err, pgx.ErrNoRows) {
		if expectedVersion > 0 {
			return ErrConflict
		}
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if expectedVersion >= 0 && expectedVersion != version {
		return ErrConflict
	}
	ct, err := tx.Exec(ctx, `DELETE FROM system_settings WHERE key=$1`, key)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

// ---- Skills ----

const skillCols = `id, name, description, version, entrypoint, enabled, scope, owner_user_id, source_type, source_url, source_ref, source_path, bundle_object_key, content_sha256, manifest_json, frontmatter_json, skill_md, file_count, size_bytes, created_at, updated_at, created_by, updated_by`

func scanSkill(row pgx.Row) (Skill, error) {
	var s Skill
	err := row.Scan(&s.ID, &s.Name, &s.Description, &s.Version, &s.Entrypoint,
		&s.Enabled, &s.Scope, &s.OwnerUserID, &s.SourceType, &s.SourceURL,
		&s.SourceRef, &s.SourcePath, &s.BundleObjectKey, &s.ContentSHA256,
		&s.ManifestJSON, &s.FrontmatterJSON, &s.SkillMD, &s.FileCount,
		&s.SizeBytes, &s.CreatedAt, &s.UpdatedAt, &s.CreatedBy, &s.UpdatedBy)
	return s, err
}

func (p *Postgres) CreateSkill(ctx context.Context, s Skill) error {
	s = normalizeSkill(s)
	const q = `INSERT INTO skill_entries (` + skillCols + `)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)`
	_, err := p.pool.Exec(ctx, q,
		s.ID, s.Name, s.Description, s.Version, s.Entrypoint, s.Enabled,
		s.Scope, s.OwnerUserID, s.SourceType, s.SourceURL, s.SourceRef, s.SourcePath,
		s.BundleObjectKey, s.ContentSHA256, s.ManifestJSON, s.FrontmatterJSON, s.SkillMD,
		s.FileCount, s.SizeBytes, s.CreatedAt, s.UpdatedAt, s.CreatedBy, s.UpdatedBy)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func (p *Postgres) GetSkill(ctx context.Context, id string) (Skill, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+skillCols+` FROM skill_entries WHERE id=$1`, id)
	s, err := scanSkill(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Skill{}, ErrNotFound
	}
	return s, err
}

func (p *Postgres) ListSkills(ctx context.Context, onlyEnabled bool) ([]Skill, error) {
	q := `SELECT ` + skillCols + ` FROM skill_entries`
	if onlyEnabled {
		q += ` WHERE enabled=TRUE`
	}
	q += ` ORDER BY id`
	rows, err := p.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Skill, 0)
	for rows.Next() {
		s, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) ListSkillsForUser(ctx context.Context, userID string) ([]Skill, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+skillCols+`
		FROM skill_entries
		WHERE scope='user' AND owner_user_id=$1
		ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Skill, 0)
	for rows.Next() {
		s, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) UpdateSkill(ctx context.Context, s Skill) error {
	s = normalizeSkill(s)
	const q = `UPDATE skill_entries
		SET name=$2, description=$3, version=$4, entrypoint=$5, enabled=$6,
		    scope=$7, owner_user_id=$8, source_type=$9, source_url=$10, source_ref=$11,
		    source_path=$12, bundle_object_key=$13, content_sha256=$14, manifest_json=$15,
		    frontmatter_json=$16, skill_md=$17, file_count=$18, size_bytes=$19,
		    created_at=$20, updated_at=$21, created_by=$22, updated_by=$23
		WHERE id=$1`
	ct, err := p.pool.Exec(ctx, q,
		s.ID, s.Name, s.Description, s.Version, s.Entrypoint, s.Enabled,
		s.Scope, s.OwnerUserID, s.SourceType, s.SourceURL, s.SourceRef, s.SourcePath,
		s.BundleObjectKey, s.ContentSHA256, s.ManifestJSON, s.FrontmatterJSON, s.SkillMD,
		s.FileCount, s.SizeBytes, s.CreatedAt, s.UpdatedAt, s.CreatedBy, s.UpdatedBy)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) DeleteSkill(ctx context.Context, id string) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM skill_entries WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) SetUserSkillPreference(ctx context.Context, pref UserSkillPreference) error {
	const q = `INSERT INTO user_skill_preferences (user_id, skill_id, enabled, updated_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (user_id, skill_id)
		DO UPDATE SET enabled=EXCLUDED.enabled, updated_at=EXCLUDED.updated_at`
	_, err := p.pool.Exec(ctx, q, pref.UserID, pref.SkillID, pref.Enabled, pref.UpdatedAt)
	if isForeignKeyViolation(err) {
		return ErrNotFound
	}
	return err
}

func (p *Postgres) ListUserSkillPreferences(ctx context.Context, userID string) ([]UserSkillPreference, error) {
	rows, err := p.pool.Query(ctx, `SELECT user_id, skill_id, enabled, updated_at
		FROM user_skill_preferences
		WHERE user_id=$1
		ORDER BY skill_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]UserSkillPreference, 0)
	for rows.Next() {
		var pref UserSkillPreference
		if err := rows.Scan(&pref.UserID, &pref.SkillID, &pref.Enabled, &pref.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, pref)
	}
	return out, rows.Err()
}

func (p *Postgres) DeleteUserSkillPreference(ctx context.Context, userID, skillID string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM user_skill_preferences WHERE user_id=$1 AND skill_id=$2`, userID, skillID)
	return err
}

// ---- MCP servers ----

const mcpServerCols = `id, name, description, transport, command, args_json, url, url_var_ciphertext_json, url_var_hint_json, env_ciphertext_json, env_hint_json, header_ciphertext_json, header_hint_json, enabled, default_enabled, source, status, created_at, updated_at, created_by, updated_by`

func scanMCPServer(row pgx.Row) (MCPServer, error) {
	var s MCPServer
	err := row.Scan(&s.ID, &s.Name, &s.Description, &s.Transport, &s.Command,
		&s.ArgsJSON, &s.URL, &s.URLVarCiphertextJSON, &s.URLVarHintJSON,
		&s.EnvCiphertextJSON, &s.EnvHintJSON,
		&s.HeaderCiphertextJSON, &s.HeaderHintJSON, &s.Enabled,
		&s.DefaultEnabled, &s.Source, &s.Status, &s.CreatedAt, &s.UpdatedAt,
		&s.CreatedBy, &s.UpdatedBy)
	return s, err
}

func (p *Postgres) CreateMCPServer(ctx context.Context, s MCPServer) error {
	s = normalizeMCPServer(s)
	const q = `INSERT INTO mcp_servers (` + mcpServerCols + `)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`
	_, err := p.pool.Exec(ctx, q,
		s.ID, s.Name, s.Description, s.Transport, s.Command, s.ArgsJSON, s.URL,
		s.URLVarCiphertextJSON, s.URLVarHintJSON, s.EnvCiphertextJSON, s.EnvHintJSON,
		s.HeaderCiphertextJSON, s.HeaderHintJSON,
		s.Enabled, s.DefaultEnabled, s.Source, s.Status, s.CreatedAt, s.UpdatedAt,
		s.CreatedBy, s.UpdatedBy)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func (p *Postgres) GetMCPServer(ctx context.Context, id string) (MCPServer, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+mcpServerCols+` FROM mcp_servers WHERE id=$1`, id)
	s, err := scanMCPServer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return MCPServer{}, ErrNotFound
	}
	return s, err
}

func (p *Postgres) ListMCPServers(ctx context.Context, onlyEnabled bool) ([]MCPServer, error) {
	q := `SELECT ` + mcpServerCols + ` FROM mcp_servers`
	if onlyEnabled {
		q += ` WHERE enabled=TRUE`
	}
	q += ` ORDER BY id`
	rows, err := p.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MCPServer, 0)
	for rows.Next() {
		s, err := scanMCPServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) UpdateMCPServer(ctx context.Context, s MCPServer) error {
	s = normalizeMCPServer(s)
	const q = `UPDATE mcp_servers
		SET name=$2, description=$3, transport=$4, command=$5, args_json=$6, url=$7,
		    url_var_ciphertext_json=$8, url_var_hint_json=$9, env_ciphertext_json=$10,
		    env_hint_json=$11, header_ciphertext_json=$12, header_hint_json=$13,
		    enabled=$14, default_enabled=$15, source=$16, status=$17, created_at=$18,
		    updated_at=$19, created_by=$20, updated_by=$21
		WHERE id=$1`
	ct, err := p.pool.Exec(ctx, q,
		s.ID, s.Name, s.Description, s.Transport, s.Command, s.ArgsJSON, s.URL,
		s.URLVarCiphertextJSON, s.URLVarHintJSON, s.EnvCiphertextJSON, s.EnvHintJSON,
		s.HeaderCiphertextJSON, s.HeaderHintJSON,
		s.Enabled, s.DefaultEnabled, s.Source, s.Status, s.CreatedAt, s.UpdatedAt,
		s.CreatedBy, s.UpdatedBy)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) DeleteMCPServer(ctx context.Context, id string) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM mcp_servers WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) SetUserMCPPreference(ctx context.Context, pref UserMCPPreference) error {
	const q = `INSERT INTO user_mcp_preferences (user_id, mcp_id, enabled, updated_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (user_id, mcp_id)
		DO UPDATE SET enabled=EXCLUDED.enabled, updated_at=EXCLUDED.updated_at`
	_, err := p.pool.Exec(ctx, q, pref.UserID, pref.MCPID, pref.Enabled, pref.UpdatedAt)
	if isForeignKeyViolation(err) {
		return ErrNotFound
	}
	return err
}

func (p *Postgres) ListUserMCPPreferences(ctx context.Context, userID string) ([]UserMCPPreference, error) {
	rows, err := p.pool.Query(ctx, `SELECT user_id, mcp_id, enabled, updated_at
		FROM user_mcp_preferences
		WHERE user_id=$1
		ORDER BY mcp_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]UserMCPPreference, 0)
	for rows.Next() {
		var pref UserMCPPreference
		if err := rows.Scan(&pref.UserID, &pref.MCPID, &pref.Enabled, &pref.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, pref)
	}
	return out, rows.Err()
}

func (p *Postgres) DeleteUserMCPPreference(ctx context.Context, userID, mcpID string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM user_mcp_preferences WHERE user_id=$1 AND mcp_id=$2`, userID, mcpID)
	return err
}

// ---- Agent prompts ----

const agentPromptCols = `id, name, content, enabled, scope, priority, version, created_at, updated_at, created_by, updated_by`

func scanAgentPrompt(row pgx.Row) (AgentPrompt, error) {
	var p AgentPrompt
	err := row.Scan(&p.ID, &p.Name, &p.Content, &p.Enabled, &p.Scope, &p.Priority,
		&p.Version, &p.CreatedAt, &p.UpdatedAt, &p.CreatedBy, &p.UpdatedBy)
	return p, err
}

func (p *Postgres) CreateAgentPrompt(ctx context.Context, prompt AgentPrompt) error {
	prompt = normalizeAgentPrompt(prompt)
	const q = `INSERT INTO agent_prompts (` + agentPromptCols + `)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	_, err := p.pool.Exec(ctx, q,
		prompt.ID, prompt.Name, prompt.Content, prompt.Enabled, prompt.Scope,
		prompt.Priority, prompt.Version, prompt.CreatedAt, prompt.UpdatedAt,
		prompt.CreatedBy, prompt.UpdatedBy)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func (p *Postgres) GetAgentPrompt(ctx context.Context, id string) (AgentPrompt, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+agentPromptCols+` FROM agent_prompts WHERE id=$1`, id)
	prompt, err := scanAgentPrompt(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentPrompt{}, ErrNotFound
	}
	return prompt, err
}

func (p *Postgres) ListAgentPrompts(ctx context.Context, onlyEnabled bool) ([]AgentPrompt, error) {
	q := `SELECT ` + agentPromptCols + ` FROM agent_prompts`
	if onlyEnabled {
		q += ` WHERE enabled=TRUE`
	}
	q += ` ORDER BY priority, id`
	rows, err := p.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AgentPrompt, 0)
	for rows.Next() {
		prompt, err := scanAgentPrompt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, prompt)
	}
	return out, rows.Err()
}

func (p *Postgres) UpdateAgentPrompt(ctx context.Context, prompt AgentPrompt) error {
	prompt = normalizeAgentPrompt(prompt)
	const q = `UPDATE agent_prompts
		SET name=$2, content=$3, enabled=$4, scope=$5, priority=$6, version=$7,
		    created_at=$8, updated_at=$9, created_by=$10, updated_by=$11
		WHERE id=$1`
	ct, err := p.pool.Exec(ctx, q,
		prompt.ID, prompt.Name, prompt.Content, prompt.Enabled, prompt.Scope,
		prompt.Priority, prompt.Version, prompt.CreatedAt, prompt.UpdatedAt,
		prompt.CreatedBy, prompt.UpdatedBy)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- LLM model configuration ----

const llmProviderCols = `id, name, type, base_url, api_key_ciphertext, api_key_hint, enabled, created_at, updated_at`

func scanLLMProvider(row pgx.Row) (LLMProvider, error) {
	var p LLMProvider
	err := row.Scan(&p.ID, &p.Name, &p.Type, &p.BaseURL, &p.APIKeyCiphertext,
		&p.APIKeyHint, &p.Enabled, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func (p *Postgres) CreateLLMProvider(ctx context.Context, provider LLMProvider) error {
	const q = `INSERT INTO llm_providers (` + llmProviderCols + `)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`
	_, err := p.pool.Exec(ctx, q,
		provider.ID, provider.Name, provider.Type, provider.BaseURL,
		provider.APIKeyCiphertext, provider.APIKeyHint, provider.Enabled,
		provider.CreatedAt, provider.UpdatedAt)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func (p *Postgres) GetLLMProvider(ctx context.Context, id string) (LLMProvider, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+llmProviderCols+` FROM llm_providers WHERE id=$1`, id)
	provider, err := scanLLMProvider(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return LLMProvider{}, ErrNotFound
	}
	return provider, err
}

func (p *Postgres) ListLLMProviders(ctx context.Context) ([]LLMProvider, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+llmProviderCols+` FROM llm_providers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]LLMProvider, 0)
	for rows.Next() {
		provider, err := scanLLMProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, provider)
	}
	return out, rows.Err()
}

func (p *Postgres) UpdateLLMProvider(ctx context.Context, provider LLMProvider) error {
	const q = `UPDATE llm_providers
		SET name=$2, type=$3, base_url=$4, api_key_ciphertext=$5, api_key_hint=$6,
		    enabled=$7, created_at=$8, updated_at=$9
		WHERE id=$1`
	ct, err := p.pool.Exec(ctx, q,
		provider.ID, provider.Name, provider.Type, provider.BaseURL,
		provider.APIKeyCiphertext, provider.APIKeyHint, provider.Enabled,
		provider.CreatedAt, provider.UpdatedAt)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) DeleteLLMProvider(ctx context.Context, id string) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM llm_providers WHERE id=$1`, id)
	if isForeignKeyViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const llmModelCols = `alias, provider_id, real_model, runtime, label, icon_type, icon_slug, icon_url, enabled, visible, is_default, sort_order, created_at, updated_at`

func scanLLMModelRoute(row pgx.Row) (LLMModelRoute, error) {
	var route LLMModelRoute
	err := row.Scan(&route.Alias, &route.ProviderID, &route.RealModel, &route.Runtime,
		&route.Label, &route.IconType, &route.IconSlug, &route.IconURL, &route.Enabled,
		&route.Visible, &route.IsDefault, &route.SortOrder, &route.CreatedAt, &route.UpdatedAt)
	return route, err
}

func (p *Postgres) CreateLLMModelRoute(ctx context.Context, route LLMModelRoute) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if route.IsDefault {
		if _, err := tx.Exec(ctx, `UPDATE llm_model_routes SET is_default=FALSE WHERE is_default=TRUE`); err != nil {
			return err
		}
	}
	const q = `INSERT INTO llm_model_routes (` + llmModelCols + `)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`
	_, err = tx.Exec(ctx, q,
		route.Alias, route.ProviderID, route.RealModel, route.Runtime, route.Label,
		route.IconType, route.IconSlug, route.IconURL, route.Enabled, route.Visible,
		route.IsDefault, route.SortOrder, route.CreatedAt, route.UpdatedAt)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if isForeignKeyViolation(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) GetLLMModelRoute(ctx context.Context, alias string) (LLMModelRoute, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+llmModelCols+` FROM llm_model_routes WHERE alias=$1`, alias)
	route, err := scanLLMModelRoute(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return LLMModelRoute{}, ErrNotFound
	}
	return route, err
}

func (p *Postgres) ListLLMModelRoutes(ctx context.Context) ([]LLMModelRoute, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+llmModelCols+` FROM llm_model_routes ORDER BY is_default DESC, sort_order, alias`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]LLMModelRoute, 0)
	for rows.Next() {
		route, err := scanLLMModelRoute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, route)
	}
	return out, rows.Err()
}

func (p *Postgres) UpdateLLMModelRoute(ctx context.Context, route LLMModelRoute) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if route.IsDefault {
		if _, err := tx.Exec(ctx, `UPDATE llm_model_routes SET is_default=FALSE WHERE alias<>$1 AND is_default=TRUE`, route.Alias); err != nil {
			return err
		}
	}
	const q = `UPDATE llm_model_routes
		SET provider_id=$2, real_model=$3, runtime=$4, label=$5, icon_type=$6,
		    icon_slug=$7, icon_url=$8, enabled=$9, visible=$10, is_default=$11,
		    sort_order=$12, created_at=$13, updated_at=$14
		WHERE alias=$1`
	ct, err := tx.Exec(ctx, q,
		route.Alias, route.ProviderID, route.RealModel, route.Runtime, route.Label,
		route.IconType, route.IconSlug, route.IconURL, route.Enabled, route.Visible,
		route.IsDefault, route.SortOrder, route.CreatedAt, route.UpdatedAt)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if isForeignKeyViolation(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

func (p *Postgres) DeleteLLMModelRoute(ctx context.Context, alias string) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM llm_model_routes WHERE alias=$1`, alias)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- Scheduled tasks ----

const scheduledTaskCols = `id, owner_type, owner_user_id, conversation_id, name, description, status, schedule_kind, schedule_spec, timezone, prompt, model_alias, max_turns, config_json, expires_at, next_run_at, last_run_at, run_count, last_status, last_error, created_at, updated_at, created_by, updated_by`

func scanScheduledTask(row pgx.Row) (ScheduledTask, error) {
	var task ScheduledTask
	var expiresAt, nextRun, lastRun *time.Time
	err := row.Scan(&task.ID, &task.OwnerType, &task.OwnerUserID, &task.ConversationID, &task.Name, &task.Description, &task.Status,
		&task.ScheduleKind, &task.ScheduleSpec, &task.Timezone, &task.Prompt, &task.ModelAlias,
		&task.MaxTurns, &task.ConfigJSON, &expiresAt, &nextRun, &lastRun, &task.RunCount, &task.LastStatus,
		&task.LastError, &task.CreatedAt, &task.UpdatedAt, &task.CreatedBy, &task.UpdatedBy)
	if err != nil {
		return ScheduledTask{}, err
	}
	if expiresAt != nil {
		task.ExpiresAt = *expiresAt
	}
	if nextRun != nil {
		task.NextRunAt = *nextRun
	}
	if lastRun != nil {
		task.LastRunAt = *lastRun
	}
	return task, nil
}

func scanScheduledTaskAttachment(row pgx.Row) (ScheduledTaskAttachment, error) {
	var att ScheduledTaskAttachment
	err := row.Scan(&att.ID, &att.TaskID, &att.Filename, &att.Mime, &att.SizeBytes, &att.ObjectKey, &att.ContentB64, &att.CreatedAt, &att.CreatedBy)
	return att, err
}

func (p *Postgres) CreateScheduledTask(ctx context.Context, task ScheduledTask, attachments []ScheduledTaskAttachment) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	const q = `INSERT INTO scheduled_tasks (` + scheduledTaskCols + `)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)`
	_, err = tx.Exec(ctx, q,
		task.ID, task.OwnerType, task.OwnerUserID, task.ConversationID, task.Name, task.Description, task.Status, task.ScheduleKind,
		task.ScheduleSpec, task.Timezone, task.Prompt, task.ModelAlias, task.MaxTurns,
		task.ConfigJSON, nullableTime(task.ExpiresAt), nullableTime(task.NextRunAt), nullableTime(task.LastRunAt), task.RunCount,
		task.LastStatus, task.LastError, task.CreatedAt, task.UpdatedAt, task.CreatedBy, task.UpdatedBy)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if err := insertScheduledTaskAttachments(ctx, tx, attachments); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertScheduledTaskAttachments(ctx context.Context, tx pgx.Tx, attachments []ScheduledTaskAttachment) error {
	const q = `INSERT INTO scheduled_task_attachments
		(id, task_id, filename, mime, size_bytes, object_key, content_b64, created_at, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`
	for _, att := range attachments {
		_, err := tx.Exec(ctx, q, att.ID, att.TaskID, att.Filename, att.Mime, att.SizeBytes, att.ObjectKey, att.ContentB64, att.CreatedAt, att.CreatedBy)
		if isUniqueViolation(err) {
			return ErrConflict
		}
		if isForeignKeyViolation(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Postgres) GetScheduledTask(ctx context.Context, id string) (ScheduledTask, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+scheduledTaskCols+` FROM scheduled_tasks WHERE id=$1`, id)
	task, err := scanScheduledTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return ScheduledTask{}, ErrNotFound
	}
	return task, err
}

func (p *Postgres) GetScheduledTaskForOwner(ctx context.Context, id, ownerUserID string) (ScheduledTask, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+scheduledTaskCols+` FROM scheduled_tasks
		WHERE id=$1 AND owner_user_id=$2`, id, ownerUserID)
	task, err := scanScheduledTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return ScheduledTask{}, ErrNotFound
	}
	return task, err
}

func (p *Postgres) ListScheduledTasks(ctx context.Context) ([]ScheduledTask, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+scheduledTaskCols+` FROM scheduled_tasks ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ScheduledTask, 0)
	for rows.Next() {
		task, err := scanScheduledTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

func (p *Postgres) ListScheduledTasksForOwner(ctx context.Context, ownerUserID string) ([]ScheduledTask, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+scheduledTaskCols+` FROM scheduled_tasks
		WHERE owner_user_id=$1 ORDER BY updated_at DESC`, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ScheduledTask, 0)
	for rows.Next() {
		task, err := scanScheduledTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

func (p *Postgres) UpdateScheduledTask(ctx context.Context, task ScheduledTask, replaceAttachments bool, attachments []ScheduledTaskAttachment) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	const q = `UPDATE scheduled_tasks
		SET owner_type=$2, owner_user_id=$3, conversation_id=$4, name=$5, description=$6,
		    status=$7, schedule_kind=$8, schedule_spec=$9, timezone=$10, prompt=$11,
		    model_alias=$12, max_turns=$13, config_json=$14, expires_at=$15, next_run_at=$16,
		    last_run_at=$17, run_count=$18, last_status=$19, last_error=$20,
		    created_at=$21, updated_at=$22, created_by=$23, updated_by=$24
		WHERE id=$1`
	ct, err := tx.Exec(ctx, q,
		task.ID, task.OwnerType, task.OwnerUserID, task.ConversationID, task.Name, task.Description, task.Status, task.ScheduleKind,
		task.ScheduleSpec, task.Timezone, task.Prompt, task.ModelAlias, task.MaxTurns,
		task.ConfigJSON, nullableTime(task.ExpiresAt), nullableTime(task.NextRunAt), nullableTime(task.LastRunAt), task.RunCount,
		task.LastStatus, task.LastError, task.CreatedAt, task.UpdatedAt, task.CreatedBy, task.UpdatedBy)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	if replaceAttachments {
		if _, err := tx.Exec(ctx, `DELETE FROM scheduled_task_attachments WHERE task_id=$1`, task.ID); err != nil {
			return err
		}
		if err := insertScheduledTaskAttachments(ctx, tx, attachments); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (p *Postgres) DeleteScheduledTask(ctx context.Context, id string) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM scheduled_tasks WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) DeleteScheduledTaskForOwner(ctx context.Context, id, ownerUserID string) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM scheduled_tasks WHERE id=$1 AND owner_user_id=$2`, id, ownerUserID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) ListScheduledTaskAttachments(ctx context.Context, taskID string) ([]ScheduledTaskAttachment, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, task_id, filename, mime, size_bytes, object_key, content_b64, created_at, created_by
		FROM scheduled_task_attachments WHERE task_id=$1 ORDER BY created_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ScheduledTaskAttachment, 0)
	for rows.Next() {
		att, err := scanScheduledTaskAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, att)
	}
	return out, rows.Err()
}

func (p *Postgres) ListDueScheduledTasks(ctx context.Context, now time.Time, limit int) ([]ScheduledTask, error) {
	q := `SELECT ` + scheduledTaskCols + ` FROM scheduled_tasks
		WHERE status='active' AND owner_user_id <> '' AND next_run_at IS NOT NULL AND next_run_at <= $1
		  AND (expires_at IS NULL OR expires_at >= $1)
		ORDER BY next_run_at ASC`
	args := []any{now}
	if limit > 0 {
		q += ` LIMIT $2`
		args = append(args, limit)
	}
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ScheduledTask, 0)
	for rows.Next() {
		task, err := scanScheduledTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

func (p *Postgres) ExpireScheduledTasks(ctx context.Context, now time.Time, limit int) ([]ScheduledTask, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := p.pool.Query(ctx, `WITH expired AS (
		SELECT id FROM scheduled_tasks
		WHERE status IN ('active','paused') AND expires_at IS NOT NULL AND expires_at < $1
		ORDER BY expires_at ASC LIMIT $2 FOR UPDATE SKIP LOCKED
	)
	UPDATE scheduled_tasks AS task
	SET status='expired', next_run_at=NULL, updated_at=$1
	WHERE task.id IN (SELECT id FROM expired)
	RETURNING `+scheduledTaskCols, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ScheduledTask, 0)
	for rows.Next() {
		task, err := scanScheduledTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

func (p *Postgres) TryStartScheduledTaskRun(ctx context.Context, taskID string, run ScheduledTaskRun, nextRunAt time.Time) (ScheduledTask, bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return ScheduledTask{}, false, err
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `SELECT `+scheduledTaskCols+` FROM scheduled_tasks
		WHERE id=$1 AND status='active' AND next_run_at IS NOT NULL AND next_run_at <= $2
		AND owner_user_id <> '' AND (expires_at IS NULL OR expires_at >= $2)
		AND NOT EXISTS (
			SELECT 1 FROM scheduled_task_runs
			WHERE task_id=$1 AND status IN ('queued','running')
		)
		FOR UPDATE`, taskID, run.ScheduledFor)
	task, err := scanScheduledTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return ScheduledTask{}, false, nil
	}
	if err != nil {
		return ScheduledTask{}, false, err
	}
	const rq = `INSERT INTO scheduled_task_runs
		(id, task_id, scheduled_for, status, worker_id, session_id, model_alias, output_text, error, started_at, finished_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`
	_, err = tx.Exec(ctx, rq,
		run.ID, run.TaskID, nullableTime(run.ScheduledFor), run.Status, run.WorkerID,
		run.SessionID, run.ModelAlias, run.OutputText, run.Error, nullableTime(run.StartedAt),
		nullableTime(run.FinishedAt), run.CreatedAt, run.UpdatedAt)
	if isUniqueViolation(err) {
		return ScheduledTask{}, false, ErrConflict
	}
	if err != nil {
		return ScheduledTask{}, false, err
	}
	_, err = tx.Exec(ctx, `UPDATE scheduled_tasks
		SET next_run_at=$2,
		    updated_at=$3,
		    conversation_id=CASE WHEN conversation_id='' THEN 'sched-' || id ELSE conversation_id END
		WHERE id=$1`,
		taskID, nullableTime(nextRunAt), run.UpdatedAt)
	if err != nil {
		return ScheduledTask{}, false, err
	}
	if task.ConversationID == "" {
		task.ConversationID = "sched-" + task.ID
	}
	return task, true, tx.Commit(ctx)
}

func scanScheduledTaskRun(row pgx.Row) (ScheduledTaskRun, error) {
	var run ScheduledTaskRun
	var scheduledFor, startedAt, finishedAt *time.Time
	err := row.Scan(&run.ID, &run.TaskID, &scheduledFor, &run.Status, &run.WorkerID,
		&run.SessionID, &run.ModelAlias, &run.OutputText, &run.Error, &startedAt,
		&finishedAt, &run.CreatedAt, &run.UpdatedAt)
	if err != nil {
		return ScheduledTaskRun{}, err
	}
	if scheduledFor != nil {
		run.ScheduledFor = *scheduledFor
	}
	if startedAt != nil {
		run.StartedAt = *startedAt
	}
	if finishedAt != nil {
		run.FinishedAt = *finishedAt
	}
	return run, nil
}

func (p *Postgres) GetScheduledTaskRun(ctx context.Context, id string) (ScheduledTaskRun, error) {
	row := p.pool.QueryRow(ctx, `SELECT id, task_id, scheduled_for, status, worker_id, session_id, model_alias, output_text, error, started_at, finished_at, created_at, updated_at
		FROM scheduled_task_runs WHERE id=$1`, id)
	run, err := scanScheduledTaskRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return ScheduledTaskRun{}, ErrNotFound
	}
	return run, err
}

func (p *Postgres) ListScheduledTaskRuns(ctx context.Context, taskID, status string, limit int) ([]ScheduledTaskRun, error) {
	q := `SELECT id, task_id, scheduled_for, status, worker_id, session_id, model_alias, output_text, error, started_at, finished_at, created_at, updated_at
		FROM scheduled_task_runs WHERE TRUE`
	args := []any{}
	if taskID != "" {
		args = append(args, taskID)
		q += ` AND task_id=$` + strconv.Itoa(len(args))
	}
	if status != "" {
		args = append(args, status)
		q += ` AND status=$` + strconv.Itoa(len(args))
	}
	q += ` ORDER BY created_at DESC`
	if limit > 0 {
		args = append(args, limit)
		q += ` LIMIT $` + strconv.Itoa(len(args))
	}
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ScheduledTaskRun, 0)
	for rows.Next() {
		run, err := scanScheduledTaskRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (p *Postgres) HeartbeatScheduledTaskRun(ctx context.Context, id, workerID string, now time.Time) (bool, error) {
	ct, err := p.pool.Exec(ctx, `UPDATE scheduled_task_runs
		SET updated_at=$3
		WHERE id=$1 AND worker_id=$2 AND status='running'`, id, workerID, now)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

func (p *Postgres) ExpireStaleScheduledTaskRuns(ctx context.Context, before, now time.Time, errText string, limit int) ([]ScheduledTaskRun, error) {
	if limit <= 0 {
		limit = 50
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id, task_id, scheduled_for, status, worker_id, session_id, model_alias, output_text, error, started_at, finished_at, created_at, updated_at
		FROM scheduled_task_runs
		WHERE status='running' AND updated_at < $1
		ORDER BY updated_at ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED`, before, limit)
	if err != nil {
		return nil, err
	}
	expired := make([]ScheduledTaskRun, 0)
	for rows.Next() {
		run, err := scanScheduledTaskRun(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		expired = append(expired, run)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range expired {
		run := expired[i]
		run.Status = "error"
		run.Error = errText
		run.FinishedAt = now
		run.UpdatedAt = now
		ct, err := tx.Exec(ctx, `UPDATE scheduled_task_runs
			SET status=$2, error=$3, finished_at=$4, updated_at=$4
			WHERE id=$1 AND status='running'`,
			run.ID, run.Status, run.Error, nullableTime(run.FinishedAt))
		if err != nil {
			return nil, err
		}
		if ct.RowsAffected() == 0 {
			continue
		}
		ct, err = tx.Exec(ctx, `UPDATE scheduled_tasks
			SET status=CASE
			        WHEN schedule_kind='once' AND next_run_at IS NULL THEN 'completed'
			        WHEN expires_at IS NOT NULL AND next_run_at IS NULL THEN 'expired'
			        ELSE status
			    END,
			    last_run_at=$2, run_count=run_count+1, last_status=$3, last_error=$4, updated_at=$2
			WHERE id=$1`,
			run.TaskID, run.FinishedAt, run.Status, run.Error)
		if err != nil {
			return nil, err
		}
		if ct.RowsAffected() == 0 {
			return nil, ErrNotFound
		}
		expired[i] = run
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return expired, nil
}

func (p *Postgres) UpdateScheduledTaskRun(ctx context.Context, run ScheduledTaskRun, taskNextRunAt time.Time, terminal bool) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	const q = `UPDATE scheduled_task_runs
		SET status=$2, worker_id=$3, session_id=$4, model_alias=$5, output_text=$6,
		    error=$7, started_at=$8, finished_at=$9, created_at=$10, updated_at=$11
		WHERE id=$1`
	ct, err := tx.Exec(ctx, q,
		run.ID, run.Status, run.WorkerID, run.SessionID, run.ModelAlias, run.OutputText,
		run.Error, nullableTime(run.StartedAt), nullableTime(run.FinishedAt), run.CreatedAt, run.UpdatedAt)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	if terminal {
		ct, err = tx.Exec(ctx, `UPDATE scheduled_tasks
			SET status=CASE
			        WHEN schedule_kind='once' AND $5::timestamptz IS NULL THEN 'completed'
			        WHEN expires_at IS NOT NULL AND $5::timestamptz IS NULL THEN 'expired'
			        ELSE status
			    END,
			    last_run_at=$2, run_count=run_count+1, last_status=$3, last_error=$4,
			    next_run_at=$5::timestamptz, updated_at=$6
			WHERE id=$1`, run.TaskID, nullableTime(run.FinishedAt), run.Status, run.Error, nullableTime(taskNextRunAt), run.UpdatedAt)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return ErrNotFound
		}
	}
	return tx.Commit(ctx)
}

func (p *Postgres) AppendScheduledTaskRunEvent(ctx context.Context, event ScheduledTaskRunEvent) error {
	const q = `INSERT INTO scheduled_task_run_events (run_id, seq, kind, data_json, created_at)
		VALUES ($1,$2,$3,$4,$5)`
	_, err := p.pool.Exec(ctx, q, event.RunID, event.Seq, event.Kind, event.DataJSON, event.CreatedAt)
	if isForeignKeyViolation(err) {
		return ErrNotFound
	}
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func (p *Postgres) ListScheduledTaskRunEvents(ctx context.Context, runID string) ([]ScheduledTaskRunEvent, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, run_id, seq, kind, data_json, created_at
		FROM scheduled_task_run_events WHERE run_id=$1 ORDER BY seq`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ScheduledTaskRunEvent, 0)
	for rows.Next() {
		var event ScheduledTaskRunEvent
		if err := rows.Scan(&event.ID, &event.RunID, &event.Seq, &event.Kind, &event.DataJSON, &event.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

// ---- Audit ----

func (p *Postgres) AppendAudit(ctx context.Context, e AuditEntry) error {
	detail := map[string]any{}
	detail["legacy_entry"] = true
	if e.Detail != "" {
		detail["detail"] = e.Detail
	}
	return p.AppendAuditEvent(ctx, AuditEvent{
		At:           e.At,
		ActorType:    "admin",
		ActorUserID:  e.Actor,
		ActorEmail:   e.Actor,
		Action:       e.Action,
		ResourceType: auditResourceType(e.Action),
		ResourceID:   e.Resource,
		Result:       "success",
		Metadata:     detail,
	})
}

func (p *Postgres) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	events, err := p.ListAuditEvents(ctx, AuditEventQuery{Limit: limit, LegacyOnly: true})
	if err != nil {
		return nil, err
	}
	out := make([]AuditEntry, 0)
	for _, e := range events {
		if isLegacyAuditEvent(e) {
			out = append(out, legacyAuditEntry(e))
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (p *Postgres) AppendAuditEvent(ctx context.Context, e AuditEvent) error {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	if e.Result == "" {
		e.Result = "success"
	}
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	meta, err := json.Marshal(e.Metadata)
	if err != nil {
		return err
	}
	const q = `INSERT INTO audit_events (
		ts, actor_type, actor_user_id, actor_email, action, resource_type,
		resource_id, result, http_method, route, status_code, request_id, trace_id,
		client_ip, user_agent, metadata_json, error_code
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`
	_, err = p.pool.Exec(ctx, q,
		e.At, e.ActorType, e.ActorUserID, e.ActorEmail, e.Action, e.ResourceType,
		e.ResourceID, e.Result, e.HTTPMethod, e.Route, e.StatusCode, e.RequestID, e.TraceID,
		e.ClientIP, e.UserAgent, meta, e.ErrorCode)
	return err
}

func (p *Postgres) ListAuditEvents(ctx context.Context, query AuditEventQuery) ([]AuditEvent, error) {
	q := `SELECT id, ts, actor_type, actor_user_id, actor_email, action, resource_type,
		resource_id, result, http_method, route, status_code, request_id, trace_id,
		client_ip, user_agent, metadata_json, error_code FROM audit_events`
	conds := make([]string, 0)
	args := make([]any, 0)
	add := func(field string, value any) {
		args = append(args, value)
		conds = append(conds, fmt.Sprintf("%s = $%d", field, len(args)))
	}
	addTime := func(expr string, value time.Time) {
		args = append(args, value)
		conds = append(conds, fmt.Sprintf("%s $%d", expr, len(args)))
	}
	if strings.TrimSpace(query.ActorUserID) != "" {
		add("actor_user_id", strings.TrimSpace(query.ActorUserID))
	}
	if strings.TrimSpace(query.ActorEmail) != "" {
		add("actor_email", strings.TrimSpace(query.ActorEmail))
	}
	if strings.TrimSpace(query.Action) != "" {
		add("action", strings.TrimSpace(query.Action))
	}
	if strings.TrimSpace(query.ResourceType) != "" {
		add("resource_type", strings.TrimSpace(query.ResourceType))
	}
	if strings.TrimSpace(query.ResourceID) != "" {
		add("resource_id", strings.TrimSpace(query.ResourceID))
	}
	if strings.TrimSpace(query.Result) != "" {
		add("result", strings.TrimSpace(query.Result))
	}
	if strings.TrimSpace(query.RequestID) != "" {
		add("request_id", strings.TrimSpace(query.RequestID))
	}
	if strings.TrimSpace(query.TraceID) != "" {
		add("trace_id", strings.TrimSpace(query.TraceID))
	}
	if !query.Since.IsZero() {
		addTime("ts >=", query.Since)
	}
	if !query.Until.IsZero() {
		addTime("ts <=", query.Until)
	}
	if query.LegacyOnly {
		conds = append(conds, "(metadata_json ? 'legacy_entry' OR metadata_json->>'legacy_table' = 'audit_log')")
	}
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += ` ORDER BY ts DESC, id DESC`
	if query.Limit > 0 {
		args = append(args, query.Limit)
		q += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if query.Offset > 0 {
		args = append(args, query.Offset)
		q += fmt.Sprintf(" OFFSET $%d", len(args))
	}
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AuditEvent, 0)
	for rows.Next() {
		var e AuditEvent
		var meta []byte
		if err := rows.Scan(
			&e.ID, &e.At, &e.ActorType, &e.ActorUserID, &e.ActorEmail, &e.Action,
			&e.ResourceType, &e.ResourceID, &e.Result, &e.HTTPMethod, &e.Route,
			&e.StatusCode, &e.RequestID, &e.TraceID, &e.ClientIP, &e.UserAgent, &meta, &e.ErrorCode,
		); err != nil {
			return nil, err
		}
		if len(meta) > 0 {
			if err := json.Unmarshal(meta, &e.Metadata); err != nil {
				return nil, err
			}
		}
		if e.Metadata == nil {
			e.Metadata = map[string]any{}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (p *Postgres) ListTraceEvents(ctx context.Context, query TraceEventQuery) ([]TraceEvent, error) {
	traceID := strings.TrimSpace(query.TraceID)
	if traceID == "" {
		return []TraceEvent{}, nil
	}
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	const q = `SELECT id, trace_id, service, name, category, started_at, duration_ms,
		status, metadata_json
		FROM trace_events
		WHERE trace_id = $1
		ORDER BY started_at ASC, id ASC
		LIMIT $2`
	rows, err := p.pool.Query(ctx, q, traceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TraceEvent, 0)
	for rows.Next() {
		var e TraceEvent
		var meta []byte
		if err := rows.Scan(
			&e.ID, &e.TraceID, &e.Service, &e.Name, &e.Category, &e.StartedAt,
			&e.DurationMS, &e.Status, &meta,
		); err != nil {
			return nil, err
		}
		if len(meta) > 0 {
			if err := json.Unmarshal(meta, &e.Metadata); err != nil {
				return nil, err
			}
		}
		if e.Metadata == nil {
			e.Metadata = map[string]any{}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func tokenUsageWhere(q TokenUsageQuery) (string, []any) {
	parts := []string{}
	args := []any{}
	if !q.From.IsZero() {
		args = append(args, q.From)
		parts = append(parts, fmt.Sprintf("ts >= $%d", len(args)))
	}
	if !q.To.IsZero() {
		args = append(args, q.To)
		parts = append(parts, fmt.Sprintf("ts < $%d", len(args)))
	}
	if strings.TrimSpace(q.UserID) != "" {
		args = append(args, strings.TrimSpace(q.UserID))
		parts = append(parts, fmt.Sprintf("user_id = $%d", len(args)))
	}
	if len(parts) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

func tokenUsageBucketExpr(bucket string) string {
	if bucket == "hour" {
		return "date_trunc('hour', ts)"
	}
	return "date_trunc('day', ts)"
}

func (p *Postgres) TokenUsageSummary(ctx context.Context, q TokenUsageQuery) (TokenUsageSummary, error) {
	where, args := tokenUsageWhere(q)
	sql := `SELECT count(*) AS calls,
		count(DISTINCT NULLIF(user_id, '')) AS user_count,
		COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens,
		COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
		COALESCE(SUM(cost_usd), 0) AS cost_usd
		FROM usage_ledger` + where
	var s TokenUsageSummary
	if err := p.pool.QueryRow(ctx, sql, args...).Scan(
		&s.Calls, &s.UserCount, &s.PromptTokens, &s.CompletionTokens, &s.CostUSD,
	); err != nil {
		return TokenUsageSummary{}, err
	}
	s.TotalTokens = s.PromptTokens + s.CompletionTokens
	return s, nil
}

func (p *Postgres) TokenUsageTrend(ctx context.Context, q TokenUsageQuery) ([]TokenUsagePoint, error) {
	where, args := tokenUsageWhere(q)
	sql := `SELECT ` + tokenUsageBucketExpr(q.Bucket) + ` AS bucket_start,
		count(*) AS calls,
		COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens,
		COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
		COALESCE(SUM(cost_usd), 0) AS cost_usd
		FROM usage_ledger` + where + `
		GROUP BY bucket_start
		ORDER BY bucket_start ASC`
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TokenUsagePoint, 0)
	for rows.Next() {
		var point TokenUsagePoint
		if err := rows.Scan(
			&point.BucketStart, &point.Calls, &point.PromptTokens,
			&point.CompletionTokens, &point.CostUSD,
		); err != nil {
			return nil, err
		}
		point.BucketStart = point.BucketStart.UTC()
		point.TotalTokens = point.PromptTokens + point.CompletionTokens
		out = append(out, point)
	}
	return out, rows.Err()
}

func (p *Postgres) TokenUsageUsers(ctx context.Context, q TokenUsageQuery) ([]TokenUsageUser, error) {
	where, args := tokenUsageWhere(q)
	sql := `WITH grouped AS (
			SELECT user_id,
				count(*) AS calls,
				COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens,
				COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
				COALESCE(SUM(cost_usd), 0) AS cost_usd,
				MAX(ts) AS last_used_at
			FROM usage_ledger` + where + `
			GROUP BY user_id
		)
		SELECT g.user_id,
			COALESCE(u.username_normalized, '') AS username,
			COALESCE(u.email_normalized, '') AS email,
			COALESCE(u.name, '') AS name,
			COALESCE(u.role, '') AS role,
			COALESCE(u.enabled, FALSE) AS enabled,
			(u.id IS NOT NULL) AS known_user,
			g.calls, g.prompt_tokens, g.completion_tokens, g.cost_usd, g.last_used_at
		FROM grouped g
		LEFT JOIN LATERAL (
			SELECT id, username_normalized, email_normalized, name, role, enabled
			FROM auth_users
			WHERE deleted_at IS NULL
				AND (id = g.user_id
					OR email_normalized = lower(g.user_id)
					OR username_normalized = lower(g.user_id))
			ORDER BY CASE
				WHEN id = g.user_id THEN 0
				WHEN email_normalized = lower(g.user_id) THEN 1
				ELSE 2
			END
			LIMIT 1
		) u ON TRUE
		ORDER BY (g.prompt_tokens + g.completion_tokens) DESC, g.user_id ASC`
	if q.Limit > 0 {
		args = append(args, q.Limit)
		sql += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if q.Offset > 0 {
		args = append(args, q.Offset)
		sql += fmt.Sprintf(" OFFSET $%d", len(args))
	}
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TokenUsageUser, 0)
	for rows.Next() {
		var user TokenUsageUser
		if err := rows.Scan(
			&user.UserID, &user.Username, &user.Email, &user.Name, &user.Role,
			&user.Enabled, &user.KnownUser, &user.Calls, &user.PromptTokens,
			&user.CompletionTokens, &user.CostUSD, &user.LastUsedAt,
		); err != nil {
			return nil, err
		}
		user.LastUsedAt = user.LastUsedAt.UTC()
		user.TotalTokens = user.PromptTokens + user.CompletionTokens
		out = append(out, user)
	}
	return out, rows.Err()
}
