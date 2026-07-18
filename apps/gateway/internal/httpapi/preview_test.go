package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/sandboxmgr"
	"github.com/cocola-project/cocola/packages/go-common/logger"
)

// fakeResolver is a stub EndpointResolver that points the proxy at a local
// backend and records the resolve args it was called with.
type fakeResolver struct {
	url        string
	headers    map[string]string
	err        error
	gotUser    string
	gotSession string
	gotPort    int
}

func (f *fakeResolver) ResolveEndpoint(_ context.Context, userID, sessionID string, port int) (*sandboxmgr.ResolvedEndpoint, error) {
	f.gotUser, f.gotSession, f.gotPort = userID, sessionID, port
	if f.err != nil {
		return nil, f.err
	}
	return &sandboxmgr.ResolvedEndpoint{URL: f.url, Headers: f.headers}, nil
}

func TestPreviewProxy_ForwardsToResolvedBackend(t *testing.T) {
	// Backend stands in for the in-sandbox dev server. It echoes the path it
	// received and asserts the replayed auth header arrived.
	var gotPath, gotHeader string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("X-EXECD-ACCESS-TOKEN")
		_, _ = io.WriteString(w, "hello from sandbox")
	}))
	defer backend.Close()

	res := &fakeResolver{url: backend.URL, headers: map[string]string{"X-EXECD-ACCESS-TOKEN": "tok"}}
	h := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithSandboxResolver(res).Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/preview/sess-1/3000/foo/bar?x=1", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "hello from sandbox") {
		t.Fatalf("body = %q, want proxied backend body", rec.Body.String())
	}
	if gotPath != "/foo/bar" {
		t.Fatalf("backend path = %q, want /foo/bar", gotPath)
	}
	if gotHeader != "tok" {
		t.Fatalf("replayed auth header = %q, want tok", gotHeader)
	}
	if res.gotSession != "sess-1" || res.gotPort != 3000 {
		t.Fatalf("resolve args = (%q,%d), want (sess-1,3000)", res.gotSession, res.gotPort)
	}
	if res.gotUser != auth.DevIdentity.UserID {
		t.Fatalf("resolve user = %q, want verified identity %q", res.gotUser, auth.DevIdentity.UserID)
	}
}

func TestPreviewProxy_DisabledReturns501(t *testing.T) {
	h := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/preview/sess-1/3000/", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}

func TestPreviewProxy_RejectsBadPort(t *testing.T) {
	res := &fakeResolver{url: "http://127.0.0.1:1"}
	h := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithSandboxResolver(res).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/preview/sess-1/0/", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
