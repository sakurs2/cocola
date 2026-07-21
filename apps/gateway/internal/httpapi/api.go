// Package httpapi is the gateway's public BFF surface. It owns HTTP termination,
// auth (via internal/auth), request shaping, and streaming agent events back to
// the browser over Server-Sent Events (SSE).
//
// Why SSE rather than WebSocket? The agent interaction is one-shot and
// unidirectional once started: the client POSTs a prompt and consumes a stream
// of events until done. SSE is plain HTTP (one response, chunked + flushed),
// needs no extra dependency, survives proxies, and reconnects natively in the
// browser. WebSocket's bidirectional channel buys nothing here.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/apps/gateway/internal/memory"
	"github.com/cocola-project/cocola/apps/gateway/internal/objstore"
	"github.com/cocola-project/cocola/apps/gateway/internal/project"
	"github.com/cocola-project/cocola/apps/gateway/internal/sandboxmgr"
	traceevents "github.com/cocola-project/cocola/apps/gateway/internal/traceevent"
	"github.com/cocola-project/cocola/packages/go-common/logger"
	"github.com/cocola-project/cocola/packages/go-common/metrics"
	"github.com/cocola-project/cocola/packages/go-common/token"
	"github.com/cocola-project/cocola/packages/go-common/tracing"
)

// DefaultInlineMaxBytes is the attachment size threshold used when none is
// configured: files at or below it are pushed inline into the sandbox, larger
// ones are delivered key-only and pulled by agent-runtime (ADR-0017 P1a).
const DefaultInlineMaxBytes int64 = 16 * 1024 * 1024

type conversationRootSpanKey struct{}

var traceAttributeAllowlist = map[string]bool{
	"accepted_count": true, "action": true, "artifact_count": true,
	"attachment_count": true, "chat_type": true, "content_length": true,
	"conversation_id": true, "error_code": true, "error_type": true,
	"event_count": true, "event_kind": true, "inline_count": true,
	"mcp_count": true, "mcp_names": true, "model_route_id": true, "model_alias": true,
	"object_count": true, "part_count": true, "prompt_count": true,
	"prompt_ids": true, "prompt_versions": true, "restored": true,
	"resumed": true, "reused": true, "sandbox_id": true, "session_id": true,
	"runtime_id":  true,
	"skill_count": true, "target": true, "text_chunk_count": true,
	"thinking_chunk_count": true, "tool_name": true, "tool_result_count": true,
	"tool_type": true, "tool_use_count": true,
}

func conversationRootSpan(ctx context.Context) string {
	rootSpanID, _ := ctx.Value(conversationRootSpanKey{}).(string)
	return rootSpanID
}

func safeTraceAttributes(attributes map[string]any) map[string]any {
	if len(attributes) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any)
	for key, value := range attributes {
		if traceAttributeAllowlist[key] {
			out[key] = value
		}
	}
	return out
}

// API wires the BFF dependencies. The agent.Streamer is an interface so tests
// can inject a fake without a real agent-runtime.
type API struct {
	streamer     agent.Streamer
	releaser     agent.Releaser
	gitInspector agent.GitInspector
	verifier     *auth.Verifier
	log          logger.Logger
	metrics      *metrics.Registry // optional; nil => no instrumentation (tests)
	// store is the attachment source-of-truth object store. nil => P0 path:
	// attachments are pushed inline only, no upload (feature stays dark until
	// MinIO is configured). inlineMaxBytes is the small/large split threshold.
	store          objstore.Store
	inlineMaxBytes int64
	// convo is the conversation-persistence store (route A UI-message mirror).
	// Production always uses Postgres; nil is supported only by non-chat tests.
	convo convo.Store
	trace traceevents.Store
	// sandboxTokenIssuer mints a fresh per-user cocola token per chat turn from
	// the verified identity; agent-runtime injects it into the sandbox as
	// ANTHROPIC_AUTH_TOKEN so downstream quota/usage/revocation bind to the real
	// user. nil is supported by unauthenticated tests; production always wires
	// the issuer. sandboxTokenTTL is the mint TTL.
	sandboxTokenIssuer *token.Issuer
	sandboxTokenTTL    time.Duration
	runs               *runController
	runtimes           []agent.Runtime
	runtimeByID        map[string]agent.Runtime
	// sandboxResolver powers the Preview Proxy: it maps a session + in-sandbox
	// port to a reachable URL via sandbox-manager. nil disables /v1/preview
	// (the route returns 501), keeping the feature dark until wired in main.
	sandboxResolver sandboxmgr.EndpointResolver
	memory          *memory.Service
	projects        *project.Service
}

