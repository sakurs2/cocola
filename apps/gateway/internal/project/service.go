package project

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const projectOperationStaleAfter = 15 * time.Minute

const (
	taskBranchPrefix    = "cocola/task-"
	taskBranchSuffixMax = 48
)

type Config struct {
	SecretKey               string
	PublicOrigins           string
	MaxRepositoryMB         int64
	HTTPClient              *http.Client
	DisableLocalProjects    bool
	DisableGitHubConnector  bool
	DisableGitHubAgentWrite bool
	ForgejoAPIURL           string
	ForgejoCloneURL         string
	ForgejoUsername         string
	ForgejoPassword         string
}

func (c Config) validate() error {
	if c.MaxRepositoryMB <= 0 {
		return errors.New("COCOLA_PROJECT_MAX_REPOSITORY_MB must be positive")
	}
	localConfigured := strings.TrimSpace(c.ForgejoAPIURL) != "" ||
		strings.TrimSpace(c.ForgejoCloneURL) != "" || strings.TrimSpace(c.ForgejoUsername) != "" ||
		strings.TrimSpace(c.ForgejoPassword) != ""
	if strings.TrimSpace(c.SecretKey) == "" &&
		(!c.DisableGitHubConnector || !c.DisableGitHubAgentWrite ||
			(!c.DisableLocalProjects && localConfigured)) {
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
	forgejo                 *forgejoClient
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
	var forgejo *forgejoClient
	if !cfg.DisableLocalProjects {
		var forgejoErr error
		forgejo, forgejoErr = newForgejoClient(client, cfg.ForgejoAPIURL, cfg.ForgejoCloneURL,
			cfg.ForgejoUsername, cfg.ForgejoPassword)
		if forgejoErr != nil && !errors.Is(forgejoErr, ErrInternalSCMUnavailable) {
			return nil, forgejoErr
		}
	}
	s := &Service{
		store: store, box: box, http: client, maxKB: cfg.MaxRepositoryMB * 1024,
		publicOrigins:           parsePublicOrigins(cfg.PublicOrigins),
		now:                     func() time.Time { return time.Now().UTC() },
		githubConnectorEnabled:  !cfg.DisableGitHubConnector,
		githubAgentWriteEnabled: !cfg.DisableGitHubAgentWrite,
		localProjectsEnabled:    !cfg.DisableLocalProjects && forgejo != nil,
		forgejo:                 forgejo,
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
		"default_permissions": map[string]string{
			"actions": "write", "administration": "write", "checks": "write",
			"contents": "write", "deployments": "write", "environments": "write",
			"issues": "write", "metadata": "read", "packages": "write",
			"pages": "write", "pull_requests": "write", "repository_hooks": "write",
			"secret_scanning_alerts": "write", "secrets": "write",
			"security_events": "write", "statuses": "write", "vulnerability_alerts": "write",
			"actions_variables": "write", "workflows": "write",
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
	projectID := uuid.NewString()
	provisionAttemptID := uuid.NewString()
	repositoryName := input.RepositoryName
	status, defaultBranch, visibility := ProjectProvisioning, "", input.Visibility
	if provider == ProviderLocal {
		status, defaultBranch, visibility = ProjectProvisioning, "main", "private"
		repositoryName = "p-" + strings.ReplaceAll(projectID, "-", "")
	}
	v, err := s.store.CreateProject(ctx, Project{
		ID: projectID, TenantID: id.TenantID, OwnerUserID: id.UserID,
		Name: input.Name, Description: input.Description, RuntimeID: input.RuntimeID,
		RepositoryMode: input.Mode, RepositoryProvider: provider,
		RepositoryExternalID: input.RepositoryID, RepositoryName: repositoryName,
		Visibility: visibility, DefaultBranch: defaultBranch,
		Status: status, ProvisionRequestID: input.ClientRequestID,
		ProvisionAttemptID: provisionAttemptID, ProvisionStartedAt: now,
		ProvisionAttemptStartedAt: now, CreatedAt: now, UpdatedAt: now,
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
		return s.provisionLocalProject(ctx, id, v)
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
		failed, failErr := s.failProvisionAttempt(ctx, id, v, githubErrorCode(err))
		if failErr == nil {
			return failed, nil
		}
		return Project{}, failErr
	}
	if err := s.validateRepository(repo, connection); err != nil {
		failed, failErr := s.failProvisionAttempt(ctx, id, v, projectErrorCode(err))
		if failErr != nil {
			return Project{}, failErr
		}
		return failed, nil
	}
	if installErr := s.ensureInstalledRepository(ctx, github, token, connection, repo.ID); installErr != nil {
		code := githubErrorCode(installErr)
		if errors.Is(installErr, ErrNotFound) {
			code = projectErrorCode(ErrRepositoryNotInstalled)
		}
		failed, failErr := s.failProvisionAttempt(ctx, id, v, code)
		if failErr != nil {
			return Project{}, failErr
		}
		return failed, nil
	}
	repo = github.repositoryWarnings(ctx, token, repo)
	completeContext, completeCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer completeCancel()
	return s.store.CompleteProject(
		completeContext, id, v.ID, v.ProvisionAttemptID, repo, connection.InstallationID, s.now(),
	)
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
	if v.Status == ProjectArchiving || v.Status == ProjectArchiveFailed {
		return Project{}, ErrConflict
	}
	v, err = s.claimProvisionAttempt(ctx, id, v)
	if err != nil {
		return Project{}, err
	}
	if v.Status == ProjectReady || v.Status == ProjectArchived {
		return v, nil
	}
	if v.RepositoryProvider == ProviderLocal {
		if !s.LocalProjectsEnabled() {
			return s.failProvisionAttempt(ctx, id, v, "INTERNAL_SCM_UNAVAILABLE")
		}
		return s.provisionLocalProject(ctx, id, v)
	}
	token, c, github, err := s.readyConnection(ctx, id)
	if err != nil {
		return s.failProvisionAttempt(ctx, id, v, githubErrorCode(err))
	}
	var repo Repository
	createdInRetry := false
	if v.RepositoryMode == "create" {
		repo, createdInRetry, err = s.retryCreateRepository(ctx, v, token, c.ExternalLogin, github)
	} else if v.RepositoryMode == "import" && v.RepositoryExternalID > 0 {
		repo, err = github.repository(ctx, token, v.RepositoryExternalID)
	} else {
		return s.failProvisionAttempt(ctx, id, v, "REPOSITORY_CONFIGURATION_INVALID")
	}
	if err != nil {
		return s.failProvisionAttempt(ctx, id, v, githubErrorCode(err))
	}
	if repo.OwnerID != c.ExternalUserID ||
		(v.RepositoryMode == "create" && !createdInRetry &&
			!repositoryCreatedNear(repo, v.ProvisionStartedAt)) {
		return s.failProvisionAttempt(ctx, id, v, "REPOSITORY_RECONCILIATION_CONFLICT")
	}
	if err := s.validateRepository(repo, c); err != nil {
		return s.failProvisionAttempt(ctx, id, v, projectErrorCode(err))
	}
	if err := s.ensureInstalledRepository(ctx, github, token, c, repo.ID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return s.failProvisionAttempt(ctx, id, v, projectErrorCode(ErrRepositoryNotInstalled))
		}
		return s.failProvisionAttempt(ctx, id, v, githubErrorCode(err))
	}
	repo = github.repositoryWarnings(ctx, token, repo)
	completeContext, completeCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer completeCancel()
	return s.store.CompleteProject(
		completeContext, id, v.ID, v.ProvisionAttemptID, repo, c.InstallationID, s.now(),
	)
}

func (s *Service) claimProvisionAttempt(
	ctx context.Context,
	id Identity,
	value Project,
) (Project, error) {
	now := s.now()
	claimed, err := s.store.ClaimProjectProvisionAttempt(
		ctx, id, value.ID, uuid.NewString(), now, now.Add(-projectOperationStaleAfter),
	)
	if !errors.Is(err, ErrNotFound) {
		return claimed, err
	}
	current, getErr := s.store.GetProject(ctx, id, value.ID)
	if getErr != nil {
		return Project{}, getErr
	}
	if current.Status == ProjectReady || current.Status == ProjectArchived {
		return current, nil
	}
	return Project{}, ErrConflict
}

func (s *Service) failProvisionAttempt(
	ctx context.Context,
	id Identity,
	value Project,
	code string,
) (Project, error) {
	failContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	failed, err := s.store.FailProject(
		failContext, id, value.ID, value.ProvisionAttemptID, code, s.now(),
	)
	if !errors.Is(err, ErrNotFound) {
		return failed, err
	}
	return s.store.GetProject(failContext, id, value.ID)
}

func (s *Service) provisionLocalProject(ctx context.Context, id Identity, value Project) (Project, error) {
	repo, err := s.forgejo.createRepository(ctx, value.ID, value.Description)
	if err != nil {
		return s.failLocalProject(ctx, id, value, "INTERNAL_SCM_PROVISION_FAILED")
	}
	tokenName := "cocola-project-" + value.ID
	if err := s.forgejo.deleteRepositoryTokensByName(ctx, tokenName); err != nil {
		return s.failLocalProject(ctx, id, value, "INTERNAL_SCM_TOKEN_FAILED")
	}
	tokenID, token, err := s.forgejo.createRepositoryToken(ctx, repo, value.ID)
	if err != nil {
		return s.failLocalProject(ctx, id, value, "INTERNAL_SCM_TOKEN_FAILED")
	}
	cleanupToken := true
	defer func() {
		if cleanupToken {
			_ = s.forgejo.deleteRepositoryToken(context.WithoutCancel(ctx), tokenID)
		}
	}()
	if _, err = s.forgejo.branchSHA(ctx, token, repo.Owner, repo.Name, "main"); forgejoStatus(err, http.StatusNotFound) {
		if _, err = s.forgejo.initializeRepository(ctx, repo, token); err != nil {
			return s.failLocalProject(ctx, id, value, forgejoInitializationErrorCode(err))
		}
	} else if err != nil {
		return s.failLocalProject(ctx, id, value, "INTERNAL_SCM_INIT_FAILED")
	}
	if err = s.forgejo.protectMain(ctx, repo); err != nil {
		return s.failLocalProject(ctx, id, value, "INTERNAL_SCM_PROTECTION_FAILED")
	}
	ciphertext, err := s.box.encrypt(token, projectTokenAAD(id, value.ID))
	if err != nil {
		return s.failLocalProject(ctx, id, value, "INTERNAL_SCM_CREDENTIAL_ENCRYPT_FAILED")
	}
	completeContext, completeCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer completeCancel()
	completed, err := s.store.CompleteLocalProject(
		completeContext, id, value.ID, value.ProvisionAttemptID,
		repo, tokenID, ciphertext, repo.CloneURL, s.now(),
	)
	if err != nil {
		return Project{}, err
	}
	cleanupToken = false
	return completed, nil
}

func forgejoInitializationErrorCode(err error) string {
	var gitErr *forgejoGitError
	if !errors.As(err, &gitErr) {
		return "INTERNAL_SCM_INIT_FAILED"
	}
	stage := strings.ToUpper(strings.ReplaceAll(gitErr.Stage, "-", "_"))
	return "INTERNAL_SCM_INIT_" + stage + "_FAILED"
}

func (s *Service) failLocalProject(
	ctx context.Context,
	id Identity,
	value Project,
	code string,
) (Project, error) {
	return s.failProvisionAttempt(ctx, id, value, code)
}

func (s *Service) retryCreateRepository(
	ctx context.Context,
	value Project,
	token string,
	owner string,
	github *githubClient,
) (Repository, bool, error) {
	repo, err := github.repositoryByName(ctx, token, owner, value.RepositoryName)
	if !githubStatus(err, http.StatusNotFound) {
		return repo, false, err
	}
	repo, err = github.createRepository(
		ctx, token, value.RepositoryName, value.Description, value.Visibility == "private",
	)
	return repo, err == nil, err
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
	if value.Status == ProjectArchived {
		return value, nil
	}
	if value.Version != expected {
		return Project{}, ErrVersionConflict
	}
	if value.Status != ProjectReady && value.Status != ProjectFailed &&
		value.Status != ProjectArchiveFailed && value.Status != ProjectArchiving {
		return Project{}, ErrConflict
	}
	now := s.now()
	attemptID := uuid.NewString()
	value, err = s.store.ClaimProjectArchive(
		ctx, id, projectID, expected, attemptID, now, now.Add(-projectOperationStaleAfter),
	)
	if err != nil {
		return Project{}, err
	}
	operationContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancel()
	if value.RepositoryProvider == ProviderLocal {
		provider := forgejoRepositoryProvider{client: s.forgejo}
		if s.forgejo == nil {
			return s.failArchiveAttempt(operationContext, id, value, "INTERNAL_SCM_UNAVAILABLE")
		}
		if err := provider.ArchiveProject(operationContext, "", value); err != nil {
			return s.failArchiveAttempt(operationContext, id, value, "INTERNAL_SCM_ARCHIVE_FAILED")
		}
		if value.RepositoryTokenID > 0 {
			err = s.forgejo.deleteRepositoryToken(operationContext, value.RepositoryTokenID)
		} else {
			err = s.forgejo.deleteRepositoryTokensByName(
				operationContext, "cocola-project-"+value.ID,
			)
		}
		if err != nil {
			return s.failArchiveAttempt(operationContext, id, value, "INTERNAL_SCM_TOKEN_REVOKE_FAILED")
		}
	}
	if value.RepositoryExternalID > 0 {
		if err := s.revokeProjectTokenLeases(operationContext, id, projectID); err != nil && !errors.Is(err, ErrNotFound) {
			return s.failArchiveAttempt(operationContext, id, value, "SCM_LEASE_REVOKE_FAILED")
		}
	}
	completed, err := s.store.CompleteProjectArchive(
		operationContext, id, projectID, attemptID, s.now(),
	)
	if errors.Is(err, ErrNotFound) {
		return s.store.GetProject(operationContext, id, projectID)
	}
	return completed, err
}

func (s *Service) failArchiveAttempt(
	ctx context.Context,
	id Identity,
	value Project,
	code string,
) (Project, error) {
	failed, err := s.store.FailProjectArchive(
		ctx, id, value.ID, value.ArchiveAttemptID, code, s.now(),
	)
	if errors.Is(err, ErrNotFound) {
		return s.store.GetProject(ctx, id, value.ID)
	}
	return failed, err
}

func (s *Service) Tasks(ctx context.Context, id Identity, projectID string) ([]Task, error) {
	if _, err := uuid.Parse(projectID); err != nil {
		return nil, ErrInvalidArgument
	}
	tasks, err := s.store.ListTasks(ctx, id, projectID)
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s *Service) PrepareChangeRequest(ctx context.Context, id Identity, projectID, conversationID string) (ChangeRequestPreparation, error) {
	workspace, value, err := s.store.GetWorkspace(ctx, id, conversationID)
	if err != nil {
		return ChangeRequestPreparation{}, err
	}
	if value.ID != projectID || value.Status != ProjectReady || workspace.BootstrapStatus != "ready" {
		return ChangeRequestPreparation{}, ErrProjectNotReady
	}
	var existing *ChangeRequest
	if current, existingErr := s.store.GetChangeRequest(ctx, id, conversationID); existingErr == nil {
		if current.Status == "merged" {
			return ChangeRequestPreparation{}, ErrChangeRequestMerged
		}
		existing = &current
	} else if !errors.Is(existingErr, ErrNotFound) {
		return ChangeRequestPreparation{}, existingErr
	}
	permissions := map[string]string{"contents": "read"}
	if value.RepositoryProvider == ProviderGitHub {
		permissions = map[string]string{"contents": "write", "pull_requests": "write"}
	}
	contextValue, token, workspace, value, err := s.projectContextForWorkspace(
		ctx, id, workspace, value, permissions,
	)
	if err != nil {
		return ChangeRequestPreparation{}, err
	}
	cloneURL := contextValue.CloneURL
	return ChangeRequestPreparation{
		Project: value, Workspace: workspace, Context: contextValue,
		Token: token, CloneURL: cloneURL, Existing: existing,
	}, nil
}

func (s *Service) CompleteChangeRequest(ctx context.Context, id Identity, preparation ChangeRequestPreparation, headSHA string) (ChangeRequest, error) {
	if preparation.Existing != nil {
		provider, err := s.repositoryProvider(ctx, id, preparation.Project)
		if err != nil {
			return ChangeRequest{}, err
		}
		return s.refreshChangeRequestWithAccess(
			ctx, id, *preparation.Existing, preparation.Workspace, preparation.Project,
			provider, preparation.Token,
		)
	}
	if len(headSHA) != 40 {
		return ChangeRequest{}, ErrInvalidArgument
	}
	title := "Cocola task " + shortTaskID(preparation.Workspace.ConversationID)
	now := s.now()
	value := ChangeRequest{
		ConversationID: preparation.Workspace.ConversationID, ProjectID: preparation.Project.ID,
		Provider: preparation.Project.RepositoryProvider, Status: "open",
		BaseSHA: preparation.Workspace.BaseSHA, HeadSHA: headSHA, CreatedAt: now, UpdatedAt: now,
	}
	provider, err := s.repositoryProvider(ctx, id, preparation.Project)
	if err != nil {
		return ChangeRequest{}, err
	}
	pull, err := provider.CreateChangeRequest(
		ctx, preparation.Token, preparation.Project, preparation.Workspace, title,
	)
	if err != nil {
		return ChangeRequest{}, err
	}
	value.ExternalNumber, value.ExternalURL = pull.Number, pull.URL
	return s.store.UpsertChangeRequest(ctx, id, value)
}

func (s *Service) RefreshChangeRequest(ctx context.Context, id Identity, projectID, conversationID string) (ChangeRequest, error) {
	value, err := s.store.GetChangeRequest(ctx, id, conversationID)
	if err != nil {
		return ChangeRequest{}, err
	}
	if value.ProjectID != projectID {
		return ChangeRequest{}, ErrNotFound
	}
	workspace, projectValue, err := s.store.GetWorkspace(ctx, id, conversationID)
	if err != nil {
		return ChangeRequest{}, err
	}
	if value.Status == "merged" || value.Status == "closed" {
		return value, nil
	}
	provider, token, release, err := s.changeRequestAccess(ctx, id, projectValue, false)
	if err != nil {
		return ChangeRequest{}, err
	}
	defer release()
	return s.refreshChangeRequestWithAccess(
		ctx, id, value, workspace, projectValue, provider, token,
	)
}

func (s *Service) refreshChangeRequestWithAccess(
	ctx context.Context,
	id Identity,
	value ChangeRequest,
	workspace Workspace,
	projectValue Project,
	provider RepositoryProvider,
	token string,
) (ChangeRequest, error) {
	if value.Status == "merged" || value.Status == "closed" {
		return value, nil
	}
	providerValue, err := provider.GetChangeRequestStatus(
		ctx, token, projectValue, value.ExternalNumber,
	)
	if err != nil {
		return ChangeRequest{}, err
	}
	value.Status, value.ErrorCode = providerValue.Status, providerValue.ErrorCode
	value.HeadSHA, value.MergedAt, value.UpdatedAt = providerValue.HeadSHA, providerValue.MergedAt, s.now()
	if providerValue.URL != "" {
		value.ExternalURL = providerValue.URL
	}
	value.BaseSHA = workspace.BaseSHA
	return s.store.UpsertChangeRequest(ctx, id, value)
}

func (s *Service) MergeChangeRequest(ctx context.Context, id Identity, projectID, conversationID string) (ChangeRequest, error) {
	value, err := s.store.GetChangeRequest(ctx, id, conversationID)
	if err != nil {
		return ChangeRequest{}, err
	}
	if value.ProjectID != projectID {
		return ChangeRequest{}, ErrNotFound
	}
	if value.Status == "merged" {
		return value, nil
	}
	workspace, projectValue, err := s.store.GetWorkspace(ctx, id, conversationID)
	if err != nil {
		return ChangeRequest{}, err
	}
	provider, token, release, err := s.changeRequestAccess(ctx, id, projectValue, true)
	if err != nil {
		return ChangeRequest{}, err
	}
	defer release()
	value, err = s.refreshChangeRequestWithAccess(
		ctx, id, value, workspace, projectValue, provider, token,
	)
	if err != nil {
		return ChangeRequest{}, err
	}
	if value.Status == "merged" {
		return value, nil
	}
	if value.Status != "open" {
		return ChangeRequest{}, ErrChangeRequestNotReady
	}
	title := "Cocola task " + shortTaskID(conversationID)
	mergeSHA, err := provider.SquashMerge(
		ctx, token, projectValue, value.ExternalNumber, value.HeadSHA, title,
	)
	if err != nil {
		// Provider merge is atomic on the expected head SHA. If the response was
		// lost or a concurrent retry won, reconcile instead of surfacing a false
		// failure or attempting a second merge commit.
		reconciled, refreshErr := s.refreshChangeRequestWithAccess(
			ctx, id, value, workspace, projectValue, provider, token,
		)
		if refreshErr == nil && reconciled.Status == "merged" {
			return reconciled, nil
		}
		return ChangeRequest{}, err
	}
	_ = provider.DeleteTaskBranch(
		context.WithoutCancel(ctx), token, projectValue, workspace.BranchName,
	)
	now := s.now()
	value.Status, value.MergeSHA, value.MergedAt, value.UpdatedAt = "merged", mergeSHA, &now, now
	return s.store.UpsertChangeRequest(ctx, id, value)
}

func (s *Service) repositoryProvider(
	ctx context.Context,
	id Identity,
	value Project,
) (RepositoryProvider, error) {
	if value.RepositoryProvider == ProviderLocal {
		if s.forgejo == nil {
			return nil, ErrInternalSCMUnavailable
		}
		return forgejoRepositoryProvider{client: s.forgejo}, nil
	}
	registration, err := s.store.GetAppRegistration(ctx, id)
	if err != nil {
		return nil, err
	}
	github, err := s.githubForRegistration(id, registration)
	if err != nil {
		return nil, err
	}
	return githubRepositoryProvider{client: github}, nil
}

func (s *Service) changeRequestAccess(
	ctx context.Context,
	id Identity,
	value Project,
	write bool,
) (RepositoryProvider, string, func(), error) {
	if value.RepositoryProvider == ProviderLocal {
		provider, err := s.repositoryProvider(ctx, id, value)
		if err != nil {
			return nil, "", func() {}, err
		}
		token, tokenErr := s.localProjectToken(id, value)
		return provider, token, func() {}, tokenErr
	}
	value, _, connection, github, err := s.currentProjectInstallation(ctx, id, value)
	if err != nil {
		return nil, "", func() {}, err
	}
	provider := githubRepositoryProvider{client: github}
	permissions := map[string]string{
		"contents": "read", "pull_requests": "read", "checks": "read", "statuses": "read",
	}
	if write {
		permissions["contents"] = "write"
		permissions["pull_requests"] = "write"
	}
	token, _, err := github.installationToken(
		ctx, connection.InstallationID, value.RepositoryExternalID, permissions,
	)
	release := func() {
		revokeContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = github.revokeInstallationToken(revokeContext, token)
	}
	return provider, token, release, err
}

func shortTaskID(value string) string {
	raw := strings.TrimSpace(value)
	value = strings.ReplaceAll(raw, "-", "")
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '.' || character == '_' {
			continue
		}
		digest := sha256.Sum256([]byte(raw))
		return base64.RawURLEncoding.EncodeToString(digest[:9])
	}
	if len(value) > 12 {
		return value[:12]
	}
	return value
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
	requestedBranch string,
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
		if strings.TrimSpace(requestedBranch) != "" {
			branchName, branchErr := normalizeTaskBranch(requestedBranch, conversationID)
			if branchErr != nil {
				return TaskBase{}, branchErr
			}
			if branchName != workspace.BranchName {
				return TaskBase{}, ErrTaskBranchMismatch
			}
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
		if changeRequest, changeErr := s.store.GetChangeRequest(ctx, id, conversationID); changeErr == nil && changeRequest.Status == "merged" {
			return TaskBase{}, ErrChangeRequestMerged
		} else if changeErr != nil && !errors.Is(changeErr, ErrNotFound) {
			return TaskBase{}, changeErr
		}
		return TaskBase{
			Project: value, Ref: baseRef, SHA: workspace.BaseSHA, BranchName: workspace.BranchName,
		}, nil
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
	taskBranch, err := normalizeTaskBranch(requestedBranch, conversationID)
	if err != nil {
		return TaskBase{}, err
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
		token, tokenErr := s.localProjectToken(id, value)
		if tokenErr != nil {
			return TaskBase{}, tokenErr
		}
		sha, branchErr := s.forgejo.branchSHA(ctx, token, value.RepositoryOwner, value.RepositoryName, baseRef)
		if branchErr != nil {
			return TaskBase{}, branchErr
		}
		if _, branchErr = s.forgejo.branchSHA(
			ctx, token, value.RepositoryOwner, value.RepositoryName, taskBranch,
		); branchErr == nil {
			return TaskBase{}, ErrTaskBranchExists
		} else if !forgejoStatus(branchErr, http.StatusNotFound) {
			return TaskBase{}, branchErr
		}
		return TaskBase{Project: value, Ref: "main", SHA: sha, BranchName: taskBranch}, nil
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
	if _, err = github.branchSHA(
		ctx, token, value.RepositoryOwner, value.RepositoryName, taskBranch,
	); err == nil {
		return TaskBase{}, ErrTaskBranchExists
	} else if !githubStatus(err, http.StatusNotFound) {
		return TaskBase{}, err
	}
	return TaskBase{Project: value, Ref: baseRef, SHA: sha, BranchName: taskBranch}, nil
}

func normalizeTaskBranch(requestedBranch, conversationID string) (string, error) {
	branch := strings.TrimSpace(requestedBranch)
	if branch == "" {
		branch = defaultTaskBranch(conversationID)
	}
	if !strings.HasPrefix(branch, taskBranchPrefix) {
		return "", ErrTaskBranchInvalid
	}
	suffix := strings.TrimPrefix(branch, taskBranchPrefix)
	if len(suffix) == 0 || len(suffix) > taskBranchSuffixMax ||
		!isTaskBranchAlphaNumeric(suffix[0]) || !isTaskBranchAlphaNumeric(suffix[len(suffix)-1]) {
		return "", ErrTaskBranchInvalid
	}
	for index := range len(suffix) {
		char := suffix[index]
		if isTaskBranchAlphaNumeric(char) || char == '.' || char == '_' || char == '-' {
			continue
		}
		return "", ErrTaskBranchInvalid
	}
	return branch, nil
}

func isTaskBranchAlphaNumeric(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')
}

func defaultTaskBranch(conversationID string) string {
	raw := strings.TrimSpace(conversationID)
	suffix := strings.ToLower(strings.ReplaceAll(raw, "-", ""))
	if len(suffix) > 12 {
		suffix = suffix[:12]
	}
	if suffix != "" {
		valid := true
		for index := range len(suffix) {
			if !isTaskBranchAlphaNumeric(suffix[index]) && suffix[index] != '.' && suffix[index] != '_' {
				valid = false
				break
			}
		}
		if valid && isTaskBranchAlphaNumeric(suffix[0]) && isTaskBranchAlphaNumeric(suffix[len(suffix)-1]) {
			return taskBranchPrefix + suffix
		}
	}
	digest := sha256.Sum256([]byte(raw))
	return taskBranchPrefix + hex.EncodeToString(digest[:6])
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
	result, token, _, _, err := s.projectContextForWorkspace(
		ctx, id, w, v, map[string]string{"contents": "read"},
	)
	return result, token, err
}

func (s *Service) projectContextForWorkspace(
	ctx context.Context,
	id Identity,
	w Workspace,
	v Project,
	permissions map[string]string,
) (ProjectContext, string, Workspace, Project, error) {
	if v.Status != ProjectReady {
		return ProjectContext{}, "", Workspace{}, Project{}, ErrProjectNotReady
	}
	gitAuthorName, gitAuthorEmail := gitAuthorIdentity(id)
	if v.RepositoryProvider == ProviderLocal {
		if !s.LocalProjectsEnabled() {
			return ProjectContext{}, "", Workspace{}, Project{}, ErrLocalProjectsDisabled
		}
		token, tokenErr := s.localProjectToken(id, v)
		if tokenErr != nil {
			return ProjectContext{}, "", Workspace{}, Project{}, tokenErr
		}
		baseRef := strings.TrimSpace(w.BaseRef)
		if baseRef == "" {
			baseRef = v.DefaultBranch
		}
		if w.BaseSHA == "" {
			sha, branchErr := s.forgejo.branchSHA(ctx, token, v.RepositoryOwner, v.RepositoryName, baseRef)
			if branchErr != nil {
				return ProjectContext{}, "", Workspace{}, Project{}, branchErr
			}
			locked, lockErr := s.store.LockBaseSHA(ctx, id, w.ConversationID, sha, s.now())
			if lockErr != nil {
				return ProjectContext{}, "", Workspace{}, Project{}, lockErr
			}
			w = locked
		}
		return ProjectContext{
			ProjectID: v.ID, RepositoryExternalID: v.RepositoryExternalID,
			CloneURL:      v.RepositoryCloneURL,
			DefaultBranch: v.DefaultBranch, BaseRef: baseRef, BaseSHA: w.BaseSHA, BranchName: w.BranchName,
			GitAuthorName: gitAuthorName, GitAuthorEmail: gitAuthorEmail,
			RepositoryProvider: ProviderLocal,
			RepositoryFullName: strings.Trim(v.RepositoryOwner+"/"+v.RepositoryName, "/"),
			CredentialMode:     "ephemeral",
		}, token, w, v, nil
	}
	v, _, connection, github, err := s.currentProjectInstallation(ctx, id, v)
	if err != nil {
		return ProjectContext{}, "", Workspace{}, Project{}, err
	}
	installationID := connection.InstallationID
	cloneURL := "https://github.com/" + v.RepositoryOwner + "/" + v.RepositoryName + ".git"
	baseRef := strings.TrimSpace(w.BaseRef)
	if baseRef == "" {
		baseRef = v.DefaultBranch
	}
	token, _, err := github.installationToken(
		ctx, v.InstallationID, v.RepositoryExternalID, permissions,
	)
	if err != nil {
		return ProjectContext{}, "", Workspace{}, Project{}, err
	}
	if w.BaseSHA == "" {
		var sha string
		var branchErr error
		sha, branchErr = github.branchSHA(ctx, token, v.RepositoryOwner, v.RepositoryName, baseRef)
		if branchErr != nil {
			_ = github.revokeInstallationToken(ctx, token)
			return ProjectContext{}, "", Workspace{}, Project{}, branchErr
		}
		w, err = s.store.LockBaseSHA(ctx, id, w.ConversationID, sha, s.now())
		if err != nil {
			_ = github.revokeInstallationToken(ctx, token)
			return ProjectContext{}, "", Workspace{}, Project{}, err
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
	}, token, w, v, nil
}

func (s *Service) localProjectToken(id Identity, value Project) (string, error) {
	if s == nil || s.box == nil || s.forgejo == nil || value.RepositoryTokenCipher == "" {
		return "", ErrInternalSCMUnavailable
	}
	return s.box.decrypt(value.RepositoryTokenCipher, projectTokenAAD(id, value.ID))
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
