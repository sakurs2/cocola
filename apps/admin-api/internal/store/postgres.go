package store

import (
	"context"
	"errors"
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

// nullableTime converts a zero time.Time to NULL on write. expires_at and
// revoked_at use NULL for "unset"; everything else stores the value as-is.
func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
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

// ---- Skills ----

const skillCols = `id, name, description, version, entrypoint, enabled, created_at, updated_at`

func scanSkill(row pgx.Row) (Skill, error) {
	var s Skill
	err := row.Scan(&s.ID, &s.Name, &s.Description, &s.Version, &s.Entrypoint,
		&s.Enabled, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

func (p *Postgres) CreateSkill(ctx context.Context, s Skill) error {
	const q = `INSERT INTO skill_entries (` + skillCols + `) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`
	_, err := p.pool.Exec(ctx, q,
		s.ID, s.Name, s.Description, s.Version, s.Entrypoint, s.Enabled, s.CreatedAt, s.UpdatedAt)
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

func (p *Postgres) UpdateSkill(ctx context.Context, s Skill) error {
	const q = `UPDATE skill_entries
		SET name=$2, description=$3, version=$4, entrypoint=$5, enabled=$6, created_at=$7, updated_at=$8
		WHERE id=$1`
	ct, err := p.pool.Exec(ctx, q,
		s.ID, s.Name, s.Description, s.Version, s.Entrypoint, s.Enabled, s.CreatedAt, s.UpdatedAt)
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

// ---- Audit ----

func (p *Postgres) AppendAudit(ctx context.Context, e AuditEntry) error {
	// id is BIGSERIAL; the DB assigns it. ListAudit reflects the assigned ids.
	const q = `INSERT INTO audit_log (ts, actor, action, resource, detail) VALUES ($1,$2,$3,$4,$5)`
	_, err := p.pool.Exec(ctx, q, e.At, e.Actor, e.Action, e.Resource, e.Detail)
	return err
}

func (p *Postgres) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	q := `SELECT id, ts, actor, action, resource, detail FROM audit_log ORDER BY id DESC`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT $1`
		args = append(args, limit)
	}
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AuditEntry, 0)
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.At, &e.Actor, &e.Action, &e.Resource, &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
