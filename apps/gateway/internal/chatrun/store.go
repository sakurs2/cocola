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
	ErrNotFound             = errors.New("chatrun: not found")
	ErrConflict             = errors.New("chatrun: conversation already has an active run")
	ErrRuntimeMismatch      = errors.New("chatrun: conversation runtime mismatch")
	ErrFolderNotFound       = errors.New("chatrun: folder not found")
	ErrFolderMismatch       = errors.New("chatrun: conversation folder mismatch")
	ErrProjectNotFound      = errors.New("chatrun: project not found")
	ErrProjectNotReady      = errors.New("chatrun: project not ready")
	ErrProjectMismatch      = errors.New("chatrun: conversation project mismatch")
	ErrProjectSingleTask    = errors.New("chatrun: local project already has a task")
	ErrPlanNotCurrent       = errors.New("chatrun: plan is not current")
	ErrPlanState            = errors.New("chatrun: plan state does not allow this operation")
	ErrPlanModelUnavailable = errors.New("chatrun: plan model is unavailable")
)

const (
	StatusRunning     = "running"
	StatusSuccess     = "success"
	StatusError       = "error"
	StatusCancelled   = "cancelled"
	StatusInterrupted = "interrupted"

	InteractionModeExecute = "execute"
	InteractionModePlan    = "plan"

	PlanStatusReady      = "ready"
	PlanStatusExecuting  = "executing"
	PlanStatusCompleted  = "completed"
	PlanStatusStopped    = "stopped"
	PlanStatusFailed     = "failed"
	PlanStatusSuperseded = "superseded"
	PlanStatusCancelled  = "cancelled"
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
	InteractionMode   string     `json:"interaction_mode"`
	PlanID            string     `json:"plan_id,omitempty"`
	Status            string     `json:"status"`
	StartedAt         time.Time  `json:"started_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	LastActivityAt    time.Time  `json:"last_activity_at"`
	ErrorCode         string     `json:"error_code,omitempty"`
}

type StartInput struct {
	Run            Run
	Conversation   convo.Conversation
	UserMessage    convo.Message
	ProjectBaseRef string
	ProjectBaseSHA string
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
	CompletedAt       time.Time
	PlanCandidate     *PlanCandidate
}

type Plan struct {
	ID                string     `json:"id"`
	ConversationID    string     `json:"conversation_id"`
	Version           int        `json:"version"`
	Status            string     `json:"status"`
	SourceRunID       string     `json:"source_run_id"`
	RuntimeID         string     `json:"runtime_id"`
	ModelRouteID      string     `json:"model_route_id"`
	ModelAlias        string     `json:"model_alias"`
	ContentMarkdown   string     `json:"content_markdown"`
	WorkspaceRevision string     `json:"workspace_revision,omitempty"`
	ApprovedBy        string     `json:"approved_by,omitempty"`
	ApprovedAt        *time.Time `json:"approved_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type PlanCandidate struct {
	ID                string
	RuntimeID         string
	ModelRouteID      string
	ModelAlias        string
	ContentMarkdown   string
	WorkspaceRevision string
}

type FinalizeResult struct {
	Run              Run
	Plan             *Plan
	SupersededPlanID string
}

type PlanExecutionInput struct {
	Run             Run
	ConversationID  string
	UserID          string
	ExpectedVersion int
	PlanID          string
	ApprovedAt      time.Time
}

type PlanExecutionResult struct {
	Run          Run
	Conversation convo.Conversation
	Plan         Plan
	Created      bool
}

type Store interface {
	Start(ctx context.Context, in StartInput) (StartResult, error)
	StartPlanExecution(ctx context.Context, in PlanExecutionInput) (PlanExecutionResult, error)
	CancelPlan(ctx context.Context, conversationID, planID, userID string, expectedVersion int, now time.Time) (Plan, error)
	GetPlan(ctx context.Context, conversationID, planID, userID string) (Plan, error)
	GetRequest(ctx context.Context, conversationID, userID, clientRequestID string) (Run, error)
	GetOwned(ctx context.Context, runID, userID string) (Run, error)
	Active(ctx context.Context, conversationID, userID string) (Run, error)
	SaveDraft(ctx context.Context, runID, userID string, message convo.Message) error
	Finalize(ctx context.Context, in FinalizeInput) (FinalizeResult, error)
	InterruptRunning(ctx context.Context, now time.Time) (int64, error)
	Close()
}

func IsTerminal(status string) bool {
	return status == StatusSuccess || status == StatusError ||
		status == StatusCancelled || status == StatusInterrupted
}

func normalizeInteractionMode(mode string) string {
	if mode == InteractionModePlan {
		return InteractionModePlan
	}
	return InteractionModeExecute
}