// New builds the BFF API.
func New(streamer agent.Streamer, verifier *auth.Verifier, log logger.Logger) *API {
	a := &API{streamer: streamer, verifier: verifier, log: log}
	if releaser, ok := streamer.(agent.Releaser); ok {
		a.releaser = releaser
	}
	if inspector, ok := streamer.(agent.GitInspector); ok {
		a.gitInspector = inspector
	}
	return a.WithAgentRuntimes([]agent.Runtime{{
		ID: "claude-code", Label: "Claude Code", ModelProtocol: "anthropic-messages", IsDefault: true,
	}})
}

// WithAgentRuntimes installs the startup-cached catalog returned by
// agent-runtime. Production always calls this before serving HTTP.
func (a *API) WithAgentRuntimes(runtimes []agent.Runtime) *API {
	a.runtimes = append([]agent.Runtime(nil), runtimes...)
	a.runtimeByID = make(map[string]agent.Runtime, len(runtimes))
	for _, runtime := range runtimes {
		a.runtimeByID[runtime.ID] = runtime
	}
	return a
}

// WithMetrics enables RED instrumentation on the public routes. The registry is
// shared with the observability port mounted in main; passing nil (the default)
// leaves the API uninstrumented, which keeps unit tests dependency-light.
func (a *API) WithMetrics(reg *metrics.Registry) *API { a.metrics = reg; return a }

// WithSandboxResolver enables the Preview Proxy route. Production passes the
// sandbox-manager gRPC client; nil (the default) leaves /v1/preview returning
// 501 so environments without a reachable sandbox-manager stay dark.
func (a *API) WithSandboxResolver(r sandboxmgr.EndpointResolver) *API {
	a.sandboxResolver = r
	return a
}

// WithObjStore enables the attachment source-of-truth path: every uploaded file
// is PutObject'd to the store, then split by inlineMaxBytes — files at or below
// it keep their inline bytes AND carry the object key; larger files are
// delivered key-only and pulled by agent-runtime on the model's behalf
// (ADR-0017 P1a). A non-positive threshold falls back to DefaultInlineMaxBytes.
// Production always passes a store; nil is retained only for focused handler tests.
func (a *API) WithObjStore(store objstore.Store, inlineMaxBytes int64) *API {
	a.store = store
	if inlineMaxBytes <= 0 {
		inlineMaxBytes = DefaultInlineMaxBytes
	}
	a.inlineMaxBytes = inlineMaxBytes
	return a
}

// WithConvoStore enables conversation persistence (route A): the chat handler
// mirrors each turn's rendered messages into the store, and the read endpoints
// serve a user's conversation list + history. Production always configures the
// Postgres store; tests may inject an in-memory implementation.
func (a *API) WithConvoStore(store convo.Store) *API { a.convo = store; return a }

// WithTraceStore enables conversation audit summaries and detailed traces.
func (a *API) WithTraceStore(store traceevents.Store) *API { a.trace = store; return a }

// WithMemory installs the optional OpenViking integration. The rest of the
// Gateway only depends on this high-level module and never calls OpenViking.
func (a *API) WithMemory(service *memory.Service) *API { a.memory = service; return a }

// WithProjects installs the high-level GitHub Project module. GitHub remains
// unreachable from every other gateway package.
func (a *API) WithProjects(service *project.Service) *API { a.projects = service; return a }

// WithChatRuns configures the single-Gateway background execution path. It is
// required for chat; there is deliberately no feature flag or distributed
// worker mode.
func (a *API) WithChatRuns(store chatrun.Store, cfg RunConfig) *API {
	a.runs = newRunController(store, cfg)
	return a
}

// WithAgentReleaser injects the best-effort session releaser used by
// conversation deletion tests or alternate runtimes.
func (a *API) WithAgentReleaser(releaser agent.Releaser) *API { a.releaser = releaser; return a }

// WithSandboxTokenIssuer enables per-user sandbox tokens: the chat handler mints
// a fresh cocola token per turn (sub=identity.UserID, ten=identity.TenantID) and
// forwards it to agent-runtime, which injects it as the sandbox
// ANTHROPIC_AUTH_TOKEN. Passing nil is reserved for unauthenticated tests.
func (a *API) WithSandboxTokenIssuer(issuer *token.Issuer, ttl time.Duration) *API {
	a.sandboxTokenIssuer = issuer
	a.sandboxTokenTTL = ttl
	return a
}

