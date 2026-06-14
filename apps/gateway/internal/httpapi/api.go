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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/packages/go-common/logger"
	"github.com/cocola-project/cocola/packages/go-common/metrics"
	"github.com/cocola-project/cocola/packages/go-common/tracing"
)

// API wires the BFF dependencies. The agent.Streamer is an interface so tests
// can inject a fake without a real agent-runtime.
type API struct {
	streamer agent.Streamer
	verifier *auth.Verifier
	log      logger.Logger
	metrics  *metrics.Registry // optional; nil => no instrumentation (tests)
}

// New builds the BFF API.
func New(streamer agent.Streamer, verifier *auth.Verifier, log logger.Logger) *API {
	return &API{streamer: streamer, verifier: verifier, log: log}
}

// WithMetrics enables RED instrumentation on the public routes. The registry is
// shared with the observability port mounted in main; passing nil (the default)
// leaves the API uninstrumented, which keeps unit tests dependency-light.
func (a *API) WithMetrics(reg *metrics.Registry) *API { a.metrics = reg; return a }

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
	// Tracing: wrap the whole mux so an inbound W3C traceparent is extracted and
	// a server span is started before auth/handlers run; the span context then
	// flows into the agent gRPC call (client stats handler) for an end-to-end
	// trace. No-op overhead when tracing is disabled.
	return tracing.HTTPHandler("gateway.http", mux)
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// chatRequest is the client-supplied body. user_id/session scoping comes from
// the verified identity, NOT from the body, so a caller cannot impersonate
// another user. session_id and sandbox_id are caller-chosen routing hints.
type chatRequest struct {
	Prompt    string `json:"prompt"`
	SessionID string `json:"session_id"`
	SandboxID string `json:"sandbox_id"`
	MaxTurns  int32  `json:"max_turns"`
}

// chat is the SSE entrypoint: verify -> open agent stream -> flush events.
func (a *API) chat(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}

	var req chatRequest
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

	flusher, ok := w.(http.Flusher)
	if !ok {
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

	q := agent.Query{
		UserID:    id.UserID,
		SessionID: req.SessionID,
		Prompt:    req.Prompt,
		SandboxID: req.SandboxID,
		MaxTurns:  req.MaxTurns,
	}

	err := a.streamer.Stream(r.Context(), q, func(ev agent.Event) error {
		return writeSSE(w, flusher, ev)
	})
	if err != nil {
		// Best-effort terminal error event; the connection may already be gone.
		_ = writeSSE(w, flusher, agent.Event{Kind: "error", Data: map[string]string{"error": err.Error()}})
		a.log.Warn("agent stream ended with error: " + err.Error())
	}
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
