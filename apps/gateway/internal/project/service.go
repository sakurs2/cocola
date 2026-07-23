package project

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Config struct {
	SecretKey               string
	PublicOrigins           string
	MaxRepositoryMB         int64
	HTTPClient              *http.Client
	DisableLocalProjects    bool
	DisableGitHubConnector  bool
	DisableGitHubAgentWrite bool
}

func (c Config) validate() error {
	if c.MaxRepositoryMB <= 0 {
		return errors.New("COCOLA_PROJECT_MAX_REPOSITORY_MB must be positive")
	}
	if strings.TrimSpace(c.SecretKey) == "" && (!c.DisableGitHubConnector || !c.DisableGitHubAgentWrite) {
		return errors.New("COCOLA_SCM_SECRET_KEY is required")
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
	ClientRequestID string             `json:"client_request_id"`
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	RuntimeID       string             `json:"runtime_id"`
	Mode            string             `json:"mode"`
	RepositoryName  string             `json:"repository_name"`
	RepositoryID    int64              `json:"repository_id"`
	Visibility      string             `json:"visibility"`
	Source          ProjectSourceInput `json:"source"`
}

type ProjectSourceInput struct {
	Type           string `json:"type"`
	RepositoryName string `json:"repository_name,omitempty"`
	RepositoryID   int64  `json:"repository_id,omitempty"`
	Visibility     string `json:"visibility,omitempty"`
}

type UpdateInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	RuntimeID       string `json:"runtime_id"`
}

type PublishInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	RepositoryName  string `json:"repository_name"`
	Visibility      string `json:"visibility"`
}

type PublishPreparation struct {
	Project        Project
	Workspace      Workspace
	Repository     Repository
	CloneURL       string
	Token          string
	InstallationID int64
}

type Service struct {
	store                   Store
	box                     *secretBox
	http                    *http.Client
	publicOrigins           map[string]struct{}
	maxKB                   int64
	now                     func() time.Time
	githubConnectorEnabled  bool
	githubAgentWriteEnabled bool
	localProjectsEnabled    bool
}

