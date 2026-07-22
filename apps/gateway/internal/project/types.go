// Package project owns Cocola projects, GitHub connections and the Git state
// associated with project conversations. No package outside this module talks
// to GitHub directly.
package project

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrDisabled               = errors.New("project: github integration disabled")
	ErrLocalProjectsDisabled  = errors.New("project: local projects disabled")
	ErrNotFound               = errors.New("project: not found")
	ErrConflict               = errors.New("project: conflict")
	ErrVersionConflict        = errors.New("project: version conflict")
	ErrInvalidArgument        = errors.New("project: invalid argument")
	ErrConnectionRequired     = errors.New("project: github connection required")
	ErrInstallationRequired   = errors.New("project: github installation required")
	ErrRepositoryNotInstalled = errors.New("project: repository is not available to the github installation")
	ErrRepositoryTooLarge     = errors.New("project: repository too large")
	ErrProjectNotReady        = errors.New("project: project not ready")
	ErrApprovalRequired       = errors.New("project: command approval required")
	ErrApprovalDenied         = errors.New("project: command approval denied")
	ErrRunInactive            = errors.New("project: run is no longer active")
)

const (
	ProviderGitHub = "github"
	ProviderLocal  = "local"

	ConnectionInstallationRequired = "installation_required"
	ConnectionReady                = "ready"
	ConnectionReauthorization      = "reauthorization_required"

	ProjectProvisioning = "provisioning"
	ProjectReady        = "ready"
	ProjectFailed       = "failed"
	ProjectArchived     = "archived"
)

const (
	RegistrationAppCreated        = "app_created"
	RegistrationInstallRequired   = "installation_required"
	RegistrationAuthorizeRequired = "authorization_required"
	RegistrationReady             = "ready"
	RegistrationReauthorization   = "reauthorization_required"
	RegistrationError             = "error"
)

type Identity struct {
	TenantID string
	UserID   string
	Email    string
	Name     string
	Username string
}

