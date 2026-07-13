// Package chatrun owns the minimal durable state for one interactive Agent run.
// Execution and subscriptions stay in one Gateway process; PostgreSQL only
// stores idempotency, terminal state and the latest assistant draft.
package chatrun

import (
	"context"
	"errors"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
)

var (
	ErrNotFound        = errors.New("chatrun: not found")
	ErrConflict        = errors.New("chatrun: conversation already has an active run")
	ErrRuntimeMismatch = errors.New("chatrun: conversation runtime mismatch")
)

const (
	StatusRunning     = "running"
	StatusSuccess     = "success"
	StatusError       = "error"
	StatusCancelled   = "cancelled"
	StatusInterrupted = "interrupted"
)

type Run struct {
	ID                string     `json:"run_id"`
	RootSpanID        string     `json:"-"`
	ConversationID    string     `json:"conversation_id"`
	ConversationTitle string     `json:"-"`
	UserID            string     `json:"-"`
	Source            string     `json:"source"`
	ModelRouteID      string     `json:"model_route_id,omitempty"`
	ModelAlias        string     `json:"model_alias,omitempty"`
	ClientRequestID   string     `json:"client_request_id,omitempty"`
	Status            string     `json:"status"`
	StartedAt         time.Time  `json:"started_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	LastActivityAt    time.Time  `json:"last_activity_at"`
	ErrorCode         string     `json:"error_code,omitempty"`
}

type StartInput struct {
	Run          Run
	Conversation convo.Conversation
	UserMessage  convo.Message
}

type StartResult struct {
	Run          Run
	Conversation convo.Conversation
	Created      bool
}

type FinalizeInput struct {
	RunID             string
	UserID            string
	Status            string
	ErrorCode         string
	AssistantMessage  *convo.Message
	Reveal            bool
	ConversationTitle string
}

type Store interface {
	Start(ctx context.Context, in StartInput) (StartResult, error)
	GetOwned(ctx context.Context, runID, userID string) (Run, error)
	Active(ctx context.Context, conversationID, userID string) (Run, error)
	SaveDraft(ctx context.Context, runID, userID string, message convo.Message) error
	Finalize(ctx context.Context, in FinalizeInput) (Run, error)
	InterruptRunning(ctx context.Context, now time.Time) (int64, error)
	Close()
}

func IsTerminal(status string) bool {
	return status == StatusSuccess || status == StatusError ||
		status == StatusCancelled || status == StatusInterrupted
}
