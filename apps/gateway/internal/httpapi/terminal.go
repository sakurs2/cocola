package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/apps/gateway/internal/sandboxmgr"
)

const (
	terminalExecdPort             = 44772
	terminalDisconnectGrace       = 30 * time.Second
	terminalCleanupRequestTimeout = 5 * time.Second
	projectWorkspaceMarker        = "/workspace/project/.git/cocola-project.json"
)

type createTerminalRequest struct {
	Cwd     string `json:"cwd"`
	Command string `json:"command,omitempty"`
}

// createTerminal creates one root PTY in the conversation's currently bound
// sandbox. The work directory is derived from the persisted conversation, not
// accepted from the browser, so Project terminals consistently start inside
// the project worktree.
func (a *API) createTerminal(w http.ResponseWriter, r *http.Request) {
	conv, ep, ok := a.resolveTerminalTarget(w, r)
	if !ok {
		return
	}

	request := createTerminalRequest{
		Cwd:     "/workspace",
		Command: "export TERM=xterm-256color COLORTERM=truecolor; exec /bin/bash --noprofile --norc -i",
	}
	if conv.ProjectID != "" {
		ready, err := projectWorkspaceReady(r.Context(), ep)
		if err != nil {
			a.log.Warn("terminal project readiness check failed: " + err.Error())
			writeErr(w, http.StatusBadGateway, "UNAVAILABLE", "project workspace readiness could not be checked")
			return
		}
		if !ready {
			writeErr(w, http.StatusTooEarly, "PROJECT_WORKSPACE_PREPARING", "project workspace is still preparing")
			return
		}
		request.Cwd = "/workspace/project"
	}
	body, err := json.Marshal(request)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "could not create terminal request")
		return
	}
	a.proxyTerminal(w, r, ep, "/pty", body, func(response *http.Response) {
		a.observeCreatedTerminal(response, conv, ep)
	})
}

func (a *API) terminalSessionProxy(w http.ResponseWriter, r *http.Request) {
	terminalID := r.PathValue("terminal_id")
	if !validTerminalID(terminalID) {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid terminal id")
		return
	}
	conv, ep, ok := a.resolveTerminalTarget(w, r)
	if !ok {
		return
	}
	var observe func(*http.Response)
	if r.Method == http.MethodDelete {
		observe = func(response *http.Response) {
			if response.StatusCode >= 200 && response.StatusCode < 300 && a.terminalLeases != nil {
				a.terminalLeases.forget(terminalLeaseKey{
					userID: conv.UserID, conversationID: conv.ID, terminalID: terminalID,
				})
			}
		}
	}
	a.proxyTerminal(w, r, ep, "/pty/"+url.PathEscape(terminalID), nil, observe)
}

func (a *API) terminalWebSocketProxy(w http.ResponseWriter, r *http.Request) {
	terminalID := r.PathValue("terminal_id")
	if !validTerminalID(terminalID) {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid terminal id")
		return
	}
	conv, ep, ok := a.resolveTerminalTarget(w, r)
	if !ok {
		return
	}
	release := func() {}
	if a.terminalLeases != nil {
		release = a.terminalLeases.attach(terminalLeaseKey{
			userID: conv.UserID, conversationID: conv.ID, terminalID: terminalID,
		}, ep)
	}
	defer release()
	a.proxyTerminal(w, r, ep, "/pty/"+url.PathEscape(terminalID)+"/ws", nil, nil)
}

func (a *API) resolveTerminalTarget(
	w http.ResponseWriter,
	r *http.Request,
) (convo.Conversation, *sandboxmgr.ResolvedEndpoint, bool) {
	id, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return convo.Conversation{}, nil, false
	}
	if a.sandboxResolver == nil {
		writeErr(w, http.StatusNotImplemented, "UNIMPLEMENTED", "terminal is not configured")
		return convo.Conversation{}, nil, false
	}
	if a.convo == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "conversation not found")
		return convo.Conversation{}, nil, false
	}

	conversationID := r.PathValue("id")
	if conversationID == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "conversation id is required")
		return convo.Conversation{}, nil, false
	}
	conversation, err := a.convo.GetConversation(r.Context(), conversationID, id.UserID)
	if err != nil {
		if errors.Is(err, convo.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "conversation not found")
			return convo.Conversation{}, nil, false
		}
		a.log.Warn("terminal conversation lookup failed: " + err.Error())
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "could not open terminal")
		return convo.Conversation{}, nil, false
	}

	ep, err := a.sandboxResolver.ResolveEndpoint(r.Context(), id.UserID, conversationID, terminalExecdPort)
	if err != nil {
		a.log.Warn("terminal endpoint resolve failed: " + err.Error())
		writeErr(w, http.StatusBadGateway, "UNAVAILABLE", "sandbox is not available")
		return convo.Conversation{}, nil, false
	}
	return conversation, ep, true
}