// mintSandboxToken issues a per-user, short-lived cocola token for one turn. A
// mint failure is logged and the downstream Run fails closed without silently
// switching to a shared identity.
func (a *API) mintSandboxToken(id auth.Identity) string {
	if a.sandboxTokenIssuer == nil {
		return ""
	}
	tok, _, err := a.sandboxTokenIssuer.Issue(id.UserID, id.TenantID, a.sandboxTokenTTL, 0)
	if err != nil {
		a.log.Warn("sandbox token mint failed; run will fail closed: " + err.Error())
		return ""
	}
	return tok
}

// instrument wraps a handler with the RED middleware under a fixed route label,
// or returns it unchanged when metrics are disabled.
func (a *API) instrument(route string, h http.Handler) http.Handler {
	if a.metrics == nil {
		return h
	}
	return a.metrics.HTTPMiddleware(route, h)
}

// Handler returns the fully-wired http.Handler (stdlib mux; the BFF has a tiny
// route set so chi would be overkill here).
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", a.instrument("GET /healthz", http.HandlerFunc(a.health)))
	// Auth-guarded chat endpoint. The mux dispatches by method+path; the
	// verifier middleware wraps just this handler, and the RED middleware wraps
	// the whole chain so latency includes auth.
	mux.Handle("POST /v1/chat", a.instrument("POST /v1/chat",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.chat))))
	mux.Handle("GET /v1/agent-runtimes", a.instrument("GET /v1/agent-runtimes",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.listAgentRuntimes))))
	mux.Handle("GET /v1/chat/runs/{run_id}", a.instrument("GET /v1/chat/runs/{run_id}",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.streamRun))))
	mux.Handle("DELETE /v1/chat/runs/{run_id}", a.instrument("DELETE /v1/chat/runs/{run_id}",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.cancelRun))))
	mux.Handle("GET /v1/chat/runs/active", a.instrument("GET /v1/chat/runs/active",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.activeRun))))
	// Conversation history (route A). Both are auth-guarded so a caller only ever
	// sees their own conversations (ownership from the verified identity).
	mux.Handle("GET /v1/conversations", a.instrument("GET /v1/conversations",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.listConversations))))
	mux.Handle("GET /v1/folders", a.instrument("GET /v1/folders",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.listFolders))))
	mux.Handle("POST /v1/folders", a.instrument("POST /v1/folders",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.createFolder))))
	mux.Handle("PATCH /v1/folders/{id}", a.instrument("PATCH /v1/folders/{id}",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.renameFolder))))
	mux.Handle("DELETE /v1/folders/{id}", a.instrument("DELETE /v1/folders/{id}",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.deleteFolder))))
	mux.Handle("PATCH /v1/conversations/{id}", a.instrument("PATCH /v1/conversations/{id}",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.renameConversation))))
	mux.Handle("PUT /v1/conversations/{id}/folder", a.instrument("PUT /v1/conversations/{id}/folder",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.moveConversationToFolder))))
	mux.Handle("DELETE /v1/conversations/{id}", a.instrument("DELETE /v1/conversations/{id}",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.deleteConversation))))
	mux.Handle("GET /v1/conversations/{id}/messages", a.instrument("GET /v1/conversations/{id}/messages",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.conversationMessages))))
	mux.Handle("GET /v1/conversations/{id}/artifacts/{artifact_id}", a.instrument("GET /v1/conversations/{id}/artifacts/{artifact_id}",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.downloadArtifact))))
	mux.Handle("GET /v1/memory/settings", a.instrument("GET /v1/memory/settings",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.memorySettings))))
	mux.Handle("PATCH /v1/memory/settings", a.instrument("PATCH /v1/memory/settings",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.updateMemorySettings))))
	mux.Handle("GET /v1/memory/items", a.instrument("GET /v1/memory/items",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.memoryItems))))
	mux.Handle("GET /v1/memory/items/{id}", a.instrument("GET /v1/memory/items/{id}",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.memoryItem))))
	mux.Handle("DELETE /v1/memory/items/{id}", a.instrument("DELETE /v1/memory/items/{id}",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.deleteMemoryItem))))
	mux.Handle("DELETE /v1/memory/items", a.instrument("DELETE /v1/memory/items",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.clearMemory))))
	mux.Handle("GET /v1/scm/github/connection", a.instrument("GET /v1/scm/github/connection",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.githubConnection))))
	mux.Handle("POST /v1/scm/github/oauth/start", a.instrument("POST /v1/scm/github/oauth/start",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.githubOAuthStart))))
	mux.Handle("POST /v1/scm/github/oauth/callback", a.instrument("POST /v1/scm/github/oauth/callback",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.githubOAuthCallback))))
	mux.Handle("DELETE /v1/scm/github/connection", a.instrument("DELETE /v1/scm/github/connection",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.githubDisconnect))))
	mux.Handle("GET /v1/scm/github/repositories", a.instrument("GET /v1/scm/github/repositories",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.githubRepositories))))
	mux.Handle("GET /v1/projects", a.instrument("GET /v1/projects",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.listProjects))))
	mux.Handle("POST /v1/projects", a.instrument("POST /v1/projects",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.createProject))))
	mux.Handle("GET /v1/projects/{id}", a.instrument("GET /v1/projects/{id}",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.getProject))))
	mux.Handle("PATCH /v1/projects/{id}", a.instrument("PATCH /v1/projects/{id}",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.updateProject))))
	mux.Handle("DELETE /v1/projects/{id}", a.instrument("DELETE /v1/projects/{id}",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.archiveProject))))
	mux.Handle("POST /v1/projects/{id}/retry", a.instrument("POST /v1/projects/{id}/retry",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.retryProject))))
	mux.Handle("GET /v1/projects/{id}/tasks", a.instrument("GET /v1/projects/{id}/tasks",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.projectTasks))))
	mux.Handle("GET /v1/conversations/{id}/git/status", a.instrument("GET /v1/conversations/{id}/git/status",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.gitStatus))))
	mux.Handle("POST /v1/conversations/{id}/git/inspect", a.instrument("POST /v1/conversations/{id}/git/inspect",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.inspectGit))))
	// Preview Proxy: reverse-proxy a user-launched in-sandbox dev server. The
	// trailing {rest...} wildcard captures the remaining path so nested asset
	// requests are proxied too.
	mux.Handle("/v1/preview/{session_id}/{port}/{rest...}", a.instrument("/v1/preview",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.previewProxy))))
	mux.Handle("/v1/preview/{session_id}/{port}", a.instrument("/v1/preview",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.previewProxy))))
	// Tracing: wrap the whole mux so an inbound W3C traceparent is extracted and
	// a server span is started before auth/handlers run; the span context then
	// flows into the agent gRPC call (client stats handler) for an end-to-end
	// trace. No-op overhead when tracing is disabled.
	return tracing.HTTPHandler("gateway.http", mux)
}

