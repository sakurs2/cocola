package convo

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres is the durable Store backend. It implements the same Store contract
// as Memory, so main.go swaps it in by env (COCOLA_PG_DSN) with no handler
// change. Schema is owned by the goose migrations in the db module (single
// source of truth); this type only reads/writes rows and never declares DDL.
type Postgres struct {
	pool *pgxpool.Pool
}

var _ Store = (*Postgres)(nil)

// NewPostgres connects a pool to dsn and verifies connectivity. The caller owns
// the lifecycle and must call Close. Migration is a separate concern (migrate.go)
// and is expected to have run before queries are issued.
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

func (p *Postgres) UpsertConversation(ctx context.Context, c Conversation) error {
	// ON CONFLICT: refresh updated_at only; title is set once (COALESCE keeps
	// the existing non-empty title so a follow-up turn never rewrites it).
	const q = `INSERT INTO conversations (id, user_id, tenant_id, title, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (id) DO UPDATE SET updated_at = EXCLUDED.updated_at`
	_, err := p.pool.Exec(ctx, q,
		c.ID, c.UserID, c.TenantID, c.Title, c.CreatedAt, c.UpdatedAt)
	return err
}

func (p *Postgres) InsertMessage(ctx context.Context, m Message) error {
	partsJSON, err := json.Marshal(m.Parts)
	if err != nil {
		return err
	}
	const q = `INSERT INTO messages (id, conversation_id, role, parts_json, created_at)
		VALUES ($1,$2,$3,$4,$5)`
	_, err = p.pool.Exec(ctx, q,
		m.ID, m.ConversationID, m.Role, partsJSON, m.CreatedAt)
	return err
}

func (p *Postgres) ListConversations(ctx context.Context, userID string) ([]Conversation, error) {
	const q = `SELECT id, user_id, tenant_id, title, created_at, updated_at
		FROM conversations WHERE user_id = $1 ORDER BY updated_at DESC, id DESC`
	rows, err := p.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Conversation, 0)
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.UserID, &c.TenantID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p *Postgres) GetMessages(ctx context.Context, convID, userID string) ([]Message, error) {
	// Ownership gate: one round-trip that both checks the owner and pulls rows.
	// If the conversation is missing or owned by someone else, no rows come back
	// AND the owner check fails, so we return ErrNotFound (no existence oracle).
	const ownerQ = `SELECT 1 FROM conversations WHERE id = $1 AND user_id = $2`
	var one int
	if err := p.pool.QueryRow(ctx, ownerQ, convID, userID).Scan(&one); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	const q = `SELECT id, conversation_id, role, parts_json, created_at
		FROM messages WHERE conversation_id = $1 ORDER BY created_at ASC, id ASC`
	rows, err := p.pool.Query(ctx, q, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Message, 0)
	for rows.Next() {
		var m Message
		var partsJSON []byte
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &partsJSON, &m.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(partsJSON, &m.Parts); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
