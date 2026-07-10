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

type SystemSetting struct {
	Key       string          `json:"key"`
	ValueJSON json.RawMessage `json:"value_json"`
	Version   int64           `json:"version"`
	UpdatedAt time.Time       `json:"updated_at"`
	UpdatedBy string          `json:"updated_by"`
}

// Skill is a Skill-Market entry: a named, versioned capability employees can
// enable. The admin-api owns the catalog; the runtime consumes Enabled entries.
type Skill struct {
	ID              string          `json:"id"`   // stable kebab id, unique per scope/owner in v1
	Name            string          `json:"name"` // display name
	Description     string          `json:"description"`
	Version         string          `json:"version"`
	Entrypoint      string          `json:"entrypoint"` // module/path the runtime loads
	Enabled         bool            `json:"enabled"`
	Scope           string          `json:"scope"` // "admin" | "user"
	OwnerUserID     string          `json:"owner_user_id,omitempty"`
	SourceType      string          `json:"source_type,omitempty"` // "manual" | "archive" | "git"
	SourceURL       string          `json:"source_url,omitempty"`
	SourceRef       string          `json:"source_ref,omitempty"`
	SourcePath      string          `json:"source_path,omitempty"`
	BundleObjectKey string          `json:"bundle_object_key,omitempty"`
	ContentSHA256   string          `json:"content_sha256,omitempty"`
	ManifestJSON    json.RawMessage `json:"manifest_json,omitempty"`
	FrontmatterJSON json.RawMessage `json:"frontmatter_json,omitempty"`
	SkillMD         string          `json:"skill_md,omitempty"`
	FileCount       int             `json:"file_count"`
	SizeBytes       int64           `json:"size_bytes"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	CreatedBy       string          `json:"created_by,omitempty"`
	UpdatedBy       string          `json:"updated_by,omitempty"`
}

type UserSkillPreference struct {
	UserID    string    `json:"user_id"`
	SkillID   string    `json:"skill_id"`
	Enabled   bool      `json:"enabled"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MCPServer is an administrator-managed Model Context Protocol server
// definition. Sensitive env/header values are stored encrypted and never
// serialized; clients only see masked hint JSON.
type MCPServer struct {
	ID                   string          `json:"id"`
	Name                 string          `json:"name"`
	Description          string          `json:"description"`
	Transport            string          `json:"transport"` // "stdio" | "http" | "sse"
	Command              string          `json:"command,omitempty"`
	ArgsJSON             json.RawMessage `json:"args,omitempty"`
	URL                  string          `json:"-"`
	URLVarCiphertextJSON json.RawMessage `json:"-"`
	URLVarHintJSON       json.RawMessage `json:"-"`
	EnvCiphertextJSON    json.RawMessage `json:"-"`
	EnvHintJSON          json.RawMessage `json:"env_hints,omitempty"`
	HeaderCiphertextJSON json.RawMessage `json:"-"`
	HeaderHintJSON       json.RawMessage `json:"header_hints,omitempty"`
	Enabled              bool            `json:"enabled"`
	DefaultEnabled       bool            `json:"default_enabled"`
	Source               string          `json:"source"`
	Status               string          `json:"status"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
	CreatedBy            string          `json:"created_by,omitempty"`
	UpdatedBy            string          `json:"updated_by,omitempty"`
}

type UserMCPPreference struct {
	UserID    string    `json:"user_id"`
	MCPID     string    `json:"mcp_id"`
	Enabled   bool      `json:"enabled"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AgentPrompt is an administrator-managed system-prompt policy injected into
// new agent sessions. v1 exposes only the global prompt, while scope/priority
// leave room for future team/model/session-specific policy layers.
type AgentPrompt struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Content   string    `json:"content"`
	Enabled   bool      `json:"enabled"`
	Scope     string    `json:"scope"`
	Priority  int       `json:"priority"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by,omitempty"`
	UpdatedBy string    `json:"updated_by,omitempty"`
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
	Alias    string       `json:"alias"`
	Label    string       `json:"label"`
	Provider string       `json:"provider"`
	Family   string       `json:"family"`
	IconSlug string       `json:"icon_slug"`
	Icon     LLMModelIcon `json:"icon"`
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
	TenantID        string    `json:"tenant_id"` // ten: authoritative team/tenant for minted tokens
	Role            string    `json:"role"`      // "user" | "admin"
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

// AuditEntry is the legacy admin audit shape kept for /admin/audit
// compatibility. New code should prefer AuditEvent.
type AuditEntry struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	Actor    string    `json:"actor"`    // admin principal
	Action   string    `json:"action"`   // e.g. "token.issue", "skill.delete"
	Resource string    `json:"resource"` // affected id
	Detail   string    `json:"detail"`   // human-readable summary
}

// AuditEvent is one structured, append-only user or system audit event.
type AuditEvent struct {
	ID           int64          `json:"id"`
	At           time.Time      `json:"at"`
	ActorType    string         `json:"actor_type"`
	ActorUserID  string         `json:"actor_user_id,omitempty"`
	ActorEmail   string         `json:"actor_email,omitempty"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type,omitempty"`
	ResourceID   string         `json:"resource_id,omitempty"`
	Result       string         `json:"result"`
	HTTPMethod   string         `json:"http_method,omitempty"`
	Route        string         `json:"route,omitempty"`
	StatusCode   int            `json:"status_code,omitempty"`
	RequestID    string         `json:"request_id,omitempty"`
	TraceID      string         `json:"trace_id,omitempty"`
	ClientIP     string         `json:"client_ip,omitempty"`
	UserAgent    string         `json:"user_agent,omitempty"`
	Metadata     map[string]any `json:"metadata_json,omitempty"`
	ErrorCode    string         `json:"error_code,omitempty"`
}

// AuditEventQuery filters audit-event list calls. Empty fields are ignored.
type AuditEventQuery struct {
	Limit        int
	Offset       int
	LegacyOnly   bool
	ActorUserID  string
	ActorEmail   string
	Action       string
	ResourceType string
	ResourceID   string
	Result       string
	RequestID    string
	TraceID      string
	Since        time.Time
	Until        time.Time
}

// TraceEvent is one in-product timing span used by the admin trace UI. It is
// intentionally storage-backed so diagnostics work even when external OTel
// collection is disabled.
type TraceEvent struct {
	ID         int64          `json:"id"`
	TraceID    string         `json:"trace_id"`
	Service    string         `json:"service"`
	Name       string         `json:"name"`
	Category   string         `json:"category,omitempty"`
	StartedAt  time.Time      `json:"started_at"`
	DurationMS int64          `json:"duration_ms"`
	Status     string         `json:"status"`
	Metadata   map[string]any `json:"metadata_json,omitempty"`
}

type TraceEventQuery struct {
	TraceID string
	Limit   int
}

// TokenUsageQuery filters usage_ledger aggregate reads. Bucket is "hour" or
// "day"; empty lets the service choose based on the range.
type TokenUsageQuery struct {
	From   time.Time
	To     time.Time
	Bucket string
	Limit  int
	Offset int
	UserID string
}

// TokenUsageSummary is a rolled-up view over usage_ledger rows.
type TokenUsageSummary struct {
	Calls            int64   `json:"calls"`
	UserCount        int64   `json:"user_count"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

// TokenUsagePoint is one time bucket in a token usage trend.
type TokenUsagePoint struct {
	BucketStart      time.Time `json:"bucket_start"`
	Calls            int64     `json:"calls"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	CostUSD          float64   `json:"cost_usd"`
}

// TokenUsageUser is one ranked row in the admin token usage dashboard.
type TokenUsageUser struct {
	UserID           string    `json:"user_id"`
	Username         string    `json:"username,omitempty"`
	Email            string    `json:"email,omitempty"`
	Name             string    `json:"name,omitempty"`
	Role             string    `json:"role,omitempty"`
	Enabled          bool      `json:"enabled"`
	KnownUser        bool      `json:"known_user"`
	Calls            int64     `json:"calls"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	CostUSD          float64   `json:"cost_usd"`
	LastUsedAt       time.Time `json:"last_used_at,omitempty"`
}

// TokenUsageReport is the full aggregate read used by the dashboard and export.
type TokenUsageReport struct {
	From    time.Time         `json:"from"`
	To      time.Time         `json:"to"`
	Bucket  string            `json:"bucket"`
	Summary TokenUsageSummary `json:"summary"`
	Trend   []TokenUsagePoint `json:"trend"`
	Users   []TokenUsageUser  `json:"users,omitempty"`
	Limit   int               `json:"limit,omitempty"`
	Offset  int               `json:"offset,omitempty"`
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

	// System settings
	GetSystemSetting(ctx context.Context, key string) (SystemSetting, error)
	ListSystemSettings(ctx context.Context) ([]SystemSetting, error)
	SetSystemSetting(ctx context.Context, setting SystemSetting, expectedVersion int64) (SystemSetting, error)
	DeleteSystemSetting(ctx context.Context, key string, expectedVersion int64) error

	// Skills
	CreateSkill(ctx context.Context, s Skill) error
	GetSkill(ctx context.Context, id string) (Skill, error)
	ListSkills(ctx context.Context, onlyEnabled bool) ([]Skill, error)
	ListSkillsForUser(ctx context.Context, userID string) ([]Skill, error)
	UpdateSkill(ctx context.Context, s Skill) error
	DeleteSkill(ctx context.Context, id string) error
	SetUserSkillPreference(ctx context.Context, pref UserSkillPreference) error
	ListUserSkillPreferences(ctx context.Context, userID string) ([]UserSkillPreference, error)
	DeleteUserSkillPreference(ctx context.Context, userID, skillID string) error

	// MCP servers
	CreateMCPServer(ctx context.Context, s MCPServer) error
	GetMCPServer(ctx context.Context, id string) (MCPServer, error)
	ListMCPServers(ctx context.Context, onlyEnabled bool) ([]MCPServer, error)
	UpdateMCPServer(ctx context.Context, s MCPServer) error
	DeleteMCPServer(ctx context.Context, id string) error
	SetUserMCPPreference(ctx context.Context, pref UserMCPPreference) error
	ListUserMCPPreferences(ctx context.Context, userID string) ([]UserMCPPreference, error)
	DeleteUserMCPPreference(ctx context.Context, userID, mcpID string) error

	// Agent prompts
	CreateAgentPrompt(ctx context.Context, p AgentPrompt) error
	GetAgentPrompt(ctx context.Context, id string) (AgentPrompt, error)
	ListAgentPrompts(ctx context.Context, onlyEnabled bool) ([]AgentPrompt, error)
	UpdateAgentPrompt(ctx context.Context, p AgentPrompt) error

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
	HeartbeatScheduledTaskRun(ctx context.Context, id, workerID string, now time.Time) (bool, error)
	ExpireStaleScheduledTaskRuns(ctx context.Context, before, now time.Time, errText string, limit int) ([]ScheduledTaskRun, error)
	UpdateScheduledTaskRun(ctx context.Context, run ScheduledTaskRun, taskNextRunAt time.Time, terminal bool) error
	AppendScheduledTaskRunEvent(ctx context.Context, event ScheduledTaskRunEvent) error
	ListScheduledTaskRunEvents(ctx context.Context, runID string) ([]ScheduledTaskRunEvent, error)

	// Audit
	AppendAudit(ctx context.Context, e AuditEntry) error
	ListAudit(ctx context.Context, limit int) ([]AuditEntry, error)
	AppendAuditEvent(ctx context.Context, e AuditEvent) error
	ListAuditEvents(ctx context.Context, q AuditEventQuery) ([]AuditEvent, error)
	ListTraceEvents(ctx context.Context, q TraceEventQuery) ([]TraceEvent, error)

	// Token usage dashboard
	TokenUsageSummary(ctx context.Context, q TokenUsageQuery) (TokenUsageSummary, error)
	TokenUsageTrend(ctx context.Context, q TokenUsageQuery) ([]TokenUsagePoint, error)
	TokenUsageUsers(ctx context.Context, q TokenUsageQuery) ([]TokenUsageUser, error)
}
