package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	defaultTenantTokenCacheEntries = 1024
	tenantTokenEarlyRefresh        = time.Minute
	tenantTokenRequestTimeout      = 30 * time.Second
	maxTokenResponseBytes          = int64(1 << 20)
)

type tenantTokenCacheEntry struct {
	credential       RuntimeCredential
	connectorVersion int64
	domain           string
	lastUsed         time.Time
}

// TenantTokenProvider mints and caches TATs without background goroutines.
// The hard entry cap prevents a long-running Gateway from retaining one cache
// entry per historical Connector forever.
type TenantTokenProvider struct {
	mu         sync.Mutex
	http       *http.Client
	now        func() time.Time
	maxEntries int
	cache      map[string]tenantTokenCacheEntry
	refresh    singleflight.Group
}

func NewTenantTokenProvider(client *http.Client) *TenantTokenProvider {
	if client == nil {
		client = &http.Client{
			Timeout: tenantTokenRequestTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &TenantTokenProvider{
		http: client, now: func() time.Time { return time.Now().UTC() },
		maxEntries: defaultTenantTokenCacheEntries,
		cache:      make(map[string]tenantTokenCacheEntry),
	}
}

func (p *TenantTokenProvider) SetHTTPClient(client *http.Client) {
	if client == nil {
		return
	}
	p.mu.Lock()
	p.http = client
	p.mu.Unlock()
}

func (p *TenantTokenProvider) Resolve(
	ctx context.Context,
	connector Connector,
	appSecret string,
) (RuntimeCredential, error) {
	if connector.ID == "" || connector.AppID == "" || appSecret == "" ||
		(connector.Domain != DomainFeishu && connector.Domain != DomainLark) {
		return RuntimeCredential{}, ErrInvalid
	}
	if credential, ok := p.cached(connector); ok {
		return credential, nil
	}
	result := p.refresh.DoChan(tenantTokenFlightKey(connector), func() (any, error) {
		if credential, ok := p.cached(connector); ok {
			return credential, nil
		}
		refreshCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			tenantTokenRequestTimeout,
		)
		defer cancel()
		credential, err := p.fetch(refreshCtx, connector, appSecret)
		if err != nil {
			return RuntimeCredential{}, err
		}
		p.store(connector, credential)
		return credential, nil
	})
	select {
	case <-ctx.Done():
		return RuntimeCredential{}, ctx.Err()
	case resolved := <-result:
		if resolved.Err != nil {
			return RuntimeCredential{}, resolved.Err
		}
		credential, ok := resolved.Val.(RuntimeCredential)
		if !ok {
			return RuntimeCredential{}, errors.New("Feishu tenant token result was invalid")
		}
		return credential, nil
	}
}

func tenantTokenFlightKey(connector Connector) string {
	return fmt.Sprintf(
		"%s\x00%d\x00%s\x00%s",
		connector.ID,
		connector.Version,
		connector.Domain,
		connector.AppID,
	)
}

func (p *TenantTokenProvider) cached(
	connector Connector,
) (RuntimeCredential, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.cache[connector.ID]
	now := p.now()
	if !ok ||
		entry.connectorVersion != connector.Version ||
		entry.domain != connector.Domain ||
		entry.credential.AppID != connector.AppID ||
		!now.Add(tenantTokenEarlyRefresh).Before(entry.credential.ExpiresAt) {
		if ok {
			delete(p.cache, connector.ID)
		}
		return RuntimeCredential{}, false
	}
	entry.lastUsed = now
	p.cache[connector.ID] = entry
	return entry.credential, true
}

func (p *TenantTokenProvider) store(
	connector Connector,
	credential RuntimeCredential,
) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if existing, ok := p.cache[connector.ID]; ok &&
		existing.connectorVersion > connector.Version {
		// An older refresh may finish after a reconfiguration. Never let it
		// replace the newer version already cached for this Connector.
		return
	}
	for id, entry := range p.cache {
		if !now.Before(entry.credential.ExpiresAt) {
			delete(p.cache, id)
		}
	}
	if _, exists := p.cache[connector.ID]; !exists && len(p.cache) >= p.maxEntries {
		oldestID := ""
		var oldest time.Time
		for id, entry := range p.cache {
			if oldestID == "" || entry.lastUsed.Before(oldest) {
				oldestID = id
				oldest = entry.lastUsed
			}
		}
		if oldestID != "" {
			delete(p.cache, oldestID)
		}
	}
	p.cache[connector.ID] = tenantTokenCacheEntry{
		credential: credential, connectorVersion: connector.Version,
		domain: connector.Domain, lastUsed: now,
	}
}

func (p *TenantTokenProvider) Invalidate(connectorID string) {
	p.mu.Lock()
	delete(p.cache, connectorID)
	p.mu.Unlock()
}

func (p *TenantTokenProvider) InvalidateAll() {
	p.mu.Lock()
	clear(p.cache)
	p.mu.Unlock()
}

func (p *TenantTokenProvider) fetch(
	ctx context.Context,
	connector Connector,
	appSecret string,
) (RuntimeCredential, error) {
	baseURL := "https://open.feishu.cn"
	if connector.Domain == DomainLark {
		baseURL = "https://open.larksuite.com"
	}
	payload, err := json.Marshal(map[string]string{
		"app_id": connector.AppID, "app_secret": appSecret,
	})
	if err != nil {
		return RuntimeCredential{}, err
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		baseURL+"/open-apis/auth/v3/tenant_access_token/internal",
		bytes.NewReader(payload),
	)
	if err != nil {
		return RuntimeCredential{}, err
	}
	req.Header.Set("content-type", "application/json")
	p.mu.Lock()
	client := p.http
	p.mu.Unlock()
	resp, err := client.Do(req)
	if err != nil {
		return RuntimeCredential{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxTokenResponseBytes))
		return RuntimeCredential{}, fmt.Errorf("Feishu tenant token returned %d", resp.StatusCode)
	}
	var result struct {
		Code              int    `json:"code"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxTokenResponseBytes))
	if err := decoder.Decode(&result); err != nil {
		return RuntimeCredential{}, err
	}
	if result.Code != 0 || result.TenantAccessToken == "" {
		return RuntimeCredential{}, errors.New("Feishu tenant token was rejected")
	}
	expiresIn := result.Expire
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	now := p.now()
	return RuntimeCredential{
		Status:            RuntimeCredentialReady,
		AppID:             connector.AppID,
		Brand:             connector.Domain,
		TenantAccessToken: result.TenantAccessToken,
		ExpiresAt:         now.Add(time.Duration(expiresIn) * time.Second),
	}, nil
}
