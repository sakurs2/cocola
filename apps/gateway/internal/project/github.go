package project

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	githubAPIBase = "https://api.github.com"
	githubWebBase = "https://github.com"
)

type githubToken struct {
	AccessToken  string
	ExpiresAt    *time.Time
	RefreshToken string
	RefreshAt    *time.Time
}

type githubUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

type githubManifestConversion struct {
	ID           int64      `json:"id"`
	Slug         string     `json:"slug"`
	ClientID     string     `json:"client_id"`
	ClientSecret string     `json:"client_secret"`
	PEM          string     `json:"pem"`
	Owner        githubUser `json:"owner"`
}

type githubInstallation struct {
	ID      int64 `json:"id"`
	Account struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"account"`
}

type githubHTTPError struct {
	Status  int
	Message string
}

func (e *githubHTTPError) Error() string {
	return fmt.Sprintf("github request failed with status %d", e.Status)
}

type githubClient struct {
	http         *http.Client
	clientID     string
	clientSecret string
	appID        string
	appSlug      string
	callbackURL  string
	privateKey   *rsa.PrivateKey
	apiBase      string
	webBase      string
	userAgent    string
}

type githubClientConfig struct {
	AppID        int64
	AppSlug      string
	ClientID     string
	ClientSecret string
	PrivateKey   string
	CallbackURL  string
	HTTPClient   *http.Client
	APIBase      string
	WebBase      string
}

func newGitHubClient(cfg githubClientConfig) (*githubClient, error) {
	block, _ := pem.Decode([]byte(cfg.PrivateKey))
	if block == nil {
		return nil, errors.New("GitHub App private key is not valid PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return configuredGitHubClient(cfg, rsaKey), nil
		}
	}
	rsaKey, rsaErr := x509.ParsePKCS1PrivateKey(block.Bytes)
	if rsaErr != nil {
		return nil, errors.New("GitHub App private key is not an RSA private key")
	}
	return configuredGitHubClient(cfg, rsaKey), nil
}

func configuredGitHubClient(cfg githubClientConfig, key *rsa.PrivateKey) *githubClient {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &githubClient{
		http: client, clientID: cfg.ClientID, clientSecret: cfg.ClientSecret,
		appID: strconv.FormatInt(cfg.AppID, 10), appSlug: cfg.AppSlug, callbackURL: cfg.CallbackURL,
		privateKey: key, apiBase: firstNonEmpty(cfg.APIBase, githubAPIBase),
		webBase:   firstNonEmpty(cfg.WebBase, githubWebBase),
		userAgent: "cocola-projects/1",
	}
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimRight(value, "/")
	}
	return fallback
}

func githubManifestRegistrationURL(state string) string {
	return githubWebBase + "/settings/apps/new?state=" + url.QueryEscape(state)
}

func convertGitHubManifest(ctx context.Context, client *http.Client, code string) (githubManifestConversion, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		githubAPIBase+"/app-manifests/"+url.PathEscape(strings.TrimSpace(code))+"/conversions", nil)
	if err != nil {
		return githubManifestConversion{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "cocola-projects/1")
	response, err := client.Do(req)
	if err != nil {
		return githubManifestConversion{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return githubManifestConversion{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return githubManifestConversion{}, &githubHTTPError{Status: response.StatusCode}
	}
	var conversion githubManifestConversion
	if err := json.Unmarshal(body, &conversion); err != nil {
		return githubManifestConversion{}, err
	}
	if conversion.ID <= 0 || conversion.Slug == "" || conversion.ClientID == "" ||
		conversion.ClientSecret == "" || conversion.PEM == "" {
		return githubManifestConversion{}, errors.New("github manifest conversion returned incomplete credentials")
	}
	return conversion, nil
}

func (g *githubClient) authorizeURL(state string) string {
	q := url.Values{"client_id": {g.clientID}, "redirect_uri": {g.callbackURL}, "state": {state}}
	return g.webBase + "/login/oauth/authorize?" + q.Encode()
}

func (g *githubClient) installationURL() string {
	return g.webBase + "/apps/" + url.PathEscape(g.appSlug) + "/installations/new"
}

func (g *githubClient) exchange(ctx context.Context, code string) (githubToken, error) {
	return g.oauthToken(ctx, url.Values{
		"client_id": {g.clientID}, "client_secret": {g.clientSecret},
		"code": {code}, "redirect_uri": {g.callbackURL},
	})
}

func (g *githubClient) refresh(ctx context.Context, refreshToken string) (githubToken, error) {
	return g.oauthToken(ctx, url.Values{
		"client_id": {g.clientID}, "client_secret": {g.clientSecret},
		"grant_type": {"refresh_token"}, "refresh_token": {refreshToken},
	})
}

func (g *githubClient) oauthToken(ctx context.Context, values url.Values) (githubToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		g.webBase+"/login/oauth/access_token", strings.NewReader(values.Encode()))
	if err != nil {
		return githubToken{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var payload struct {
		AccessToken      string `json:"access_token"`
		ExpiresIn        int64  `json:"expires_in"`
		RefreshToken     string `json:"refresh_token"`
		RefreshExpiresIn int64  `json:"refresh_token_expires_in"`
		Error            string `json:"error"`
	}
	if err := g.do(req, &payload); err != nil {
		return githubToken{}, err
	}
	if payload.AccessToken == "" || payload.Error != "" {
		return githubToken{}, &githubHTTPError{Status: http.StatusUnauthorized}
	}
	now := time.Now().UTC()
	result := githubToken{AccessToken: payload.AccessToken, RefreshToken: payload.RefreshToken}
	if payload.ExpiresIn > 0 {
		expires := now.Add(time.Duration(payload.ExpiresIn) * time.Second)
		result.ExpiresAt = &expires
	}
	if payload.RefreshExpiresIn > 0 {
		expires := now.Add(time.Duration(payload.RefreshExpiresIn) * time.Second)
		result.RefreshAt = &expires
	}
	return result, nil
}

func (g *githubClient) user(ctx context.Context, token string) (githubUser, error) {
	var result githubUser
	err := g.api(ctx, http.MethodGet, "/user", token, nil, &result)
	return result, err
}

func (g *githubClient) installations(ctx context.Context, token string) ([]githubInstallation, error) {
	result := make([]githubInstallation, 0)
	for page := 1; page <= 100; page++ {
		var payload struct {
			Installations []githubInstallation `json:"installations"`
		}
		endpoint := fmt.Sprintf("/user/installations?per_page=100&page=%d", page)
		if err := g.api(ctx, http.MethodGet, endpoint, token, nil, &payload); err != nil {
			return nil, err
		}
		result = append(result, payload.Installations...)
		if len(payload.Installations) < 100 {
			break
		}
	}
	return result, nil
}

func (g *githubClient) repositories(ctx context.Context, token string, installationID int64, page int) ([]Repository, bool, error) {
	if page < 1 {
		page = 1
	}
	var payload struct {
		Repositories []githubRepository `json:"repositories"`
	}
	endpoint := fmt.Sprintf("/user/installations/%d/repositories?per_page=100&page=%d", installationID, page)
	if err := g.api(ctx, http.MethodGet, endpoint, token, nil, &payload); err != nil {
		return nil, false, err
	}
	out := make([]Repository, 0, len(payload.Repositories))
	for _, repo := range payload.Repositories {
		out = append(out, repo.repository())
	}
	return out, len(payload.Repositories) == 100, nil
}

func (g *githubClient) createRepository(ctx context.Context, token, name, description string, private bool) (Repository, error) {
	var payload githubRepository
	err := g.api(ctx, http.MethodPost, "/user/repos", token, map[string]any{
		"name": name, "description": description, "private": private, "auto_init": true,
	}, &payload)
	return payload.repository(), err
}

func (g *githubClient) createEmptyRepository(ctx context.Context, token, name, description string, private bool) (Repository, error) {
	var payload githubRepository
	err := g.api(ctx, http.MethodPost, "/user/repos", token, map[string]any{
		"name": name, "description": description, "private": private, "auto_init": false,
	}, &payload)
	return payload.repository(), err
}

func (g *githubClient) repository(ctx context.Context, token string, repositoryID int64) (Repository, error) {
	var payload githubRepository
	err := g.api(ctx, http.MethodGet, "/repositories/"+strconv.FormatInt(repositoryID, 10), token, nil, &payload)
	return payload.repository(), err
}

func (g *githubClient) repositoryByName(ctx context.Context, token, owner, name string) (Repository, error) {
	var payload githubRepository
	endpoint := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
	err := g.api(ctx, http.MethodGet, endpoint, token, nil, &payload)
	return payload.repository(), err
}

func (g *githubClient) branchSHA(ctx context.Context, token, owner, repo, branch string) (string, error) {
	var payload struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	endpoint := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) +
		"/branches/" + url.PathEscape(branch)
	err := g.api(ctx, http.MethodGet, endpoint, token, nil, &payload)
	return payload.Commit.SHA, err
}

func (g *githubClient) repositoryWarnings(ctx context.Context, token string, repo Repository) Repository {
	if content, exists, err := g.repositoryFile(ctx, token, repo, ".gitattributes"); err == nil && exists {
		repo.HasLFS = strings.Contains(strings.ToLower(string(content)), "filter=lfs")
	}
	if _, exists, err := g.repositoryFile(ctx, token, repo, ".gitmodules"); err == nil {
		repo.HasSubmodule = exists
	}
	return repo
}

func (g *githubClient) repositoryFile(ctx context.Context, token string, repo Repository, path string) ([]byte, bool, error) {
	var payload struct {
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	endpoint := "/repos/" + url.PathEscape(repo.Owner) + "/" + url.PathEscape(repo.Name) +
		"/contents/" + url.PathEscape(path) + "?ref=" + url.QueryEscape(repo.DefaultBranch)
	err := g.api(ctx, http.MethodGet, endpoint, token, nil, &payload)
	var httpErr *githubHTTPError
	if errors.As(err, &httpErr) && httpErr.Status == http.StatusNotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if payload.Encoding != "base64" {
		return nil, true, nil
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(payload.Content, "\n", ""))
	return content, true, err
}

func (g *githubClient) installationToken(
	ctx context.Context,
	installationID int64,
	repositoryID int64,
	permissions map[string]string,
) (string, time.Time, error) {
	jwt, err := g.appJWT(time.Now().UTC())
	if err != nil {
		return "", time.Time{}, err
	}
	var payload struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	endpoint := fmt.Sprintf("/app/installations/%d/access_tokens", installationID)
	err = g.api(ctx, http.MethodPost, endpoint, jwt, map[string]any{
		"repository_ids": []int64{repositoryID},
		"permissions":    permissions,
	}, &payload)
	return payload.Token, payload.ExpiresAt, err
}

func (g *githubClient) revokeInstallationToken(ctx context.Context, token string) error {
	return g.api(ctx, http.MethodDelete, "/installation/token", token, nil, nil)
}

func (g *githubClient) appJWT(now time.Time) (string, error) {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	payload, _ := json.Marshal(map[string]any{
		"iat": now.Add(-30 * time.Second).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": g.appID,
	})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	hash := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, g.privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

type githubRepository struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	FullName      string     `json:"full_name"`
	HTMLURL       string     `json:"html_url"`
	CloneURL      string     `json:"clone_url"`
	DefaultBranch string     `json:"default_branch"`
	Visibility    string     `json:"visibility"`
	Private       bool       `json:"private"`
	Size          int64      `json:"size"`
	CreatedAt     time.Time  `json:"created_at"`
	Owner         githubUser `json:"owner"`
}

func (r githubRepository) repository() Repository {
	return Repository{ID: r.ID, OwnerID: r.Owner.ID, Owner: r.Owner.Login, Name: r.Name,
		FullName: r.FullName, HTMLURL: r.HTMLURL, CloneURL: r.CloneURL,
		DefaultBranch: r.DefaultBranch, Visibility: r.Visibility, Private: r.Private,
		SizeKB: r.Size, CreatedAt: r.CreatedAt}
}

func (g *githubClient) api(ctx context.Context, method, endpoint, token string, body any, result any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.apiBase+endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", g.userAgent)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return g.do(req, result)
}

func (g *githubClient) do(req *http.Request, result any) error {
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &payload)
		return &githubHTTPError{Status: resp.StatusCode, Message: payload.Message}
	}
	if result != nil && len(body) > 0 {
		if err := json.Unmarshal(body, result); err != nil {
			return err
		}
	}
	return nil
}