func (a *API) listAgentRuntimes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.runtimes)
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	if a.runs != nil && (a.runs.shutting.Load() || a.runs.databaseUnavailable.Load()) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) recordTrace(ctx context.Context, traceID, name, category string, startedAt time.Time, status string, metadata map[string]any) {
	parentSpanID := conversationRootSpan(ctx)
	if a.trace == nil || traceID == "" || parentSpanID == "" {
		return
	}
	if status == "" || status == "ok" {
		status = "success"
	}
	span := traceevents.Span{
		TraceID:       traceID,
		SpanID:        traceevents.NewSpanID(),
		ParentSpanID:  parentSpanID,
		SchemaVersion: 1,
		Service:       "gateway",
		Name:          name,
		Category:      category,
		StartedAt:     startedAt.UTC(),
		DurationUS:    time.Since(startedAt).Microseconds(),
		Status:        status,
		Attributes:    safeTraceAttributes(metadata),
	}
	a.upsertTraceSpan(ctx, span)
}

func (a *API) upsertTraceSpan(ctx context.Context, span traceevents.Span) {
	if a.trace == nil || span.TraceID == "" || span.Name == "" {
		return
	}
	if err := a.trace.UpsertConversationTraceSpan(context.Background(), span); err != nil {
		a.log.Warn("trace event write failed: " + err.Error())
	}
}

func traceEventStartedAt(data map[string]string, fallback time.Time) time.Time {
	raw := strings.TrimSpace(data["started_at_unix_ms"])
	if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
		return time.UnixMilli(parsed)
	}
	return fallback
}

func traceEventDurationUS(data map[string]string) int64 {
	if raw := strings.TrimSpace(data["duration_us"]); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			return parsed
		}
	}
	if raw := strings.TrimSpace(data["duration_ms"]); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			return parsed * 1000
		}
	}
	return 0
}

func environmentPreparationComplete(data map[string]string) bool {
	var snapshot struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal([]byte(data["snapshot"]), &snapshot); err != nil {
		return false
	}
	return snapshot.State == "ready" || snapshot.State == "degraded"
}

