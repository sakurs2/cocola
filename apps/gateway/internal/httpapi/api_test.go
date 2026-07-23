package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	"github.com/cocola-project/cocola/apps/gateway/internal/project"
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

type fakeProjectStore struct {
	project.Store
	created        project.Project
	current        project.Project
	updatedRuntime string
}

func (f *fakeProjectStore) GetProjectByRequest(
	_ context.Context,
	_ project.Identity,
	_ string,
) (project.Project, error) {
	return project.Project{}, project.ErrNotFound
}

func (f *fakeProjectStore) CreateProject(
	_ context.Context,
	value project.Project,
) (project.Project, error) {
	f.created = value
	return value, nil
}

func (f *fakeProjectStore) GetProject(
	_ context.Context,
	_ project.Identity,
	projectID string,
) (project.Project, error) {
	if f.current.ID != projectID {
		return project.Project{}, project.ErrNotFound
	}
	return f.current, nil
}

func (f *fakeProjectStore) UpdateProject(
	_ context.Context,
	_ project.Identity,
	_ string,
	_ int64,
	name string,
	description string,
	runtimeID string,
	updatedAt time.Time,
) (project.Project, error) {
	f.updatedRuntime = runtimeID
	f.current.Name = name
	f.current.Description = description
	f.current.RuntimeID = runtimeID
	f.current.UpdatedAt = updatedAt
	return f.current, nil
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
			PingEvery:   time.Hour,
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

func TestValidGitCommitSHA(t *testing.T) {
	for _, value := range []string{strings.Repeat("a", 40), strings.Repeat("F", 40)} {
		if !validGitCommitSHA(value) {
			t.Fatalf("validGitCommitSHA(%q) = false", value)
		}
	}
	for _, value := range []string{"", "abc123", strings.Repeat("g", 40), strings.Repeat("a", 41)} {
		if validGitCommitSHA(value) {
			t.Fatalf("validGitCommitSHA(%q) = true", value)
		}
	}
}

func TestProjectGitCommitCopiesDetailFields(t *testing.T) {
	value := projectGitCommit(agent.GitCommit{
		SHA: "a", Parents: []string{"b"}, Subject: "subject", Body: "body",
		AuthorName: "Ada", AuthoredAt: "2026-07-22T12:00:00Z", Refs: []string{"HEAD"},
		FilesChanged: 2, Additions: 3, Deletions: 1,
	})
	if value.SHA != "a" || value.Body != "body" || value.AuthorName != "Ada" || value.FilesChanged != 2 || value.Additions != 3 || value.Deletions != 1 {
		t.Fatalf("projectGitCommit() = %#v", value)
	}
	if len(value.Parents) != 1 || value.Parents[0] != "b" || len(value.Refs) != 1 || value.Refs[0] != "HEAD" {
		t.Fatalf("projectGitCommit slices = %#v", value)
	}
}

func TestAgentRuntimeCatalog(t *testing.T) {
	api := newConfiguredTestAPI(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithAgentRuntimes([]agent.Runtime{
			{ID: "claude-code", Label: "Claude Code", ModelProtocol: "anthropic-messages", IsDefault: true},
			{ID: "codex", Label: "Codex", ModelProtocol: "openai-responses"},
		})
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/agent-runtimes", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("runtime catalog status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{`"id":"claude-code"`, `"id":"codex"`, `"model_protocol":"openai-responses"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("runtime catalog missing %s: %s", want, body)
		}
	}
}

func TestProductConfig(t *testing.T) {
	api := newConfiguredTestAPI(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithProductConfig(ProductConfig{AgentRuntime: AgentRuntimeProductConfig{
			DefaultID: "claude-code", PickerEnabled: false,
		}})
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/product-config", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("product config status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var body ProductConfig
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.AgentRuntime.DefaultID != "claude-code" || body.AgentRuntime.PickerEnabled {
		t.Fatalf("product config = %+v", body)
	}
}

func TestProductConfigRequiresAuthWhenEnabled(t *testing.T) {
	api := newConfiguredTestAPI(
		&fakeStreamer{},
		auth.NewVerifier(auth.Config{Secret: "s", Issuer: "cocola"}),
		logger.Must(),
	)
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/product-config", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without token, got %d", recorder.Code)
	}
}

func TestChatUsesConfiguredDefaultRuntime(t *testing.T) {
	streamer := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	api := newConfiguredTestAPI(streamer, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithAgentRuntimes([]agent.Runtime{
			{ID: "claude-code", Label: "Claude Code", ModelProtocol: "anthropic-messages"},
			{ID: "codex", Label: "Codex", ModelProtocol: "openai-responses"},
		}).
		WithProductConfig(ProductConfig{AgentRuntime: AgentRuntimeProductConfig{
			DefaultID: "codex",
		}})

	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, httptest.NewRequest(
		http.MethodPost,
		"/v1/chat",
		strings.NewReader(`{"prompt":"hello","session_id":"configured-default"}`),
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("chat status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if streamer.gotQuery.RuntimeID != "codex" {
		t.Fatalf("forwarded runtime = %q, want codex", streamer.gotQuery.RuntimeID)
	}
}

func TestScheduledChatUsesConfiguredDefaultRuntime(t *testing.T) {
	streamer := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	api := newConfiguredTestAPI(streamer, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithAgentRuntimes([]agent.Runtime{
			{ID: "claude-code", Label: "Claude Code", ModelProtocol: "anthropic-messages"},
			{ID: "codex", Label: "Codex", ModelProtocol: "openai-responses"},
		}).
		WithProductConfig(ProductConfig{AgentRuntime: AgentRuntimeProductConfig{
			DefaultID: "codex",
		}})

	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, httptest.NewRequest(
		http.MethodPost,
		"/v1/chat",
		strings.NewReader(`{"prompt":"hello","session_id":"scheduled-default","conversation_type":"scheduled_task"}`),
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("scheduled chat status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if streamer.gotQuery.RuntimeID != "codex" {
		t.Fatalf("scheduled runtime = %q, want codex", streamer.gotQuery.RuntimeID)
	}
}

func TestCreateProjectUsesConfiguredDefaultRuntime(t *testing.T) {
	store := &fakeProjectStore{}
	service, err := project.New(store, project.Config{
		MaxRepositoryMB:         512,
		DisableGitHubConnector:  true,
		DisableGitHubAgentWrite: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	api := newConfiguredTestAPI(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithAgentRuntimes([]agent.Runtime{
			{ID: "claude-code", Label: "Claude Code", ModelProtocol: "anthropic-messages"},
			{ID: "codex", Label: "Codex", ModelProtocol: "openai-responses"},
		}).
		WithProductConfig(ProductConfig{AgentRuntime: AgentRuntimeProductConfig{
			DefaultID: "codex",
		}}).
		WithProjects(service)

	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, httptest.NewRequest(
		http.MethodPost,
		"/v1/projects",
		strings.NewReader(`{"client_request_id":"request-1","name":"Project","mode":"empty"}`),
	))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create project status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if store.created.RuntimeID != "codex" {
		t.Fatalf("project runtime = %q, want codex", store.created.RuntimeID)
	}
}

func TestUpdateProjectWithoutRuntimePreservesBinding(t *testing.T) {
	const projectID = "11111111-1111-1111-1111-111111111111"
	store := &fakeProjectStore{current: project.Project{
		ID: projectID, Name: "Project", RuntimeID: "codex", Version: 1,
	}}
	service, err := project.New(store, project.Config{
		MaxRepositoryMB:         512,
		DisableGitHubConnector:  true,
		DisableGitHubAgentWrite: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	api := newConfiguredTestAPI(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithAgentRuntimes([]agent.Runtime{
			{ID: "claude-code", Label: "Claude Code", ModelProtocol: "anthropic-messages"},
			{ID: "codex", Label: "Codex", ModelProtocol: "openai-responses"},
		}).
		WithProductConfig(DefaultProductConfig()).
		WithProjects(service)

	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, httptest.NewRequest(
		http.MethodPatch,
		"/v1/projects/"+projectID,
		strings.NewReader(`{"expected_version":1,"name":"Project","description":"updated"}`),
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("update project status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if store.updatedRuntime != "codex" {
		t.Fatalf("updated runtime = %q, want codex", store.updatedRuntime)
	}
}

func TestChatRuntimeIsImmutableAndMismatchHasNoWrites(t *testing.T) {
	streamer := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	conversations := convo.NewMemory()
	api := newConfiguredTestAPIWithConvo(
		streamer,
		auth.NewVerifier(auth.Config{}),
		logger.Must(),
		conversations,
	).WithAgentRuntimes([]agent.Runtime{
		{ID: "claude-code", Label: "Claude Code", ModelProtocol: "anthropic-messages", IsDefault: true},
		{ID: "codex", Label: "Codex", ModelProtocol: "openai-responses"},
	})
	handler := api.Handler()

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
		`{"prompt":"hello","session_id":"conversation-1","client_request_id":"request-1","runtime_id":"codex"}`,
	)))
	if first.Code != http.StatusOK {
		t.Fatalf("first chat status = %d, body=%s", first.Code, first.Body.String())
	}
	if streamer.gotQuery.RuntimeID != "codex" {
		t.Fatalf("forwarded runtime = %q, want codex", streamer.gotQuery.RuntimeID)
	}
	before, err := conversations.GetMessages(context.Background(), "conversation-1", auth.DevIdentity.UserID)
	if err != nil {
		t.Fatal(err)
	}

	mismatch := httptest.NewRecorder()
	handler.ServeHTTP(mismatch, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
		`{"prompt":"change runtime","session_id":"conversation-1","client_request_id":"request-2","runtime_id":"claude-code"}`,
	)))
	if mismatch.Code != http.StatusConflict || !strings.Contains(mismatch.Body.String(), "RUNTIME_MISMATCH") {
		t.Fatalf("runtime mismatch = %d, body=%s", mismatch.Code, mismatch.Body.String())
	}
	after, err := conversations.GetMessages(context.Background(), "conversation-1", auth.DevIdentity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("runtime mismatch wrote messages: before=%d after=%d", len(before), len(after))
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

func TestChatForwardsModelRouteID(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	h := newAPI(t, fs)

	req := httptest.NewRequest(
		"POST",
		"/v1/chat",
		strings.NewReader(`{"prompt":"hi","session_id":"s1","model_route_id":"route-1","model_alias":"claude-sonnet"}`),
	)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if fs.gotQuery.ModelRouteID != "route-1" {
		t.Fatalf("model route id not forwarded, got %q", fs.gotQuery.ModelRouteID)
	}
}

func TestChatForwardsAndPersistsSelectedSkill(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	store := convo.NewMemory()
	h := newConfiguredTestAPIWithConvo(
		fs, auth.NewVerifier(auth.Config{}), logger.Must(), store,
	).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(
		http.MethodPost,
		"/v1/chat",
		strings.NewReader(`{"prompt":"summarize","session_id":"s1","skill_id":"pdf"}`),
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("chat status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if fs.gotQuery.SkillID != "pdf" {
		t.Fatalf("forwarded skill = %q, want pdf", fs.gotQuery.SkillID)
	}
	messages, err := store.GetMessages(context.Background(), "s1", auth.DevIdentity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 || messages[0].Metadata["skill_id"] != "pdf" {
		t.Fatalf("user skill metadata = %#v", messages)
	}
}

func TestChatRejectsInvalidSkillBeforeWrites(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	store := convo.NewMemory()
	h := newConfiguredTestAPIWithConvo(
		fs, auth.NewVerifier(auth.Config{}), logger.Must(), store,
	).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(
		http.MethodPost,
		"/v1/chat",
		strings.NewReader(`{"prompt":"hello","session_id":"s1","skill_id":"../private"}`),
	))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "INVALID_SKILL_ID") {
		t.Fatalf("invalid skill status = %d, body=%s", rec.Code, rec.Body.String())
	}
	conversations, err := store.ListConversations(context.Background(), auth.DevIdentity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 0 || fs.gotQuery.SessionID != "" {
		t.Fatalf("invalid skill produced side effects: conversations=%+v query=%+v", conversations, fs.gotQuery)
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
