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
	"encoding/base64"
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
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/apps/gateway/internal/objstore"
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
type conversationStageSpansKey struct{}

type conversationStageSpans struct {
	request      string
	environment  string
	agent        string
	agentInit    string
	finalization string
}

var traceAttributeAllowlist = map[string]bool{
	"accepted_count": true, "action": true, "artifact_count": true,
	"attachment_count": true, "chat_type": true, "content_length": true,
	"conversation_id": true, "error_code": true, "error_type": true,
	"event_count": true, "event_kind": true, "inline_count": true,
	"mcp_count": true, "mcp_names": true, "model_alias": true,
	"object_count": true, "part_count": true, "prompt_count": true,
	"prompt_ids": true, "prompt_versions": true, "restored": true,
	"resumed": true, "reused": true, "sandbox_id": true, "session_id": true,
	"skill_count": true, "target": true, "text_chunk_count": true,
	"thinking_chunk_count": true, "tool_name": true, "tool_result_count": true,
	"tool_type": true, "tool_use_count": true,
	"session_auth_ms": true, "runtime_token_ms": true, "decode_ms": true,
	"persist_user_ms": true, "first_event_ms": true, "first_reasoning_ms": true,
	"first_token_ms": true, "first_tool_ms": true,
}

func traceParentForCategory(ctx context.Context, category string) string {
	rootSpanID, _ := ctx.Value(conversationRootSpanKey{}).(string)
	stages, _ := ctx.Value(conversationStageSpansKey{}).(conversationStageSpans)
	switch strings.TrimSpace(category) {
	case "request", "gateway":
		return stages.request
	case "sandbox", "environment":
		return stages.environment
	case "agent_init":
		return stages.agentInit
	case "agent", "model", "tool":
		return stages.agent
	case "persistence", "artifact", "finalization":
		return stages.finalization
	default:
		return rootSpanID
	}
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
	streamer agent.Streamer
	releaser agent.Releaser
	verifier *auth.Verifier
	log      logger.Logger
	metrics  *metrics.Registry // optional; nil => no instrumentation (tests)
	// store is the attachment source-of-truth object store. nil => P0 path:
	// attachments are pushed inline only, no upload (feature stays dark until
	// MinIO is configured). inlineMaxBytes is the small/large split threshold.
	store          objstore.Store
	inlineMaxBytes int64
	// convo is the conversation-persistence store (route A UI-message mirror).
	// nil => persistence is dark: chat still streams, but nothing is stored and
	// the list/messages endpoints return empty. Enabled by COCOLA_PG_DSN in main.
	convo convo.Store
	trace traceevents.Store
	// sandboxTokenIssuer mints a fresh per-user cocola token per chat turn from
	// the verified identity; agent-runtime injects it into the sandbox as
	// ANTHROPIC_AUTH_TOKEN so downstream quota/usage/revocation bind to the real
	// user. nil => the per-user token feature is dark and agent-runtime falls
	// back to its baked static token. sandboxTokenTTL is the mint TTL.
	sandboxTokenIssuer *token.Issuer
	sandboxTokenTTL    time.Duration
}

// New builds the BFF API.
func New(streamer agent.Streamer, verifier *auth.Verifier, log logger.Logger) *API {
	a := &API{streamer: streamer, verifier: verifier, log: log}
	if releaser, ok := streamer.(agent.Releaser); ok {
		a.releaser = releaser
	}
	return a
}

// WithMetrics enables RED instrumentation on the public routes. The registry is
// shared with the observability port mounted in main; passing nil (the default)
// leaves the API uninstrumented, which keeps unit tests dependency-light.
func (a *API) WithMetrics(reg *metrics.Registry) *API { a.metrics = reg; return a }