func (a *API) recordAgentTrace(ctx context.Context, traceID string, data map[string]string) {
	if a.trace == nil || traceID == "" {
		return
	}
	name := strings.TrimSpace(data["name"])
	if name == "" {
		return
	}
	durationUS := traceEventDurationUS(data)
	startedAt := traceEventStartedAt(data, time.Now().Add(-time.Duration(durationUS)*time.Microsecond))
	service := strings.TrimSpace(data["service"])
	if service == "" {
		service = "agent-runtime"
	}
	category := strings.TrimSpace(data["category"])
	if category == "" {
		category = service
	}
	status := strings.TrimSpace(data["status"])
	if status == "" || status == "ok" {
		status = "success"
	}
	metadata := make(map[string]any, len(data))
	for k, v := range data {
		switch k {
		case "name", "category", "service", "started_at_unix_ms", "duration_ms", "duration_us", "status", "span_id", "parent_span_id", "schema_version":
			continue
		default:
			metadata[k] = v
		}
	}
	spanID := strings.TrimSpace(data["span_id"])
	if spanID == "" {
		spanID = traceevents.NewSpanID()
	}
	parentSpanID := strings.TrimSpace(data["parent_span_id"])
	if parentSpanID == "" {
		parentSpanID = conversationRootSpan(ctx)
	}
	a.upsertTraceSpan(ctx, traceevents.Span{
		TraceID:       traceID,
		SpanID:        spanID,
		ParentSpanID:  parentSpanID,
		SchemaVersion: 1,
		Service:       service,
		Name:          name,
		Category:      category,
		StartedAt:     startedAt.UTC(),
		DurationUS:    durationUS,
		Status:        status,
		Attributes:    safeTraceAttributes(metadata),
	})
}

// chatRequest is the client-supplied body. user_id/session scoping comes from
// the verified identity, NOT from the body, so a caller cannot impersonate
// another user. session_id and sandbox_id are caller-chosen routing hints.
type chatRequest struct {
	Prompt                               string            `json:"prompt"`
	SessionID                            string            `json:"session_id"`
	SandboxID                            string            `json:"sandbox_id"`
	MaxTurns                             int32             `json:"max_turns"`
	ModelRouteID                         string            `json:"model_route_id"`
	ModelAlias                           string            `json:"model_alias"`
	ModelLabel                           string            `json:"model_label"`
	ModelProvider                        string            `json:"model_provider"`
	ModelFamily                          string            `json:"model_family"`
	ModelIconSlug                        string            `json:"model_icon_slug"`
	ModelIcon                            map[string]string `json:"model_icon"`
	ConversationTitle                    string            `json:"conversation_title"`
	ConversationType                     string            `json:"conversation_type"`
	DeferConversationVisibilityUntilDone bool              `json:"defer_conversation_visibility_until_done"`
	Attachments                          []attachmentDTO   `json:"attachments"`
	ClientRequestID                      string            `json:"client_request_id"`
	RuntimeID                            string            `json:"runtime_id"`
	FolderID                             string            `json:"folder_id"`
	ProjectID                            string            `json:"project_id"`
	SkillID                              string            `json:"skill_id"`
	AllowWorkspaceReset                  bool              `json:"allow_workspace_reset"`
}

// attachmentDTO is one user-uploaded file carried inline in the chat body.
// Content is base64 because JSON has no binary type; the gateway decodes it to
// raw bytes before forwarding over gRPC (proto `bytes`), which keeps images and
// other binaries intact. This is the P0 inline transport (push model, ADR-0017);
// large-file/OSS offload is a documented TODO.
type attachmentDTO struct {
	Filename   string `json:"filename"`
	ContentB64 string `json:"content_b64"`
	Mime       string `json:"mime"`
}

func (a *API) startConversationRun(ctx context.Context, id auth.Identity, req chatRequest, traceID, rootSpanID string, startedAt time.Time) traceevents.Run {
	source := "interactive"
	if chatTypeForConversation(req) == "scheduled_task" {
		source = "scheduled_task"
	}
	run := traceevents.Run{
		TraceID:           traceID,
		RootSpanID:        rootSpanID,
		ConversationID:    strings.TrimSpace(req.SessionID),
		ConversationTitle: titleForConversation(req),
		UserID:            id.UserID,
		UserEmail:         id.Email,
		Source:            source,
		ModelAlias:        strings.TrimSpace(req.ModelAlias),
		Status:            "running",
		StartedAt:         startedAt.UTC(),
		LastActivityAt:    time.Now().UTC(),
		DetailStatus:      "available",
	}
	if a.trace == nil {
		return run
	}
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelWrite()
	if err := a.trace.UpsertConversationRun(writeCtx, run); err != nil {
		a.log.Warn("conversation run start failed: " + err.Error())
		return run
	}
	a.upsertTraceSpan(ctx, traceevents.Span{
		TraceID: traceID, SpanID: rootSpanID, SchemaVersion: 1,
		Service: "gateway", Name: "conversation.run", Category: "request",
		StartedAt: startedAt.UTC(), Status: "running",
		Attributes: map[string]any{
			"conversation_id": run.ConversationID,
			"chat_type":       source,
			"model_alias":     run.ModelAlias,
		},
	})
	return run
}

