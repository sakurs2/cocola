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
	ErrNotFound               = errors.New("project: not found")
	ErrConflict               = errors.New("project: conflict")
	ErrVersionConflict        = errors.New("project: version conflict")
	ErrInvalidArgument        = errors.New("project: invalid argument")
	ErrConnectionRequired     = errors.New("project: github connection required")
	ErrInstallationRequired   = errors.New("project: github installation required")
	ErrRepositoryNotInstalled = errors.New("project: repository is not available to the github installation")
	ErrRepositoryTooLarge     = errors.New("project: repository too large")
	ErrProjectNotReady        = errors.New("project: project not ready")
)

const (
	ProviderGitHub = "github"

	ConnectionInstallationRequired = "installation_required"
	ConnectionReady                = "ready"
	ConnectionReauthorization      = "reauthorization_required"

	ProjectProvisioning = "provisioning"
	ProjectReady        = "ready"
	ProjectFailed       = "failed"
	ProjectArchived     = "archived"
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
	InstallationURL        string     `json:"installation_url,omitempty"`
	ReauthorizationURL     string     `json:"reauthorization_url,omitempty"`
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

type GitSnapshot struct {
	Branch     string    `json:"branch"`
	BaseRef    string    `json:"base_ref"`
	BaseSHA    string    `json:"base_sha"`
	HeadSHA    string    `json:"head_sha"`
	Ahead      int       `json:"ahead"`
	Dirty      bool      `json:"dirty"`
	Changes    []Change  `json:"changes"`
	Truncated  bool      `json:"truncated"`
	CapturedAt time.Time `json:"captured_at"`
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
}

type Store interface {
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
	CompleteProject(context.Context, Identity, string, Repository, int64, time.Time) (Project, error)
	FailProject(context.Context, Identity, string, string, time.Time) (Project, error)
	UpdateProject(context.Context, Identity, string, int64, string, string, string, time.Time) (Project, error)
	ArchiveProject(context.Context, Identity, string, int64, time.Time) (Project, error)
	ListTasks(context.Context, Identity, string) ([]Task, error)

	GetWorkspace(context.Context, Identity, string) (Workspace, Project, error)
	LockBaseSHA(context.Context, Identity, string, string, time.Time) (Workspace, error)
	SaveSnapshot(context.Context, Identity, string, GitSnapshot, string, string, time.Time) error
	MarkBootstrapFailed(context.Context, Identity, string, string, time.Time) error
	Close()
}