// WithObjStore enables the attachment source-of-truth path: every uploaded file
// is PutObject'd to the store, then split by inlineMaxBytes — files at or below
// it keep their inline bytes AND carry the object key; larger files are
// delivered key-only and pulled by agent-runtime on the model's behalf
// (ADR-0017 P1a). A non-positive threshold falls back to DefaultInlineMaxBytes.
// Passing a nil store leaves the API on the P0 inline-only path.
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
// serve a user's conversation list + history. Passing nil (the default) leaves
// persistence dark. See docs/plan/conversation-persistence-history-rendering.md.
func (a *API) WithConvoStore(store convo.Store) *API { a.convo = store; return a }

// WithTraceStore enables conversation audit summaries and detailed traces.
func (a *API) WithTraceStore(store traceevents.Store) *API { a.trace = store; return a }

// WithAgentReleaser injects the best-effort session releaser used by
// conversation deletion tests or alternate runtimes.
func (a *API) WithAgentReleaser(releaser agent.Releaser) *API { a.releaser = releaser; return a }

// WithSandboxTokenIssuer enables per-user sandbox tokens: the chat handler mints
// a fresh cocola token per turn (sub=identity.UserID, ten=identity.TenantID) and
// forwards it to agent-runtime, which injects it as the sandbox
// ANTHROPIC_AUTH_TOKEN. Passing nil (the default) leaves the feature dark and
// agent-runtime keeps its baked static token.
func (a *API) WithSandboxTokenIssuer(issuer *token.Issuer, ttl time.Duration) *API {
	a.sandboxTokenIssuer = issuer
	a.sandboxTokenTTL = ttl
	return a
}