func (a *API) finishConversationRun(ctx context.Context, run traceevents.Run, status, errorCode string, ttftMS int64, toolCalls int64) {
	if a.trace == nil || run.TraceID == "" {
		return
	}
	now := time.Now().UTC()
	run.Status = status
	run.CompletedAt = now
	run.LastActivityAt = now
	run.DurationMS = now.Sub(run.StartedAt).Milliseconds()
	run.TTFTMS = ttftMS
	run.ToolCallCount = toolCalls
	run.ErrorCode = errorCode
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelWrite()
	if err := a.trace.UpsertConversationRun(writeCtx, run); err != nil {
		a.log.Warn("conversation run finish failed: " + err.Error())
	}
	a.upsertTraceSpan(ctx, traceevents.Span{
		TraceID: run.TraceID, SpanID: run.RootSpanID, SchemaVersion: 1,
		Service: "gateway", Name: "conversation.run", Category: "request",
		StartedAt: run.StartedAt, DurationUS: now.Sub(run.StartedAt).Microseconds(), Status: status,
		Attributes: safeTraceAttributes(map[string]any{
			"conversation_id": run.ConversationID,
			"chat_type":       run.Source,
			"model_alias":     run.ModelAlias,
			"tool_use_count":  toolCalls,
			"error_code":      errorCode,
		}),
	})
}

func chatStartedAt(r *http.Request) time.Time {
	raw, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get("x-cocola-chat-started-at-ms")), 10, 64)
	if err != nil || raw <= 0 {
		return time.Now()
	}
	started := time.UnixMilli(raw)
	age := time.Since(started)
	if age < 0 || age > time.Minute {
		return time.Now()
	}
	return started
}

func assistantMetadata(req chatRequest) map[string]any {
	out := make(map[string]any)
	if routeID := effectiveModelRouteID(req); routeID != "" {
		out["model_route_id"] = routeID
	}
	if alias := strings.TrimSpace(req.ModelAlias); alias != "" {
		out["model_alias"] = alias
	}
	if label := strings.TrimSpace(req.ModelLabel); label != "" {
		out["model_label"] = label
	}
	if provider := strings.TrimSpace(req.ModelProvider); provider != "" {
		out["model_provider"] = provider
	}
	if family := strings.TrimSpace(req.ModelFamily); family != "" {
		out["model_family"] = family
	}
	if iconSlug := strings.TrimSpace(req.ModelIconSlug); iconSlug != "" {
		out["model_icon_slug"] = iconSlug
	}
	if req.ModelIcon != nil {
		iconType := strings.TrimSpace(req.ModelIcon["type"])
		slug := strings.TrimSpace(req.ModelIcon["slug"])
		src := strings.TrimSpace(req.ModelIcon["src"])
		if iconType == "image" && src != "" {
			icon := map[string]string{"type": iconType, "src": src}
			if slug != "" {
				icon["slug"] = slug
			}
			out["model_icon"] = icon
		} else if iconType != "" && slug != "" {
			out["model_icon"] = map[string]string{"type": iconType, "slug": slug}
		}
	}
	return out
}

func userMetadata(req chatRequest) map[string]any {
	out := make(map[string]any)
	if skillID := strings.TrimSpace(req.SkillID); skillID != "" {
		out["skill_id"] = skillID
	}
	return out
}

func effectiveModelRouteID(req chatRequest) string {
	if routeID := strings.TrimSpace(req.ModelRouteID); routeID != "" {
		return routeID
	}
	// Existing routes were migrated with id=alias, so old browser tabs and
	// scheduled tasks remain valid after a unified stack upgrade.
	return strings.TrimSpace(req.ModelAlias)
}

