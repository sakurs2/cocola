// Package convo is the gateway's conversation-persistence seam: durable storage
// of a user's conversations and the rendered messages within them, so the web
// sidebar can list conversations and clicking one re-renders its history.
//
// This is a "route A" UI-message MIRROR (see docs/plan/
// conversation-persistence-history-rendering.md): we store the exact parts the
// web client renders (text / reasoning / tool-call / file), NOT the agent's
// on-disk claude JSONL (which stays the source of truth for --resume). The Store
// contract has two backends behind one interface, matching the project rule
// (go-common/redis.KV, admin-api/store): Memory for tests/zero-dep dev, Postgres
// when COCOLA_PG_DSN is set. Schema is owned by db/migrations (goose); this
// package only reads/writes rows and never declares DDL.
package convo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrNotFound is returned when a conversation lookup misses (or the caller does
// not own it). Handlers map it to a 404 so ownership misses are indistinguishable
// from "does not exist" (no cross-user existence oracle).
var (
	ErrNotFound            = errors.New("convo: not found")
	ErrRuntimeMismatch     = errors.New("convo: runtime mismatch")
	ErrFolderNameConflict  = errors.New("convo: folder name conflict")
	ErrInvalidFolderName   = errors.New("convo: invalid folder name")
	ErrUnsupportedChatType = errors.New("convo: unsupported chat type")
)

const DefaultRuntimeID = "claude-code"

func normalizeFolderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 80 {
		return "", ErrInvalidFolderName
	}
	return name, nil
}

// PartType enumerates the UiPart shapes the web client renders. These string
// values are the WIRE CONTRACT with apps/web/app/runtime-provider.tsx (UiPart):
// changing them requires a matching frontend change.
const (
	PartText          = "text"
	PartReasoning     = "reasoning"
	PartToolCall      = "tool-call"
	PartFile          = "file"
	PartEnvironment   = "environment"
	PartSessionStatus = "session-status"
	PartProgress      = "progress"
)

// Part mirrors the frontend UiPart union. A text/reasoning part uses Text; a
// tool-call part uses the ToolCall* fields; a file part uses the artifact
// metadata fields. Persisted as JSONB verbatim so a
// read replays straight into convertMessage with zero schema drift.
//
// Only the fields relevant to a given Type are populated; the rest stay at
// their zero value and omitempty keeps the stored JSON compact. Result is a
// pointer so we can distinguish "no result yet" (nil) from an empty-string
// result, matching the frontend's optional `result`.
type Part struct {
	Type string `json:"type"`

	// text | reasoning
	Text string `json:"text,omitempty"`

	// tool-call
	ToolCallID string  `json:"toolCallId,omitempty"`
	ToolName   string  `json:"toolName,omitempty"`
	ArgsText   string  `json:"argsText,omitempty"`
	Result     *string `json:"result,omitempty"`
	IsError    bool    `json:"isError,omitempty"`

	// file
	ID          string `json:"id,omitempty"`
	Filename    string `json:"filename,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
	Size        int64  `json:"size,omitempty"`
	DownloadURL string `json:"downloadUrl,omitempty"`

	// environment: an opaque, versioned snapshot. Keeping the raw JSON intact
	// lets newer component kinds/fields survive an older gateway unchanged.
	Environment json.RawMessage `json:"environment,omitempty"`

	// session-status: the latest secret-free Skills/MCP initialization snapshot.
	SessionStatus json.RawMessage `json:"sessionStatus,omitempty"`

	// progress: one adapter-owned replaceable progress snapshot.
	ProgressID    string          `json:"progressId,omitempty"`
	ProgressItems json.RawMessage `json:"items,omitempty"`
}

// Conversation is one row in the sidebar. ID reuses the frontend session_id.
type Conversation struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	TenantID  string    `json:"tenant_id"`
	Title     string    `json:"title"`
	ChatType  string    `json:"chat_type"`
	FolderID  string    `json:"folder_id,omitempty"`
	Hidden    bool      `json:"hidden"`
	RuntimeID string    `json:"runtime_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Folder is one user-owned, flat container for ordinary conversations.
type Folder struct {
	ID        string    `json:"id"`
	UserID    string    `json:"-"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Message is one rendered message within a conversation.
type Message struct {
	ID             string         `json:"id"`
	ConversationID string         `json:"conversation_id"`
	Role           string         `json:"role"` // "user" | "assistant"
	Parts          []Part         `json:"parts"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

// Artifact is one downloadable agent-produced file. Bytes are stored in the
// object store; this row is the auth-scoped metadata index.
type Artifact struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	UserID         string    `json:"user_id"`
	TenantID       string    `json:"tenant_id"`
	Filename       string    `json:"filename"`
	Mime           string    `json:"mime"`
	Size           int64     `json:"size"`
	ObjectKey      string    `json:"object_key"`
	CreatedAt      time.Time `json:"created_at"`
}

// Store is the persistence contract the gateway depends on. All reads are
// scoped by userID so a caller can only ever see their own conversations
// (ownership is enforced in the store, never trusted from the request body).
type Store interface {
	// UpsertConversation inserts a new conversation or, on id conflict, refreshes
	// updated_at. Title is set only on first insert (never overwritten), so the
	// MVP "first user message" title is stable across follow-up turns.
	UpsertConversation(ctx context.Context, c Conversation) error
	// RevealConversation makes a hidden conversation visible after a background
	// run finishes, optionally setting a stable title if it was created hidden.
	RevealConversation(ctx context.Context, convID, userID, title string, updatedAt time.Time) error
	// InsertMessage appends a message to a conversation.
	InsertMessage(ctx context.Context, m Message) error
	// UpsertMessage creates or replaces one deterministic message. Chat runs use
	// this for their single assistant draft/final row.
	UpsertMessage(ctx context.Context, m Message) error
	// ListConversations returns userID's conversations, most-recently-updated first.
	ListConversations(ctx context.Context, userID string) ([]Conversation, error)
	// GetConversation returns one conversation only when userID owns it.
	GetConversation(ctx context.Context, convID, userID string) (Conversation, error)
	// GetMessages returns a conversation's messages in chronological order, but
	// ONLY if userID owns it; otherwise ErrNotFound (no cross-user leak).
	GetMessages(ctx context.Context, convID, userID string) ([]Message, error)
	// RenameConversation updates a conversation title only when userID owns it.
	RenameConversation(ctx context.Context, convID, userID, title string) (Conversation, error)
	// DeleteConversation deletes a conversation only when userID owns it.
	DeleteConversation(ctx context.Context, convID, userID string) error
	// Folder operations are always scoped by userID. DeleteFolder returns the
	// conversation ids removed by the database cascade for external cleanup.
	ListFolders(ctx context.Context, userID string) ([]Folder, error)
	GetFolder(ctx context.Context, folderID, userID string) (Folder, error)
	CreateFolder(ctx context.Context, folder Folder) (Folder, error)
	RenameFolder(ctx context.Context, folderID, userID, name string, updatedAt time.Time) (Folder, error)
	ListFolderConversationIDs(ctx context.Context, folderID, userID string) ([]string, error)
	DeleteFolder(ctx context.Context, folderID, userID string) ([]string, error)
	MoveConversation(ctx context.Context, convID, userID, folderID string, updatedAt time.Time) (Conversation, error)
	// UpsertArtifact records a downloadable artifact's metadata.
	UpsertArtifact(ctx context.Context, a Artifact) error
	// GetArtifact returns an artifact only when userID owns its conversation.
	GetArtifact(ctx context.Context, convID, artifactID, userID string) (Artifact, error)
}
