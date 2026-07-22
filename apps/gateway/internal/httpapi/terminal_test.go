package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/apps/gateway/internal/sandboxmgr"
	"github.com/cocola-project/cocola/packages/go-common/logger"
)

func TestCreateTerminal_UsesProjectWorktreeAndExecdEndpoint(t *testing.T) {
	var gotPath, gotAuth, gotCookie, gotExecdToken, gotReadyPath string
	var gotBody createTerminalRequest
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files/info" {
			gotReadyPath = r.URL.Query().Get("path")
			if r.Header.Get("X-EXECD-ACCESS-TOKEN") != "execd-token" {
				t.Errorf("readiness request omitted execd token")
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCookie = r.Header.Get("Cookie")
		gotExecdToken = r.Header.Get("X-EXECD-ACCESS-TOKEN")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"session_id":"pty-1"}`)
	}))
	defer backend.Close()

	store := convo.NewMemory()
	now := time.Now().UTC()
	if err := store.UpsertConversation(context.Background(), convo.Conversation{
		ID: "conv-1", UserID: auth.DevIdentity.UserID, ProjectID: "project-1", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	resolver := &fakeResolver{
		url: backend.URL, headers: map[string]string{"X-EXECD-ACCESS-TOKEN": "execd-token"},
	}
	api := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(store).
		WithSandboxResolver(resolver)
	defer api.terminalLeases.close()
	h := api.Handler()

	req := httptest.NewRequest(http.MethodPost, "/v1/conversations/conv-1/terminal", nil)
	req.Header.Set("Authorization", "Bearer browser-token")
	req.Header.Set("Cookie", "session=browser-cookie")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/pty" {
		t.Fatalf("upstream path = %q, want /pty", gotPath)
	}
	if gotReadyPath != projectWorkspaceMarker {
		t.Fatalf("readiness path = %q, want %q", gotReadyPath, projectWorkspaceMarker)
	}
	if gotBody.Cwd != "/workspace/project" {
		t.Fatalf("cwd = %q, want project worktree", gotBody.Cwd)
	}
	if gotBody.Command != "export TERM=xterm-256color COLORTERM=truecolor; exec /bin/bash --noprofile --norc -i" {
		t.Fatalf("command = %q, want project shell", gotBody.Command)
	}
	if gotAuth != "" || gotCookie != "" {
		t.Fatalf("browser credentials leaked upstream: authorization=%q cookie=%q", gotAuth, gotCookie)
	}
	if gotExecdToken != "execd-token" {
		t.Fatalf("execd token = %q, want injected token", gotExecdToken)
	}
	if resolver.gotUser != auth.DevIdentity.UserID || resolver.gotSession != "conv-1" || resolver.gotPort != terminalExecdPort {
		t.Fatalf("resolve args = (%q,%q,%d)", resolver.gotUser, resolver.gotSession, resolver.gotPort)
	}
}

func TestCreateTerminal_UsesWorkspaceForOrdinaryChat(t *testing.T) {
	var gotBody createTerminalRequest
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer backend.Close()

	store := convo.NewMemory()
	now := time.Now().UTC()
	if err := store.UpsertConversation(context.Background(), convo.Conversation{
		ID: "conv-1", UserID: auth.DevIdentity.UserID, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	api := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(store).
		WithSandboxResolver(&fakeResolver{url: backend.URL})
	defer api.terminalLeases.close()
	h := api.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/conversations/conv-1/terminal", nil))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if gotBody.Cwd != "/workspace" {
		t.Fatalf("cwd = %q, want /workspace", gotBody.Cwd)
	}
	if gotBody.Command != "export TERM=xterm-256color COLORTERM=truecolor; exec /bin/bash --noprofile --norc -i" {
		t.Fatalf("ordinary shell command = %q, want root interactive bash", gotBody.Command)
	}
}

func TestCreateTerminal_WaitsForProjectMarker(t *testing.T) {
	ptyRequests := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path == "/pty" {
			ptyRequests++
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	store := convo.NewMemory()
	now := time.Now().UTC()
	if err := store.UpsertConversation(context.Background(), convo.Conversation{
		ID: "conv-1", UserID: auth.DevIdentity.UserID, ProjectID: "project-1", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	api := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(store).
		WithSandboxResolver(&fakeResolver{url: backend.URL})
	defer api.terminalLeases.close()
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost,
		"/v1/conversations/conv-1/terminal", nil))

	if recorder.Code != http.StatusTooEarly {
		t.Fatalf("status = %d, want 425; body=%s", recorder.Code, recorder.Body.String())
	}
	if ptyRequests != 0 {
		t.Fatalf("created PTY before project marker was ready")
	}
}

func TestTerminalLeaseRegistry_CleansOnlyAfterDisconnect(t *testing.T) {
	cleaned := make(chan terminalLeaseKey, 1)
	registry := newTerminalLeaseRegistry(20*time.Millisecond,
		func(key terminalLeaseKey, _ sandboxmgr.ResolvedEndpoint) { cleaned <- key })
	defer registry.close()
	key := terminalLeaseKey{userID: "user-1", conversationID: "conv-1", terminalID: "pty-1"}
	endpoint := &sandboxmgr.ResolvedEndpoint{URL: "http://execd", Headers: map[string]string{"token": "value"}}

	registry.arm(key, endpoint)
	release := registry.attach(key, endpoint)
	releaseOverlappingAttempt := registry.attach(key, endpoint)
	releaseOverlappingAttempt()
	select {
	case <-cleaned:
		t.Fatal("cleaned a terminal while another attachment was still active")
	case <-time.After(60 * time.Millisecond):
	}
	release()
	select {
	case got := <-cleaned:
		if got != key {
			t.Fatalf("cleanup key = %#v, want %#v", got, key)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("detached terminal was not cleaned")
	}
}

func TestCreateTerminal_UnattachedSessionIsReclaimed(t *testing.T) {
	deleted := make(chan string, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/pty":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"session_id":"pty-orphan"}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/pty/pty-orphan":
			deleted <- r.Header.Get("X-EXECD-ACCESS-TOKEN")
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer backend.Close()

	store := convo.NewMemory()
	now := time.Now().UTC()
	if err := store.UpsertConversation(context.Background(), convo.Conversation{
		ID: "conv-1", UserID: auth.DevIdentity.UserID, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	api := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(store).
		WithSandboxResolver(&fakeResolver{
			url: backend.URL, headers: map[string]string{"X-EXECD-ACCESS-TOKEN": "execd-token"},
		})
	api.terminalLeases.grace = 20 * time.Millisecond
	defer api.terminalLeases.close()
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost,
		"/v1/conversations/conv-1/terminal", nil))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", recorder.Code, recorder.Body.String())
	}

	select {
	case token := <-deleted:
		if token != "execd-token" {
			t.Fatalf("cleanup token = %q, want execd-token", token)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("unattached terminal was not reclaimed")
	}
}

func TestTerminalSessionProxy_RejectsInvalidIDBeforeResolving(t *testing.T) {
	resolver := &fakeResolver{url: "http://127.0.0.1:1"}
	h := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(convo.NewMemory()).
		WithSandboxResolver(resolver).
		Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete,
		"/v1/conversations/conv-1/terminal/not%2Fvalid", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resolver.gotPort != 0 {
		t.Fatalf("resolver called for invalid terminal id")
	}
}

func TestTerminalWebSocketProxy_PreservesReconnectQuery(t *testing.T) {
	var gotPath, gotQuery string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	store := convo.NewMemory()
	now := time.Now().UTC()
	if err := store.UpsertConversation(context.Background(), convo.Conversation{
		ID: "conv-1", UserID: auth.DevIdentity.UserID, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	api := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(store).
		WithSandboxResolver(&fakeResolver{url: backend.URL})
	defer api.terminalLeases.close()
	h := api.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/v1/conversations/conv-1/terminal/pty-1/ws?since=42&takeover=1", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if gotPath != "/pty/pty-1/ws" || gotQuery != "since=42&takeover=1" {
		t.Fatalf("upstream target = %q?%s", gotPath, gotQuery)
	}
}

func TestCreateTerminal_RequiresOwnedConversation(t *testing.T) {
	resolver := &fakeResolver{url: "http://127.0.0.1:1"}
	h := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(convo.NewMemory()).
		WithSandboxResolver(resolver).
		Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
		"/v1/conversations/missing/terminal", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if resolver.gotPort != 0 {
		t.Fatalf("resolver called for missing conversation")
	}
}

func TestValidTerminalID(t *testing.T) {
	for _, tc := range []struct {
		id   string
		want bool
	}{
		{id: "123e4567-e89b-12d3-a456-426614174000", want: true},
		{id: "pty_123", want: true},
		{id: "", want: false},
		{id: "../pty", want: false},
		{id: "pty/session", want: false},
	} {
		if got := validTerminalID(tc.id); got != tc.want {
			t.Errorf("validTerminalID(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}
