package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTenantTokenProviderCachesAndRefreshesByConnectorVersion(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	provider := NewTenantTokenProvider(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			call := calls.Add(1)
			return jsonResponse(
				`{"code":0,"tenant_access_token":"token-` +
					string(rune('0'+call)) + `","expire":3600}`,
			), nil
		}),
	})
	provider.now = func() time.Time { return now }
	connector := tokenTestConnector("connector-1")

	first, err := provider.Resolve(context.Background(), connector, "secret")
	if err != nil {
		t.Fatalf("Resolve first token: %v", err)
	}
	second, err := provider.Resolve(context.Background(), connector, "secret")
	if err != nil {
		t.Fatalf("Resolve cached token: %v", err)
	}
	if first.TenantAccessToken != second.TenantAccessToken || calls.Load() != 1 {
		t.Fatalf("cached token = %q, calls = %d", second.TenantAccessToken, calls.Load())
	}

	connector.Version++
	refreshed, err := provider.Resolve(context.Background(), connector, "secret")
	if err != nil {
		t.Fatalf("Resolve version-refreshed token: %v", err)
	}
	if refreshed.TenantAccessToken == first.TenantAccessToken || calls.Load() != 2 {
		t.Fatalf("version refresh token = %q, calls = %d", refreshed.TenantAccessToken, calls.Load())
	}

	now = refreshed.ExpiresAt.Add(-tenantTokenEarlyRefresh)
	if _, err := provider.Resolve(context.Background(), connector, "secret"); err != nil {
		t.Fatalf("Resolve expiry-refreshed token: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expiry refresh calls = %d, want 3", calls.Load())
	}
}

func TestTenantTokenProviderBoundsCache(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	provider := NewTenantTokenProvider(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(
				`{"code":0,"tenant_access_token":"token","expire":3600}`,
			), nil
		}),
	})
	provider.now = func() time.Time { return now }
	provider.maxEntries = 2

	for _, id := range []string{"connector-1", "connector-2", "connector-3"} {
		if _, err := provider.Resolve(
			context.Background(),
			tokenTestConnector(id),
			"secret",
		); err != nil {
			t.Fatalf("Resolve %s: %v", id, err)
		}
		now = now.Add(time.Second)
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.cache) != 2 {
		t.Fatalf("cache size = %d, want 2", len(provider.cache))
	}
	if _, exists := provider.cache["connector-1"]; exists {
		t.Fatal("least recently used cache entry was not evicted")
	}
}

func TestTenantTokenProviderCoalescesConcurrentRefresh(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once
	provider := NewTenantTokenProvider(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			enteredOnce.Do(func() { close(entered) })
			<-release
			return jsonResponse(
				`{"code":0,"tenant_access_token":"token","expire":3600}`,
			), nil
		}),
	})

	const workers = 8
	var wait sync.WaitGroup
	wait.Add(workers)
	errs := make(chan error, workers)
	for range workers {
		go func() {
			defer wait.Done()
			_, err := provider.Resolve(
				context.Background(),
				tokenTestConnector("connector-1"),
				"secret",
			)
			errs <- err
		}()
	}
	<-entered
	close(release)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Resolve concurrent token: %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", calls.Load())
	}
}