// mintSandboxToken issues a per-user, short-lived cocola token for one turn. A
// mint failure is non-fatal: it is logged and the caller proceeds without a
// per-user token (agent-runtime falls back to its baked static token) so a
// signing hiccup never breaks chat.
func (a *API) mintSandboxToken(id auth.Identity) string {
	if a.sandboxTokenIssuer == nil {
		return ""
	}
	tok, _, err := a.sandboxTokenIssuer.Issue(id.UserID, id.TenantID, a.sandboxTokenTTL, 0)
	if err != nil {
		a.log.Warn("sandbox token mint failed; using runtime default token: " + err.Error())
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
	// Conversation history (route A). Both are auth-guarded so a caller only ever
	// sees their own conversations (ownership from the verified identity).
	mux.Handle("GET /v1/conversations", a.instrument("GET /v1/conversations",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.listConversations))))
	mux.Handle("PATCH /v1/conversations/{id}", a.instrument("PATCH /v1/conversations/{id}",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.renameConversation))))
	mux.Handle("DELETE /v1/conversations/{id}", a.instrument("DELETE /v1/conversations/{id}",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.deleteConversation))))
	mux.Handle("GET /v1/conversations/{id}/messages", a.instrument("GET /v1/conversations/{id}/messages",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.conversationMessages))))
	mux.Handle("GET /v1/conversations/{id}/artifacts/{artifact_id}", a.instrument("GET /v1/conversations/{id}/artifacts/{artifact_id}",
		a.verifier.Middleware(writeErr)(http.HandlerFunc(a.downloadArtifact))))
	// Tracing: wrap the whole mux so an inbound W3C traceparent is extracted and
	// a server span is started before auth/handlers run; the span context then
	// flows into the agent gRPC call (client stats handler) for an end-to-end
	// trace. No-op overhead when tracing is disabled.
	return tracing.HTTPHandler("gateway.http", mux)
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) recordTrace(ctx context.Context, traceID, name, category string, startedAt time.Time, status string, metadata map[string]any) {
	parentSpanID := traceParentForCategory(ctx, category)
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
		parentSpanID = traceParentForCategory(ctx, category)
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
		UserEmail:         id.UserID,
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
	if err := a.trace.UpsertConversationRun(context.Background(), run); err != nil {
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
	if err := a.trace.UpsertConversationRun(context.Background(), run); err != nil {
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

func boundedTimingHeader(r *http.Request, name string) time.Duration {
	raw, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get(name)), 10, 64)
	if err != nil || raw < 0 || raw > 60_000 {
		return 0
	}
	return time.Duration(raw) * time.Millisecond
}

// chat is the SSE entrypoint: verify -> open agent stream -> flush events.
func (a *API) chat(w http.ResponseWriter, r *http.Request) {
	start := chatStartedAt(r)
	id, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	traceID := tracing.TraceID(r.Context())

	var req chatRequest
	decodeStart := time.Now()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON body")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "prompt is required")
		return
	}
	decodeDuration := time.Since(decodeStart)
	rootSpanID := traceevents.NewSpanID()
	stages := conversationStageSpans{
		request:      traceevents.NewSpanID(),
		environment:  traceevents.NewSpanID(),
		agent:        traceevents.NewSpanID(),
		agentInit:    traceevents.NewSpanID(),
		finalization: traceevents.NewSpanID(),
	}
	r = r.WithContext(context.WithValue(r.Context(), conversationRootSpanKey{}, rootSpanID))
	r = r.WithContext(context.WithValue(r.Context(), conversationStageSpansKey{}, stages))
	run := a.startConversationRun(r.Context(), id, req, traceID, rootSpanID, start)
	authDuration := boundedTimingHeader(r, "x-cocola-session-auth-ms")
	tokenDuration := boundedTimingHeader(r, "x-cocola-runtime-token-ms")

	flusher, ok := w.(http.Flusher)
	if !ok {
		now := time.Now()
		a.upsertTraceSpan(r.Context(), traceevents.Span{
			TraceID: traceID, SpanID: stages.request, ParentSpanID: rootSpanID, SchemaVersion: 1,
			Service: "gateway", Name: "request.prepare", Category: "request",
			StartedAt: start, DurationUS: now.Sub(start).Microseconds(), Status: "error",
			Attributes: safeTraceAttributes(map[string]any{"error_code": "INTERNAL"}),
		})
		a.finishConversationRun(r.Context(), run, "error", "INTERNAL", 0, 0)
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "streaming unsupported")
		return
	}

	// SSE headers. Once written, the response is committed; errors after this
	// point are surfaced as an in-band "error" event, not an HTTP status.
	h := w.Header()
	h.Set("content-type", "text/event-stream")
	h.Set("cache-control", "no-cache")
	h.Set("connection", "keep-alive")
	h.Set("x-accel-buffering", "no") // disable proxy buffering (nginx)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	atts := make([]agent.Attachment, 0, len(req.Attachments))
	inlineCount := 0
	objectCount := 0
	for i := range req.Attachments {
		content, derr := base64.StdEncoding.DecodeString(req.Attachments[i].ContentB64)
		if derr != nil {
			a.log.Warn("dropping attachment with invalid base64 content")
			continue
		}
		att := agent.Attachment{
			Filename: req.Attachments[i].Filename,
			Content:  content,
			Mime:     req.Attachments[i].Mime,
			Size:     int64(len(content)),
		}
		// Source-of-truth upload + threshold split (ADR-0017 P1a). When the
		// store is unconfigured (nil) we stay on the P0 inline-only path. A
		// PutObject failure degrades gracefully to inline delivery for that
		// file rather than dropping it, since the bytes are still in hand.
		if a.store != nil {
			key := objectKey(req.SessionID, att.Filename)
			if err := a.store.Put(r.Context(), key, content, att.Mime); err != nil {
				a.log.Warn("attachment object-store upload failed, delivering inline: " + err.Error())
			} else {
				att.OssKey = key
				objectCount++
				if att.Size > a.inlineMaxBytes {
					// Large file: hand agent-runtime the key only; it pulls the
					// bytes from the store before the run (backend-pull).
					att.Content = nil
				} else {
					inlineCount++
				}
			}
		} else {
			inlineCount++
		}
		atts = append(atts, att)
	}
	q := agent.Query{
		UserID:           id.UserID,
		SessionID:        req.SessionID,
		Prompt:           req.Prompt,
		SandboxID:        req.SandboxID,
		MaxTurns:         req.MaxTurns,
		ModelAlias:       strings.TrimSpace(req.ModelAlias),
		TraceID:          traceID,
		ParentSpanID:     stages.agent,
		SandboxAuthToken: a.mintSandboxToken(id),
		Attachments:      atts,
	}

	// Persist the user turn (route A UI-message mirror). All persistence is a
	// best-effort SIDE CHANNEL: any store error is logged and swallowed so it can
	// never break the SSE stream the user is watching. Requires a session_id to
	// key the conversation; if empty (dev/no-frontend), we skip persistence.
	persist := a.convo != nil && req.SessionID != ""
	var persistUserDuration time.Duration
	if persist {
		persistUserStart := time.Now()
		a.persistUserTurn(r.Context(), id, req)
		persistUserDuration = time.Since(persistUserStart)
	}
	requestEnd := time.Now()
	a.upsertTraceSpan(r.Context(), traceevents.Span{
		TraceID: traceID, SpanID: stages.request, ParentSpanID: rootSpanID, SchemaVersion: 1,
		Service: "gateway", Name: "request.prepare", Category: "request",
		StartedAt: start, DurationUS: requestEnd.Sub(start).Microseconds(), Status: "success",
		Attributes: safeTraceAttributes(map[string]any{
			"conversation_id": strings.TrimSpace(req.SessionID),
			"chat_type":       chatTypeForConversation(req), "model_alias": strings.TrimSpace(req.ModelAlias),
			"attachment_count": len(req.Attachments), "accepted_count": len(atts),
			"inline_count": inlineCount, "object_count": objectCount,
			"session_auth_ms": authDuration.Milliseconds(), "runtime_token_ms": tokenDuration.Milliseconds(),
			"decode_ms": decodeDuration.Milliseconds(), "persist_user_ms": persistUserDuration.Milliseconds(),
		}),
	})

	// reducer mirrors the frontend's reducePart so the stored assistant message
	// has the exact parts the browser renders. Only populated when persisting.
	var reducer *convo.Reducer
	if persist {
		reducer = convo.NewReducer()
	}

	artifactCount := 0
	streamStart := time.Now()
	streamEventCount := 0
	textChunkCount := 0
	thinkingChunkCount := 0
	toolUseCount := 0
	toolResultCount := 0
	firstEventRecorded := false
	firstTextRecorded := false
	firstReasoningRecorded := false
	firstToolUseRecorded := false
	var ttftMS int64
	var firstEventMS int64
	var firstReasoningMS int64
	var firstToolMS int64
	environmentEnd := streamStart
	var environmentReadyAt time.Time
	var firstEnvironmentOperationStart time.Time
	var lastEnvironmentOperationEnd time.Time
	var firstAgentInitOperationStart time.Time
	var lastAgentInitOperationEnd time.Time
	var agentInitializationStatusAt time.Time
	var firstAgentEventAt time.Time
	type openToolSpan struct {
		spanID string
		name   string
		start  time.Time
	}
	openTools := make(map[string]openToolSpan)
	// Persist phase parents before cross-service children can arrive. These are
	// updated with their final timing/status after the stream completes.
	a.upsertTraceSpan(r.Context(), traceevents.Span{
		TraceID: traceID, SpanID: stages.environment, ParentSpanID: rootSpanID, SchemaVersion: 1,
		Service: "agent-runtime", Name: "environment.prepare", Category: "environment",
		StartedAt: streamStart, Status: "running",
	})
	a.upsertTraceSpan(r.Context(), traceevents.Span{
		TraceID: traceID, SpanID: stages.agent, ParentSpanID: rootSpanID, SchemaVersion: 1,
		Service: "agent-runtime", Name: "agent.execute", Category: "agent",
		StartedAt: streamStart, Status: "running",
	})
	a.upsertTraceSpan(r.Context(), traceevents.Span{
		TraceID: traceID, SpanID: stages.agentInit, ParentSpanID: stages.agent, SchemaVersion: 1,
		Service: "agent-runtime", Name: "agent.initialize", Category: "agent",
		StartedAt: streamStart, Status: "running",
	})
	err := a.streamer.Stream(r.Context(), q, func(ev agent.Event) error {
		if ev.Kind == "trace" {
			category := strings.TrimSpace(ev.Data["category"])
			durationUS := traceEventDurationUS(ev.Data)
			observedAt := time.Now()
			operationStart := traceEventStartedAt(
				ev.Data,
				observedAt.Add(-time.Duration(durationUS)*time.Microsecond),
			)
			operationEnd := operationStart.Add(time.Duration(durationUS) * time.Microsecond)
			if category == "sandbox" || category == "environment" {
				environmentEnd = observedAt
				if firstEnvironmentOperationStart.IsZero() || operationStart.Before(firstEnvironmentOperationStart) {
					firstEnvironmentOperationStart = operationStart
				}
				if operationEnd.After(lastEnvironmentOperationEnd) {
					lastEnvironmentOperationEnd = operationEnd
				}
			}
			if category == "agent_init" {
				if firstAgentInitOperationStart.IsZero() || operationStart.Before(firstAgentInitOperationStart) {
					firstAgentInitOperationStart = operationStart
				}
				if operationEnd.After(lastAgentInitOperationEnd) {
					lastAgentInitOperationEnd = operationEnd
				}
			}
			a.recordAgentTrace(r.Context(), traceID, ev.Data)
			return nil
		}
		streamEventCount++
		isEnvironmentEvent := ev.Kind == "sandbox" || ev.Kind == "environment_prepare" || ev.Kind == "environment_status"
		if isEnvironmentEvent {
			observedAt := time.Now()
			switch ev.Kind {
			case "sandbox":
				environmentEnd = observedAt
			case "environment_prepare":
				if environmentPreparationComplete(ev.Data) {
					environmentReadyAt = observedAt
					environmentEnd = observedAt
				}
			case "environment_status":
				agentInitializationStatusAt = observedAt
			}
		} else if firstAgentEventAt.IsZero() {
			firstAgentEventAt = time.Now()
		}
		if !isEnvironmentEvent && !firstEventRecorded {
			firstEventRecorded = true
			firstEventMS = time.Since(streamStart).Milliseconds()
		}
		switch ev.Kind {
		case "text":
			textChunkCount++
			if !firstTextRecorded {
				firstTextRecorded = true
				ttftMS = time.Since(streamStart).Milliseconds()
			}
		case "thinking":
			thinkingChunkCount++
			if !firstReasoningRecorded {
				firstReasoningRecorded = true
				firstReasoningMS = time.Since(streamStart).Milliseconds()
			}
		case "tool_use":
			toolUseCount++
			toolID := strings.TrimSpace(ev.Data["id"])
			if toolID != "" {
				openTools[toolID] = openToolSpan{
					spanID: traceevents.NewSpanID(),
					name:   strings.TrimSpace(ev.Data["name"]),
					start:  time.Now(),
				}
			}
			if !firstToolUseRecorded {
				firstToolUseRecorded = true
				firstToolMS = time.Since(streamStart).Milliseconds()
			}
		case "tool_result":
			toolResultCount++
			toolID := strings.TrimSpace(ev.Data["tool_use_id"])
			if tool, ok := openTools[toolID]; ok {
				status := "success"
				if strings.EqualFold(ev.Data["is_error"], "true") {
					status = "error"
				}
				a.upsertTraceSpan(r.Context(), traceevents.Span{
					TraceID: traceID, SpanID: tool.spanID, ParentSpanID: stages.agent, SchemaVersion: 1,
					Service: "sandbox-shim", Name: "tool.execute", Category: "tool",
					StartedAt: tool.start, DurationUS: time.Since(tool.start).Microseconds(), Status: status,
					Attributes: safeTraceAttributes(map[string]any{
						"tool_name": tool.name,
						"tool_type": toolType(tool.name),
					}),
				})
				delete(openTools, toolID)
			}
		}
		if ev.Kind == "file" {
			artifactCount++
			artifactStart := time.Now()
			ev = a.registerArtifact(context.Background(), id, req.SessionID, ev)
			a.recordTrace(r.Context(), traceID, "artifact.register", "agent", artifactStart, "ok", map[string]any{
				"artifact_count": artifactCount,
				"filename":       ev.Data["filename"],
				"mime":           ev.Data["mime"],
				"size":           ev.Data["size"],
			})
		}
		if reducer != nil {
			reducer.Apply(ev.Kind, ev.Data)
		}
		return writeSSE(w, flusher, ev)
	})
	streamEnd := time.Now()
	for _, tool := range openTools {
		a.upsertTraceSpan(r.Context(), traceevents.Span{
			TraceID: traceID, SpanID: tool.spanID, ParentSpanID: stages.agent, SchemaVersion: 1,
			Service: "sandbox-shim", Name: "tool.execute", Category: "tool",
			StartedAt: tool.start, DurationUS: time.Since(tool.start).Microseconds(), Status: "interrupted",
			Attributes: safeTraceAttributes(map[string]any{
				"tool_name": tool.name,
				"tool_type": toolType(tool.name),
			}),
		})
	}
	if !environmentReadyAt.IsZero() {
		environmentEnd = environmentReadyAt
	} else if !firstAgentInitOperationStart.IsZero() {
		environmentEnd = firstAgentInitOperationStart
	} else if !lastEnvironmentOperationEnd.IsZero() {
		environmentEnd = lastEnvironmentOperationEnd
	}
	if environmentEnd.Before(streamStart) || environmentEnd.After(streamEnd) {
		environmentEnd = streamStart
	}
	if !firstAgentEventAt.IsZero() && environmentEnd.After(firstAgentEventAt) {
		environmentEnd = firstAgentEventAt
	}
	if !firstEnvironmentOperationStart.IsZero() {
		dispatchEnd := firstEnvironmentOperationStart
		if dispatchEnd.After(environmentEnd) {
			dispatchEnd = environmentEnd
		}
		if dispatchEnd.Sub(streamStart) >= time.Millisecond {
			a.upsertTraceSpan(r.Context(), traceevents.Span{
				TraceID: traceID, SpanID: traceevents.NewSpanID(), ParentSpanID: stages.environment, SchemaVersion: 1,
				Service: "gateway", Name: "environment.runtime_dispatch", Category: "environment",
				StartedAt: streamStart, DurationUS: dispatchEnd.Sub(streamStart).Microseconds(), Status: "success",
			})
		}
	}
	agentStart := environmentEnd
	agentInitializationEnd := agentInitializationStatusAt
	if agentInitializationEnd.IsZero() || agentInitializationEnd.Before(agentStart) {
		agentInitializationEnd = lastAgentInitOperationEnd
	}
	if agentInitializationEnd.IsZero() || agentInitializationEnd.Before(agentStart) {
		agentInitializationEnd = firstAgentEventAt
	}
	if agentInitializationEnd.IsZero() || agentInitializationEnd.Before(agentStart) {
		agentInitializationEnd = agentStart
	}
	if !firstAgentEventAt.IsZero() && agentInitializationEnd.After(firstAgentEventAt) {
		agentInitializationEnd = firstAgentEventAt
	}
	if agentInitializationEnd.After(streamEnd) {
		agentInitializationEnd = streamEnd
	}
	if !lastAgentInitOperationEnd.IsZero() {
		initializeStart := lastAgentInitOperationEnd
		if initializeStart.Before(agentStart) {
			initializeStart = agentStart
		}
		if agentInitializationEnd.Sub(initializeStart) >= time.Millisecond {
			a.upsertTraceSpan(r.Context(), traceevents.Span{
				TraceID: traceID, SpanID: traceevents.NewSpanID(), ParentSpanID: stages.agentInit, SchemaVersion: 1,
				Service: "sandbox-shim", Name: "agent.sdk_initialize", Category: "agent_init",
				StartedAt: initializeStart, DurationUS: agentInitializationEnd.Sub(initializeStart).Microseconds(), Status: "success",
			})
		}
	}
	a.upsertTraceSpan(r.Context(), traceevents.Span{
		TraceID: traceID, SpanID: stages.environment, ParentSpanID: rootSpanID, SchemaVersion: 1,
		Service: "agent-runtime", Name: "environment.prepare", Category: "environment",
		StartedAt: streamStart, DurationUS: environmentEnd.Sub(streamStart).Microseconds(), Status: "success",
	})
	a.upsertTraceSpan(r.Context(), traceevents.Span{
		TraceID: traceID, SpanID: stages.agentInit, ParentSpanID: stages.agent, SchemaVersion: 1,
		Service: "agent-runtime", Name: "agent.initialize", Category: "agent",
		StartedAt: agentStart, DurationUS: agentInitializationEnd.Sub(agentStart).Microseconds(), Status: "success",
	})
	agentStatus := "success"
	agentErrorCode := ""
	if err != nil {
		agentStatus = "error"
		agentErrorCode = "AGENT_STREAM_ERROR"
		if errors.Is(err, context.Canceled) {
			agentStatus = "cancelled"
			agentErrorCode = "CLIENT_CANCELLED"
		}
		// Best-effort terminal error event; the connection may already be gone.
		errEv := agent.Event{Kind: "error", Data: map[string]string{"error": err.Error()}}
		if reducer != nil {
			reducer.Apply(errEv.Kind, errEv.Data)
		}
		_ = writeSSE(w, flusher, errEv)
		a.log.Warn("agent stream ended with error: " + err.Error())
	}
	a.upsertTraceSpan(r.Context(), traceevents.Span{
		TraceID: traceID, SpanID: stages.agent, ParentSpanID: rootSpanID, SchemaVersion: 1,
		Service: "agent-runtime", Name: "agent.execute", Category: "agent",
		StartedAt: agentStart, DurationUS: streamEnd.Sub(agentStart).Microseconds(), Status: agentStatus,
		Attributes: safeTraceAttributes(map[string]any{
			"event_count": streamEventCount, "text_chunk_count": textChunkCount,
			"thinking_chunk_count": thinkingChunkCount, "tool_use_count": toolUseCount,
			"tool_result_count": toolResultCount, "artifact_count": artifactCount,
			"first_event_ms": firstEventMS, "first_reasoning_ms": firstReasoningMS,
			"first_token_ms": ttftMS, "first_tool_ms": firstToolMS,
			"error_code": agentErrorCode,
		}),
	})

	// Persist the assistant turn with whatever was aggregated (even a partial
	// stream on error/abort is worth keeping so the history renders). Use a
	// background context so a client disconnect (r.Context() cancelled) does not
	// abort the write.
	finalizationStart := streamEnd
	a.upsertTraceSpan(r.Context(), traceevents.Span{
		TraceID: traceID, SpanID: stages.finalization, ParentSpanID: rootSpanID, SchemaVersion: 1,
		Service: "gateway", Name: "run.finalize", Category: "finalization",
		StartedAt: finalizationStart, Status: "running",
	})
	if persist {
		persistAssistantStart := time.Now()
		a.persistAssistantTurn(context.Background(), req.SessionID, reducer.Parts(), assistantMetadata(req))
		if req.DeferConversationVisibilityUntilDone {
			a.revealConversation(context.Background(), id, req)
		}
		a.recordTrace(
			r.Context(),
			traceID,
			"conversation.persist_assistant",
			"persistence",
			persistAssistantStart,
			"ok",
			map[string]any{
				"conversation_id": strings.TrimSpace(req.SessionID),
				"part_count":      len(reducer.Parts()),
			},
		)
	}
	finalizationEnd := time.Now()
	partCount := 0
	if reducer != nil {
		partCount = len(reducer.Parts())
	}
	a.upsertTraceSpan(r.Context(), traceevents.Span{
		TraceID: traceID, SpanID: stages.finalization, ParentSpanID: rootSpanID, SchemaVersion: 1,
		Service: "gateway", Name: "run.finalize", Category: "finalization",
		StartedAt: finalizationStart, DurationUS: finalizationEnd.Sub(finalizationStart).Microseconds(), Status: "success",
		Attributes: safeTraceAttributes(map[string]any{
			"artifact_count": artifactCount, "part_count": partCount,
		}),
	})
	if err != nil {
		a.finishConversationRun(r.Context(), run, agentStatus, agentErrorCode, ttftMS, int64(toolUseCount))
	} else {
		a.finishConversationRun(r.Context(), run, "success", "", ttftMS, int64(toolUseCount))
	}
}

