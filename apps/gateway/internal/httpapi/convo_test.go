package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/packages/go-common/logger"
)

// newAPIWithConvo builds a handler with persistence enabled (Memory store) so
// we can assert both the write side (chat mirrors the turn) and the read side
// (list/messages). Auth disabled => DevIdentity is the owner.
func newAPIWithConvo(t *testing.T, fs *fakeStreamer, cs convo.Store) http.Handler {
	t.Helper()
	v := auth.NewVerifier(auth.Config{})
	return New(fs, v, logger.Must()).WithConvoStore(cs).Handler()
}

type fakeReleaser struct {
	calls []string
	err   error
}

func (f *fakeReleaser) ReleaseSession(_ context.Context, userID, sessionID string) error {
	f.calls = append(f.calls, userID+":"+sessionID)
	return f.err
}

// TestChatPersistsTurn: a chat with a session_id mirrors the user prompt and the
// aggregated assistant reply into the store; the conversation then lists and its
// messages read back with the right shape.
func TestChatPersistsTurn(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{
		{Kind: "environment_prepare", Data: map[string]string{
			"snapshot": `{"schema_version":1,"part_id":"environment","state":"preparing","components":[]}`,
		}},
		{Kind: "environment_prepare", Data: map[string]string{
			"snapshot": `{"schema_version":1,"part_id":"environment","state":"ready","components":[{"kind":"skills","status":"ready","label":"Skills","summary":"2 loaded"}]}`,
		}},
		{Kind: "text", Data: map[string]string{"text": "hel"}},
		{Kind: "text", Data: map[string]string{"text": "lo"}},
		{Kind: "tool_use", Data: map[string]string{"id": "t1", "name": "bash", "input": "{}"}},
		{Kind: "tool_result", Data: map[string]string{"tool_use_id": "t1", "content": "done"}},
		{Kind: "done", Data: map[string]string{"reason": "stop"}},
	}}
	cs := convo.NewMemory()
	h := newAPIWithConvo(t, fs, cs)

	// Fire the chat.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat",
		strings.NewReader(`{"prompt":"first question\nsecond line","session_id":"conv-1"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("chat status = %d", rec.Code)
	}

	// List: one conversation, title = truncated first line.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/conversations", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var convs []convo.Conversation
	mustJSON(t, rec.Body.Bytes(), &convs)
	if len(convs) != 1 || convs[0].ID != "conv-1" {
		t.Fatalf("bad list: %+v", convs)
	}
	if convs[0].Title != "first question" {
		t.Fatalf("title should be first line only, got %q", convs[0].Title)
	}

	// Messages: [user, assistant]; assistant has coalesced text + paired tool-call.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/conversations/conv-1/messages", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("messages status = %d", rec.Code)
	}
	var msgs []convo.Message
	mustJSON(t, rec.Body.Bytes(), &msgs)
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Fatalf("bad messages: %+v", msgs)
	}
	ap := msgs[1].Parts
	if len(ap) != 3 || ap[0].Type != convo.PartEnvironment {
		t.Fatalf("assistant environment not persisted first: %+v", ap)
	}
	var environment map[string]any
	if err := json.Unmarshal(ap[0].Environment, &environment); err != nil || environment["state"] != "ready" {
		t.Fatalf("assistant environment snapshot not updated: %v %#v", err, environment)
	}
	if ap[1].Type != convo.PartText || ap[1].Text != "hello" {
		t.Fatalf("assistant text not coalesced: %+v", ap)
	}
	if ap[2].Type != convo.PartToolCall || ap[2].Result == nil || *ap[2].Result != "done" {
		t.Fatalf("assistant tool-call not paired: %+v", ap)
	}
}

func TestChatCanRevealDeferredConversationWithExplicitTitle(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{
		{Kind: "text", Data: map[string]string{"text": "done"}},
		{Kind: "done", Data: map[string]string{"reason": "stop"}},
	}}
	cs := convo.NewMemory()
	h := newAPIWithConvo(t, fs, cs)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat", strings.NewReader(`{
		"prompt":"run scheduled task",
		"session_id":"sched-1",
		"conversation_title":"Daily digest",
		"conversation_type":"scheduled_task",
		"defer_conversation_visibility_until_done":true
	}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("chat status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/conversations", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var convs []convo.Conversation
	mustJSON(t, rec.Body.Bytes(), &convs)
	if len(convs) != 1 ||
		convs[0].ID != "sched-1" ||
		convs[0].Title != "Daily digest" ||
		convs[0].ChatType != "scheduled_task" ||
		convs[0].Hidden {
		t.Fatalf("conversation not revealed with task title: %+v", convs)
	}
}

func TestChatPersistsArtifactAndDownloads(t *testing.T) {
	fs := &fakeStreamer{script: []agent.Event{
		{Kind: "file", Data: map[string]string{
			"id":         "art-1",
			"filename":   "report.txt",
			"mime":       "text/plain",
			"size":       "11",
			"object_key": "artifacts/dev-user/conv-1/art-1-report.txt",
		}},
		{Kind: "done", Data: map[string]string{"reason": "stop"}},
	}}
	cs := convo.NewMemory()
	store := newFakeObjStore()
	store.puts["artifacts/dev-user/conv-1/art-1-report.txt"] = []byte("hello world")
	h := New(fs, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(cs).
		WithObjStore(store, 1024).
		Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat",
		strings.NewReader(`{"prompt":"make a file","session_id":"conv-1"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("chat status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "object_key") {
		t.Fatalf("object_key must not be exposed to browser: %q", body)
	}
	if !strings.Contains(body, `"download_url":"/api/conversations/conv-1/artifacts/art-1"`) {
		t.Fatalf("file event missing download_url: %q", body)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/conversations/conv-1/messages", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("messages status = %d", rec.Code)
	}
	var msgs []convo.Message
	mustJSON(t, rec.Body.Bytes(), &msgs)
	ap := msgs[1].Parts
	if len(ap) != 1 || ap[0].Type != convo.PartFile || ap[0].ID != "art-1" {
		t.Fatalf("assistant file part not persisted: %+v", ap)
	}
	if ap[0].DownloadURL != "/api/conversations/conv-1/artifacts/art-1" {
		t.Fatalf("bad download url: %+v", ap[0])
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/conversations/conv-1/artifacts/art-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("content-type") != "text/plain" {
		t.Fatalf("content-type = %q", rec.Header().Get("content-type"))
	}
	if rec.Body.String() != "hello world" {
		t.Fatalf("download body = %q", rec.Body.String())
	}
}

// TestChatWithoutSessionIDSkipsPersistence: no session_id => nothing stored, but
// the stream still succeeds.
func TestChatWithoutSessionIDSkipsPersistence(t *testing.T) {
	cs := convo.NewMemory()
	h := newAPIWithConvo(t, &fakeStreamer{script: []agent.Event{{Kind: "done"}}}, cs)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat", strings.NewReader(`{"prompt":"hi"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("chat status = %d", rec.Code)
	}
	got, _ := cs.ListConversations(context.Background(), "dev-user")
	if len(got) != 0 {
		t.Fatalf("expected no conversations without session_id, got %d", len(got))
	}
}

// TestConversationsEndpointsWithoutStore: with persistence disabled, both read
// endpoints return an empty list and never 500.
func TestConversationsEndpointsWithoutStore(t *testing.T) {
	v := auth.NewVerifier(auth.Config{})
	h := New(&fakeStreamer{}, v, logger.Must()).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/conversations", nil))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("want empty list, got %d %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/conversations/whatever/messages", nil))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("want empty messages, got %d %q", rec.Code, rec.Body.String())
	}
}

// TestConversationMessagesOwnershipMiss: a conversation owned by someone else
// (or missing) returns 404, never leaks another user's history.
func TestConversationMessagesOwnershipMiss(t *testing.T) {
	cs := convo.NewMemory()
	// Seed a conversation owned by a DIFFERENT user.
	_ = cs.UpsertConversation(context.Background(), convo.Conversation{ID: "other", UserID: "someone-else"})
	h := newAPIWithConvo(t, &fakeStreamer{}, cs)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/conversations/other/messages", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("ownership miss should be 404, got %d", rec.Code)
	}
}

func TestRenameConversationEndpoint(t *testing.T) {
	cs := convo.NewMemory()
	_ = cs.UpsertConversation(context.Background(), convo.Conversation{ID: "conv-1", UserID: auth.DevIdentity.UserID, Title: "old"})
	h := newAPIWithConvo(t, &fakeStreamer{}, cs)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("PATCH", "/v1/conversations/conv-1",
		strings.NewReader(`{"title":"new title"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got convo.Conversation
	mustJSON(t, rec.Body.Bytes(), &got)
	if got.Title != "new title" {
		t.Fatalf("title = %q", got.Title)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("PATCH", "/v1/conversations/conv-1",
		strings.NewReader(`{"title":"   "}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty title should be 400, got %d", rec.Code)
	}
}

func TestDeleteConversationEndpointReleasesAndDeletes(t *testing.T) {
	cs := convo.NewMemory()
	_ = cs.UpsertConversation(context.Background(), convo.Conversation{ID: "conv-1", UserID: auth.DevIdentity.UserID})
	_ = cs.InsertMessage(context.Background(), convo.Message{ID: "m1", ConversationID: "conv-1", Role: "user"})
	releaser := &fakeReleaser{}
	h := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(cs).
		WithAgentReleaser(releaser).
		Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("DELETE", "/v1/conversations/conv-1", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(releaser.calls) != 1 || releaser.calls[0] != auth.DevIdentity.UserID+":conv-1" {
		t.Fatalf("release calls = %+v", releaser.calls)
	}
	if _, err := cs.GetConversation(context.Background(), "conv-1", auth.DevIdentity.UserID); err != convo.ErrNotFound {
		t.Fatalf("conversation should be deleted, got %v", err)
	}
}

func TestDeleteConversationReleaseFailureStillDeletes(t *testing.T) {
	cs := convo.NewMemory()
	_ = cs.UpsertConversation(context.Background(), convo.Conversation{ID: "conv-1", UserID: auth.DevIdentity.UserID})
	releaser := &fakeReleaser{err: errors.New("release failed")}
	h := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(cs).
		WithAgentReleaser(releaser).
		Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("DELETE", "/v1/conversations/conv-1", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := cs.GetConversation(context.Background(), "conv-1", auth.DevIdentity.UserID); err != convo.ErrNotFound {
		t.Fatalf("conversation should be deleted despite release failure, got %v", err)
	}
}

func mustJSON(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, string(b))
	}
}
