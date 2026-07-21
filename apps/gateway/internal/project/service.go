package project

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Config struct {
	ConfigurationPresent bool
	AppID                string
	AppSlug              string
	ClientID             string
	ClientSecret         string
	PrivateKey           string
	CallbackURL          string
	SecretKey            string
	MaxRepositoryMB      int64
	HTTPClient           *http.Client
}

func (c Config) configured() bool {
	return c.ConfigurationPresent || c.AppID != "" || c.AppSlug != "" || c.ClientID != "" || c.ClientSecret != "" ||
		c.PrivateKey != "" || c.CallbackURL != "" || c.SecretKey != ""
}

func (c Config) validate() error {
	if !c.configured() {
		return nil
	}
	missing := make([]string, 0)
	for key, value := range map[string]string{
		"COCOLA_GITHUB_APP_ID": c.AppID, "COCOLA_GITHUB_APP_SLUG": c.AppSlug,
		"COCOLA_GITHUB_CLIENT_ID": c.ClientID, "COCOLA_GITHUB_CLIENT_SECRET": c.ClientSecret,
		"COCOLA_GITHUB_PRIVATE_KEY": c.PrivateKey, "COCOLA_GITHUB_CALLBACK_URL": c.CallbackURL,
		"COCOLA_SCM_SECRET_KEY": c.SecretKey,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("incomplete GitHub Project configuration; missing %s", strings.Join(missing, ", "))
	}
	if c.MaxRepositoryMB <= 0 {
		return errors.New("COCOLA_PROJECT_MAX_REPOSITORY_MB must be positive")
	}
	if callback, err := http.NewRequest(http.MethodGet, c.CallbackURL, nil); err != nil ||
		(callback.URL.Scheme != "http" && callback.URL.Scheme != "https") || callback.URL.Host == "" {
		return errors.New("COCOLA_GITHUB_CALLBACK_URL must be an absolute URL")
	}
	return nil
}

type ConnectionView struct {
	Status             string `json:"status"`
	ExternalLogin      string `json:"external_login,omitempty"`
	InstallationURL    string `json:"installation_url,omitempty"`
	ReauthorizationURL string `json:"reauthorization_url,omitempty"`
	Enabled            bool   `json:"enabled"`
}

type OAuthStart struct {
	AuthorizationURL string `json:"authorization_url"`
}

type OAuthResult struct {
	Connection ConnectionView `json:"connection"`
	ReturnTo   string         `json:"return_to"`
}