func (a *API) proxyTerminal(
	w http.ResponseWriter,
	r *http.Request,
	ep *sandboxmgr.ResolvedEndpoint,
	upstreamPath string,
	body []byte,
	observe func(*http.Response),
) {
	base, err := url.Parse(ep.URL)
	if err != nil || base.Host == "" {
		a.log.Warn("terminal resolve returned malformed url")
		writeErr(w, http.StatusBadGateway, "UNAVAILABLE", "terminal target is unreachable")
		return
	}

	basePath := strings.TrimRight(base.Path, "/")
	proxy := &httputil.ReverseProxy{
		FlushInterval: 50 * time.Millisecond,
		Director: func(req *http.Request) {
			req.URL.Scheme = base.Scheme
			req.URL.Host = base.Host
			req.URL.Path = basePath + upstreamPath
			req.URL.RawPath = ""
			req.Host = base.Host
			req.Header.Del("Authorization")
			req.Header.Del("Cookie")
			for key, value := range ep.Headers {
				req.Header.Set(key, value)
			}
			if body != nil {
				req.Body = http.NoBody
				if len(body) > 0 {
					req.Body = io.NopCloser(bytes.NewReader(body))
				}
				req.ContentLength = int64(len(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Content-Length", strconv.Itoa(len(body)))
			}
		},
		ModifyResponse: func(response *http.Response) error {
			if observe != nil {
				observe(response)
			}
			return nil
		},
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, proxyErr error) {
			a.log.Warn("terminal proxy error: " + proxyErr.Error())
			writeErr(rw, http.StatusBadGateway, "UNAVAILABLE", "terminal target request failed")
		},
	}
	proxy.ServeHTTP(w, r)
}

func projectWorkspaceReady(ctx context.Context, ep *sandboxmgr.ResolvedEndpoint) (bool, error) {
	target, err := terminalTargetURL(ep, "/files/info")
	if err != nil {
		return false, err
	}
	query := target.Query()
	query.Set("path", projectWorkspaceMarker)
	target.RawQuery = query.Encode()
	checkCtx, cancel := context.WithTimeout(ctx, terminalCleanupRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(checkCtx, http.MethodGet, target.String(), nil)
	if err != nil {
		return false, err
	}
	for key, value := range ep.Headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	switch response.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("execd files info returned status %d", response.StatusCode)
	}
}

func (a *API) observeCreatedTerminal(
	response *http.Response,
	conv convo.Conversation,
	ep *sandboxmgr.ResolvedEndpoint,
) {
	if response.StatusCode < 200 || response.StatusCode >= 300 || response.Body == nil {
		return
	}
	payload, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(payload))
	response.ContentLength = int64(len(payload))
	response.Header.Set("Content-Length", strconv.Itoa(len(payload)))
	if err != nil {
		a.log.Warn("terminal create response could not be read: " + err.Error())
		return
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(payload, &created); err != nil || !validTerminalID(created.SessionID) {
		a.log.Warn("terminal create response did not contain a valid session id")
		return
	}
	if a.terminalLeases != nil {
		a.terminalLeases.arm(terminalLeaseKey{
			userID: conv.UserID, conversationID: conv.ID, terminalID: created.SessionID,
		}, ep)
	}
}

func terminalTargetURL(ep *sandboxmgr.ResolvedEndpoint, upstreamPath string) (*url.URL, error) {
	base, err := url.Parse(ep.URL)
	if err != nil || base.Host == "" {
		return nil, errors.New("terminal endpoint is malformed")
	}
	base.Path = strings.TrimRight(base.Path, "/") + upstreamPath
	base.RawPath = ""
	return base, nil
}

func (a *API) cleanupTerminalLease(key terminalLeaseKey, ep sandboxmgr.ResolvedEndpoint) {
	deleteCtx, cancelDelete := context.WithTimeout(context.Background(), terminalCleanupRequestTimeout)
	err := deleteTerminalAtEndpoint(deleteCtx, &ep, key.terminalID)
	cancelDelete()
	if err == nil {
		return
	}
	if a.sandboxResolver == nil {
		return
	}
	resolveCtx, cancelResolve := context.WithTimeout(context.Background(), terminalCleanupRequestTimeout)
	current, err := a.sandboxResolver.ResolveEndpoint(
		resolveCtx, key.userID, key.conversationID, terminalExecdPort,
	)
	cancelResolve()
	if err != nil {
		// No cleanup request can be delivered while the sandbox endpoint is unavailable.
		return
	}
	retryCtx, cancelRetry := context.WithTimeout(context.Background(), terminalCleanupRequestTimeout)
	defer cancelRetry()
	if err := deleteTerminalAtEndpoint(retryCtx, current, key.terminalID); err != nil {
		a.log.Warn("terminal cleanup failed after endpoint refresh: " + err.Error())
	}
}

func deleteTerminalAtEndpoint(
	ctx context.Context,
	ep *sandboxmgr.ResolvedEndpoint,
	terminalID string,
) error {
	target, err := terminalTargetURL(ep, "/pty/"+url.PathEscape(terminalID))
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, target.String(), nil)
	if err != nil {
		return err
	}
	for header, value := range ep.Headers {
		request.Header.Set(header, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode == http.StatusNotFound || (response.StatusCode >= 200 && response.StatusCode < 300) {
		return nil
	}
	return fmt.Errorf("terminal cleanup returned status %d", response.StatusCode)
}

type terminalLeaseKey struct {
	userID         string
	conversationID string
	terminalID     string
}

type terminalLease struct {
	version     uint64
	connections map[uint64]struct{}
	endpoint    sandboxmgr.ResolvedEndpoint
	timer       *time.Timer
}

type terminalLeaseRegistry struct {
	mu      sync.Mutex
	grace   time.Duration
	entries map[terminalLeaseKey]*terminalLease
	cleanup func(terminalLeaseKey, sandboxmgr.ResolvedEndpoint)
}

func newTerminalLeaseRegistry(
	grace time.Duration,
	cleanup func(terminalLeaseKey, sandboxmgr.ResolvedEndpoint),
) *terminalLeaseRegistry {
	return &terminalLeaseRegistry{
		grace: grace, entries: make(map[terminalLeaseKey]*terminalLease), cleanup: cleanup,
	}
}

func (r *terminalLeaseRegistry) arm(key terminalLeaseKey, ep *sandboxmgr.ResolvedEndpoint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.entryLocked(key, ep)
	if len(entry.connections) != 0 {
		return
	}
	entry.version++
	r.armLocked(key, entry, entry.version)
}

func (r *terminalLeaseRegistry) attach(
	key terminalLeaseKey,
	ep *sandboxmgr.ResolvedEndpoint,
) func() {
	r.mu.Lock()
	entry := r.entryLocked(key, ep)
	entry.version++
	connection := entry.version
	entry.connections[connection] = struct{}{}
	if entry.timer != nil {
		entry.timer.Stop()
		entry.timer = nil
	}
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			current := r.entries[key]
			if current != entry {
				return
			}
			delete(current.connections, connection)
			if len(current.connections) != 0 {
				return
			}
			current.version++
			r.armLocked(key, current, current.version)
		})
	}
}

