package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// scrape runs the /metrics handler via httptest (no real port bound) and returns
// the exposition body.
func scrape(t *testing.T, r *Registry) string {
	t.Helper()
	srv := httptest.NewServer(r.Mux())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func TestMetrics_ServiceLabelAndBaseline(t *testing.T) {
	r := New("test-svc")
	// A labelled series only appears after at least one observation, so drive one
	// request through the HTTP middleware before asserting the const label.
	h := r.HTTPMiddleware("GET /x", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	body := scrape(t, r)
	// Baseline runtime/process collectors present.
	if !strings.Contains(body, "go_goroutines") {
		t.Errorf("expected go_goroutines in exposition")
	}
	// Our request metrics carry the service const label.
	if !strings.Contains(body, `service="test-svc"`) {
		t.Errorf("expected service=\"test-svc\" const label, got:\n%s", body)
	}
}

func TestHTTPMiddleware_RecordsRED(t *testing.T) {
	r := New("http-svc")
	h := r.HTTPMiddleware("GET /ping", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hi"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status passthrough = %d, want 418", rec.Code)
	}
	body := scrape(t, r)
	if !strings.Contains(body, `cocola_requests_total{`) ||
		!strings.Contains(body, `code="418"`) ||
		!strings.Contains(body, `method="GET /ping"`) ||
		!strings.Contains(body, `transport="http"`) {
		t.Errorf("RED counter not recorded as expected, got:\n%s", body)
	}
	if !strings.Contains(body, "cocola_request_duration_seconds_bucket") {
		t.Errorf("expected duration histogram buckets")
	}
}

func TestUnaryServerInterceptor_RecordsCodeAndDuration(t *testing.T) {
	r := New("grpc-svc")
	interceptor := r.UnaryServerInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/cocola.sandbox.v1.SandboxService/Acquire"}

	// success path -> code OK
	_, err := interceptor(context.Background(), nil, info,
		func(context.Context, any) (any, error) { return "ok", nil })
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	// error path -> code NotFound
	_, _ = interceptor(context.Background(), nil, info,
		func(context.Context, any) (any, error) { return nil, status.Error(codes.NotFound, "nope") })

	body := scrape(t, r)
	if !strings.Contains(body, `transport="grpc"`) ||
		!strings.Contains(body, `method="/cocola.sandbox.v1.SandboxService/Acquire"`) {
		t.Errorf("grpc method label missing, got:\n%s", body)
	}
	if !strings.Contains(body, `code="OK"`) || !strings.Contains(body, `code="NotFound"`) {
		t.Errorf("expected both OK and NotFound codes, got:\n%s", body)
	}
}

func TestMustRegister_CustomCollector(t *testing.T) {
	r := New("custom-svc")
	// Attaching a custom collector onto the shared registry must not panic and
	// must surface in the exposition (this is the seam sandbox-manager uses).
	r.MustRegister(newDummyCollector())
	body := scrape(t, r)
	if !strings.Contains(body, "cocola_dummy_metric") {
		t.Errorf("custom collector metric missing, got:\n%s", body)
	}
}