type Connection struct {
	ID                     string     `json:"-"`
	TenantID               string     `json:"-"`
	UserID                 string     `json:"-"`
	Provider               string     `json:"provider"`
	ExternalUserID         int64      `json:"external_user_id"`
	ExternalLogin          string     `json:"external_login"`
	InstallationID         int64      `json:"installation_id,omitempty"`
	AccessTokenCiphertext  string     `json:"-"`
	AccessTokenExpiresAt   *time.Time `json:"-"`
	RefreshTokenCiphertext string     `json:"-"`
	RefreshTokenExpiresAt  *time.Time `json:"-"`
	Status                 string     `json:"status"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	RegistrationID         string     `json:"-"`
	InstallationURL        string     `json:"installation_url,omitempty"`
	ReauthorizationURL     string     `json:"reauthorization_url,omitempty"`
}

// AppRegistration is the encrypted, per-user GitHub App created through the
// manifest flow. Secret fields never leave the project module.
type AppRegistration struct {
	ID                     string    `json:"-"`
	TenantID               string    `json:"-"`
	UserID                 string    `json:"-"`
	Provider               string    `json:"provider"`
	AppID                  int64     `json:"app_id"`
	AppSlug                string    `json:"app_slug"`
	ClientID               string    `json:"client_id"`
	ClientSecretCiphertext string    `json:"-"`
	PrivateKeyCiphertext   string    `json:"-"`
	OwnerExternalID        int64     `json:"owner_external_id,omitempty"`
	OwnerLogin             string    `json:"owner_login,omitempty"`
	PublicOrigin           string    `json:"-"`
	Status                 string    `json:"status"`
	Version                int64     `json:"version"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type FlowState struct {
	NonceHash      string
	TenantID       string
	UserID         string
	Provider       string
	FlowType       string
	ReturnTo       string
	PublicOrigin   string
	RegistrationID string
	ExpiresAt      time.Time
	CreatedAt      time.Time
}

type ManifestStart struct {
	RegistrationURL string         `json:"registration_url"`
	State           string         `json:"state"`
	Manifest        map[string]any `json:"manifest"`
}

type ConnectorResult struct {
	Connection ConnectionView `json:"connection"`
	ReturnTo   string         `json:"return_to"`
}

type Project struct {
	ID                     string     `json:"id"`
	TenantID               string     `json:"-"`
	OwnerUserID            string     `json:"-"`
	Name                   string     `json:"name"`
	Description            string     `json:"description"`
	RuntimeID              string     `json:"runtime_id"`
	RepositoryMode         string     `json:"repository_mode"`
	RepositoryProvider     string     `json:"repository_provider"`
	RepositoryExternalID   int64      `json:"repository_external_id,omitempty"`
	RepositoryOwner        string     `json:"repository_owner"`
	RepositoryName         string     `json:"repository_name"`
	RepositoryHTMLURL      string     `json:"repository_html_url"`
	InstallationID         int64      `json:"installation_id,omitempty"`
	DefaultBranch          string     `json:"default_branch"`
	Visibility             string     `json:"visibility"`
	RepositorySizeKB       int64      `json:"repository_size_kb"`
	Status                 string     `json:"status"`
	ProvisionErrorCode     string     `json:"provision_error_code,omitempty"`
	ProvisionRequestID     string     `json:"-"`
	ProvisionStartedAt     time.Time  `json:"-"`
	Version                int64      `json:"version"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	ArchivedAt             *time.Time `json:"archived_at,omitempty"`
	RepositoryHasLFS       bool       `json:"repository_has_lfs,omitempty"`
	RepositoryHasSubmodule bool       `json:"repository_has_submodules,omitempty"`
	PrimaryConversationID  string     `json:"primary_conversation_id,omitempty"`
	GitHubPublishStatus    string     `json:"github_publish_status"`
}

type Repository struct {
	ID            int64  `json:"id"`
	OwnerID       int64  `json:"-"`
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	Visibility    string `json:"visibility"`
	Private       bool   `json:"private"`
	SizeKB        int64  `json:"size_kb"`
	HasLFS        bool   `json:"has_lfs,omitempty"`
	HasSubmodule  bool   `json:"has_submodule,omitempty"`
	CreatedAt     time.Time
}

type RepositoryPage struct {
	Repositories []Repository `json:"repositories"`
	NextCursor   string       `json:"next_cursor,omitempty"`
}

type Change struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"`
	Status  string `json:"status"`
	Area    string `json:"area"`
}

type GitCommit struct {
	SHA          string   `json:"sha"`
	Parents      []string `json:"parents"`
	Subject      string   `json:"subject"`
	AuthorName   string   `json:"author_name"`
	AuthoredAt   string   `json:"authored_at"`
	Refs         []string `json:"refs,omitempty"`
	FilesChanged int      `json:"files_changed,omitempty"`
	Additions    int      `json:"additions,omitempty"`
	Deletions    int      `json:"deletions,omitempty"`
	Body         string   `json:"body,omitempty"`
}

type GitCommitFile struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"`
	Status  string `json:"status"`
	Binary  bool   `json:"binary,omitempty"`
}

type GitSnapshot struct {
	Branch           string      `json:"branch"`
	BaseRef          string      `json:"base_ref"`
	BaseSHA          string      `json:"base_sha"`
	HeadSHA          string      `json:"head_sha"`
	Ahead            int         `json:"ahead"`
	Dirty            bool        `json:"dirty"`
	Changes          []Change    `json:"changes"`
	Truncated        bool        `json:"truncated"`
	Commits          []GitCommit `json:"commits,omitempty"`
	HistoryTruncated bool        `json:"history_truncated,omitempty"`
	CapturedAt       time.Time   `json:"captured_at"`
}

