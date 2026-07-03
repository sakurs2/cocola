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
	metadata := m.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	const q = `INSERT INTO messages (id, conversation_id, role, parts_json, metadata_json, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)`
	_, err = p.pool.Exec(ctx, q,
		m.ID, m.ConversationID, m.Role, partsJSON, metadataJSON, m.CreatedAt)
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

func (p *Postgres) GetConversation(ctx context.Context, convID, userID string) (Conversation, error) {
	const q = `SELECT id, user_id, tenant_id, title, created_at, updated_at
		FROM conversations WHERE id = $1 AND user_id = $2`
	var c Conversation
	if err := p.pool.QueryRow(ctx, q, convID, userID).Scan(
		&c.ID, &c.UserID, &c.TenantID, &c.Title, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return Conversation{}, ErrNotFound
		}
		return Conversation{}, err
	}
	return c, nil
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
	const q = `SELECT id, conversation_id, role, parts_json, metadata_json, created_at
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
		var metadataJSON []byte
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &partsJSON, &metadataJSON, &m.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(partsJSON, &m.Parts); err != nil {
			return nil, err
		}
		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &m.Metadata); err != nil {
				return nil, err
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (p *Postgres) RenameConversation(ctx context.Context, convID, userID, title string) (Conversation, error) {
	const q = `UPDATE conversations SET title = $3
		WHERE id = $1 AND user_id = $2
		RETURNING id, user_id, tenant_id, title, created_at, updated_at`
	var c Conversation
	if err := p.pool.QueryRow(ctx, q, convID, userID, title).Scan(
		&c.ID, &c.UserID, &c.TenantID, &c.Title, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return Conversation{}, ErrNotFound
		}
		return Conversation{}, err
	}
	return c, nil
}

func (p *Postgres) DeleteConversation(ctx context.Context, convID, userID string) error {
	const q = `DELETE FROM conversations WHERE id = $1 AND user_id = $2`
	tag, err := p.pool.Exec(ctx, q, convID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) UpsertArtifact(ctx context.Context, a Artifact) error {
	const q = `INSERT INTO artifacts
		(id, conversation_id, user_id, tenant_id, filename, mime, size_bytes, object_key, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (id) DO UPDATE SET
			filename = EXCLUDED.filename,
			mime = EXCLUDED.mime,
			size_bytes = EXCLUDED.size_bytes,
			object_key = EXCLUDED.object_key`
	_, err := p.pool.Exec(ctx, q,
		a.ID, a.ConversationID, a.UserID, a.TenantID, a.Filename, a.Mime, a.Size, a.ObjectKey, a.CreatedAt)
	return err
}

func (p *Postgres) GetArtifact(ctx context.Context, convID, artifactID, userID string) (Artifact, error) {
	const q = `SELECT a.id, a.conversation_id, a.user_id, a.tenant_id,
			a.filename, a.mime, a.size_bytes, a.object_key, a.created_at
		FROM artifacts a
		JOIN conversations c ON c.id = a.conversation_id
		WHERE a.id = $1 AND a.conversation_id = $2 AND c.user_id = $3`
	var a Artifact
	if err := p.pool.QueryRow(ctx, q, artifactID, convID, userID).Scan(
		&a.ID, &a.ConversationID, &a.UserID, &a.TenantID,
		&a.Filename, &a.Mime, &a.Size, &a.ObjectKey, &a.CreatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return Artifact{}, ErrNotFound
		}
		return Artifact{}, err
	}
	return a, nil
}
