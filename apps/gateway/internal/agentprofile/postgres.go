package agentprofile

import (
	"context"
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

const columns = `id::text, tenant_id, owner_user_id, name, description, instructions,
	avatar_key, avatar_color, runtime_id, model_route_id, model_alias, status,
	version, created_at, updated_at, archived_at`

func scanAgent(row pgx.Row) (Agent, error) {
	var value Agent
	err := row.Scan(
		&value.ID, &value.TenantID, &value.OwnerUserID, &value.Name,
		&value.Description, &value.Instructions, &value.AvatarKey, &value.AvatarColor,
		&value.RuntimeID, &value.ModelRouteID, &value.ModelAlias, &value.Status,
		&value.Version, &value.CreatedAt, &value.UpdatedAt, &value.ArchivedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Agent{}, ErrNotFound
	}
	return value, err
}

func (p *Postgres) List(ctx context.Context, id Identity) ([]Agent, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+columns+` FROM agents
		WHERE tenant_id=$1 AND owner_user_id=$2 AND status='active'
		ORDER BY updated_at DESC, id DESC`, id.TenantID, id.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Agent, 0)
	for rows.Next() {
		value, scanErr := scanAgent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (p *Postgres) Get(ctx context.Context, id Identity, agentID string) (Agent, error) {
	return scanAgent(p.pool.QueryRow(ctx, `SELECT `+columns+` FROM agents
		WHERE id=$1::uuid AND tenant_id=$2 AND owner_user_id=$3`,
		agentID, id.TenantID, id.UserID))
}

func (p *Postgres) Create(ctx context.Context, value Agent) (Agent, error) {
	const query = `INSERT INTO agents (
		id, tenant_id, owner_user_id, name, description, instructions, avatar_key,
		avatar_color, runtime_id, model_route_id, model_alias, status, version,
		created_at, updated_at
	) VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'active',1,$12,$13)
	RETURNING ` + columns
	result, err := scanAgent(p.pool.QueryRow(ctx, query,
		value.ID, value.TenantID, value.OwnerUserID, value.Name, value.Description,
		value.Instructions, value.AvatarKey, value.AvatarColor, value.RuntimeID,
		value.ModelRouteID, value.ModelAlias, value.CreatedAt, value.UpdatedAt,
	))
	if postgresCode(err, "23505") {
		return Agent{}, ErrConflict
	}
	return result, err
}

func (p *Postgres) Update(
	ctx context.Context,
	id Identity,
	value Agent,
	expected int64,
) (Agent, error) {
	const query = `UPDATE agents SET
		name=$5, description=$6, instructions=$7, avatar_key=$8, avatar_color=$9,
		runtime_id=$10, model_route_id=$11, model_alias=$12,
		version=version+1, updated_at=$13
	WHERE id=$1::uuid AND tenant_id=$2 AND owner_user_id=$3
		AND version=$4 AND status='active'
	RETURNING ` + columns
	result, err := scanAgent(p.pool.QueryRow(ctx, query,
		value.ID, id.TenantID, id.UserID, expected, value.Name, value.Description,
		value.Instructions, value.AvatarKey, value.AvatarColor, value.RuntimeID,
		value.ModelRouteID, value.ModelAlias, value.UpdatedAt,
	))
	if postgresCode(err, "23505") {
		return Agent{}, ErrConflict
	}
	if errors.Is(err, ErrNotFound) {
		current, getErr := p.Get(ctx, id, value.ID)
		switch {
		case errors.Is(getErr, ErrNotFound):
			return Agent{}, ErrNotFound
		case getErr != nil:
			return Agent{}, getErr
		case current.Status == StatusArchived:
			return Agent{}, ErrArchived
		default:
			return Agent{}, ErrVersionConflict
		}
	}
	return result, err
}

func (p *Postgres) Archive(
	ctx context.Context,
	id Identity,
	agentID string,
	expected int64,
	now time.Time,
) (Agent, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Agent{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := scanAgent(tx.QueryRow(ctx, `SELECT `+columns+` FROM agents
		WHERE id=$1::uuid AND tenant_id=$2 AND owner_user_id=$3
		FOR UPDATE`, agentID, id.TenantID, id.UserID))
	if err != nil {
		return Agent{}, err
	}
	if current.Status == StatusArchived {
		return current, nil
	}
	var inUse bool
	if err := tx.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM channel_connectors WHERE agent_id=$1::uuid)
		OR EXISTS(
			SELECT 1 FROM channel_registration_flows
			WHERE agent_id=$1::uuid
				AND status IN ('starting','awaiting_user','authorizing')
		)`, agentID).Scan(&inUse); err != nil {
		return Agent{}, err
	}
	if inUse {
		return Agent{}, ErrInUse
	}
	if current.Version != expected {
		return Agent{}, ErrVersionConflict
	}
	result, err := scanAgent(tx.QueryRow(ctx, `UPDATE agents SET
		status='archived', archived_at=$2, version=version+1, updated_at=$2
		WHERE id=$1::uuid AND status='active'
		RETURNING `+columns, agentID, now))
	if err != nil {
		return Agent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Agent{}, err
	}
	return result, nil
}

func postgresCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