type Workspace struct {
	ConversationID     string          `json:"conversation_id"`
	ProjectID          string          `json:"project_id"`
	BaseRef            string          `json:"base_ref"`
	BaseSHA            string          `json:"base_sha"`
	BranchName         string          `json:"branch_name"`
	HeadSHA            string          `json:"head_sha"`
	BootstrapStatus    string          `json:"bootstrap_status"`
	BootstrapErrorCode string          `json:"bootstrap_error_code,omitempty"`
	GitSnapshot        GitSnapshot     `json:"git_snapshot"`
	GitSnapshotRaw     json.RawMessage `json:"-"`
	GitSnapshotAt      *time.Time      `json:"git_snapshot_at,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type Task struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	RuntimeID string    `json:"runtime_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Workspace Workspace `json:"workspace"`
}

type BrokerCredentialClaims struct {
	TenantID           string `json:"tenant_id"`
	UserID             string `json:"user_id"`
	ConversationID     string `json:"conversation_id"`
	RunID              string `json:"run_id"`
	ProjectID          string `json:"project_id"`
	RepositoryID       int64  `json:"repository_id"`
	RepositoryFullName string `json:"repository_full_name"`
	InstallationID     int64  `json:"installation_id"`
	RegistrationID     string `json:"registration_id"`
	TaskBranch         string `json:"task_branch"`
	ExpiresAt          int64  `json:"expires_at"`
}

type BrokerCommand struct {
	Operation           string   `json:"operation"`
	Arguments           []string `json:"argv"`
	DeclaredPermissions []string `json:"permissions,omitempty"`
	RequestID           string   `json:"request_id"`
}

type BrokerDecision struct {
	Status      string            `json:"status"`
	LeaseID     string            `json:"lease_id,omitempty"`
	Token       string            `json:"token,omitempty"`
	ExpiresAt   time.Time         `json:"expires_at,omitempty"`
	ApprovalID  string            `json:"approval_id,omitempty"`
	Category    string            `json:"category"`
	Label       string            `json:"label"`
	Permissions map[string]string `json:"permissions"`
}

