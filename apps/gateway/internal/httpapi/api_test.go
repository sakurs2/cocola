package httpapi

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	traceevents "github.com/cocola-project/cocola/apps/gateway/internal/traceevent"
	"github.com/cocola-project/cocola/packages/go-common/logger"
	"github.com/cocola-project/cocola/packages/go-common/metrics"
	"github.com/cocola-project/cocola/packages/go-common/token"
)

// fakeStreamer records the query it received and replays a fixed event script.
type fakeStreamer struct {
	gotQuery agent.Query
	script   []agent.Event
	err      error
	delay    time.Duration
}

type fakeTraceStore struct {
	runs  []traceevents.Run
	spans []traceevents.Span
}

func (f *fakeTraceStore) UpsertConversationRun(_ context.Context, run traceevents.Run) error {
	f.runs = append(f.runs, run)
	return nil
}

func (f *fakeTraceStore) UpsertConversationTraceSpan(_ context.Context, span traceevents.Span) error {
	f.spans = append(f.spans, span)
	return nil
}

func (f *fakeTraceStore) MarkConversationRunPartial(_ context.Context, traceID string) error {
	for index := range f.runs {
		if f.runs[index].TraceID == traceID {
			f.runs[index].DetailStatus = "partial"
		}
	}
	return nil
}

func (f *fakeStreamer) Stream(_ context.Context, q agent.Query, onEvent func(agent.Event) error) error {
	f.gotQuery = q
	for _, ev := range f.script {
		if f.delay > 0 {
			time.Sleep(f.delay)
		}
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
	return newConfiguredTestAPI(fs, v, log).Handler()
}

func newConfiguredTestAPI(fs agent.Streamer, v *auth.Verifier, log logger.Logger) *API {
	return newConfiguredTestAPIWithConvo(fs, v, log, convo.NewMemory())
}

func newConfiguredTestAPIWithConvo(fs agent.Streamer, v *auth.Verifier, log logger.Logger, conversations convo.Store) *API {
	return New(fs, v, log).
		WithConvoStore(conversations).
		WithChatRuns(chatrun.NewMemory(conversations), RunConfig{
			RunTimeout: time.Minute, PingEvery: time.Hour,
			MergeWindow: time.Millisecond, DraftInterval: time.Millisecond,
		})
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

func TestChatRequiresRunStore(t *testing.T) {
	h := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat", strings.NewReader(
		`{"prompt":"hi","session_id":"s1"}`,
	)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 without run store, got %d", rec.Code)
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

func TestChatConsumesInternalTraceEvents(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{
		{Kind: "trace", Data: map[string]string{
			"name": "sandbox.create", "category": "sandbox", "service": "agent-runtime",
			"duration_ms": "1", "status": "ok", "sandbox_id": "box-1", "reused": "false",
		}},
		{Kind: "trace", Data: map[string]string{
			"name": "sandbox.mcp_config_load", "category": "agent_init",
			"service": "agent-runtime", "duration_ms": "1", "status": "success",
		}},
		{Kind: "text", Data: map[string]string{"text": "hello"}},
		{Kind: "done", Data: map[string]string{"reason": "stop"}},
	}}
	trace := &fakeTraceStore{}
	h := newConfiguredTestAPI(fs, auth.NewVerifier(auth.Config{}), logger.Must()).WithTraceStore(trace).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat", strings.NewReader(
		`{"prompt":"hi","session_id":"s1"}`,
	)))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "event: trace") {
		t.Fatalf("internal trace event leaked to SSE: %s", rec.Body.String())
	}

	latest := make(map[string]traceevents.Span)
	for _, span := range trace.spans {
		latest[span.Name] = span
	}
	root := latest["conversation.run"]
	if root.SpanID == "" {
		t.Fatalf("missing conversation root: %+v", trace.spans)
	}
	for _, name := range []string{"sandbox.create", "sandbox.mcp_config_load"} {
		span := latest[name]
		if span.SpanID == "" || span.ParentSpanID != root.SpanID {
			t.Fatalf("trace %q is not attached to root: %+v", name, span)
		}
	}
	if latest["sandbox.create"].Attributes["sandbox_id"] != "box-1" {
		t.Fatalf("sandbox trace metadata = %+v", latest["sandbox.create"].Attributes)
	}
	if fs.gotQuery.ParentSpanID != root.SpanID {
		t.Fatalf("agent query parent=%q want root %q", fs.gotQuery.ParentSpanID, root.SpanID)
	}
}

