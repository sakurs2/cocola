// Package feishu owns the user-scoped Feishu connector lifecycle.
package feishu

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound           = errors.New("feishu connector: not found")
	ErrConflict           = errors.New("feishu connector: conflict")
	ErrInvalid            = errors.New("feishu connector: invalid argument")
	ErrQueueFull          = errors.New("feishu connector: inbox queue full")
	ErrAttachmentTooLarge = errors.New("feishu connector: attachment too large")
	ErrUnsupportedMedia   = errors.New("feishu connector: unsupported media")
	ErrLeaseLost          = errors.New("feishu connector: lease lost")
	ErrFlowTerminated     = errors.New("feishu connector: registration flow terminated")
)

const (
	ProviderFeishu = "feishu"

	DomainFeishu = "feishu"
	DomainLark   = "lark"

	StatusAwaitingBind   = "awaiting_bind"
	StatusConnecting     = "connecting"
	StatusReady          = "ready"
	StatusActionRequired = "action_required"
	StatusDisabled       = "disabled"
	StatusError          = "error"

	FlowStarting     = "starting"
	FlowAwaitingUser = "awaiting_user"
	FlowAuthorizing  = "authorizing"
	FlowReady        = "ready"
	FlowDenied       = "denied"
	FlowExpired      = "expired"
	FlowFailed       = "failed"
	FlowInterrupted  = "interrupted"
	FlowCancelled    = "cancelled"

	InboxPending    = "pending"
	InboxProcessing = "processing"
	InboxRetry      = "retry"
	InboxDone       = "done"
	InboxRejected   = "rejected"
	InboxFailed     = "failed"
)

type Identity struct {
	TenantID string
	UserID   string
	Email    string
	Name     string
	Username string
}

type Connector struct {
	ID                  string
	TenantID            string
	UserID              string
	Provider            string
	Domain              string
	AppID               string
	AppSecretCiphertext string
	OwnerOpenID         string
	BotOpenID           string
	BotName             string
	ModelRouteID        string
	ModelAlias          string
	DesiredEnabled      bool
	Status              string
	BindCodeHash        string
	BindExpiresAt       *time.Time
	LastConnectedAt     *time.Time
	LastErrorCode       string
	LeaseOwner          string
	LeaseExpiresAt      *time.Time
	Version             int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type ConnectorView struct {
	Connected       bool              `json:"connected"`
	Enabled         bool              `json:"enabled"`
	Status          string            `json:"status"`
	Domain          string            `json:"domain,omitempty"`
	BotName         string            `json:"bot_name,omitempty"`
	ModelRouteID    string            `json:"model_route_id,omitempty"`
	ModelAlias      string            `json:"model_alias,omitempty"`
	LastConnectedAt *time.Time        `json:"last_connected_at,omitempty"`
	LastErrorCode   string            `json:"last_error_code,omitempty"`
	Registration    *RegistrationFlow `json:"registration,omitempty"`
}

type RegistrationFlow struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"-"`
	UserID          string    `json:"-"`
	Provider        string    `json:"provider"`
	Status          string    `json:"status"`
	VerificationURL string    `json:"verification_url,omitempty"`
	ExpiresAt       time.Time `json:"expires_at"`
	ErrorCode       string    `json:"error_code,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ManualResult struct {
	Connection ConnectorView `json:"connection"`
	BindCode   string        `json:"bind_code"`
	ExpiresAt  time.Time     `json:"expires_at"`
}

type RegistrationInput struct {
	AppName   string
	AppDesc   string
	AvatarURL string
}

type RegistrationResult struct {
	AppID       string
	AppSecret   string
	OwnerOpenID string
	TenantBrand string
}

type RegistrationUpdate struct {
	VerificationURL string
	ExpiresIn       time.Duration
	Status          string
}

type RegistrationError struct {
	Code string
	Err  error
}

func (e *RegistrationError) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Err.Error()
}

func (e *RegistrationError) Unwrap() error { return e.Err }

// Registrar is the narrow seam around the official application-registration SDK.
type Registrar interface {
	Register(
		ctx context.Context,
		input RegistrationInput,
		onUpdate func(RegistrationUpdate),
	) (RegistrationResult, error)
}

type Resource struct {
	Type     string `json:"type"`
	FileKey  string `json:"file_key"`
	Filename string `json:"filename,omitempty"`
}

type InboxPayload struct {
	EventID      string     `json:"event_id"`
	MessageID    string     `json:"message_id"`
	ChatID       string     `json:"chat_id"`
	ChatType     string     `json:"chat_type"`
	SenderOpenID string     `json:"sender_open_id"`
	Text         string     `json:"text"`
	ContentType  string     `json:"content_type"`
	Resources    []Resource `json:"resources,omitempty"`
	CreateTimeMS int64      `json:"create_time_ms"`
}

type InboxItem struct {
	ID                string
	ConnectorID       string
	EventID           string
	ExternalMessageID string
	ExternalChatID    string
	Payload           InboxPayload
	Priority          int
	Status            string
	Attempts          int
	NextAttemptAt     time.Time
	LeaseOwner        string
	LeaseExpiresAt    *time.Time
	ErrorCode         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Session struct {
	ConnectorID            string
	ExternalChatID         string
	ConversationID         string
	PendingQuestionID      string
	PendingQuestionVersion int
	PendingOptions         []QuestionOption
	UpdatedAt              time.Time
}

type QuestionOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Store interface {
	Close()

	GetConnector(context.Context, Identity) (Connector, error)
	GetConnectorByID(context.Context, string) (Connector, error)
	UpsertConnector(context.Context, Connector) (Connector, error)
	DeleteConnector(context.Context, Identity) error
	SetConnectorEnabled(context.Context, Identity, bool, time.Time) (Connector, error)
	SetConnectorModel(context.Context, Identity, string, string, time.Time) (Connector, error)
	UpdateConnectorState(
		context.Context,
		string,
		string,
		string,
		string,
		string,
		string,
		*time.Time,
		time.Time,
	) error
	BindConnectorOwner(context.Context, string, string, string, time.Time) (Connector, error)
	ClaimConnectors(context.Context, string, time.Time, time.Time, int) ([]Connector, error)
	OwnedConnectors(context.Context, string) ([]Connector, error)
	RenewConnectorLease(context.Context, string, string, time.Time, time.Time) error
	ReleaseConnectorLease(context.Context, string, string, time.Time) error

	CreateRegistrationFlow(context.Context, RegistrationFlow) error
	GetRegistrationFlow(context.Context, Identity, string) (RegistrationFlow, error)
	GetActiveRegistrationFlow(context.Context, Identity) (RegistrationFlow, error)
	UpdateRegistrationFlow(context.Context, string, string, string, string, time.Time, time.Time) error
	CancelRegistrationFlow(context.Context, Identity, string, time.Time) error
	CompleteRegistration(context.Context, Identity, string, Connector, time.Time) error
	InterruptRegistrationFlows(context.Context, time.Time, time.Time) error

	GetSession(context.Context, string, string) (Session, error)
	UpsertSession(context.Context, Session) error
	DeleteSessions(context.Context, string) error

	EnqueueInbox(context.Context, InboxItem, int) (bool, error)
	ClaimNextInbox(context.Context, string, string, time.Time, time.Time) (InboxItem, error)
	NextInboxAttempt(context.Context, string) (time.Time, error)
	FinishInbox(context.Context, string, string, string, time.Time) error
	RetryInbox(context.Context, string, string, int, time.Time, string, time.Time) error
	CleanupInbox(context.Context, time.Time) error
}