// registerArtifact records a file event's private object-store key, then
// returns the browser-safe event shape (download URL, no object_key).
func (a *API) registerArtifact(ctx context.Context, id auth.Identity, sessionID string, ev agent.Event) agent.Event {
	data := make(map[string]string, len(ev.Data)+1)
	for k, v := range ev.Data {
		if k != "object_key" {
			data[k] = v
		}
	}
	artifactID := ev.Data["id"]
	if artifactID == "" {
		artifactID = uuid.NewString()
		data["id"] = artifactID
	}
	data["download_url"] = artifactDownloadURL(sessionID, artifactID)

	if a.convo == nil || sessionID == "" || ev.Data["object_key"] == "" {
		return agent.Event{Kind: ev.Kind, Data: data}
	}
	size, _ := strconv.ParseInt(ev.Data["size"], 10, 64)
	mime := ev.Data["mime"]
	if mime == "" {
		mime = "application/octet-stream"
	}
	if err := a.convo.UpsertArtifact(ctx, convo.Artifact{
		ID:             artifactID,
		ConversationID: sessionID,
		UserID:         id.UserID,
		TenantID:       id.TenantID,
		Filename:       ev.Data["filename"],
		Mime:           mime,
		Size:           size,
		ObjectKey:      ev.Data["object_key"],
		CreatedAt:      time.Now().UTC(),
	}); err != nil {
		a.log.Warn("persist artifact failed: " + err.Error())
	}
	return agent.Event{Kind: ev.Kind, Data: data}
}

// titleFromPrompt derives the MVP conversation title: the first line of the
// prompt, trimmed and truncated to a display-friendly length (rune-safe).
func titleFromPrompt(prompt string) string {
	title := strings.TrimSpace(prompt)
	if i := strings.IndexAny(title, "\r\n"); i >= 0 {
		title = strings.TrimSpace(title[:i])
	}
	const maxRunes = 60
	r := []rune(title)
	if len(r) > maxRunes {
		title = strings.TrimSpace(string(r[:maxRunes])) + "\u2026"
	}
	return title
}

func titleForConversation(req chatRequest) string {
	if title := strings.TrimSpace(req.ConversationTitle); title != "" {
		return title
	}
	return titleFromPrompt(req.Prompt)
}

func chatTypeForConversation(req chatRequest) string {
	switch strings.TrimSpace(req.ConversationType) {
	case "scheduled_task":
		return "scheduled_task"
	default:
		return "chat"
	}
}

func artifactDownloadURL(sessionID, artifactID string) string {
	return "/api/conversations/" + url.PathEscape(sessionID) + "/artifacts/" + url.PathEscape(artifactID)
}

// listConversations serves the sidebar: the caller's conversations, newest
// first. When persistence is disabled it returns an empty list (never 500).
func (a *API) listConversations(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	if a.convo == nil {
		writeJSON(w, http.StatusOK, []convo.Conversation{})
		return
	}
	convs, err := a.convo.ListConversations(r.Context(), id.UserID)
	if err != nil {
		a.log.Warn("list conversations failed: " + err.Error())
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "could not list conversations")
		return
	}
	writeJSON(w, http.StatusOK, convs)
}

type renameConversationRequest struct {
	Title string `json:"title"`
}

func (a *API) renameConversation(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	convID := r.PathValue("id")
	if convID == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "session id is required")
		return
	}
	var req renameConversationRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON body")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "title is required")
		return
	}
	if a.convo == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "conversation not found")
		return
	}
	conv, err := a.convo.RenameConversation(r.Context(), convID, id.UserID, title)
	if err != nil {
		if errors.Is(err, convo.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "conversation not found")
			return
		}
		a.log.Warn("rename conversation failed: " + err.Error())
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "could not rename conversation")
		return
	}
	writeJSON(w, http.StatusOK, conv)
}

func (a *API) deleteConversation(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	convID := r.PathValue("id")
	if convID == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "session id is required")
		return
	}
	if a.convo == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "conversation not found")
		return
	}
	if _, err := a.convo.GetConversation(r.Context(), convID, id.UserID); err != nil {
		if errors.Is(err, convo.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "conversation not found")
			return
		}
		a.log.Warn("get conversation before delete failed: " + err.Error())
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "could not delete conversation")
		return
	}
	// Serialize with Run creation. A 202 from the cancel endpoint only means the
	// cancellation was requested; deletion is safe only after Finalize committed
	// the terminal Run and assistant message transaction.
	unlockRunMutation := func() {}
	if a.runs != nil {
		a.runs.mutationMu.Lock()
		unlockRunMutation = a.runs.mutationMu.Unlock
		active, err := a.runs.store.Active(r.Context(), convID, id.UserID)
		if err == nil {
			unlockRunMutation()
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": map[string]string{
					"code": "RUN_IN_PROGRESS", "message": "stop the running answer and wait for it to finish before deleting",
				},
				"run_id": active.ID,
			})
			return
		}
		if !errors.Is(err, chatrun.ErrNotFound) {
			unlockRunMutation()
			a.runs.databaseUnavailable.Store(true)
			a.log.Warn("active run check before delete failed: " + err.Error())
			writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "could not verify conversation run state")
			return
		}
		a.runs.databaseUnavailable.Store(false)
	}
	if err := a.convo.DeleteConversation(r.Context(), convID, id.UserID); err != nil {
		unlockRunMutation()
		if errors.Is(err, convo.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "conversation not found")
			return
		}
		a.log.Warn("delete conversation failed: " + err.Error())
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "could not delete conversation")
		return
	}
	unlockRunMutation()
	// Storage cleanup is request-driven but must not hold the process-wide Run
	// mutation lock. Once the conversation is gone, a failure is visible as an
	// orphan in Admin and can be retried manually without blocking user deletion.
	if a.releaser != nil {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
		if err := a.releaser.ReleaseSession(releaseCtx, id.UserID, convID); err != nil {
			a.log.Warn("release deleted conversation session failed: " + err.Error())
		}
		cancel()
	}
	w.WriteHeader(http.StatusNoContent)
}