func New(store Store, cfg Config) (*Service, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	var box *secretBox
	if strings.TrimSpace(cfg.SecretKey) != "" {
		var err error
		box, err = newSecretBox(cfg.SecretKey)
		if err != nil {
			return nil, err
		}
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	s := &Service{
		store: store, box: box, http: client, maxKB: cfg.MaxRepositoryMB * 1024,
		publicOrigins:           parsePublicOrigins(cfg.PublicOrigins),
		now:                     func() time.Time { return time.Now().UTC() },
		githubConnectorEnabled:  !cfg.DisableGitHubConnector,
		githubAgentWriteEnabled: !cfg.DisableGitHubAgentWrite,
		localProjectsEnabled:    !cfg.DisableLocalProjects,
	}
	return s, nil
}

func (s *Service) Enabled() bool { return s != nil }

func (s *Service) LocalProjectsEnabled() bool { return s != nil && s.localProjectsEnabled }

func (s *Service) GitHubConnectorEnabled() bool {
	return s != nil && s.githubConnectorEnabled
}

func (s *Service) GitHubAgentWriteEnabled() bool {
	return s != nil && s.githubAgentWriteEnabled
}

func (s *Service) Connection(ctx context.Context, id Identity) (ConnectionView, error) {
	if !s.GitHubConnectorEnabled() {
		return ConnectionView{Status: "disabled", Enabled: false}, nil
	}
	registration, err := s.store.GetAppRegistration(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return ConnectionView{Status: "not_configured", Enabled: true}, nil
	}
	if err != nil {
		return ConnectionView{}, err
	}
	github, err := s.githubForRegistration(id, registration)
	if err != nil {
		return ConnectionView{Status: RegistrationError, Enabled: true}, nil
	}
	c, err := s.store.GetConnection(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return s.registrationView(github, registration), nil
	}
	if err != nil {
		return ConnectionView{}, err
	}
	if c.RegistrationID != registration.ID {
		return ConnectionView{Status: ConnectionReauthorization, Enabled: true}, nil
	}
	if c.Status != ConnectionReauthorization {
		token, tokenErr := s.userToken(ctx, id, github)
		if tokenErr != nil {
			c.Status = ConnectionReauthorization
			c.UpdatedAt = s.now()
			_, _ = s.store.UpsertConnection(ctx, c)
		} else {
			c, err = s.store.GetConnection(ctx, id)
			if err != nil {
				return ConnectionView{}, err
			}
			installation, installErr := s.personalInstallation(ctx, github, token, c.ExternalUserID)
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
	return s.connectionView(github, c), nil
}

func (s *Service) StartManifest(
	ctx context.Context,
	id Identity,
	returnTo string,
	requestOrigin string,
) (ManifestStart, error) {
	if !s.GitHubConnectorEnabled() {
		return ManifestStart{}, ErrDisabled
	}
	origin, err := s.allowedOrigin(requestOrigin)
	if err != nil {
		return ManifestStart{}, err
	}
	now := s.now()
	state, err := s.box.signFlowState(id, "manifest", returnTo, origin, "", time.Hour, now)
	if err != nil {
		return ManifestStart{}, err
	}
	decoded, err := s.box.verifyFlowState(state, id, "manifest", now)
	if err != nil {
		return ManifestStart{}, err
	}
	if err := s.store.SaveFlowState(ctx, FlowState{
		NonceHash: nonceHash(decoded.Nonce), TenantID: id.TenantID, UserID: id.UserID,
		Provider: ProviderGitHub, FlowType: "manifest", ReturnTo: decoded.ReturnTo,
		PublicOrigin: origin, ExpiresAt: time.Unix(decoded.Expires, 0), CreatedAt: now,
	}); err != nil {
		return ManifestStart{}, err
	}
	appName := "Cocola " + strings.TrimSpace(id.Username)
	if strings.TrimSpace(id.Username) == "" {
		appName = "Cocola Personal Agent"
	}
	manifest := githubManifest(origin, appName)
	return ManifestStart{
		RegistrationURL: githubManifestRegistrationURL(state), State: state, Manifest: manifest,
	}, nil
}

func githubManifest(origin, appName string) map[string]any {
	return map[string]any{
		"name":                     appName,
		"url":                      origin,
		"redirect_url":             origin + "/connectors/github/manifest/callback",
		"callback_urls":            []string{origin + "/connectors/github/oauth/callback"},
		"setup_url":                origin + "/connectors/github/installation/callback",
		"setup_on_update":          true,
		"request_oauth_on_install": false,
		"public":                   false,
		"hook_attributes": map[string]any{
			"url": origin + "/connectors/github/webhooks/disabled", "active": false,
		},
		"default_permissions": map[string]string{
			"actions": "write", "administration": "write", "checks": "write",
			"contents": "write", "deployments": "write", "environments": "write",
			"issues": "write", "metadata": "read", "packages": "write",
			"pages": "write", "pull_requests": "write", "repository_hooks": "write",
			"secret_scanning_alerts": "write", "secrets": "write",
			"security_events": "write", "statuses": "write", "vulnerability_alerts": "write",
			"variables": "write", "workflows": "write",
		},
	}
}

func (s *Service) CompleteManifest(
	ctx context.Context,
	id Identity,
	state string,
	code string,
) (ConnectorResult, error) {
	if !s.GitHubConnectorEnabled() {
		return ConnectorResult{}, ErrDisabled
	}
	if strings.TrimSpace(code) == "" {
		return ConnectorResult{}, ErrInvalidArgument
	}
	now := s.now()
	decoded, err := s.box.verifyFlowState(state, id, "manifest", now)
	if err != nil {
		return ConnectorResult{}, err
	}
	flow, err := s.store.ConsumeFlowState(ctx, id, nonceHash(decoded.Nonce), "manifest", now)
	if err != nil || flow.PublicOrigin != decoded.PublicOrigin {
		return ConnectorResult{}, ErrInvalidArgument
	}
	conversion, err := convertGitHubManifest(ctx, s.http, code)
	if err != nil {
		return ConnectorResult{}, err
	}
	registrationID := uuid.NewString()
	clientSecret, err := s.box.encrypt(conversion.ClientSecret,
		registrationAAD(id, registrationID, "client_secret"))
	if err != nil {
		return ConnectorResult{}, err
	}
	privateKey, err := s.box.encrypt(conversion.PEM,
		registrationAAD(id, registrationID, "private_key"))
	if err != nil {
		return ConnectorResult{}, err
	}
	if err := s.store.DeleteConnection(ctx, id); err != nil && !errors.Is(err, ErrNotFound) {
		return ConnectorResult{}, err
	}
	if err := s.store.DeleteAppRegistration(ctx, id); err != nil && !errors.Is(err, ErrNotFound) {
		return ConnectorResult{}, err
	}
	registration, err := s.store.UpsertAppRegistration(ctx, AppRegistration{
		ID: registrationID, TenantID: id.TenantID, UserID: id.UserID, Provider: ProviderGitHub,
		AppID: conversion.ID, AppSlug: conversion.Slug, ClientID: conversion.ClientID,
		ClientSecretCiphertext: clientSecret, PrivateKeyCiphertext: privateKey,
		OwnerExternalID: conversion.Owner.ID, OwnerLogin: conversion.Owner.Login,
		PublicOrigin: flow.PublicOrigin, Status: RegistrationInstallRequired,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return ConnectorResult{}, err
	}
	github, err := s.githubForRegistration(id, registration)
	if err != nil {
		return ConnectorResult{}, err
	}
	return ConnectorResult{
		Connection: s.registrationView(github, registration), ReturnTo: flow.ReturnTo,
	}, nil
}

func (s *Service) StartOAuth(ctx context.Context, id Identity, returnTo string) (OAuthStart, error) {
	if !s.GitHubConnectorEnabled() {
		return OAuthStart{}, ErrDisabled
	}
	registration, err := s.store.GetAppRegistration(ctx, id)
	if err != nil {
		return OAuthStart{}, ErrConnectionRequired
	}
	github, err := s.githubForRegistration(id, registration)
	if err != nil {
		return OAuthStart{}, err
	}
	now := s.now()
	state, err := s.box.signFlowState(id, "oauth", returnTo, registration.PublicOrigin,
		registration.ID, 10*time.Minute, now)
	if err != nil {
		return OAuthStart{}, err
	}
	decoded, err := s.box.verifyFlowState(state, id, "oauth", now)
	if err != nil {
		return OAuthStart{}, err
	}
	if err := s.store.SaveFlowState(ctx, FlowState{
		NonceHash: nonceHash(decoded.Nonce), TenantID: id.TenantID, UserID: id.UserID,
		Provider: ProviderGitHub, FlowType: "oauth", ReturnTo: decoded.ReturnTo,
		PublicOrigin: registration.PublicOrigin, RegistrationID: registration.ID,
		ExpiresAt: time.Unix(decoded.Expires, 0), CreatedAt: now,
	}); err != nil {
		return OAuthStart{}, err
	}
	return OAuthStart{AuthorizationURL: github.authorizeURL(state)}, nil
}

func (s *Service) CompleteOAuth(ctx context.Context, id Identity, state, code string) (OAuthResult, error) {
	if !s.GitHubConnectorEnabled() {
		return OAuthResult{}, ErrDisabled
	}
	if strings.TrimSpace(code) == "" {
		return OAuthResult{}, ErrInvalidArgument
	}
	now := s.now()
	decoded, err := s.box.verifyFlowState(state, id, "oauth", now)
	if err != nil {
		return OAuthResult{}, err
	}
	flow, err := s.store.ConsumeFlowState(ctx, id, nonceHash(decoded.Nonce), "oauth", now)
	if err != nil || flow.RegistrationID == "" || flow.RegistrationID != decoded.RegistrationID {
		return OAuthResult{}, ErrInvalidArgument
	}
	registration, err := s.store.GetAppRegistration(ctx, id)
	if err != nil || registration.ID != flow.RegistrationID {
		return OAuthResult{}, ErrConnectionRequired
	}
	github, err := s.githubForRegistration(id, registration)
	if err != nil {
		return OAuthResult{}, err
	}
	token, err := github.exchange(ctx, code)
	if err != nil {
		return OAuthResult{}, err
	}
	user, err := github.user(ctx, token.AccessToken)
	if err != nil {
		return OAuthResult{}, err
	}
	if registration.OwnerExternalID > 0 && registration.OwnerExternalID != user.ID {
		return OAuthResult{}, ErrInvalidArgument
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
	if installation, installErr := s.personalInstallation(ctx, github, token.AccessToken, user.ID); installErr == nil {
		status, installationID = ConnectionReady, installation.ID
	} else if !errors.Is(installErr, ErrInstallationRequired) {
		return OAuthResult{}, installErr
	}
	c, err := s.store.UpsertConnection(ctx, Connection{
		ID: uuid.NewString(), TenantID: id.TenantID, UserID: id.UserID, Provider: ProviderGitHub,
		ExternalUserID: user.ID, ExternalLogin: user.Login, InstallationID: installationID,
		AccessTokenCiphertext: access, AccessTokenExpiresAt: token.ExpiresAt,
		RefreshTokenCiphertext: refresh, RefreshTokenExpiresAt: token.RefreshAt,
		Status: status, CreatedAt: now, UpdatedAt: now, RegistrationID: registration.ID,
	})
	if err != nil {
		return OAuthResult{}, err
	}
	return OAuthResult{Connection: s.connectionView(github, c), ReturnTo: decoded.ReturnTo}, nil
}

func (s *Service) Disconnect(ctx context.Context, id Identity) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	if err := s.revokeUserTokenLeases(ctx, id); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if err := s.store.DeleteConnection(ctx, id); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if err := s.store.DeleteAppRegistration(ctx, id); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

func (s *Service) Repositories(ctx context.Context, id Identity, cursor string) (RepositoryPage, error) {
	if !s.GitHubConnectorEnabled() {
		return RepositoryPage{}, ErrDisabled
	}
	token, c, github, err := s.readyConnection(ctx, id)
	if err != nil {
		return RepositoryPage{}, err
	}
	page, err := decodeCursor(cursor)
	if err != nil {
		return RepositoryPage{}, ErrInvalidArgument
	}
	repos, more, err := github.repositories(ctx, token, c.InstallationID, page)
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
	input = normalizeCreate(input)
	if err := validateCreate(input); err != nil {
		return Project{}, err
	}
	if existing, err := s.store.GetProjectByRequest(ctx, id, input.ClientRequestID); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Project{}, err
	}
	provider := ProviderGitHub
	if input.Mode == "empty" {
		provider = ProviderLocal
		if !s.LocalProjectsEnabled() {
			return Project{}, ErrLocalProjectsDisabled
		}
	} else if !s.GitHubConnectorEnabled() {
		return Project{}, ErrDisabled
	}
	now := s.now()
	repositoryName := input.RepositoryName
	status, defaultBranch, visibility := ProjectProvisioning, "", input.Visibility
	if provider == ProviderLocal {
		status, defaultBranch, visibility = ProjectReady, "main", "private"
	}
	v, err := s.store.CreateProject(ctx, Project{
		ID: uuid.NewString(), TenantID: id.TenantID, OwnerUserID: id.UserID,
		Name: input.Name, Description: input.Description, RuntimeID: input.RuntimeID,
		RepositoryMode: input.Mode, RepositoryProvider: provider,
		RepositoryExternalID: input.RepositoryID, RepositoryName: repositoryName,
		Visibility: visibility, DefaultBranch: defaultBranch,
		Status: status, ProvisionRequestID: input.ClientRequestID,
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
	if provider == ProviderLocal {
		return v, nil
	}
	var repo Repository
	var connection Connection
	var token string
	var github *githubClient
	token, connection, github, err = s.readyConnection(ctx, id)
	if err == nil && input.Mode == "create" {
		repo, err = github.createRepository(ctx, token, input.RepositoryName, input.Description, input.Visibility == "private")
	} else if err == nil {
		repo, err = github.repository(ctx, token, input.RepositoryID)
	}
	if err != nil {
		if provider == ProviderGitHub && isDefinitiveGitHubError(err) {
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
	token, _, github, readyErr := s.readyConnection(ctx, id)
	if readyErr != nil {
		return v, nil
	}
	if installErr := s.ensureInstalledRepository(ctx, github, token, connection, repo.ID); installErr != nil {
		if !errors.Is(installErr, ErrNotFound) {
			return v, nil
		}
		failed, failErr := s.store.FailProject(ctx, id, v.ID, projectErrorCode(ErrRepositoryNotInstalled), s.now())
		if failErr != nil {
			return Project{}, failErr
		}
		return failed, nil
	}
	repo = github.repositoryWarnings(ctx, token, repo)
	return s.store.CompleteProject(ctx, id, v.ID, repo, connection.InstallationID, s.now())
}

func (s *Service) Retry(ctx context.Context, id Identity, projectID string) (Project, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return Project{}, ErrInvalidArgument
	}
	v, err := s.store.GetProject(ctx, id, projectID)
	if err != nil {
		return Project{}, err
	}
	if v.Status == ProjectArchived {
		return v, nil
	}
	if v.Status == ProjectReady {
		if v.RepositoryProvider != ProviderGitHub {
			return v, nil
		}
		v, _, _, _, err = s.currentProjectInstallation(ctx, id, v)
		return v, err
	}
	if v.RepositoryProvider == ProviderLocal {
		return v, nil
	}
	token, c, github, err := s.readyConnection(ctx, id)
	if err != nil {
		return Project{}, err
	}
	var repo Repository
	createdInRetry := false
	if v.RepositoryMode == "create" {
		v, repo, createdInRetry, err = s.retryCreateRepository(
			ctx, id, v, token, c.ExternalLogin, github,
		)
	} else if v.RepositoryMode == "import" && v.RepositoryExternalID > 0 {
		repo, err = github.repository(ctx, token, v.RepositoryExternalID)
	} else {
		return Project{}, ErrInvalidArgument
	}
	if err != nil {
		return Project{}, err
	}
	if repo.OwnerID != c.ExternalUserID || (v.RepositoryMode == "create" && !createdInRetry &&
		(repo.CreatedAt.Before(v.ProvisionStartedAt.Add(-2*time.Minute)) ||
			repo.CreatedAt.After(v.ProvisionStartedAt.Add(2*time.Minute)))) {
		return Project{}, ErrConflict
	}
	if err := s.validateRepository(repo, c); err != nil {
		return Project{}, err
	}
	if err := s.ensureInstalledRepository(ctx, github, token, c, repo.ID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Project{}, ErrRepositoryNotInstalled
		}
		return Project{}, err
	}
	repo = github.repositoryWarnings(ctx, token, repo)
	return s.store.CompleteProject(ctx, id, v.ID, repo, c.InstallationID, s.now())
}

func (s *Service) retryCreateRepository(
	ctx context.Context,
	id Identity,
	value Project,
	token string,
	owner string,
	github *githubClient,
) (Project, Repository, bool, error) {
	repo, err := github.repositoryByName(ctx, token, owner, value.RepositoryName)
	if !githubStatus(err, http.StatusNotFound) {
		return value, repo, false, err
	}
	value, err = s.store.RefreshProjectProvisionAttempt(ctx, id, value.ID, s.now())
	if err != nil {
		return Project{}, Repository{}, false, err
	}
	repo, err = github.createRepository(
		ctx, token, value.RepositoryName, value.Description, value.Visibility == "private",
	)
	return value, repo, err == nil, err
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

func (s *Service) PrepareLocalPublish(
	ctx context.Context,
	id Identity,
	projectID string,
	input PublishInput,
) (PublishPreparation, error) {
	if !s.GitHubConnectorEnabled() {
		return PublishPreparation{}, ErrDisabled
	}
	if _, err := uuid.Parse(projectID); err != nil {
		return PublishPreparation{}, ErrInvalidArgument
	}
	input.RepositoryName = strings.TrimSpace(input.RepositoryName)
	input.Visibility = strings.TrimSpace(input.Visibility)
	if input.ExpectedVersion <= 0 || (input.Visibility != "private" && input.Visibility != "public") ||
		!validRepositoryName(input.RepositoryName) {
		return PublishPreparation{}, ErrInvalidArgument
	}
	value, err := s.store.GetProject(ctx, id, projectID)
	if err != nil {
		return PublishPreparation{}, err
	}
	if value.Version != input.ExpectedVersion {
		return PublishPreparation{}, ErrVersionConflict
	}
	if value.Status != ProjectReady || value.RepositoryProvider != ProviderLocal ||
		value.RepositoryMode != "empty" || value.PrimaryConversationID == "" ||
		value.GitHubPublishStatus == "published" {
		return PublishPreparation{}, ErrConflict
	}
	userToken, connection, github, err := s.readyConnection(ctx, id)
	if err != nil {
		return PublishPreparation{}, err
	}
	var repo Repository
	if value.RepositoryExternalID == 0 {
		newIntent := value.GitHubPublishStatus == "unpublished"
		createdInRequest := false
		if !newIntent && (value.GitHubPublishStatus != "pending" ||
			!strings.EqualFold(value.RepositoryName, input.RepositoryName) ||
			value.Visibility != input.Visibility) {
			return PublishPreparation{}, ErrConflict
		}
		if newIntent {
			if _, lookupErr := github.repositoryByName(ctx, userToken, connection.ExternalLogin,
				input.RepositoryName); lookupErr == nil {
				return PublishPreparation{}, ErrConflict
			} else if !githubStatus(lookupErr, http.StatusNotFound) {
				return PublishPreparation{}, lookupErr
			}
			value, err = s.store.BeginLocalProjectPublishIntent(ctx, id, value.ID, value.Version,
				input.RepositoryName, input.Visibility, s.now())
			if err != nil {
				return PublishPreparation{}, err
			}
		}
		if newIntent {
			repo, err = github.createEmptyRepository(ctx, userToken, value.RepositoryName,
				value.Description, value.Visibility == "private")
			createdInRequest = err == nil
		} else {
			repo, err = github.repositoryByName(ctx, userToken, connection.ExternalLogin,
				value.RepositoryName)
			if githubStatus(err, http.StatusNotFound) {
				repo, err = github.createEmptyRepository(ctx, userToken, value.RepositoryName,
					value.Description, value.Visibility == "private")
				createdInRequest = err == nil
			}
		}
		if err != nil {
			if isDefinitiveGitHubError(err) {
				_, _ = s.store.CancelLocalProjectPublishIntent(ctx, id, value.ID, value.Version, s.now())
			}
			return PublishPreparation{}, err
		}
		if !createdInRequest && !repositoryCreatedNear(repo, value.ProvisionStartedAt) {
			_, _ = s.store.CancelLocalProjectPublishIntent(ctx, id, value.ID, value.Version, s.now())
			return PublishPreparation{}, ErrConflict
		}
		if err := s.validatePublishRepository(repo, connection); err != nil {
			return PublishPreparation{}, err
		}
		repo.DefaultBranch = "main"
		value, err = s.store.BindLocalProjectPublishRepository(ctx, id, value.ID, value.Version,
			repo, connection.InstallationID, s.now())
		if err != nil {
			return PublishPreparation{}, err
		}
	} else {
		repo, err = github.repository(ctx, userToken, value.RepositoryExternalID)
		if err != nil {
			return PublishPreparation{}, err
		}
		if err := s.validatePublishRepository(repo, connection); err != nil {
			return PublishPreparation{}, err
		}
	}
	if err := s.ensureInstalledRepository(ctx, github, userToken, connection, repo.ID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return PublishPreparation{}, ErrRepositoryNotInstalled
		}
		return PublishPreparation{}, err
	}
	token, _, err := github.installationToken(ctx, connection.InstallationID, repo.ID,
		map[string]string{"contents": "write"})
	if err != nil {
		return PublishPreparation{}, err
	}
	workspace, _, err := s.store.GetWorkspace(ctx, id, value.PrimaryConversationID)
	if err != nil {
		_ = github.revokeInstallationToken(ctx, token)
		return PublishPreparation{}, err
	}
	return PublishPreparation{
		Project: value, Workspace: workspace, Repository: repo,
		CloneURL: "https://github.com/" + repo.Owner + "/" + repo.Name + ".git",
		Token:    token, InstallationID: connection.InstallationID,
	}, nil
}

func (s *Service) CompleteLocalPublish(
	ctx context.Context,
	id Identity,
	preparation PublishPreparation,
) (Project, error) {
	return s.store.CompleteLocalProjectPublish(ctx, id, preparation.Project.ID,
		preparation.Project.Version, s.now())
}

func (s *Service) RevokeGitHubToken(ctx context.Context, id Identity, token string) {
	if strings.TrimSpace(token) == "" {
		return
	}
	registration, err := s.store.GetAppRegistration(ctx, id)
	if err != nil {
		return
	}
	github, err := s.githubForRegistration(id, registration)
	if err == nil {
		_ = github.revokeInstallationToken(ctx, token)
	}
}

func (s *Service) Archive(ctx context.Context, id Identity, projectID string, expected int64) (Project, error) {
	if _, err := uuid.Parse(projectID); err != nil || expected <= 0 {
		return Project{}, ErrInvalidArgument
	}
	value, err := s.store.GetProject(ctx, id, projectID)
	if err != nil {
		return Project{}, err
	}
	if value.Version != expected {
		return Project{}, ErrConflict
	}
	if value.RepositoryExternalID > 0 {
		if err := s.revokeProjectTokenLeases(ctx, id, projectID); err != nil && !errors.Is(err, ErrNotFound) {
			return Project{}, err
		}
	}
	return s.store.ArchiveProject(ctx, id, projectID, expected, s.now())
}

func (s *Service) Tasks(ctx context.Context, id Identity, projectID string) ([]Task, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return nil, ErrInvalidArgument
	}
	return s.store.ListTasks(ctx, id, projectID)
}

func (s *Service) Branches(
	ctx context.Context,
	id Identity,
	projectID string,
	cursor string,
) (BranchPage, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return BranchPage{}, ErrInvalidArgument
	}
	value, err := s.store.GetProject(ctx, id, projectID)
	if err != nil {
		return BranchPage{}, err
	}
	if value.Status != ProjectReady {
		return BranchPage{}, ErrProjectNotReady
	}
	if value.RepositoryProvider == ProviderLocal {
		if !s.LocalProjectsEnabled() {
			return BranchPage{}, ErrLocalProjectsDisabled
		}
		return BranchPage{Branches: []Branch{{
			Name: "main", Default: true,
		}}}, nil
	}
	page, err := decodeCursor(cursor)
	if err != nil {
		return BranchPage{}, ErrInvalidArgument
	}
	value, _, connection, github, err := s.currentProjectInstallation(ctx, id, value)
	if err != nil {
		return BranchPage{}, err
	}
	token, _, err := github.installationToken(ctx, connection.InstallationID,
		value.RepositoryExternalID, map[string]string{"contents": "read"})
	if err != nil {
		return BranchPage{}, err
	}
	defer func() { _ = github.revokeInstallationToken(context.WithoutCancel(ctx), token) }()
	branches, more, err := github.branches(ctx, token, value.RepositoryOwner, value.RepositoryName, page)
	if err != nil {
		return BranchPage{}, err
	}
	for index := range branches {
		branches[index].Default = branches[index].Name == value.DefaultBranch
	}
	result := BranchPage{Branches: branches}
	if more {
		result.NextCursor = encodeCursor(page + 1)
	}
	return result, nil
}

func (s *Service) PrepareTaskBase(
	ctx context.Context,
	id Identity,
	projectID string,
	conversationID string,
	requestedRef string,
) (TaskBase, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return TaskBase{}, ErrInvalidArgument
	}
	requestedRef = strings.TrimSpace(requestedRef)
	if !validBaseRef(requestedRef) {
		return TaskBase{}, ErrInvalidArgument
	}
	if workspace, value, err := s.store.GetWorkspace(ctx, id, conversationID); err == nil {
		if value.ID != projectID {
			return TaskBase{}, ErrConflict
		}
		baseRef := strings.TrimSpace(workspace.BaseRef)
		if baseRef == "" {
			baseRef = value.DefaultBranch
		}
		if requestedRef != "" && requestedRef != baseRef {
			return TaskBase{}, ErrBaseRefMismatch
		}
		if value.Status != ProjectReady {
			return TaskBase{}, ErrProjectNotReady
		}
		if value.RepositoryProvider == ProviderLocal {
			if !s.LocalProjectsEnabled() {
				return TaskBase{}, ErrLocalProjectsDisabled
			}
		} else {
			var connectionErr error
			value, _, _, _, connectionErr = s.currentProjectInstallation(ctx, id, value)
			if connectionErr != nil {
				return TaskBase{}, connectionErr
			}
		}
		return TaskBase{Project: value, Ref: baseRef, SHA: workspace.BaseSHA}, nil
	} else if !errors.Is(err, ErrNotFound) {
		return TaskBase{}, err
	}
	value, err := s.store.GetProject(ctx, id, projectID)
	if err != nil {
		return TaskBase{}, err
	}
	if value.Status != ProjectReady {
		return TaskBase{}, ErrProjectNotReady
	}
	baseRef := requestedRef
	if baseRef == "" {
		baseRef = value.DefaultBranch
	}
	if value.RepositoryProvider == ProviderLocal {
		if !s.LocalProjectsEnabled() {
			return TaskBase{}, ErrLocalProjectsDisabled
		}
		if baseRef != "main" {
			return TaskBase{}, ErrBaseRefNotFound
		}
		return TaskBase{Project: value, Ref: "main"}, nil
	}
	value, _, connection, github, err := s.currentProjectInstallation(ctx, id, value)
	if err != nil {
		return TaskBase{}, err
	}
	token, _, err := github.installationToken(ctx, connection.InstallationID,
		value.RepositoryExternalID, map[string]string{"contents": "read"})
	if err != nil {
		return TaskBase{}, err
	}
	defer func() { _ = github.revokeInstallationToken(context.WithoutCancel(ctx), token) }()
	sha, err := github.branchSHA(ctx, token, value.RepositoryOwner, value.RepositoryName, baseRef)
	if githubStatus(err, http.StatusNotFound) {
		return TaskBase{}, ErrBaseRefNotFound
	}
	if err != nil {
		return TaskBase{}, err
	}
	return TaskBase{Project: value, Ref: baseRef, SHA: sha}, nil
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
	if v.RepositoryProvider == ProviderLocal {
		if !s.LocalProjectsEnabled() {
			return Project{}, ErrLocalProjectsDisabled
		}
		return v, nil
	}
	v, _, _, _, err = s.currentProjectInstallation(ctx, id, v)
	return v, err
}

func (s *Service) Workspace(ctx context.Context, id Identity, conversationID string) (Workspace, Project, error) {
	return s.store.GetWorkspace(ctx, id, conversationID)
}

func (s *Service) ProjectContext(ctx context.Context, id Identity, conversationID string) (ProjectContext, string, error) {
	w, v, err := s.store.GetWorkspace(ctx, id, conversationID)
	if err != nil {
		return ProjectContext{}, "", err
	}
	if v.Status != ProjectReady {
		return ProjectContext{}, "", ErrProjectNotReady
	}
	gitAuthorName, gitAuthorEmail := gitAuthorIdentity(id)
	if v.RepositoryProvider == ProviderLocal {
		if !s.LocalProjectsEnabled() {
			return ProjectContext{}, "", ErrLocalProjectsDisabled
		}
		repositoryID := int64(0)
		installationID := int64(0)
		repositoryFullName := ""
		credentialMode := "none"
		if v.RepositoryExternalID > 0 && v.GitHubPublishStatus == "published" {
			repositoryID = v.RepositoryExternalID
			installationID = v.InstallationID
			repositoryFullName = strings.Trim(v.RepositoryOwner+"/"+v.RepositoryName, "/")
			credentialMode = "broker"
		}
		return ProjectContext{
			ProjectID: v.ID, RepositoryExternalID: repositoryID,
			DefaultBranch: "main", BaseRef: "main", BaseSHA: w.BaseSHA, BranchName: "main",
			GitAuthorName: gitAuthorName, GitAuthorEmail: gitAuthorEmail,
			InstallationID: installationID, RepositoryProvider: ProviderLocal,
			RepositoryFullName: repositoryFullName,
			CredentialMode:     credentialMode,
		}, "", nil
	}
	v, _, connection, github, err := s.currentProjectInstallation(ctx, id, v)
	if err != nil {
		return ProjectContext{}, "", err
	}
	installationID := connection.InstallationID
	cloneURL := "https://github.com/" + v.RepositoryOwner + "/" + v.RepositoryName + ".git"
	baseRef := strings.TrimSpace(w.BaseRef)
	if baseRef == "" {
		baseRef = v.DefaultBranch
	}
	token, _, err := github.installationToken(ctx, v.InstallationID, v.RepositoryExternalID,
		map[string]string{"contents": "read"})
	if err != nil {
		return ProjectContext{}, "", err
	}
	if w.BaseSHA == "" {
		var sha string
		var branchErr error
		sha, branchErr = github.branchSHA(ctx, token, v.RepositoryOwner, v.RepositoryName, baseRef)
		if branchErr != nil {
			_ = github.revokeInstallationToken(ctx, token)
			return ProjectContext{}, "", branchErr
		}
		w, err = s.store.LockBaseSHA(ctx, id, conversationID, sha, s.now())
		if err != nil {
			_ = github.revokeInstallationToken(ctx, token)
			return ProjectContext{}, "", err
		}
	}
	return ProjectContext{
		ProjectID: v.ID, RepositoryExternalID: v.RepositoryExternalID,
		CloneURL:      cloneURL,
		DefaultBranch: v.DefaultBranch, BaseRef: baseRef, BaseSHA: w.BaseSHA, BranchName: w.BranchName,
		GitAuthorName: gitAuthorName, GitAuthorEmail: gitAuthorEmail,
		InstallationID: installationID, RepositoryProvider: v.RepositoryProvider,
		RepositoryFullName: v.RepositoryOwner + "/" + v.RepositoryName,
		CredentialMode:     "ephemeral",
	}, token, nil
}

func validBaseRef(value string) bool {
	if value == "" {
		return true
	}
	return len(value) <= 1024 && !strings.ContainsAny(value, "\x00\r\n")
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
	if len(snapshot.Commits) > 50 {
		snapshot.Commits, snapshot.HistoryTruncated = snapshot.Commits[:50], true
	}
	for index := range snapshot.Commits {
		snapshot.Commits[index].Body = ""
		if len(snapshot.Commits[index].Parents) > 16 {
			snapshot.Commits[index].Parents = snapshot.Commits[index].Parents[:16]
		}
		if len(snapshot.Commits[index].Refs) > 20 {
			snapshot.Commits[index].Refs = snapshot.Commits[index].Refs[:20]
		}
	}
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = s.now()
	}
	return s.store.SaveSnapshot(ctx, id, conversationID, snapshot, headSHA, status, snapshot.CapturedAt)
}

func (s *Service) MarkBootstrapFailed(ctx context.Context, id Identity, conversationID, code string) error {
	return s.store.MarkBootstrapFailed(ctx, id, conversationID, code, s.now())
}

func (s *Service) readyConnection(ctx context.Context, id Identity) (string, Connection, *githubClient, error) {
	registration, err := s.store.GetAppRegistration(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return "", Connection{}, nil, ErrConnectionRequired
	}
	if err != nil {
		return "", Connection{}, nil, err
	}
	github, err := s.githubForRegistration(id, registration)
	if err != nil {
		return "", Connection{}, nil, err
	}
	c, err := s.store.GetConnection(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return "", Connection{}, nil, ErrConnectionRequired
	}
	if err != nil {
		return "", Connection{}, nil, err
	}
	if c.Status == ConnectionReauthorization || c.RegistrationID != registration.ID {
		return "", Connection{}, nil, ErrConnectionRequired
	}
	token, err := s.userToken(ctx, id, github)
	if err != nil {
		return "", Connection{}, nil, ErrConnectionRequired
	}
	c, err = s.store.GetConnection(ctx, id)
	if err != nil {
		return "", Connection{}, nil, err
	}
	installation, err := s.personalInstallation(ctx, github, token, c.ExternalUserID)
	if err != nil {
		return "", Connection{}, nil, err
	}
	if installation.ID != c.InstallationID || c.Status != ConnectionReady {
		c.InstallationID, c.Status, c.UpdatedAt = installation.ID, ConnectionReady, s.now()
		c, err = s.store.UpsertConnection(ctx, c)
		if err != nil {
			return "", Connection{}, nil, err
		}
	}
	return token, c, github, nil
}

func (s *Service) currentProjectInstallation(
	ctx context.Context,
	id Identity,
	value Project,
) (Project, string, Connection, *githubClient, error) {
	token, connection, github, err := s.readyConnection(ctx, id)
	if err != nil {
		return Project{}, "", Connection{}, nil, err
	}
	if value.InstallationID == connection.InstallationID {
		return value, token, connection, github, nil
	}
	repo, err := github.repository(ctx, token, value.RepositoryExternalID)
	if err != nil {
		return Project{}, "", Connection{}, nil, err
	}
	if err := s.validatePublishRepository(repo, connection); err != nil {
		return Project{}, "", Connection{}, nil, err
	}
	if err := s.ensureInstalledRepository(ctx, github, token, connection, repo.ID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Project{}, "", Connection{}, nil, ErrRepositoryNotInstalled
		}
		return Project{}, "", Connection{}, nil, err
	}
	value, err = s.store.RebindProjectInstallation(ctx, id, value.ID, repo.ID,
		connection.InstallationID, s.now())
	if err != nil {
		return Project{}, "", Connection{}, nil, err
	}
	return value, token, connection, github, nil
}

func (s *Service) userToken(ctx context.Context, id Identity, github *githubClient) (string, error) {
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
		newToken, err := github.refresh(ctx, refreshToken)
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

func (s *Service) personalInstallation(ctx context.Context, github *githubClient, token string, userID int64) (githubInstallation, error) {
	installations, err := github.installations(ctx, token)
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

func (s *Service) connectionView(github *githubClient, c Connection) ConnectionView {
	view := ConnectionView{Status: c.Status, ExternalLogin: c.ExternalLogin, Enabled: true}
	if c.Status == ConnectionInstallationRequired {
		view.InstallationURL = github.installationURL()
	}
	if c.Status == ConnectionReauthorization {
		view.ReauthorizationURL = "/projects/new"
	}
	return view
}

func (s *Service) registrationView(github *githubClient, registration AppRegistration) ConnectionView {
	view := ConnectionView{Status: registration.Status, Enabled: true}
	if registration.Status == RegistrationAppCreated || registration.Status == RegistrationInstallRequired {
		view.Status = RegistrationInstallRequired
		view.InstallationURL = github.installationURL()
	}
	return view
}

func (s *Service) githubForRegistration(id Identity, registration AppRegistration) (*githubClient, error) {
	if s == nil || s.box == nil {
		return nil, ErrDisabled
	}
	clientSecret, err := s.box.decrypt(registration.ClientSecretCiphertext,
		registrationAAD(id, registration.ID, "client_secret"))
	if err != nil {
		return nil, err
	}
	privateKey, err := s.box.decrypt(registration.PrivateKeyCiphertext,
		registrationAAD(id, registration.ID, "private_key"))
	if err != nil {
		return nil, err
	}
	return newGitHubClient(githubClientConfig{
		AppID: registration.AppID, AppSlug: registration.AppSlug, ClientID: registration.ClientID,
		ClientSecret: clientSecret, PrivateKey: privateKey,
		CallbackURL: strings.TrimRight(registration.PublicOrigin, "/") + "/connectors/github/oauth/callback",
		HTTPClient:  s.http,
	})
}

func parsePublicOrigins(value string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, raw := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" {
			continue
		}
		result[parsed.Scheme+"://"+parsed.Host] = struct{}{}
	}
	return result
}

func (s *Service) allowedOrigin(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return "", ErrInvalidArgument
	}
	origin := parsed.Scheme + "://" + parsed.Host
	if _, ok := s.publicOrigins[origin]; !ok {
		return "", ErrInvalidArgument
	}
	return origin, nil
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

func (s *Service) validatePublishRepository(repo Repository, c Connection) error {
	if repo.ID <= 0 || repo.OwnerID != c.ExternalUserID || !strings.EqualFold(repo.Owner, c.ExternalLogin) {
		return ErrInvalidArgument
	}
	if repo.SizeKB > s.maxKB {
		return ErrRepositoryTooLarge
	}
	return nil
}

func (s *Service) ensureInstalledRepository(ctx context.Context, github *githubClient, token string, c Connection, repositoryID int64) error {
	for page := 1; page <= 100; page++ {
		repositories, more, err := github.repositories(ctx, token, c.InstallationID, page)
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
	input.Source.Type = strings.TrimSpace(input.Source.Type)
	input.Source.RepositoryName = strings.TrimSpace(input.Source.RepositoryName)
	input.Source.Visibility = strings.TrimSpace(input.Source.Visibility)
	if input.Source.Type != "" {
		switch input.Source.Type {
		case "empty":
			input.Mode, input.RepositoryName, input.RepositoryID, input.Visibility = "empty", "", 0, "private"
		case "github_create":
			input.Mode = "create"
			input.RepositoryName, input.RepositoryID = input.Source.RepositoryName, 0
			input.Visibility = input.Source.Visibility
		case "github_import":
			input.Mode = "import"
			input.RepositoryName, input.RepositoryID = input.Source.RepositoryName, input.Source.RepositoryID
			input.Visibility = input.Source.Visibility
		}
	}
	if input.RuntimeID == "" {
		input.RuntimeID = "claude-code"
	}
	if input.Visibility == "" && input.Mode != "empty" {
		input.Visibility = "private"
	}
	if input.Mode == "empty" {
		input.Visibility = "private"
	}
	return input
}

func validateCreate(input CreateInput) error {
	if input.ClientRequestID == "" || len(input.ClientRequestID) > 128 || input.Name == "" || len(input.Name) > 100 ||
		len(input.Description) > 500 || len(input.RepositoryName) > 100 ||
		(input.Mode != "empty" && input.Mode != "create" && input.Mode != "import") ||
		(input.Visibility != "private" && input.Visibility != "public") || input.RuntimeID == "" {
		return ErrInvalidArgument
	}
	if input.Mode == "empty" {
		if input.RepositoryName != "" || input.RepositoryID != 0 || input.Visibility != "private" {
			return ErrInvalidArgument
		}
		return nil
	}
	if input.RepositoryName == "" {
		return ErrInvalidArgument
	}
	if input.Mode == "import" && input.RepositoryID <= 0 {
		return ErrInvalidArgument
	}
	if input.Mode == "create" && input.RepositoryID != 0 {
		return ErrInvalidArgument
	}
	if !validRepositoryName(input.RepositoryName) {
		return ErrInvalidArgument
	}
	return nil
}

func validRepositoryName(value string) bool {
	if value == "" || len(value) > 100 {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
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

func githubStatus(err error, status int) bool {
	var httpErr *githubHTTPError
	return errors.As(err, &httpErr) && httpErr.Status == status
}

func repositoryCreatedNear(repo Repository, startedAt time.Time) bool {
	return !repo.CreatedAt.IsZero() && !startedAt.IsZero() &&
		!repo.CreatedAt.Before(startedAt.Add(-2*time.Minute)) &&
		!repo.CreatedAt.After(startedAt.Add(2*time.Minute))
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