type CreateInput struct {
	ClientRequestID string `json:"client_request_id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	RuntimeID       string `json:"runtime_id"`
	Mode            string `json:"mode"`
	RepositoryName  string `json:"repository_name"`
	RepositoryID    int64  `json:"repository_id"`
	Visibility      string `json:"visibility"`
}

type UpdateInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	RuntimeID       string `json:"runtime_id"`
}

type Service struct {
	store   Store
	github  *githubClient
	box     *secretBox
	enabled bool
	maxKB   int64
	now     func() time.Time
}

func New(store Store, cfg Config) (*Service, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	s := &Service{store: store, maxKB: cfg.MaxRepositoryMB * 1024, now: func() time.Time { return time.Now().UTC() }}
	if !cfg.configured() {
		return s, nil
	}
	box, err := newSecretBox(cfg.SecretKey)
	if err != nil {
		return nil, err
	}
	client, err := newGitHubClient(cfg)
	if err != nil {
		return nil, err
	}
	s.box, s.github, s.enabled = box, client, true
	return s, nil
}

func (s *Service) Enabled() bool { return s != nil && s.enabled }

func (s *Service) Connection(ctx context.Context, id Identity) (ConnectionView, error) {
	if !s.Enabled() {
		return ConnectionView{Status: "disabled", Enabled: false}, nil
	}
	c, err := s.store.GetConnection(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return ConnectionView{Status: "disconnected", Enabled: true}, nil
	}
	if err != nil {
		return ConnectionView{}, err
	}
	if c.Status != ConnectionReauthorization {
		token, tokenErr := s.userToken(ctx, id)
		if tokenErr != nil {
			c.Status = ConnectionReauthorization
			c.UpdatedAt = s.now()
			_, _ = s.store.UpsertConnection(ctx, c)
		} else {
			c, err = s.store.GetConnection(ctx, id)
			if err != nil {
				return ConnectionView{}, err
			}
			installation, installErr := s.personalInstallation(ctx, token, c.ExternalUserID)
			if installErr == nil {
				if installation.ID != c.InstallationID || c.Status != ConnectionReady {
					c.InstallationID, c.Status, c.UpdatedAt = installation.ID, ConnectionReady, s.now()
					c, err = s.store.UpsertConnection(ctx, c)
					if err != nil {
						return ConnectionView{}, err
					}
				}
			} else if errors.Is(installErr, ErrInstallationRequired) {
				c.InstallationID, c.Status, c.UpdatedAt = 0, ConnectionInstallationRequired, s.now()
				c, err = s.store.UpsertConnection(ctx, c)
				if err != nil {
					return ConnectionView{}, err
				}
			} else {
				return ConnectionView{}, installErr
			}
		}
	}
	return s.connectionView(c), nil
}

func (s *Service) StartOAuth(ctx context.Context, id Identity, returnTo string) (OAuthStart, error) {
	if !s.Enabled() {
		return OAuthStart{}, ErrDisabled
	}
	now := s.now()
	state, err := s.box.signState(id, returnTo, now)
	if err != nil {
		return OAuthStart{}, err
	}
	decoded, err := s.box.verifyState(state, id, now)
	if err != nil {
		return OAuthStart{}, err
	}
	if err := s.store.SaveOAuthState(ctx, id, nonceHash(decoded.Nonce), time.Unix(decoded.Expires, 0), now); err != nil {
		return OAuthStart{}, err
	}
	return OAuthStart{AuthorizationURL: s.github.authorizeURL(state)}, nil
}

func (s *Service) CompleteOAuth(ctx context.Context, id Identity, state, code string) (OAuthResult, error) {
	if !s.Enabled() {
		return OAuthResult{}, ErrDisabled
	}
	if strings.TrimSpace(code) == "" {
		return OAuthResult{}, ErrInvalidArgument
	}
	now := s.now()
	decoded, err := s.box.verifyState(state, id, now)
	if err != nil {
		return OAuthResult{}, err
	}
	if err := s.store.ConsumeOAuthState(ctx, id, nonceHash(decoded.Nonce), now); err != nil {
		return OAuthResult{}, err
	}
	token, err := s.github.exchange(ctx, code)
	if err != nil {
		return OAuthResult{}, err
	}
	user, err := s.github.user(ctx, token.AccessToken)
	if err != nil {
		return OAuthResult{}, err
	}
	access, err := s.box.encrypt(token.AccessToken, tokenAAD(id, "access_token"))
	if err != nil {
		return OAuthResult{}, err
	}
	refresh := ""
	if token.RefreshToken != "" {
		refresh, err = s.box.encrypt(token.RefreshToken, tokenAAD(id, "refresh_token"))
		if err != nil {
			return OAuthResult{}, err
		}
	}
	status, installationID := ConnectionInstallationRequired, int64(0)
	if installation, installErr := s.personalInstallation(ctx, token.AccessToken, user.ID); installErr == nil {
		status, installationID = ConnectionReady, installation.ID
	} else if !errors.Is(installErr, ErrInstallationRequired) {
		return OAuthResult{}, installErr
	}
	c, err := s.store.UpsertConnection(ctx, Connection{
		ID: uuid.NewString(), TenantID: id.TenantID, UserID: id.UserID, Provider: ProviderGitHub,
		ExternalUserID: user.ID, ExternalLogin: user.Login, InstallationID: installationID,
		AccessTokenCiphertext: access, AccessTokenExpiresAt: token.ExpiresAt,
		RefreshTokenCiphertext: refresh, RefreshTokenExpiresAt: token.RefreshAt,
		Status: status, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return OAuthResult{}, err
	}
	return OAuthResult{Connection: s.connectionView(c), ReturnTo: decoded.ReturnTo}, nil
}

func (s *Service) Disconnect(ctx context.Context, id Identity) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	err := s.store.DeleteConnection(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func (s *Service) Repositories(ctx context.Context, id Identity, cursor string) (RepositoryPage, error) {
	if !s.Enabled() {
		return RepositoryPage{}, ErrDisabled
	}
	token, c, err := s.readyConnection(ctx, id)
	if err != nil {
		return RepositoryPage{}, err
	}
	page, err := decodeCursor(cursor)
	if err != nil {
		return RepositoryPage{}, ErrInvalidArgument
	}
	repos, more, err := s.github.repositories(ctx, token, c.InstallationID, page)
	if err != nil {
		return RepositoryPage{}, err
	}
	filtered := make([]Repository, 0, len(repos))
	for _, repo := range repos {
		if repo.OwnerID == c.ExternalUserID {
			filtered = append(filtered, repo)
		}
	}
	result := RepositoryPage{Repositories: filtered}
	if more {
		result.NextCursor = encodeCursor(page + 1)
	}
	return result, nil
}

func (s *Service) Create(ctx context.Context, id Identity, input CreateInput) (Project, error) {
	if !s.Enabled() {
		return Project{}, ErrDisabled
	}
	input = normalizeCreate(input)
	if err := validateCreate(input); err != nil {
		return Project{}, err
	}
	if existing, err := s.store.GetProjectByRequest(ctx, id, input.ClientRequestID); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Project{}, err
	}
	token, connection, err := s.readyConnection(ctx, id)
	if err != nil {
		return Project{}, err
	}
	now := s.now()
	v, err := s.store.CreateProject(ctx, Project{
		ID: uuid.NewString(), TenantID: id.TenantID, OwnerUserID: id.UserID,
		Name: input.Name, Description: input.Description, RuntimeID: input.RuntimeID,
		RepositoryMode: input.Mode, RepositoryProvider: ProviderGitHub,
		RepositoryExternalID: input.RepositoryID, RepositoryName: input.RepositoryName, Visibility: input.Visibility,
		Status: ProjectProvisioning, ProvisionRequestID: input.ClientRequestID,
		ProvisionStartedAt: now, CreatedAt: now, UpdatedAt: now,
	})
	if errors.Is(err, ErrConflict) {
		if existing, lookupErr := s.store.GetProjectByRequest(ctx, id, input.ClientRequestID); lookupErr == nil {
			return existing, nil
		}
	}
	if err != nil {
		return Project{}, err
	}
	var repo Repository
	if input.Mode == "create" {
		repo, err = s.github.createRepository(ctx, token, input.RepositoryName, input.Description, input.Visibility == "private")
	} else {
		repo, err = s.github.repository(ctx, token, input.RepositoryID)
	}
	if err != nil {
		if isDefinitiveGitHubError(err) {
			failed, failErr := s.store.FailProject(ctx, id, v.ID, githubErrorCode(err), s.now())
			if failErr == nil {
				return failed, nil
			}
		}
		// Preserve provisioning on timeouts/5xx: retry can safely reconcile it.
		return v, nil
	}
	if err := s.validateRepository(repo, connection); err != nil {
		failed, failErr := s.store.FailProject(ctx, id, v.ID, projectErrorCode(err), s.now())
		if failErr != nil {
			return Project{}, failErr
		}
		return failed, nil
	}
	if installErr := s.ensureInstalledRepository(ctx, token, connection, repo.ID); installErr != nil {
		if !errors.Is(installErr, ErrNotFound) {
			return v, nil
		}
		failed, failErr := s.store.FailProject(ctx, id, v.ID, projectErrorCode(ErrRepositoryNotInstalled), s.now())
		if failErr != nil {
			return Project{}, failErr
		}
		return failed, nil
	}
	repo = s.github.repositoryWarnings(ctx, token, repo)
	return s.store.CompleteProject(ctx, id, v.ID, repo, connection.InstallationID, s.now())
}

func (s *Service) Retry(ctx context.Context, id Identity, projectID string) (Project, error) {
	if !s.Enabled() {
		return Project{}, ErrDisabled
	}
	if _, err := uuid.Parse(projectID); err != nil {
		return Project{}, ErrInvalidArgument
	}
	v, err := s.store.GetProject(ctx, id, projectID)
	if err != nil {
		return Project{}, err
	}
	if v.Status == ProjectReady || v.Status == ProjectArchived {
		return v, nil
	}
	token, c, err := s.readyConnection(ctx, id)
	if err != nil {
		return Project{}, err
	}
	var repo Repository
	if v.RepositoryMode == "create" {
		repo, err = s.github.repositoryByName(ctx, token, c.ExternalLogin, v.RepositoryName)
	} else if v.RepositoryMode == "import" && v.RepositoryExternalID > 0 {
		repo, err = s.github.repository(ctx, token, v.RepositoryExternalID)
	} else {
		return Project{}, ErrInvalidArgument
	}
	if err != nil {
		return Project{}, err
	}
	if repo.OwnerID != c.ExternalUserID || (v.RepositoryMode == "create" &&
		(repo.CreatedAt.Before(v.ProvisionStartedAt.Add(-2*time.Minute)) ||
			repo.CreatedAt.After(v.ProvisionStartedAt.Add(2*time.Minute)))) {
		return Project{}, ErrConflict
	}
	if err := s.validateRepository(repo, c); err != nil {
		return Project{}, err
	}
	if err := s.ensureInstalledRepository(ctx, token, c, repo.ID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Project{}, ErrRepositoryNotInstalled
		}
		return Project{}, err
	}
	repo = s.github.repositoryWarnings(ctx, token, repo)
	return s.store.CompleteProject(ctx, id, v.ID, repo, c.InstallationID, s.now())
}

func (s *Service) List(ctx context.Context, id Identity) ([]Project, error) {
	return s.store.ListProjects(ctx, id)
}

func (s *Service) Get(ctx context.Context, id Identity, projectID string) (Project, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return Project{}, ErrInvalidArgument
	}
	return s.store.GetProject(ctx, id, projectID)
}

func (s *Service) Update(ctx context.Context, id Identity, projectID string, input UpdateInput) (Project, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return Project{}, ErrInvalidArgument
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.RuntimeID = strings.TrimSpace(input.RuntimeID)
	if input.ExpectedVersion <= 0 || input.Name == "" || len(input.Name) > 100 || len(input.Description) > 500 || input.RuntimeID == "" {
		return Project{}, ErrInvalidArgument
	}
	return s.store.UpdateProject(ctx, id, projectID, input.ExpectedVersion, input.Name, input.Description, input.RuntimeID, s.now())
}

func (s *Service) Archive(ctx context.Context, id Identity, projectID string, expected int64) (Project, error) {
	if _, err := uuid.Parse(projectID); err != nil || expected <= 0 {
		return Project{}, ErrInvalidArgument
	}
	return s.store.ArchiveProject(ctx, id, projectID, expected, s.now())
}

func (s *Service) Tasks(ctx context.Context, id Identity, projectID string) ([]Task, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return nil, ErrInvalidArgument
	}
	return s.store.ListTasks(ctx, id, projectID)
}

func (s *Service) ValidateReady(ctx context.Context, id Identity, projectID string) (Project, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return Project{}, ErrInvalidArgument
	}
	v, err := s.store.GetProject(ctx, id, projectID)
	if err != nil {
		return Project{}, err
	}
	if v.Status != ProjectReady {
		return Project{}, ErrProjectNotReady
	}
	if !s.Enabled() {
		return Project{}, ErrDisabled
	}
	c, err := s.store.GetConnection(ctx, id)
	if errors.Is(err, ErrNotFound) || (err == nil && c.Status != ConnectionReady) {
		return Project{}, ErrConnectionRequired
	}
	if err != nil {
		return Project{}, err
	}
	if c.InstallationID != v.InstallationID {
		return Project{}, ErrInstallationRequired
	}
	return v, nil
}

func (s *Service) Workspace(ctx context.Context, id Identity, conversationID string) (Workspace, Project, error) {
	return s.store.GetWorkspace(ctx, id, conversationID)
}

func (s *Service) ProjectContext(ctx context.Context, id Identity, conversationID string) (ProjectContext, string, error) {
	if !s.Enabled() {
		return ProjectContext{}, "", ErrDisabled
	}
	w, v, err := s.store.GetWorkspace(ctx, id, conversationID)
	if err != nil {
		return ProjectContext{}, "", err
	}
	if v.Status != ProjectReady {
		return ProjectContext{}, "", ErrProjectNotReady
	}
	token, c, err := s.readyConnection(ctx, id)
	if err != nil {
		return ProjectContext{}, "", err
	}
	if c.InstallationID != v.InstallationID {
		return ProjectContext{}, "", ErrInstallationRequired
	}
	if w.BaseSHA == "" {
		sha, branchErr := s.github.branchSHA(ctx, token, v.RepositoryOwner, v.RepositoryName, v.DefaultBranch)
		if branchErr != nil {
			return ProjectContext{}, "", branchErr
		}
		w, err = s.store.LockBaseSHA(ctx, id, conversationID, sha, s.now())
		if err != nil {
			return ProjectContext{}, "", err
		}
	}
	cloneToken, _, err := s.github.installationToken(ctx, v.InstallationID, v.RepositoryExternalID)
	if err != nil {
		return ProjectContext{}, "", err
	}
	gitAuthorName, gitAuthorEmail := gitAuthorIdentity(id)
	return ProjectContext{
		ProjectID: v.ID, RepositoryExternalID: v.RepositoryExternalID,
		CloneURL:      "https://github.com/" + v.RepositoryOwner + "/" + v.RepositoryName + ".git",
		DefaultBranch: v.DefaultBranch, BaseSHA: w.BaseSHA, BranchName: w.BranchName,
		GitAuthorName: gitAuthorName, GitAuthorEmail: gitAuthorEmail,
		InstallationID: v.InstallationID,
	}, cloneToken, nil
}

func gitAuthorIdentity(id Identity) (string, string) {
	name := strings.TrimSpace(id.Name)
	if name == "" {
		name = strings.TrimSpace(id.Username)
	}
	if name == "" || len(name) > 128 || strings.ContainsAny(name, "\x00\r\n") {
		name = "Cocola User"
	}

	email := strings.TrimSpace(id.Email)
	if email != "" && len(email) <= 254 && strings.Contains(email, "@") && !strings.ContainsAny(email, "\x00\r\n") {
		return name, email
	}

	username := strings.TrimSpace(id.Username)
	if username == "" || strings.ContainsAny(username, "\x00\r\n@") {
		username = "cocola-user"
	}
	return name, username + "@localhost"
}

func (s *Service) SaveSnapshot(ctx context.Context, id Identity, conversationID string, snapshot GitSnapshot, headSHA, status string) error {
	if len(snapshot.Changes) > 500 {
		snapshot.Changes, snapshot.Truncated = snapshot.Changes[:500], true
	}
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = s.now()
	}
	return s.store.SaveSnapshot(ctx, id, conversationID, snapshot, headSHA, status, snapshot.CapturedAt)
}

func (s *Service) MarkBootstrapFailed(ctx context.Context, id Identity, conversationID, code string) error {
	return s.store.MarkBootstrapFailed(ctx, id, conversationID, code, s.now())
}

func (s *Service) readyConnection(ctx context.Context, id Identity) (string, Connection, error) {
	c, err := s.store.GetConnection(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return "", Connection{}, ErrConnectionRequired
	}
	if err != nil {
		return "", Connection{}, err
	}
	if c.Status == ConnectionReauthorization {
		return "", Connection{}, ErrConnectionRequired
	}
	token, err := s.userToken(ctx, id)
	if err != nil {
		return "", Connection{}, ErrConnectionRequired
	}
	c, err = s.store.GetConnection(ctx, id)
	if err != nil {
		return "", Connection{}, err
	}
	installation, err := s.personalInstallation(ctx, token, c.ExternalUserID)
	if err != nil {
		return "", Connection{}, err
	}
	if installation.ID != c.InstallationID || c.Status != ConnectionReady {
		c.InstallationID, c.Status, c.UpdatedAt = installation.ID, ConnectionReady, s.now()
		c, err = s.store.UpsertConnection(ctx, c)
		if err != nil {
			return "", Connection{}, err
		}
	}
	return token, c, nil
}

func (s *Service) userToken(ctx context.Context, id Identity) (string, error) {
	c, err := s.store.RefreshConnection(ctx, id, func(c Connection) (Connection, bool, error) {
		if c.AccessTokenExpiresAt == nil || c.AccessTokenExpiresAt.After(s.now().Add(2*time.Minute)) {
			return c, false, nil
		}
		if c.RefreshTokenCiphertext == "" || (c.RefreshTokenExpiresAt != nil && c.RefreshTokenExpiresAt.Before(s.now())) {
			return Connection{}, false, ErrConnectionRequired
		}
		refreshToken, err := s.box.decrypt(c.RefreshTokenCiphertext, tokenAAD(id, "refresh_token"))
		if err != nil {
			return Connection{}, false, err
		}
		newToken, err := s.github.refresh(ctx, refreshToken)
		if err != nil {
			return Connection{}, false, err
		}
		c.AccessTokenCiphertext, err = s.box.encrypt(newToken.AccessToken, tokenAAD(id, "access_token"))
		if err != nil {
			return Connection{}, false, err
		}
		c.AccessTokenExpiresAt = newToken.ExpiresAt
		if newToken.RefreshToken != "" {
			c.RefreshTokenCiphertext, err = s.box.encrypt(newToken.RefreshToken, tokenAAD(id, "refresh_token"))
			if err != nil {
				return Connection{}, false, err
			}
			c.RefreshTokenExpiresAt = newToken.RefreshAt
		}
		c.UpdatedAt = s.now()
		return c, true, nil
	})
	if err != nil {
		return "", err
	}
	return s.box.decrypt(c.AccessTokenCiphertext, tokenAAD(id, "access_token"))
}

func (s *Service) personalInstallation(ctx context.Context, token string, userID int64) (githubInstallation, error) {
	installations, err := s.github.installations(ctx, token)
	if err != nil {
		return githubInstallation{}, err
	}
	for _, installation := range installations {
		if installation.Account.ID == userID && strings.EqualFold(installation.Account.Type, "User") {
			return installation, nil
		}
	}
	return githubInstallation{}, ErrInstallationRequired
}

func (s *Service) connectionView(c Connection) ConnectionView {
	view := ConnectionView{Status: c.Status, ExternalLogin: c.ExternalLogin, Enabled: true}
	if c.Status == ConnectionInstallationRequired {
		view.InstallationURL = s.github.installationURL()
	}
	if c.Status == ConnectionReauthorization {
		view.ReauthorizationURL = "/projects/new"
	}
	return view
}

func (s *Service) validateRepository(repo Repository, c Connection) error {
	if repo.ID <= 0 || repo.OwnerID != c.ExternalUserID || !strings.EqualFold(repo.Owner, c.ExternalLogin) || repo.DefaultBranch == "" {
		return ErrInvalidArgument
	}
	if repo.SizeKB > s.maxKB {
		return ErrRepositoryTooLarge
	}
	return nil
}

func (s *Service) ensureInstalledRepository(ctx context.Context, token string, c Connection, repositoryID int64) error {
	for page := 1; page <= 100; page++ {
		repositories, more, err := s.github.repositories(ctx, token, c.InstallationID, page)
		if err != nil {
			return err
		}
		for _, repository := range repositories {
			if repository.ID == repositoryID && repository.OwnerID == c.ExternalUserID {
				return nil
			}
		}
		if !more {
			break
		}
	}
	return ErrNotFound
}

func normalizeCreate(input CreateInput) CreateInput {
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.RuntimeID = strings.TrimSpace(input.RuntimeID)
	input.Mode = strings.TrimSpace(input.Mode)
	input.RepositoryName = strings.TrimSpace(input.RepositoryName)
	input.Visibility = strings.TrimSpace(input.Visibility)
	if input.RuntimeID == "" {
		input.RuntimeID = "claude-code"
	}
	if input.Visibility == "" {
		input.Visibility = "private"
	}
	return input
}

func validateCreate(input CreateInput) error {
	if input.ClientRequestID == "" || len(input.ClientRequestID) > 128 || input.Name == "" || len(input.Name) > 100 ||
		len(input.Description) > 500 || input.RepositoryName == "" || len(input.RepositoryName) > 100 ||
		(input.Mode != "create" && input.Mode != "import") ||
		(input.Visibility != "private" && input.Visibility != "public") || input.RuntimeID == "" {
		return ErrInvalidArgument
	}
	if input.Mode == "import" && input.RepositoryID <= 0 {
		return ErrInvalidArgument
	}
	if input.Mode == "create" && input.RepositoryID != 0 {
		return ErrInvalidArgument
	}
	for _, r := range input.RepositoryName {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return ErrInvalidArgument
		}
	}
	return nil
}

func nonceHash(nonce string) string {
	sum := sha256.Sum256([]byte(nonce))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func encodeCursor(page int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(page)))
}

func decodeCursor(value string) (int, error) {
	if value == "" {
		return 1, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, err
	}
	page, err := strconv.Atoi(string(raw))
	if err != nil || page < 1 || page > 10000 {
		return 0, ErrInvalidArgument
	}
	return page, nil
}

func isDefinitiveGitHubError(err error) bool {
	var httpErr *githubHTTPError
	return errors.As(err, &httpErr) && httpErr.Status >= 400 && httpErr.Status < 500 && httpErr.Status != http.StatusRequestTimeout && httpErr.Status != http.StatusTooManyRequests
}

func githubErrorCode(err error) string {
	var httpErr *githubHTTPError
	if errors.As(err, &httpErr) {
		return "GITHUB_HTTP_" + strconv.Itoa(httpErr.Status)
	}
	return "GITHUB_REQUEST_FAILED"
}

func projectErrorCode(err error) string {
	if errors.Is(err, ErrRepositoryTooLarge) {
		return "REPOSITORY_TOO_LARGE"
	}
	if errors.Is(err, ErrRepositoryNotInstalled) {
		return "REPOSITORY_NOT_INSTALLED"
	}
	return "REPOSITORY_INVALID"
}
