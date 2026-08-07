// Package chatrun owns the minimal durable state for one interactive Agent run.
// Execution and subscriptions stay in one Gateway process; PostgreSQL only
// stores idempotency, terminal state and the latest assistant draft.
package chatrun

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
)

var (
	ErrNotFound                 = errors.New("chatrun: not found")
	ErrConflict                 = errors.New("chatrun: conversation already has an active run")
	ErrRuntimeMismatch          = errors.New("chatrun: conversation runtime mismatch")
	ErrAgentMismatch            = errors.New("chatrun: conversation agent mismatch")
	ErrAgentArchived            = errors.New("chatrun: agent archived")
	ErrFolderNotFound           = errors.New("chatrun: folder not found")
	ErrFolderMismatch           = errors.New("chatrun: conversation folder mismatch")
	ErrProjectNotFound          = errors.New("chatrun: project not found")
	ErrProjectNotReady          = errors.New("chatrun: project not ready")
	ErrProjectMismatch          = errors.New("chatrun: conversation project mismatch")
	ErrProjectSingleTask        = errors.New("chatrun: local project already has a task")
	ErrPlanNotCurrent           = errors.New("chatrun: plan is not current")
	ErrPlanState                = errors.New("chatrun: plan state does not allow this operation")
	ErrPlanModelUnavailable     = errors.New("chatrun: plan model is unavailable")
	ErrQuestionPending          = errors.New("chatrun: conversation has a pending question")
	ErrQuestionNotCurrent       = errors.New("chatrun: question is not current")
	ErrQuestionState            = errors.New("chatrun: question state does not allow this operation")
	ErrQuestionModelUnavailable = errors.New("chatrun: question model is unavailable")
)