func TestChatConversationRunExcludesPrompt(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{
		{Kind: "file", Data: map[string]string{"id": "a1", "filename": "out.html", "mime": "text/html", "size": "12"}},
		{Kind: "done"},
	}}
	trace := &fakeTraceStore{}
	v := auth.NewVerifier(auth.Config{})
	h := newConfiguredTestAPI(fs, v, logger.Must()).WithTraceStore(trace).Handler()

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/chat",
		strings.NewReader(`{"prompt":"secret prompt","session_id":"s1","model_alias":"claude","attachments":[{"filename":"a.txt","content_b64":"aGk=","mime":"text/plain"}]}`),
	)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if len(trace.runs) < 2 {
		t.Fatalf("want start and finish run writes, got %d", len(trace.runs))
	}
	run := trace.runs[len(trace.runs)-1]
	if run.ConversationID != "s1" || run.Status != "success" || run.ModelAlias != "claude" {
		t.Fatalf("bad conversation run: %+v", run)
	}
	for _, span := range trace.spans {
		if _, ok := span.Attributes["prompt"]; ok {
			t.Fatalf("prompt leaked into trace metadata: %+v", span.Attributes)
		}
	}
}

func TestChatForwardsModelAlias(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	h := newAPI(t, fs)

	req := httptest.NewRequest(
		"POST",
		"/v1/chat",
		strings.NewReader(`{"prompt":"hi","session_id":"s1","model_alias":"claude-sonnet"}`),
	)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if fs.gotQuery.ModelAlias != "claude-sonnet" {
		t.Fatalf("model alias not forwarded, got %q", fs.gotQuery.ModelAlias)
	}
}