func (r *terminalLeaseRegistry) forget(key terminalLeaseKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry := r.entries[key]; entry != nil && entry.timer != nil {
		entry.timer.Stop()
	}
	delete(r.entries, key)
}

func (r *terminalLeaseRegistry) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, entry := range r.entries {
		if entry.timer != nil {
			entry.timer.Stop()
		}
		delete(r.entries, key)
	}
}

func (r *terminalLeaseRegistry) entryLocked(
	key terminalLeaseKey,
	ep *sandboxmgr.ResolvedEndpoint,
) *terminalLease {
	entry := r.entries[key]
	if entry == nil {
		entry = &terminalLease{connections: make(map[uint64]struct{})}
		r.entries[key] = entry
	}
	entry.endpoint = sandboxmgr.ResolvedEndpoint{URL: ep.URL, Headers: make(map[string]string, len(ep.Headers))}
	for header, value := range ep.Headers {
		entry.endpoint.Headers[header] = value
	}
	return entry
}

func (r *terminalLeaseRegistry) armLocked(
	key terminalLeaseKey,
	entry *terminalLease,
	version uint64,
) {
	if entry.timer != nil {
		entry.timer.Stop()
	}
	entry.timer = time.AfterFunc(r.grace, func() {
		r.mu.Lock()
		current := r.entries[key]
		if current != entry || current.version != version || len(current.connections) != 0 {
			r.mu.Unlock()
			return
		}
		delete(r.entries, key)
		endpoint := current.endpoint
		r.mu.Unlock()
		if r.cleanup != nil {
			r.cleanup(key, endpoint)
		}
	})
}

func validTerminalID(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}
