package convo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	if c.UserID == "" {
		tag, err := p.pool.Exec(ctx, `UPDATE conversations SET updated_at=$2 WHERE id=$1`, c.ID, c.UpdatedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	}
	if c.ChatType == "" {
		c.ChatType = "chat"
	}
	if c.RuntimeID == "" {
		c.RuntimeID = DefaultRuntimeID
	}
	// A caller-controlled conversation id may only refresh its original owner.
	const q = `INSERT INTO conversations (id, user_id, tenant_id, title, chat_type, folder_id, hidden, runtime_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,$10)
		ON CONFLICT (id) DO UPDATE SET updated_at = EXCLUDED.updated_at
		WHERE conversations.user_id = EXCLUDED.user_id
			AND conversations.runtime_id = EXCLUDED.runtime_id
		RETURNING id`
	var id string
	err := p.pool.QueryRow(ctx, q,
		c.ID, c.UserID, c.TenantID, c.Title, c.ChatType, c.FolderID, c.Hidden, c.RuntimeID,
		c.CreatedAt, c.UpdatedAt).Scan(&id)
	if err == pgx.ErrNoRows {
		return ErrNotFound
	}
	if c.FolderID != "" && isPostgresCode(err, "23503") {
		return ErrNotFound
	}
	if c.FolderID != "" && isPostgresCode(err, "23514") {
		return ErrUnsupportedChatType
	}
	return err
}

func (p *Postgres) RevealConversation(ctx context.Context, convID, userID, title string, updatedAt time.Time) error {
	const q = `UPDATE conversations
		SET hidden=FALSE,
		    title=CASE WHEN $3 <> '' THEN $3 ELSE title END,
		    updated_at=$4
		WHERE id=$1 AND user_id=$2`
	tag, err := p.pool.Exec(ctx, q, convID, userID, title, updatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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
		VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (id) DO NOTHING`
	_, err = p.pool.Exec(ctx, q,
		m.ID, m.ConversationID, m.Role, partsJSON, metadataJSON, m.CreatedAt)
	return err
}

func (p *Postgres) UpsertMessage(ctx context.Context, m Message) error {
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
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (id) DO UPDATE SET
			parts_json=EXCLUDED.parts_json,
			metadata_json=EXCLUDED.metadata_json,
			created_at=EXCLUDED.created_at`
	_, err = p.pool.Exec(ctx, q,
		m.ID, m.ConversationID, m.Role, partsJSON, metadataJSON, m.CreatedAt)
	return err
}

func (p *Postgres) ListConversations(ctx context.Context, userID string) ([]Conversation, error) {
	const q = `SELECT id, user_id, tenant_id, title, chat_type, COALESCE(folder_id, ''), hidden, runtime_id, created_at, updated_at
		FROM conversations WHERE user_id = $1 AND hidden = FALSE ORDER BY updated_at DESC, id DESC`
	rows, err := p.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Conversation, 0)
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.UserID, &c.TenantID, &c.Title, &c.ChatType, &c.FolderID,
			&c.Hidden, &c.RuntimeID, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p *Postgres) GetConversation(ctx context.Context, convID, userID string) (Conversation, error) {
	const q = `SELECT id, user_id, tenant_id, title, chat_type, COALESCE(folder_id, ''), hidden, runtime_id, created_at, updated_at
		FROM conversations WHERE id = $1 AND user_id = $2`
	var c Conversation
	if err := p.pool.QueryRow(ctx, q, convID, userID).Scan(
		&c.ID, &c.UserID, &c.TenantID, &c.Title, &c.ChatType, &c.FolderID,
		&c.Hidden, &c.RuntimeID, &c.CreatedAt, &c.UpdatedAt,
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
		FROM messages WHERE conversation_id = $1
		ORDER BY created_at ASC,
			CASE role WHEN 'user' THEN 0 WHEN 'assistant' THEN 1 ELSE 2 END ASC,
			id ASC`
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
		RETURNING id, user_id, tenant_id, title, chat_type, COALESCE(folder_id, ''), hidden, runtime_id, created_at, updated_at`
	var c Conversation
	if err := p.pool.QueryRow(ctx, q, convID, userID, title).Scan(
		&c.ID, &c.UserID, &c.TenantID, &c.Title, &c.ChatType, &c.FolderID,
		&c.Hidden, &c.RuntimeID, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return Conversation{}, ErrNotFound
		}
		return Conversation{}, err
	}
	return c, nil
}

func (p *Postgres) ListFolders(ctx context.Context, userID string) ([]Folder, error) {
	const q = `SELECT id, user_id, name, created_at, updated_at
		FROM conversation_folders WHERE user_id=$1 ORDER BY LOWER(name), id`
	rows, err := p.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Folder, 0)
	for rows.Next() {
		var folder Folder
		if err := rows.Scan(&folder.ID, &folder.UserID, &folder.Name, &folder.CreatedAt, &folder.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, folder)
	}
	return out, rows.Err()
}