const (
	StatusRunning      = "running"
	StatusSuccess      = "success"
	StatusError        = "error"
	StatusCancelled    = "cancelled"
	StatusInterrupted  = "interrupted"
	StatusWaitingInput = "waiting_input"

	InteractionModeExecute = "execute"
	InteractionModePlan    = "plan"

	PlanStatusReady      = "ready"
	PlanStatusExecuting  = "executing"
	PlanStatusCompleted  = "completed"
	PlanStatusStopped    = "stopped"
	PlanStatusFailed     = "failed"
	PlanStatusSuperseded = "superseded"
	PlanStatusCancelled  = "cancelled"

	QuestionStatusPending   = "pending"
	QuestionStatusAnswering = "answering"
	QuestionStatusAnswered  = "answered"
	QuestionStatusCancelled = "cancelled"
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
	ReasoningEffort   string     `json:"reasoning_effort,omitempty"`
	ClientRequestID   string     `json:"client_request_id,omitempty"`
	InteractionMode   string     `json:"interaction_mode"`
	PlanID            string     `json:"plan_id,omitempty"`
	Status            string     `json:"status"`
	StartedAt         time.Time  `json:"started_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	LastActivityAt    time.Time  `json:"last_activity_at"`
	ErrorCode         string     `json:"error_code,omitempty"`
	DurationMS        int64      `json:"duration_ms,omitempty"`
	ToolCallCount     int64      `json:"tool_call_count,omitempty"`
	LLMCallCount      int64      `json:"llm_call_count,omitempty"`
}

type StartInput struct {
	Run                         Run
	Conversation                convo.Conversation
	UserMessage                 convo.Message
	ProjectBaseRef              string
	ProjectBaseSHA              string
	RevisionPlanID              string
	ExpectedRevisionPlanVersion int
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
	RuntimeAccepted   bool
	AssistantMessage  *convo.Message
	Reveal            bool
	ConversationTitle string
	CompletedAt       time.Time
	ToolCallCount     int64
	LLMCallCount      int64
	PlanCandidate     *PlanCandidate
	QuestionCandidate *QuestionCandidate
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
	ReasoningEffort   string     `json:"reasoning_effort,omitempty"`
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
	ReasoningEffort   string
	ContentMarkdown   string
	WorkspaceRevision string
}

type Question struct {
	ID              string                 `json:"id"`
	ConversationID  string                 `json:"conversation_id"`
	Version         int                    `json:"version"`
	Status          string                 `json:"status"`
	SourceRunID     string                 `json:"source_run_id"`
	AnswerRunID     string                 `json:"answer_run_id,omitempty"`
	InteractionMode string                 `json:"interaction_mode"`
	RuntimeID       string                 `json:"runtime_id"`
	ModelRouteID    string                 `json:"model_route_id"`
	ModelAlias      string                 `json:"model_alias"`
	ReasoningEffort string                 `json:"reasoning_effort,omitempty"`
	SkillID         string                 `json:"skill_id,omitempty"`
	Text            string                 `json:"question"`
	Options         []convo.QuestionOption `json:"options"`
	Answer          *convo.QuestionAnswer  `json:"answer,omitempty"`
	AnsweredBy      string                 `json:"answered_by,omitempty"`
	AnsweredAt      *time.Time             `json:"answered_at,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

type QuestionCandidate struct {
	ID              string
	RuntimeID       string
	ModelRouteID    string
	ModelAlias      string
	ReasoningEffort string
	SkillID         string
	InteractionMode string
	Text            string
	Options         []convo.QuestionOption
}

func validQuestionCandidate(candidate *QuestionCandidate) bool {
	if candidate == nil ||
		strings.TrimSpace(candidate.ID) == "" ||
		strings.TrimSpace(candidate.RuntimeID) == "" ||
		strings.TrimSpace(candidate.Text) == "" ||
		len(candidate.Text) > 16<<10 ||
		len(candidate.Options) > 8 {
		return false
	}
	optionIDs := make(map[string]struct{}, len(candidate.Options))
	optionLabels := make(map[string]struct{}, len(candidate.Options))
	for _, option := range candidate.Options {
		id := strings.TrimSpace(option.ID)
		label := strings.TrimSpace(option.Label)
		if id == "" || label == "" || len(label) > 1<<10 {
			return false
		}
		if _, exists := optionIDs[id]; exists {
			return false
		}
		if _, exists := optionLabels[label]; exists {
			return false
		}
		optionIDs[id] = struct{}{}
		optionLabels[label] = struct{}{}
	}
	return true
}

type FinalizeResult struct {
	Run              Run
	Plan             *Plan
	Question         *Question
	AnsweredQuestion *Question
	RestoredQuestion *Question
	SupersededPlanID string
}

type QuestionAnswerInput struct {
	Run             Run
	ConversationID  string
	UserID          string
	QuestionID      string
	ExpectedVersion int
	Answer          convo.QuestionAnswer
	UserMessage     convo.Message
	AnsweredAt      time.Time
}

type QuestionAnswerResult struct {
	Run          Run
	Conversation convo.Conversation
	Question     Question
	Created      bool
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
	StartQuestionAnswer(ctx context.Context, in QuestionAnswerInput) (QuestionAnswerResult, error)
	AcceptQuestionAnswer(ctx context.Context, runID, questionID, userID string, now time.Time) (Question, error)
	CancelQuestion(ctx context.Context, conversationID, questionID, userID string, expectedVersion int, now time.Time) (Question, error)
	GetQuestion(ctx context.Context, conversationID, questionID, userID string) (Question, error)
	ListQuestions(ctx context.Context, conversationID, userID string) ([]Question, error)
	ListAwaitingUserActionConversationIDs(ctx context.Context, userID string) ([]string, error)
	ListRuns(ctx context.Context, conversationID, userID string) ([]Run, error)
	StartPlanExecution(ctx context.Context, in PlanExecutionInput) (PlanExecutionResult, error)
	CancelPlan(ctx context.Context, conversationID, planID, userID string, expectedVersion int, now time.Time) (Plan, error)
	GetPlan(ctx context.Context, conversationID, planID, userID string) (Plan, error)
	ListPlans(ctx context.Context, conversationID, userID string) ([]Plan, error)
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
		status == StatusCancelled || status == StatusInterrupted ||
		status == StatusWaitingInput
}

func normalizeInteractionMode(mode string) string {
	if mode == InteractionModePlan {
		return InteractionModePlan
	}
	return InteractionModeExecute
}

func questionPart(question Question) convo.Part {
	return convo.Part{
		Type: convo.PartQuestion, QuestionID: question.ID, Version: question.Version,
		Status: question.Status, Question: question.Text,
		QuestionOptions: append([]convo.QuestionOption(nil), question.Options...),
		QuestionAnswer:  question.Answer,
	}
}