func TestTenantTokenProviderDoesNotShareOrOverwriteAcrossConnectorVersions(t *testing.T) {
	oldEntered := make(chan struct{})
	releaseOld := make(chan struct{})
	var oldOnce sync.Once
	var calls atomic.Int32
	provider := NewTenantTokenProvider(&http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls.Add(1)
			var payload struct {
				AppID string `json:"app_id"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				return nil, err
			}
			if payload.AppID == "old-app" {
				oldOnce.Do(func() { close(oldEntered) })
				<-releaseOld
				return jsonResponse(
					`{"code":0,"tenant_access_token":"old-token","expire":3600}`,
				), nil
			}
			return jsonResponse(
				`{"code":0,"tenant_access_token":"new-token","expire":3600}`,
			), nil
		}),
	})

	oldConnector := tokenTestConnector("connector-1")
	oldConnector.AppID = "old-app"
	oldResult := make(chan RuntimeCredential, 1)
	oldErr := make(chan error, 1)
	go func() {
		credential, err := provider.Resolve(context.Background(), oldConnector, "old-secret")
		oldResult <- credential
		oldErr <- err
	}()
	<-oldEntered

	newConnector := oldConnector
	newConnector.Version++
	newConnector.AppID = "new-app"
	newCtx, cancelNew := context.WithTimeout(context.Background(), time.Second)
	defer cancelNew()
	newCredential, err := provider.Resolve(newCtx, newConnector, "new-secret")
	if err != nil {
		close(releaseOld)
		t.Fatalf("Resolve new connector: %v", err)
	}
	if newCredential.AppID != "new-app" ||
		newCredential.TenantAccessToken != "new-token" {
		close(releaseOld)
		t.Fatalf("new connector credential = %#v", newCredential)
	}

	close(releaseOld)
	if err := <-oldErr; err != nil {
		t.Fatalf("Resolve old connector: %v", err)
	}
	if credential := <-oldResult; credential.TenantAccessToken != "old-token" {
		t.Fatalf("old connector credential = %#v", credential)
	}

	cached, err := provider.Resolve(context.Background(), newConnector, "new-secret")
	if err != nil {
		t.Fatalf("Resolve cached new connector: %v", err)
	}
	if cached.TenantAccessToken != "new-token" || calls.Load() != 2 {
		t.Fatalf("cached new connector = %#v, calls = %d", cached, calls.Load())
	}
}

func TestTenantTokenProviderCallerCancellationDoesNotPoisonSharedRefresh(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	provider := NewTenantTokenProvider(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			close(entered)
			<-release
			return jsonResponse(
				`{"code":0,"tenant_access_token":"token","expire":3600}`,
			), nil
		}),
	})
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstResult := make(chan error, 1)
	go func() {
		_, err := provider.Resolve(
			firstCtx,
			tokenTestConnector("connector-1"),
			"secret",
		)
		firstResult <- err
	}()
	<-entered
	secondResult := make(chan error, 1)
	go func() {
		_, err := provider.Resolve(
			context.Background(),
			tokenTestConnector("connector-1"),
			"secret",
		)
		secondResult <- err
	}()

	cancelFirst()
	if err := <-firstResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("first Resolve error = %v, want context.Canceled", err)
	}
	close(release)
	if err := <-secondResult; err != nil {
		t.Fatalf("second Resolve error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", calls.Load())
	}
}

func TestServiceRuntimeCredentialStates(t *testing.T) {
	store := &runtimeCredentialStore{}
	service, err := NewService(
		context.Background(),
		store,
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		nil,
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.WithTokenHTTPClient(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(
				`{"code":0,"tenant_access_token":"runtime-token","expire":3600}`,
			), nil
		}),
	})
	identity := Identity{TenantID: "tenant", UserID: "user"}
	connector := tokenTestConnector("connector-1")
	connector.TenantID = identity.TenantID
	connector.UserID = identity.UserID
	ciphertext, err := service.box.Encrypt(
		"app-secret",
		connectorSecretAAD(connector.TenantID, connector.UserID, connector.ID),
	)
	if err != nil {
		t.Fatalf("Encrypt app secret: %v", err)
	}
	connector.AppSecretCiphertext = ciphertext
	store.connector = connector

	credential, err := service.RuntimeCredentialByID(context.Background(), identity, connector.ID)
	if err != nil {
		t.Fatalf("RuntimeCredential ready: %v", err)
	}
	if credential.Status != RuntimeCredentialReady ||
		credential.AppID != connector.AppID ||
		credential.TenantAccessToken != "runtime-token" {
		t.Fatalf("RuntimeCredential ready = %#v", credential)
	}

	store.connector.DesiredEnabled = false
	credential, err = service.RuntimeCredentialByID(context.Background(), identity, connector.ID)
	if err != nil || credential.Status != RuntimeCredentialDisabled {
		t.Fatalf("RuntimeCredential disabled = %#v, %v", credential, err)
	}

	store.err = ErrNotFound
	credential, err = service.RuntimeCredentialByID(context.Background(), identity, connector.ID)
	if err != nil || credential.Status != RuntimeCredentialMissing {
		t.Fatalf("RuntimeCredential missing = %#v, %v", credential, err)
	}

	store.err = errors.New("database unavailable")
	credential, err = service.RuntimeCredentialByID(context.Background(), identity, connector.ID)
	if err == nil || credential.Status != RuntimeCredentialUnavailable {
		t.Fatalf("RuntimeCredential unavailable = %#v, %v", credential, err)
	}
}

func tokenTestConnector(id string) Connector {
	return Connector{
		ID: id, TenantID: "tenant", UserID: "user",
		Domain: DomainFeishu, AppID: "app-id", DesiredEnabled: true,
		Status: StatusReady, Version: 1,
	}
}

type runtimeCredentialStore struct {
	Store
	connector Connector
	err       error
}

func (store *runtimeCredentialStore) GetConnectorByID(
	context.Context,
	string,
) (Connector, error) {
	return store.connector, store.err
}
