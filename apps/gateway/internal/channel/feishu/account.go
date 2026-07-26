package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cocola-project/cocola/packages/go-common/token"
)

var (
	ErrAccountDisabled    = errors.New("feishu connector: account disabled")
	ErrAccountUnavailable = errors.New("feishu connector: account unavailable")
)

type Account struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	TenantID string `json:"tenant_id"`
	Enabled  bool   `json:"enabled"`
}

type AccountAuthorizer struct {
	adminURL string
	issuer   *token.Issuer
	http     *http.Client
}

func NewAccountAuthorizer(
	adminURL string,
	issuer *token.Issuer,
	httpClient *http.Client,
) (*AccountAuthorizer, error) {
	adminURL = strings.TrimRight(strings.TrimSpace(adminURL), "/")
	parsed, err := url.Parse(adminURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("COCOLA_ADMIN_URL must be an absolute http(s) URL")
	}
	if issuer == nil {
		return nil, errors.New("Feishu account authorization requires a token issuer")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &AccountAuthorizer{adminURL: adminURL, issuer: issuer, http: httpClient}, nil
}

func (a *AccountAuthorizer) Authorize(
	ctx context.Context,
	connector Connector,
) (string, Account, error) {
	probe, _, err := a.issuer.Issue(
		connector.UserID,
		connector.TenantID,
		time.Minute,
		0,
	)
	if err != nil {
		return "", Account{}, fmt.Errorf("%w: mint probe token", ErrAccountUnavailable)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		a.adminURL+"/me/account",
		nil,
	)
	if err != nil {
		return "", Account{}, fmt.Errorf("%w: build account request", ErrAccountUnavailable)
	}
	req.Header.Set("authorization", "Bearer "+probe)
	resp, err := a.http.Do(req)
	if err != nil {
		return "", Account{}, fmt.Errorf("%w: %v", ErrAccountUnavailable, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusNotFound, http.StatusUnauthorized:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return "", Account{}, ErrAccountDisabled
	case http.StatusOK:
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return "", Account{}, fmt.Errorf(
			"%w: admin-api returned %d",
			ErrAccountUnavailable,
			resp.StatusCode,
		)
	}
	var account Account
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := decoder.Decode(&account); err != nil {
		return "", Account{}, fmt.Errorf("%w: decode account", ErrAccountUnavailable)
	}
	if !account.Enabled || account.ID != connector.UserID ||
		account.TenantID != connector.TenantID {
		return "", Account{}, ErrAccountDisabled
	}
	runtimeToken, _, err := a.issuer.IssueUser(
		account.ID,
		account.TenantID,
		account.Email,
		account.Name,
		account.Username,
		5*time.Minute,
		0,
	)
	if err != nil {
		return "", Account{}, fmt.Errorf("%w: mint runtime token", ErrAccountUnavailable)
	}
	return runtimeToken, account, nil
}
