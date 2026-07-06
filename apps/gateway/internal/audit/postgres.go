package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Event mirrors db.migrations audit_events. It intentionally stays local to the
// gateway so the BFF does not import admin-api internals.
type Event struct {
	At           time.Time
	ActorType    string
	ActorUserID  string
	ActorEmail   string
	Action       string
	ResourceType string
	ResourceID   string
	Result       string
	HTTPMethod   string
	Route        string
	StatusCode   int
	RequestID    string
	TraceID      string
	ClientIP     string
	UserAgent    string
	Metadata     map[string]any
	ErrorCode    string
}

type Store interface {
	AppendAuditEvent(ctx context.Context, e Event) error
}

type Postgres struct {
	pool *pgxpool.Pool
}

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

func (p *Postgres) AppendAuditEvent(ctx context.Context, e Event) error {
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
