// Package store defines the admin-api persistence seam and an in-memory
// implementation. Every admin resource (issued-token metadata + revocations,
// per-subject quota overrides, skill-market entries, audit log) is reached
// through a small Repository interface. The in-memory Store backs unit tests
// and zero-dependency dev boots; a PostgreSQL implementation lands in M7
// (persistence tiering) behind the same interfaces — no service/handler change.
//
// Mirrors the project rule established by go-common/redis.KV: funnel all access
// through an interface so the backend is an additive swap.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrNotFound is returned when a lookup misses. Services map it to a 404.
var ErrNotFound = errors.New("store: not found")

// ErrConflict is returned on a uniqueness violation (e.g. duplicate skill id).
var ErrConflict = errors.New("store: conflict")

// TokenRecord is the metadata cocola keeps about a token it minted. The token
// string itself is NOT stored (it is a bearer credential handed to the
// employee); we keep only what is needed to list and revoke.
type TokenRecord struct {
	ID        string    `json:"id"`         // jti-like opaque id (also the revocation key)
	UserID    string    `json:"user_id"`    // sub
	TenantID  string    `json:"tenant_id"`  // ten
	Issuer    string    `json:"issuer"`     // iss
	IssuedAt  time.Time `json:"issued_at"`  // from iat
	ExpiresAt time.Time `json:"expires_at"` // zero = non-expiring
	Revoked   bool      `json:"revoked"`
	RevokedAt time.Time `json:"revoked_at,omitempty"`
	CreatedBy string    `json:"created_by"` // admin who minted it (audit trail)
}

