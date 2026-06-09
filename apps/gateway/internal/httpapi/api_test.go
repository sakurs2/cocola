package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/packages/go-common/logger"
	"github.com/cocola-project/cocola/packages/go-common/token"
)

// fakeStreamer records the query it received and replays a fixed event script.
type fakeStreamer struct {
	gotQuery agent.Query
	script   []agent.Event
	err      error
}

func (f *fakeStreamer) Stream(_ context.Context, q agent.Query, onEvent func(agent.Event) error) error {
	f.gotQuery = q
	for _, ev := range f.script {
		if err := onEvent(ev); err != nil {
			return err
		}
	}
	return f.err
}

func newAPI(t *testing.T, fs *fakeStreamer) http.Handler {
	t.Helper()
	// Auth disabled (no secret) so tests focus on the SSE path; identity becomes
	// DevIdentity. A dedicated auth-on test covers user_id derivation.
	v := auth.NewVerifier(auth.Config{})
	log := logger.Must()
	return New(fs, v, log).Handler()
}

func TestHealthz(t *testing.T) {
	h := newAPI(t, &fakeStreamer{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("health body = %q", rec.Body.String())
	}
}

func TestChatStreamsSSE(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{
		{Kind: "text", Data: map[string]string{"text": "hello"}},
		{Kind: "done", Data: map[string]string{"reason": "stop"}},
	}}
	h := newAPI(t, fs)

	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(`{"prompt":"hi","session_id":"s1"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("content-type"); ct != "text/event-stream" {
		t.Fatalf("want SSE content-type, got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: text\n") || !strings.Contains(body, `"text":"hello"`) {
		t.Fatalf("missing text frame: %q", body)
	}
	if !strings.Contains(body, "event: done\n") {
		t.Fatalf("missing done frame: %q", body)
	}
	// Each frame must terminate with a blank line (SSE record separator).
	if strings.Count(body, "\n\n") < 2 {
		t.Fatalf("want >=2 SSE records, body=%q", body)
	}
	// The prompt must be forwarded; session honored.
	if fs.gotQuery.Prompt != "hi" || fs.gotQuery.SessionID != "s1" {
		t.Fatalf("query not forwarded: %+v", fs.gotQuery)
	}
}

func TestChatRejectsEmptyPrompt(t *testing.T) {
	h := newAPI(t, &fakeStreamer{})
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(`{"prompt":"   "}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for empty prompt, got %d", rec.Code)
	}
}

func TestChatRejectsMalformedJSON(t *testing.T) {
	h := newAPI(t, &fakeStreamer{})
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(`{bad`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for bad JSON, got %d", rec.Code)
	}
}

func TestChatStreamErrorBecomesTerminalEvent(t *testing.T) {
	fs := &fakeStreamer{
		script: []agent.Event{{Kind: "text", Data: map[string]string{"text": "partial"}}},
		err:    context.DeadlineExceeded,
	}
	h := newAPI(t, fs)
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(`{"prompt":"hi"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: error\n") {
		t.Fatalf("stream error should emit a terminal error event, body=%q", body)
	}
}

func TestChatUsesIdentityUserID(t *testing.T) {
	// With auth ON, the forwarded user_id must come from the verified token,
	// not from the request body (which has no user_id field anyway).
	fs := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	v := auth.NewVerifier(auth.Config{Secret: "s", Issuer: "cocola"})
	h := New(fs, v, logger.Must()).Handler()

	tok, err := token.Encode(token.Claims{Subject: "emp-42", Issuer: "cocola"}, "s")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(`{"prompt":"hi"}`))
	req.Header.Set("authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if fs.gotQuery.UserID != "emp-42" {
		t.Fatalf("user_id must come from token, got %q", fs.gotQuery.UserID)
	}
}

func TestChatRequiresAuthWhenEnabled(t *testing.T) {
	fs := &fakeStreamer{}
	v := auth.NewVerifier(auth.Config{Secret: "s", Issuer: "cocola"})
	h := New(fs, v, logger.Must()).Handler()

	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(`{"prompt":"hi"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without token, got %d", rec.Code)
	}
}