type Approval struct {
	ID              string
	TenantID        string
	UserID          string
	ConversationID  string
	RunID           string
	ProjectID       string
	RepositoryID    int64
	CommandDigest   string
	CommandCategory string
	CommandLabel    string
	Permissions     map[string]string
	Status          string
	ExpiresAt       time.Time
	DecidedAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type TokenLease struct {
	ID              string
	ApprovalID      string
	TenantID        string
	UserID          string
	ConversationID  string
	RunID           string
	ProjectID       string
	RepositoryID    int64
	CommandCategory string
	Permissions     map[string]string
	TokenCiphertext string
	ExpiresAt       time.Time
	RevokedAt       *time.Time
	CreatedAt       time.Time
}

type BrokerRun struct {
	TenantID       string
	UserID         string
	ConversationID string
	RunID          string
	ProjectID      string
	RepositoryID   int64
	RegistrationID string
	ExpiresAt      time.Time
	RevokedAt      *time.Time
	CreatedAt      time.Time
}

type LeaseCompletion struct {
	Result string `json:"result"`
}

type AuditEvent struct {
	TenantID        string
	UserID          string
	ProjectID       string
	RepositoryID    int64
	RunID           string
	CommandCategory string
	Permissions     map[string]string
	Result          string
	DurationMS      int64
	CreatedAt       time.Time
}

type ProjectContext struct {
	ProjectID            string `json:"project_id"`
	RepositoryExternalID int64  `json:"repository_external_id"`
	CloneURL             string `json:"clone_url"`
	DefaultBranch        string `json:"default_branch"`
	BaseSHA              string `json:"base_sha"`
	BranchName           string `json:"branch_name"`
	GitAuthorName        string `json:"git_author_name"`
	GitAuthorEmail       string `json:"git_author_email"`
	InstallationID       int64  `json:"-"`
	RepositoryProvider   string `json:"repository_provider"`
	RepositoryFullName   string `json:"repository_full_name"`
	CredentialMode       string `json:"credential_mode"`
}

type Store interface {
	SaveFlowState(context.Context, FlowState) error
	ConsumeFlowState(context.Context, Identity, string, string, time.Time) (FlowState, error)
	GetAppRegistration(context.Context, Identity) (AppRegistration, error)
	UpsertAppRegistration(context.Context, AppRegistration) (AppRegistration, error)
	DeleteAppRegistration(context.Context, Identity) error

	SaveOAuthState(context.Context, Identity, string, time.Time, time.Time) error
	ConsumeOAuthState(context.Context, Identity, string, time.Time) error
	GetConnection(context.Context, Identity) (Connection, error)
	UpsertConnection(context.Context, Connection) (Connection, error)
	RefreshConnection(context.Context, Identity, func(Connection) (Connection, bool, error)) (Connection, error)
	DeleteConnection(context.Context, Identity) error

	ListProjects(context.Context, Identity) ([]Project, error)
	GetProject(context.Context, Identity, string) (Project, error)
	GetProjectByRequest(context.Context, Identity, string) (Project, error)
	CreateProject(context.Context, Project) (Project, error)
	RefreshProjectProvisionAttempt(context.Context, Identity, string, time.Time) (Project, error)
	CompleteProject(context.Context, Identity, string, Repository, int64, time.Time) (Project, error)
	BeginLocalProjectPublishIntent(context.Context, Identity, string, int64, string, string, time.Time) (Project, error)
	BindLocalProjectPublishRepository(context.Context, Identity, string, int64, Repository, int64, time.Time) (Project, error)
	CancelLocalProjectPublishIntent(context.Context, Identity, string, int64, time.Time) (Project, error)
	CompleteLocalProjectPublish(context.Context, Identity, string, int64, time.Time) (Project, error)
	RebindProjectInstallation(context.Context, Identity, string, int64, int64, time.Time) (Project, error)
	FailProject(context.Context, Identity, string, string, time.Time) (Project, error)
	UpdateProject(context.Context, Identity, string, int64, string, string, string, time.Time) (Project, error)
	ArchiveProject(context.Context, Identity, string, int64, time.Time) (Project, error)
	ListTasks(context.Context, Identity, string) ([]Task, error)

	GetWorkspace(context.Context, Identity, string) (Workspace, Project, error)
	LockBaseSHA(context.Context, Identity, string, string, time.Time) (Workspace, error)
	SaveSnapshot(context.Context, Identity, string, GitSnapshot, string, string, time.Time) error
	MarkBootstrapFailed(context.Context, Identity, string, string, time.Time) error

	GetOrCreateApproval(context.Context, Approval) (Approval, error)
	GetApproval(context.Context, string) (Approval, error)
	DecideApproval(context.Context, Identity, string, string, time.Time) (Approval, error)
	ClaimApproval(context.Context, Identity, string, time.Time) (Approval, error)
	ExpireApproval(context.Context, Identity, string, time.Time) (Approval, error)
	SaveBrokerRun(context.Context, BrokerRun) error
	BrokerRunActive(context.Context, BrokerCredentialClaims, time.Time) (bool, error)
	RevokeBrokerRun(context.Context, Identity, string, time.Time) error
	SaveTokenLease(context.Context, TokenLease) error
	GetTokenLease(context.Context, Identity, string, string) (TokenLease, error)
	ListActiveTokenLeasesForRun(context.Context, Identity, string, time.Time) ([]TokenLease, error)
	ListActiveTokenLeasesForProject(context.Context, Identity, string, time.Time) ([]TokenLease, error)
	ListActiveTokenLeasesForUser(context.Context, Identity, time.Time) ([]TokenLease, error)
	MarkTokenLeaseRevoked(context.Context, Identity, string, string, time.Time) error
	SaveAuditEvent(context.Context, AuditEvent) error
	Close()
}
