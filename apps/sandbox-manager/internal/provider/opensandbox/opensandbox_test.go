// Copyright 2026 The cocola authors. Licensed under Apache-2.0.

package opensandbox

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
)

// roundTripFunc adapts a function to http.RoundTripper so tests can stub the
// OpenSandbox REST server without opening any socket (honors the no-listening-
// process rule).
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// newStub builds a Provider whose HTTP client is backed by handler.
func newStub(t *testing.T, handler roundTripFunc) *Provider {
	t.Helper()
	p, err := New(
		WithBaseURL("http://opensandbox.test/v1"),
		WithAPIKey("test-key"),
		WithHTTPClient(&http.Client{Transport: handler}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func TestNew_RequiresBaseURL(t *testing.T) {
	t.Setenv("COCOLA_OPENSANDBOX_URL", "")
	if _, err := New(); err == nil {
		t.Fatal("expected error when base URL is unset, got nil")
	}
}

func TestNew_EnvDefaults(t *testing.T) {
	t.Setenv("COCOLA_OPENSANDBOX_URL", "http://from-env:8090/v1/")
	t.Setenv("COCOLA_OPENSANDBOX_API_KEY", "env-key")
	p, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.baseURL != "http://from-env:8090/v1" { // trailing slash trimmed
		t.Errorf("baseURL = %q, want trimmed env value", p.baseURL)
	}
	if p.apiKey != "env-key" {
		t.Errorf("apiKey = %q, want env-key", p.apiKey)
	}
}

func TestCreate_HappyPath(t *testing.T) {
	var gotMethod, gotPath, gotAPIKey, gotCT string
	gotBody := ""
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get(apiKeyHeader)
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		return jsonResp(http.StatusOK, `{"id":"sbx-123","status":{"state":"Pending"}}`), nil
	})

	sb, err := p.Create(context.Background(), provider.SandboxSpec{
		UserID:     "u1",
		SessionID:  "s1",
		Image:      "cocola/sandbox-runtime:dev",
		Resources:  provider.Resources{CPUCores: 0.5, MemoryMiB: 512},
		Networking: provider.Networking{EgressAllowlist: []string{"api.anthropic.com"}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sb.ID != "sbx-123" {
		t.Errorf("sandbox id = %q, want sbx-123", sb.ID)
	}
	if sb.UserID != "u1" || sb.SessionID != "s1" {
		t.Errorf("sandbox user/session = %q/%q, want u1/s1", sb.UserID, sb.SessionID)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/sandboxes" {
		t.Errorf("request = %s %s, want POST /v1/sandboxes", gotMethod, gotPath)
	}
	if gotAPIKey != "test-key" {
		t.Errorf("api key header = %q, want test-key", gotAPIKey)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotCT)
	}
	// Body assertions: resource + egress mapping landed on the wire.
	for _, want := range []string{`"uri":"cocola/sandbox-runtime:dev"`, `"cpu":"500m"`, `"memory":"512Mi"`, `"defaultAction":"deny"`, `"target":"api.anthropic.com"`} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("request body missing %s\nbody: %s", want, gotBody)
		}
	}
	// id mapping recorded
	if got, err := p.resolve("sbx-123"); err != nil || got != "sbx-123" {
		t.Errorf("resolve after create = %q,%v", got, err)
	}
}

func TestCreate_EmptyIDFails(t *testing.T) {
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(http.StatusOK, `{"id":"","status":{"state":"Pending"}}`), nil
	})
	if _, err := p.Create(context.Background(), provider.SandboxSpec{}); err == nil {
		t.Fatal("expected error on empty id, got nil")
	}
}

func TestCreate_ServerErrorPropagates(t *testing.T) {
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(http.StatusInternalServerError, `{"error":"boom"}`), nil
	})
	_, err := p.Create(context.Background(), provider.SandboxSpec{Image: "x"})
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("want status 500 error, got %v", err)
	}
}

func TestHealth_RunningIsHealthy(t *testing.T) {
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/sandboxes/sbx-1" {
			t.Errorf("request = %s %s, want GET /v1/sandboxes/sbx-1", r.Method, r.URL.Path)
		}
		return jsonResp(http.StatusOK, `{"id":"sbx-1","status":{"state":"Running"}}`), nil
	})
	h, err := p.Health(context.Background(), "sbx-1")
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !h.Healthy || h.Detail != "Running" {
		t.Errorf("health = %+v, want healthy Running", h)
	}
}

func TestHealth_NonRunningIsUnhealthy(t *testing.T) {
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(http.StatusOK, `{"id":"sbx-1","status":{"state":"Failed","message":"oom"}}`), nil
	})
	h, err := p.Health(context.Background(), "sbx-1")
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h.Healthy {
		t.Error("Failed state should be unhealthy")
	}
	if h.Detail != "Failed: oom" {
		t.Errorf("detail = %q, want 'Failed: oom'", h.Detail)
	}
}

func TestDestroy_DeletesAndDropsMapping(t *testing.T) {
	var gotMethod, gotPath string
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		return jsonResp(http.StatusNoContent, ""), nil
	})
	p.mu.Lock()
	p.ids["sbx-9"] = "sbx-9"
	p.mu.Unlock()

	if err := p.Destroy(context.Background(), "sbx-9"); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/v1/sandboxes/sbx-9" {
		t.Errorf("request = %s %s, want DELETE /v1/sandboxes/sbx-9", gotMethod, gotPath)
	}
	p.mu.RLock()
	_, exists := p.ids["sbx-9"]
	p.mu.RUnlock()
	if exists {
		t.Error("id mapping should be dropped after Destroy")
	}
}

func TestDeferredMethods_ReturnNotImplemented(t *testing.T) {
	p := newStub(t, func(r *http.Request) (*http.Response, error) {
		t.Fatal("deferred methods must not issue HTTP calls")
		return nil, nil
	})
	ctx := context.Background()

	if _, err := p.Exec(ctx, "sbx", provider.ExecRequest{}); !errors.Is(err, errNotImplemented) {
		t.Errorf("Exec err = %v, want errNotImplemented", err)
	}
	if err := p.WriteFile(ctx, "sbx", "/tmp/x", nil); !errors.Is(err, errNotImplemented) {
		t.Errorf("WriteFile err = %v, want errNotImplemented", err)
	}
	if _, err := p.ReadFile(ctx, "sbx", "/tmp/x"); !errors.Is(err, errNotImplemented) {
		t.Errorf("ReadFile err = %v, want errNotImplemented", err)
	}
	if err := p.Pause(ctx, "sbx"); !errors.Is(err, errNotImplemented) {
		t.Errorf("Pause err = %v, want errNotImplemented", err)
	}
	if err := p.Resume(ctx, "sbx"); !errors.Is(err, errNotImplemented) {
		t.Errorf("Resume err = %v, want errNotImplemented", err)
	}
}

func TestMapResources(t *testing.T) {
	got := mapResources(provider.Resources{CPUCores: 2, MemoryMiB: 1024})
	if got["cpu"] != "2000m" || got["memory"] != "1024Mi" {
		t.Errorf("mapResources = %v, want cpu=2000m memory=1024Mi", got)
	}
	if len(mapResources(provider.Resources{})) != 0 {
		t.Error("zero resources should map to empty limits")
	}
}