func (p *Postgres) GetFolder(ctx context.Context, folderID, userID string) (Folder, error) {
	const q = `SELECT id, user_id, name, created_at, updated_at
		FROM conversation_folders WHERE id=$1 AND user_id=$2`
	var folder Folder
	if err := p.pool.QueryRow(ctx, q, folderID, userID).Scan(
		&folder.ID, &folder.UserID, &folder.Name, &folder.CreatedAt, &folder.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return Folder{}, ErrNotFound
		}
		return Folder{}, err
	}
	return folder, nil
}

func (p *Postgres) CreateFolder(ctx context.Context, folder Folder) (Folder, error) {
	name, err := normalizeFolderName(folder.Name)
	if err != nil {
		return Folder{}, err
	}
	folder.Name = name
	const q = `INSERT INTO conversation_folders (id, user_id, name, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id, user_id, name, created_at, updated_at`
	var created Folder
	err = p.pool.QueryRow(ctx, q, folder.ID, folder.UserID, folder.Name, folder.CreatedAt, folder.UpdatedAt).Scan(
		&created.ID, &created.UserID, &created.Name, &created.CreatedAt, &created.UpdatedAt,
	)
	if isUniqueViolation(err) {
		return Folder{}, ErrFolderNameConflict
	}
	return created, err
}

func (p *Postgres) RenameFolder(ctx context.Context, folderID, userID, name string, updatedAt time.Time) (Folder, error) {
	name, err := normalizeFolderName(name)
	if err != nil {
		return Folder{}, err
	}
	const q = `UPDATE conversation_folders SET name=$3, updated_at=$4
		WHERE id=$1 AND user_id=$2
		RETURNING id, user_id, name, created_at, updated_at`
	var folder Folder
	err = p.pool.QueryRow(ctx, q, folderID, userID, name, updatedAt).Scan(
		&folder.ID, &folder.UserID, &folder.Name, &folder.CreatedAt, &folder.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return Folder{}, ErrNotFound
	}
	if isUniqueViolation(err) {
		return Folder{}, ErrFolderNameConflict
	}
	return folder, err
}

func (p *Postgres) ListFolderConversationIDs(ctx context.Context, folderID, userID string) ([]string, error) {
	if _, err := p.GetFolder(ctx, folderID, userID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT id FROM conversations
		WHERE user_id=$1 AND folder_id=$2 ORDER BY id`, userID, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (p *Postgres) DeleteFolder(ctx context.Context, folderID, userID string) ([]string, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var ownedID string
	if err := tx.QueryRow(ctx, `SELECT id FROM conversation_folders
		WHERE id=$1 AND user_id=$2 FOR UPDATE`, folderID, userID).Scan(&ownedID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM conversations
		WHERE user_id=$1 AND folder_id=$2 ORDER BY id FOR UPDATE`, userID, folderID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if _, err := tx.Exec(ctx, `DELETE FROM conversation_folders WHERE id=$1 AND user_id=$2`, folderID, userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return ids, nil
}

func (p *Postgres) MoveConversation(ctx context.Context, convID, userID, folderID string, updatedAt time.Time) (Conversation, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Conversation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if folderID != "" {
		var id string
		if err := tx.QueryRow(ctx, `SELECT id FROM conversation_folders
			WHERE id=$1 AND user_id=$2`, folderID, userID).Scan(&id); err != nil {
			if err == pgx.ErrNoRows {
				return Conversation{}, ErrNotFound
			}
			return Conversation{}, err
		}
	}
	const q = `UPDATE conversations
		SET folder_id=NULLIF($3,''), updated_at=$4
		WHERE id=$1 AND user_id=$2 AND chat_type='chat'
		RETURNING id, user_id, tenant_id, title, chat_type, COALESCE(folder_id, ''), hidden, runtime_id, created_at, updated_at`
	var conversation Conversation
	err = tx.QueryRow(ctx, q, convID, userID, folderID, updatedAt).Scan(
		&conversation.ID, &conversation.UserID, &conversation.TenantID, &conversation.Title,
		&conversation.ChatType, &conversation.FolderID, &conversation.Hidden, &conversation.RuntimeID,
		&conversation.CreatedAt, &conversation.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		var chatType string
		if lookupErr := tx.QueryRow(ctx, `SELECT chat_type FROM conversations
			WHERE id=$1 AND user_id=$2`, convID, userID).Scan(&chatType); lookupErr == nil {
			return Conversation{}, ErrUnsupportedChatType
		}
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Conversation{}, err
	}
	return conversation, nil
}

func isUniqueViolation(err error) bool {
	return isPostgresCode(err, "23505")
}

func isPostgresCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
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
