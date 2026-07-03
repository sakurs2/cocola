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
	"github.com/cocola-project/cocola/packages/go-common/logger"
	"github.com/cocola-project/cocola/packages/go-common/metrics"
	"github.com/cocola-project/cocola/packages/go-common/tracing"
)

// DefaultInlineMaxBytes is the attachment size threshold used when none is
// configured: files at or below it are pushed inline into the sandbox, larger
// ones are delivered key-only and pulled by agent-runtime (ADR-0017 P1a).
const DefaultInlineMaxBytes int64 = 16 * 1024 * 1024

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

// WithAgentReleaser injects the best-effort session releaser used by
// conversation deletion tests or alternate runtimes.
func (a *API) WithAgentReleaser(releaser agent.Releaser) *API { a.releaser = releaser; return a }

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

// chatRequest is the client-supplied body. user_id/session scoping comes from
// the verified identity, NOT from the body, so a caller cannot impersonate
// another user. session_id and sandbox_id are caller-chosen routing hints.
type chatRequest struct {
	Prompt      string            `json:"prompt"`
	SessionID   string            `json:"session_id"`
	SandboxID   string            `json:"sandbox_id"`
	MaxTurns    int32             `json:"max_turns"`
	ModelAlias  string            `json:"model_alias"`
	ModelLabel  string            `json:"model_label"`
	ModelIcon   map[string]string `json:"model_icon"`
	Attachments []attachmentDTO   `json:"attachments"`
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

	atts := make([]agent.Attachment, 0, len(req.Attachments))
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
				if att.Size > a.inlineMaxBytes {
					// Large file: hand agent-runtime the key only; it pulls the
					// bytes from the store before the run (backend-pull).
					att.Content = nil
				}
			}
		}
		atts = append(atts, att)
	}

	q := agent.Query{
		UserID:      id.UserID,
		SessionID:   req.SessionID,
		Prompt:      req.Prompt,
		SandboxID:   req.SandboxID,
		MaxTurns:    req.MaxTurns,
		ModelAlias:  strings.TrimSpace(req.ModelAlias),
		Attachments: atts,
	}

	// Persist the user turn (route A UI-message mirror). All persistence is a
	// best-effort SIDE CHANNEL: any store error is logged and swallowed so it can
	// never break the SSE stream the user is watching. Requires a session_id to
	// key the conversation; if empty (dev/no-frontend), we skip persistence.
	persist := a.convo != nil && req.SessionID != ""
	if persist {
		a.persistUserTurn(r.Context(), id, req)
	}

	// reducer mirrors the frontend's reducePart so the stored assistant message
	// has the exact parts the browser renders. Only populated when persisting.
	var reducer *convo.Reducer
	if persist {
		reducer = convo.NewReducer()
	}

	err := a.streamer.Stream(r.Context(), q, func(ev agent.Event) error {
		if ev.Kind == "file" {
			ev = a.registerArtifact(context.Background(), id, req.SessionID, ev)
		}
		if reducer != nil {
			reducer.Apply(ev.Kind, ev.Data)
		}
		return writeSSE(w, flusher, ev)
	})
	if err != nil {
		// Best-effort terminal error event; the connection may already be gone.
		_ = writeSSE(w, flusher, agent.Event{Kind: "error", Data: map[string]string{"error": err.Error()}})
		a.log.Warn("agent stream ended with error: " + err.Error())
	}

	// Persist the assistant turn with whatever was aggregated (even a partial
	// stream on error/abort is worth keeping so the history renders). Use a
	// background context so a client disconnect (r.Context() cancelled) does not
	// abort the write.
	if persist {
		a.persistAssistantTurn(context.Background(), req.SessionID, reducer.Parts(), assistantMetadata(req))
	}
}

// persistUserTurn upserts the conversation (title = truncated first prompt, set
// once) and appends the user message. Best-effort: errors are logged only.
func (a *API) persistUserTurn(ctx context.Context, id auth.Identity, req chatRequest) {
	now := time.Now().UTC()
	if err := a.convo.UpsertConversation(ctx, convo.Conversation{
		ID:        req.SessionID,
		UserID:    id.UserID,
		TenantID:  id.TenantID,
		Title:     titleFromPrompt(req.Prompt),
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
	if req.ModelIcon != nil {
		iconType := strings.TrimSpace(req.ModelIcon["type"])
		slug := strings.TrimSpace(req.ModelIcon["slug"])
		if iconType != "" && slug != "" {
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
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "conversation id is required")
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
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "conversation id is required")
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
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "conversation id is required")
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
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "conversation id and artifact id are required")
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