// conversationMessages serves one conversation's history, but only if the
// verified caller owns it (ownership miss => 404, no cross-user existence
// oracle). Empty list when persistence is disabled.
func (a *API) conversationMessages(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	convID := r.PathValue("id")
	if convID == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "session id is required")
		return
	}
	if a.convo == nil {
		writeJSON(w, http.StatusOK, []convo.Message{})
		return
	}
	msgs, err := a.convo.GetMessages(r.Context(), convID, id.UserID)
	if err != nil {
		if errors.Is(err, convo.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "conversation not found")
			return
		}
		a.log.Warn("get conversation messages failed: " + err.Error())
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "could not load conversation")
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (a *API) downloadArtifact(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	convID := r.PathValue("id")
	artifactID := r.PathValue("artifact_id")
	if convID == "" || artifactID == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "session id and artifact id are required")
		return
	}
	if a.convo == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "artifact not found")
		return
	}
	artifact, err := a.convo.GetArtifact(r.Context(), convID, artifactID, id.UserID)
	if err != nil {
		if errors.Is(err, convo.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "artifact not found")
			return
		}
		a.log.Warn("get artifact failed: " + err.Error())
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "could not load artifact")
		return
	}
	if a.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "UNAVAILABLE", "artifact object store is not configured")
		return
	}
	data, err := a.store.Get(r.Context(), artifact.ObjectKey)
	if err != nil {
		a.log.Warn("artifact object read failed: " + err.Error())
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "artifact bytes not found")
		return
	}
	mime := artifact.Mime
	if mime == "" {
		mime = "application/octet-stream"
	}
	filename := sanitizeKeySegment(artifact.Filename)
	w.Header().Set("content-type", mime)
	w.Header().Set("content-length", strconv.Itoa(len(data)))
	// Artifact bytes are always a download response. The Web UI fetches them and
	// creates an isolated blob/srcdoc preview; serving user-controlled HTML inline
	// on Cocola's authenticated origin would otherwise create a stored-XSS path.
	w.Header().Set("content-disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("x-content-type-options", "nosniff")
	w.Header().Set("cache-control", "private, no-store")
	w.Header().Set("cross-origin-resource-policy", "same-origin")
	w.Header().Set(
		"content-security-policy",
		"sandbox; default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
	)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// writeSSE serializes one event as an SSE frame: "event: <kind>\ndata: <json>\n\n".
func writeSSE(w http.ResponseWriter, flusher http.Flusher, ev agent.Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Kind, payload); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// ---- JSON helpers (small, local copy of the admin-api envelope) ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type errBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	var b errBody
	b.Error.Code = code
	b.Error.Message = msg
	writeJSON(w, status, b)
}

// objectKey builds a collision-proof, traversal-safe key for an attachment:
// attachments/<session>/<uuid>-<name>. The uuid guarantees uniqueness even for
// identical filenames across turns; both the session and name segments are
// sanitized to basenames so a crafted value cannot escape the prefix.
func objectKey(sessionID, filename string) string {
	return fmt.Sprintf("attachments/%s/%s-%s",
		sanitizeKeySegment(sessionID), uuid.NewString(), sanitizeKeySegment(filename))
}

// sanitizeKeySegment reduces a value to a safe single path segment, mirroring
// the agent-runtime _sanitize_filename defense: take the basename, drop NULs,
// leading dots and residual separators, and fall back to a fixed token.
func sanitizeKeySegment(name string) string {
	base := strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSpace(base)
	base = strings.ReplaceAll(base, "\x00", "")
	base = strings.TrimLeft(base, ".")
	base = strings.ReplaceAll(base, "/", "_")
	if base == "" {
		return "file"
	}
	return base
}