func toolType(name string) string {
	if strings.HasPrefix(name, "mcp__") {
		return "mcp"
	}
	if name == "WebSearch" || name == "WebFetch" {
		return "server"
	}
	return "builtin"
}

// persistUserTurn upserts the conversation (title = truncated first prompt, set
// once) and appends the user message. Best-effort: errors are logged only.
func (a *API) persistUserTurn(ctx context.Context, id auth.Identity, req chatRequest) {
	now := time.Now().UTC()
	if err := a.convo.UpsertConversation(ctx, convo.Conversation{
		ID:        req.SessionID,
		UserID:    id.UserID,
		TenantID:  id.TenantID,
		Title:     titleForConversation(req),
		ChatType:  chatTypeForConversation(req),
		Hidden:    req.DeferConversationVisibilityUntilDone,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		a.log.Warn("persist conversation failed: " + err.Error())
		return
	}
	if err := a.convo.InsertMessage(ctx, convo.Message{
		ID:             uuid.NewString(),
		ConversationID: req.SessionID,
		Role:           "user",
		Parts:          []convo.Part{{Type: convo.PartText, Text: req.Prompt}},
		CreatedAt:      now,
	}); err != nil {
		a.log.Warn("persist user message failed: " + err.Error())
	}
}

func (a *API) revealConversation(ctx context.Context, id auth.Identity, req chatRequest) {
	if a.convo == nil || req.SessionID == "" {
		return
	}
	if err := a.convo.RevealConversation(ctx, req.SessionID, id.UserID, titleForConversation(req), time.Now().UTC()); err != nil {
		a.log.Warn("reveal conversation failed: " + err.Error())
	}
}

// persistAssistantTurn appends the aggregated assistant message and refreshes
// the conversation's updated_at so it floats to the top of the sidebar. Skips a
// truly empty turn (no parts) to avoid storing blank rows. Best-effort.
func (a *API) persistAssistantTurn(ctx context.Context, sessionID string, parts []convo.Part, metadata map[string]any) {
	if len(parts) == 0 {
		return
	}
	now := time.Now().UTC()
	if err := a.convo.InsertMessage(ctx, convo.Message{
		ID:             uuid.NewString(),
		ConversationID: sessionID,
		Role:           "assistant",
		Parts:          parts,
		Metadata:       metadata,
		CreatedAt:      now,
	}); err != nil {
		a.log.Warn("persist assistant message failed: " + err.Error())
		return
	}
	// Refresh updated_at (UpsertConversation on an existing id updates only that).
	if err := a.convo.UpsertConversation(ctx, convo.Conversation{ID: sessionID, UpdatedAt: now}); err != nil {
		a.log.Warn("refresh conversation updated_at failed: " + err.Error())
	}
}

func assistantMetadata(req chatRequest) map[string]any {
	out := make(map[string]any)
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
	if a.releaser != nil {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := a.releaser.ReleaseSession(releaseCtx, id.UserID, convID); err != nil {
			a.log.Warn("release conversation session failed: " + err.Error())
		}
	}
	if err := a.convo.DeleteConversation(r.Context(), convID, id.UserID); err != nil {
		if errors.Is(err, convo.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "conversation not found")
			return
		}
		a.log.Warn("delete conversation failed: " + err.Error())
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "could not delete conversation")
		return
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
	w.Header().Set("content-disposition", fmt.Sprintf("inline; filename=%q", filename))
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