// QuotaOverride is a per-subject cap that supersedes the gateway's static env
// defaults. scope is "user" or "tenant"; subject is the user_id/tenant_id.
// A limit of 0 means "explicitly unlimited" for that subject.
type QuotaOverride struct {
	Scope     string    `json:"scope"`
	Subject   string    `json:"subject"`
	Limit     int64     `json:"limit"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
}

// Skill is a Skill-Market entry: a named, versioned capability employees can
// enable. The admin-api owns the catalog; the runtime consumes Enabled entries.
type Skill struct {
	ID          string    `json:"id"`   // stable kebab id, unique
	Name        string    `json:"name"` // display name
	Description string    `json:"description"`
	Version     string    `json:"version"`
	Entrypoint  string    `json:"entrypoint"` // module/path the runtime loads
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// LLMProvider is one upstream model vendor/endpoint. APIKeyCiphertext is never
// serialized to clients; APIKeyHint is the masked display value shown in admin.
type LLMProvider struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Type             string    `json:"type"` // "anthropic" | "openai_compat" | "fake"
	BaseURL          string    `json:"base_url"`
	APIKeyCiphertext string    `json:"-"`
	APIKeyHint       string    `json:"api_key_hint"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// LLMModelRoute maps a user-facing alias to a provider's real model id. The
// public chat UI sees only alias/label/icon, while the gateway consumes the full
// route for provider/model resolution and billing.
type LLMModelRoute struct {
	Alias      string    `json:"alias"`
	ProviderID string    `json:"provider_id"`
	RealModel  string    `json:"real_model"`
	Runtime    string    `json:"runtime"`
	Label      string    `json:"label"`
	IconType   string    `json:"icon_type"`
	IconSlug   string    `json:"icon_slug"`
	IconURL    string    `json:"icon_url"`
	Enabled    bool      `json:"enabled"`
	Visible    bool      `json:"visible"`
	IsDefault  bool      `json:"is_default"`
	SortOrder  int       `json:"sort_order"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type LLMModelIcon struct {
	Type string `json:"type"`
	Slug string `json:"slug,omitempty"`
	Src  string `json:"src,omitempty"`
}

type PublicLLMModel struct {
	Alias string       `json:"alias"`
	Label string       `json:"label"`
	Icon  LLMModelIcon `json:"icon"`
}

// ScheduledTask is an admin-created system task. ScheduleSpec and ConfigJSON
// intentionally hold extensible JSON so new task options can land without
// widening the core table for every experiment.
type ScheduledTask struct {
	ID             string          `json:"id"`
	OwnerType      string          `json:"owner_type"`
	OwnerUserID    string          `json:"owner_user_id,omitempty"`
	ConversationID string          `json:"conversation_id,omitempty"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Status         string          `json:"status"`
	ScheduleKind   string          `json:"schedule_kind"`
	ScheduleSpec   json.RawMessage `json:"schedule_spec"`
	Timezone       string          `json:"timezone"`
	Prompt         string          `json:"prompt"`
	ModelAlias     string          `json:"model_alias"`
	MaxTurns       int             `json:"max_turns"`
	ConfigJSON     json.RawMessage `json:"config_json"`
	NextRunAt      time.Time       `json:"next_run_at,omitempty"`
	LastRunAt      time.Time       `json:"last_run_at,omitempty"`
	RunCount       int64           `json:"run_count"`
	LastStatus     string          `json:"last_status"`
	LastError      string          `json:"last_error"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	CreatedBy      string          `json:"created_by"`
	UpdatedBy      string          `json:"updated_by"`
}

type ScheduledTaskAttachment struct {
	ID         string    `json:"id"`
	TaskID     string    `json:"task_id"`
	Filename   string    `json:"filename"`
	Mime       string    `json:"mime"`
	SizeBytes  int64     `json:"size_bytes"`
	ObjectKey  string    `json:"object_key"`
	ContentB64 string    `json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	CreatedBy  string    `json:"created_by"`
}

type ScheduledTaskRun struct {
	ID           string    `json:"id"`
	TaskID       string    `json:"task_id"`
	ScheduledFor time.Time `json:"scheduled_for,omitempty"`
	Status       string    `json:"status"`
	WorkerID     string    `json:"worker_id"`
	SessionID    string    `json:"session_id"`
	ModelAlias   string    `json:"model_alias"`
	OutputText   string    `json:"output_text"`
	Error        string    `json:"error"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ScheduledTaskRunEvent struct {
	ID        int64           `json:"id"`
	RunID     string          `json:"run_id"`
	Seq       int             `json:"seq"`
	Kind      string          `json:"kind"`
	DataJSON  json.RawMessage `json:"data_json"`
	CreatedAt time.Time       `json:"created_at"`
}

// AuthUser is one whitelisted login principal for the web app. Username and
// email are normalized to lower-case before persistence; login lookup is backed
// by AuthUserIdentifier so future identifiers such as phone numbers can share
// one uniqueness/indexing model.
// PasswordHash stores a bcrypt hash; plaintext passwords are never persisted.
type AuthUser struct {
	ID              string    `json:"id"`
	Username        string    `json:"username"`
	Email           string    `json:"email"`
	Name            string    `json:"name"`
	Role            string    `json:"role"` // "user" | "admin"
	Enabled         bool      `json:"enabled"`
	PasswordHash    string    `json:"-"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	LastLoginAt     time.Time `json:"last_login_at,omitempty"`
	CreatedBy       string    `json:"created_by"`
	UpdatedBy       string    `json:"updated_by"`
	PasswordUpdated time.Time `json:"password_updated_at,omitempty"`
	DeletedAt       time.Time `json:"-"`
	DeletedBy       string    `json:"-"`
}

// AuthUserIdentifier is one login identifier value mapped to a user. The value
// is globally unique across kinds, so "username", "email", and future "phone"
// identifiers cannot collide.
type AuthUserIdentifier struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	Kind         string    `json:"kind"`
	Value        string    `json:"value_normalized"`
	DisplayValue string    `json:"display_value"`
	Verified     bool      `json:"verified"`
	Primary      bool      `json:"primary"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func authUserIdentifiersFor(u AuthUser) []AuthUserIdentifier {
	seen := map[string]bool{}
	out := make([]AuthUserIdentifier, 0, 2)
	add := func(kind, value string, primary bool) {
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, AuthUserIdentifier{
			ID:           u.ID + ":" + kind + ":" + value,
			UserID:       u.ID,
			Kind:         kind,
			Value:        value,
			DisplayValue: value,
			Verified:     true,
			Primary:      primary,
			CreatedAt:    u.CreatedAt,
			UpdatedAt:    u.UpdatedAt,
		})
	}
	add("username", u.Username, false)
	add("email", u.Email, true)
	return out
}

// AuditEntry is one record of an admin write. Reads are not audited.
type AuditEntry struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	Actor    string    `json:"actor"`    // admin principal
	Action   string    `json:"action"`   // e.g. "token.issue", "skill.delete"
	Resource string    `json:"resource"` // affected id
	Detail   string    `json:"detail"`   // human-readable summary
}

// Store is the full persistence contract the service depends on.
type Store interface {
	// Auth users / whitelist
	CreateAuthUser(ctx context.Context, u AuthUser) error
	GetAuthUser(ctx context.Context, id string) (AuthUser, error)
	GetAuthUserByEmail(ctx context.Context, email string) (AuthUser, error)
	GetAuthUserByIdentifier(ctx context.Context, identifier string) (AuthUser, error)
	ListAuthUsers(ctx context.Context) ([]AuthUser, error)
	UpdateAuthUser(ctx context.Context, u AuthUser) error
	DeleteAuthUser(ctx context.Context, id, actor string, at time.Time) error
	TouchAuthUserLogin(ctx context.Context, id string, at time.Time) error

	// Tokens
	CreateToken(ctx context.Context, r TokenRecord) error
	GetToken(ctx context.Context, id string) (TokenRecord, error)
	ListTokens(ctx context.Context, userID string) ([]TokenRecord, error)
	RevokeToken(ctx context.Context, id string, at time.Time) error
	IsRevoked(ctx context.Context, id string) (bool, error)

	// Quota overrides
	SetQuota(ctx context.Context, q QuotaOverride) error
	GetQuota(ctx context.Context, scope, subject string) (QuotaOverride, error)
	ListQuotas(ctx context.Context) ([]QuotaOverride, error)
	DeleteQuota(ctx context.Context, scope, subject string) error

	// Skills
	CreateSkill(ctx context.Context, s Skill) error
	GetSkill(ctx context.Context, id string) (Skill, error)
	ListSkills(ctx context.Context, onlyEnabled bool) ([]Skill, error)
	UpdateSkill(ctx context.Context, s Skill) error
	DeleteSkill(ctx context.Context, id string) error

	// LLM model configuration
	CreateLLMProvider(ctx context.Context, p LLMProvider) error
	GetLLMProvider(ctx context.Context, id string) (LLMProvider, error)
	ListLLMProviders(ctx context.Context) ([]LLMProvider, error)
	UpdateLLMProvider(ctx context.Context, p LLMProvider) error
	DeleteLLMProvider(ctx context.Context, id string) error
	CreateLLMModelRoute(ctx context.Context, m LLMModelRoute) error
	GetLLMModelRoute(ctx context.Context, alias string) (LLMModelRoute, error)
	ListLLMModelRoutes(ctx context.Context) ([]LLMModelRoute, error)
	UpdateLLMModelRoute(ctx context.Context, m LLMModelRoute) error
	DeleteLLMModelRoute(ctx context.Context, alias string) error

	// Scheduled system tasks
	CreateScheduledTask(ctx context.Context, task ScheduledTask, attachments []ScheduledTaskAttachment) error
	GetScheduledTask(ctx context.Context, id string) (ScheduledTask, error)
	GetScheduledTaskForOwner(ctx context.Context, id, ownerUserID string) (ScheduledTask, error)
	ListScheduledTasks(ctx context.Context) ([]ScheduledTask, error)
	ListScheduledTasksForOwner(ctx context.Context, ownerUserID string) ([]ScheduledTask, error)
	UpdateScheduledTask(ctx context.Context, task ScheduledTask, replaceAttachments bool, attachments []ScheduledTaskAttachment) error
	DeleteScheduledTask(ctx context.Context, id string) error
	DeleteScheduledTaskForOwner(ctx context.Context, id, ownerUserID string) error
	ListScheduledTaskAttachments(ctx context.Context, taskID string) ([]ScheduledTaskAttachment, error)
	ListDueScheduledTasks(ctx context.Context, now time.Time, limit int) ([]ScheduledTask, error)
	TryStartScheduledTaskRun(ctx context.Context, taskID string, run ScheduledTaskRun, nextRunAt time.Time) (ScheduledTask, bool, error)
	GetScheduledTaskRun(ctx context.Context, id string) (ScheduledTaskRun, error)
	ListScheduledTaskRuns(ctx context.Context, taskID, status string, limit int) ([]ScheduledTaskRun, error)
	UpdateScheduledTaskRun(ctx context.Context, run ScheduledTaskRun, taskNextRunAt time.Time, terminal bool) error
	AppendScheduledTaskRunEvent(ctx context.Context, event ScheduledTaskRunEvent) error
	ListScheduledTaskRunEvents(ctx context.Context, runID string) ([]ScheduledTaskRunEvent, error)

	// Audit
	AppendAudit(ctx context.Context, e AuditEntry) error
	ListAudit(ctx context.Context, limit int) ([]AuditEntry, error)
}
