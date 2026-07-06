package traceevent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Event mirrors db.migrations trace_events. It stays local to gateway so the
// BFF can write timing data without importing admin-api internals.
type Event struct {
	TraceID    string
	Service    string
	Name       string
	Category   string
	StartedAt  time.Time
	DurationMS int64
	Status     string
	Metadata   map[string]any
}

type Store interface {
	AppendTraceEvent(ctx context.Context, e Event) error
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

func (p *Postgres) AppendTraceEvent(ctx context.Context, e Event) error {
	if e.TraceID == "" || e.Name == "" {
		return nil
	}
	if e.StartedAt.IsZero() {
		e.StartedAt = time.Now().UTC()
	}
	if e.Service == "" {
		e.Service = "gateway"
	}
	if e.Status == "" {
		e.Status = "ok"
	}
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	meta, err := json.Marshal(e.Metadata)
	if err != nil {
		return err
	}
	const q = `INSERT INTO trace_events (
		trace_id, service, name, category, started_at, duration_ms, status, metadata_json
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`
	_, err = p.pool.Exec(ctx, q,
		e.TraceID, e.Service, e.Name, e.Category, e.StartedAt, e.DurationMS, e.Status, meta)
	return err
}