func TestChatPersistsAssistantModelMetadata(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{
		{Kind: "text", Data: map[string]string{"text": "hello"}},
		{Kind: "done"},
	}}
	store := convo.NewMemory()
	v := auth.NewVerifier(auth.Config{})
	h := newConfiguredTestAPIWithConvo(fs, v, logger.Must(), store).Handler()

	req := httptest.NewRequest(
		"POST",
		"/v1/chat",
		strings.NewReader(`{"prompt":"hi","session_id":"s1","model_alias":"claude-sonnet","model_label":"Claude Sonnet","model_provider":"anthropic","model_family":"claude","model_icon_slug":"anthropic","model_icon":{"type":"lobe-icons","slug":"anthropic"}}`),
	)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	msgs, err := store.GetMessages(context.Background(), "s1", auth.DevIdentity.UserID)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("want user+assistant messages, got %d", len(msgs))
	}
	assistant := msgs[1]
	if assistant.Metadata["model_alias"] != "claude-sonnet" {
		t.Fatalf("model alias metadata = %#v", assistant.Metadata["model_alias"])
	}
	if assistant.Metadata["model_provider"] != "anthropic" ||
		assistant.Metadata["model_family"] != "claude" ||
		assistant.Metadata["model_icon_slug"] != "anthropic" {
		t.Fatalf("model identity metadata = %#v", assistant.Metadata)
	}
	icon, ok := assistant.Metadata["model_icon"].(map[string]string)
	if !ok || icon["slug"] != "anthropic" {
		t.Fatalf("model icon metadata = %#v", assistant.Metadata["model_icon"])
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
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(`{"prompt":"hi","session_id":"s1"}`))
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
	h := newConfiguredTestAPI(fs, v, logger.Must()).Handler()

	tok, err := token.Encode(token.Claims{Subject: "emp-42", Issuer: "cocola"}, "s")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(`{"prompt":"hi","session_id":"s1"}`))
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
	h := newConfiguredTestAPI(fs, v, logger.Must()).Handler()

	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(`{"prompt":"hi","session_id":"s1"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without token, got %d", rec.Code)
	}
}

// TestMetricsInstrumentation proves WithMetrics records the chat route into the
// shared registry, exposed via the metrics Mux. Auth is disabled so the request
// reaches the handler and is counted with code 200.
func TestMetricsInstrumentation(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	v := auth.NewVerifier(auth.Config{})
	reg := metrics.New("gateway-test")
	h := newConfiguredTestAPI(fs, v, logger.Must()).WithMetrics(reg).Handler()

	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(`{"prompt":"hi","session_id":"s1"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}

	srv := httptest.NewServer(reg.Mux())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	out := string(body)

	for _, want := range []string{
		`service="gateway-test"`,
		`transport="http"`,
		`method="POST /v1/chat"`,
		`code="200"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("metrics missing %q in:\n%s", want, out)
		}
	}
}

// TestChatDecodesAndForwardsAttachments proves the BFF base64-decodes inline
// attachment content to raw bytes and forwards filename/mime unchanged to the
// agent layer (push model, ADR-0017). "aGVsbG8=" decodes to "hello".
func TestChatDecodesAndForwardsAttachments(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	h := newAPI(t, fs)

	body := `{"prompt":"hi","session_id":"s1","attachments":[{"filename":"a.txt","content_b64":"aGVsbG8=","mime":"text/plain"}]}`
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if len(fs.gotQuery.Attachments) != 1 {
		t.Fatalf("want 1 attachment forwarded, got %d", len(fs.gotQuery.Attachments))
	}
	att := fs.gotQuery.Attachments[0]
	if att.Filename != "a.txt" || att.Mime != "text/plain" {
		t.Fatalf("attachment metadata not forwarded: %+v", att)
	}
	if string(att.Content) != "hello" {
		t.Fatalf("content not base64-decoded, got %q", att.Content)
	}
}

// TestChatDropsAttachmentWithInvalidBase64 proves a malformed content_b64 is
// skipped (dropped) rather than aborting the whole request.
func TestChatDropsAttachmentWithInvalidBase64(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	h := newAPI(t, fs)

	body := `{"prompt":"hi","session_id":"s1","attachments":[{"filename":"bad.bin","content_b64":"!!!not-base64!!!","mime":"application/octet-stream"}]}`
	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if len(fs.gotQuery.Attachments) != 0 {
		t.Fatalf("invalid-base64 attachment should be dropped, got %d", len(fs.gotQuery.Attachments))
	}
}

// ---- P1a: object-store upload + threshold split ----

// fakeObjStore records every Put and lets Get echo them back.
type fakeObjStore struct {
	puts map[string][]byte
	mime map[string]string
}

func newFakeObjStore() *fakeObjStore {
	return &fakeObjStore{puts: map[string][]byte{}, mime: map[string]string{}}
}
func (f *fakeObjStore) Put(_ context.Context, key string, data []byte, mime string) error {
	cp := make([]byte, len(data))
	copy(cp, data)
	f.puts[key] = cp
	f.mime[key] = mime
	return nil
}
func (f *fakeObjStore) Get(_ context.Context, key string) ([]byte, error) { return f.puts[key], nil }
func (f *fakeObjStore) Health(context.Context) error                      { return nil }

// b64 is a tiny helper for building attachment bodies.
func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func newAPIWithStore(t *testing.T, fs *fakeStreamer, store *fakeObjStore, threshold int64) http.Handler {
	t.Helper()
	v := auth.NewVerifier(auth.Config{})
	log := logger.Must()
	return newConfiguredTestAPI(fs, v, log).WithObjStore(store, threshold).Handler()
}

func TestChatUploadsAndSplitsBelowThreshold(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	store := newFakeObjStore()
	// threshold high enough that the small file stays inline.
	h := newAPIWithStore(t, fs, store, 1024)

	body := `{"prompt":"hi","session_id":"s1","attachments":[{"filename":"a.txt","content_b64":"` + b64("hello") + `","mime":"text/plain"}]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat", strings.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if len(store.puts) != 1 {
		t.Fatalf("want 1 upload, got %d", len(store.puts))
	}
	if len(fs.gotQuery.Attachments) != 1 {
		t.Fatalf("want 1 attachment forwarded, got %d", len(fs.gotQuery.Attachments))
	}
	att := fs.gotQuery.Attachments[0]
	if att.OssKey == "" {
		t.Fatal("small file should carry an OssKey")
	}
	if string(att.Content) != "hello" {
		t.Fatalf("small file should keep inline content, got %q", att.Content)
	}
	if att.Size != 5 {
		t.Fatalf("Size should be original byte length, got %d", att.Size)
	}
	if !strings.HasPrefix(att.OssKey, "attachments/s1/") || !strings.HasSuffix(att.OssKey, "-a.txt") {
		t.Fatalf("unexpected key shape: %q", att.OssKey)
	}
}

func TestChatDropsInlineContentAboveThreshold(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	store := newFakeObjStore()
	// threshold 3 bytes: "hello" (5) is large => key-only.
	h := newAPIWithStore(t, fs, store, 3)

	body := `{"prompt":"hi","session_id":"s1","attachments":[{"filename":"big.bin","content_b64":"` + b64("hello") + `","mime":"application/octet-stream"}]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat", strings.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	att := fs.gotQuery.Attachments[0]
	if att.OssKey == "" {
		t.Fatal("large file must carry an OssKey")
	}
	if att.Content != nil {
		t.Fatalf("large file must not carry inline content, got %d bytes", len(att.Content))
	}
	if att.Size != 5 {
		t.Fatalf("Size should still be 5, got %d", att.Size)
	}
	// The store still holds the full bytes (source of truth).
	if got := store.puts[att.OssKey]; string(got) != "hello" {
		t.Fatalf("store should hold full bytes, got %q", got)
	}
}

func TestChatWithoutStoreStaysInline(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	h := newAPI(t, fs) // no store wired

	body := `{"prompt":"hi","session_id":"s1","attachments":[{"filename":"a.txt","content_b64":"` + b64("hello") + `"}]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat", strings.NewReader(body)))

	att := fs.gotQuery.Attachments[0]
	if att.OssKey != "" {
		t.Fatalf("no store => no OssKey, got %q", att.OssKey)
	}
	if string(att.Content) != "hello" {
		t.Fatalf("inline content expected, got %q", att.Content)
	}
}

func TestSanitizeKeySegment(t *testing.T) {
	cases := map[string]string{
		"a.txt":     "a.txt",
		"a/b/c.png": "c.png",
		"":          "file",
		"...":       "file",
	}
	cases["../../"+"e"+"tc/pw"] = "pw"
	cases["a\\b\\c.png"] = "c.png"
	for in, want := range cases {
		if got := sanitizeKeySegment(in); got != want {
			t.Errorf("sanitizeKeySegment(%q)=%q want %q", in, got, want)
		}
	}
}
