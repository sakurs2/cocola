package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/packages/go-common/logger"
)

func TestFolderCRUDAndChatBinding(t *testing.T) {
	store := convo.NewMemory()
	handler := newConfiguredTestAPIWithConvo(
		&fakeStreamer{script: []agent.Event{{Kind: "done"}}},
		auth.NewVerifier(auth.Config{}), logger.Must(), store,
	).Handler()

	created := httptest.NewRecorder()
	handler.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/v1/folders", strings.NewReader(`{"name":"  Research  "}`)))
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", created.Code, created.Body.String())
	}
	var folder convo.Folder
	mustJSON(t, created.Body.Bytes(), &folder)
	if folder.Name != "Research" {
		t.Fatalf("trimmed name = %q", folder.Name)
	}

	duplicate := httptest.NewRecorder()
	handler.ServeHTTP(duplicate, httptest.NewRequest(http.MethodPost, "/v1/folders", strings.NewReader(`{"name":"research"}`)))
	if duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), "FOLDER_NAME_EXISTS") {
		t.Fatalf("duplicate status = %d, body=%s", duplicate.Code, duplicate.Body.String())
	}

	chat := httptest.NewRecorder()
	handler.ServeHTTP(chat, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(`{
		"prompt":"hello", "session_id":"chat-1", "client_request_id":"request-1",
		"folder_id":"`+folder.ID+`"
	}`)))
	if chat.Code != http.StatusOK {
		t.Fatalf("chat status = %d, body=%s", chat.Code, chat.Body.String())
	}
	conversation, err := store.GetConversation(context.Background(), "chat-1", auth.DevIdentity.UserID)
	if err != nil || conversation.FolderID != folder.ID {
		t.Fatalf("folder binding = %+v, %v", conversation, err)
	}

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/v1/folders", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d", list.Code)
	}
	var folders []convo.Folder
	mustJSON(t, list.Body.Bytes(), &folders)
	if len(folders) != 1 || folders[0].ID != folder.ID {
		t.Fatalf("folder list = %+v", folders)
	}
}

func TestInvalidFolderChatHasNoWrites(t *testing.T) {
	store := convo.NewMemory()
	handler := newConfiguredTestAPIWithConvo(
		&fakeStreamer{script: []agent.Event{{Kind: "done"}}},
		auth.NewVerifier(auth.Config{}), logger.Must(), store,
	).Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(`{
		"prompt":"hello", "session_id":"chat-1", "client_request_id":"request-1",
		"folder_id":"missing"
	}`)))
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), "FOLDER_NOT_FOUND") {
		t.Fatalf("chat status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	conversations, err := store.ListConversations(context.Background(), auth.DevIdentity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 0 {
		t.Fatalf("invalid folder created conversations: %+v", conversations)
	}
}

func TestFolderMoveAndDeleteRejectActiveRun(t *testing.T) {
	ctx := context.Background()
	store := convo.NewMemory()
	runStore := chatrun.NewMemory(store)
	now := time.Now().UTC()
	folder, err := store.CreateFolder(ctx, convo.Folder{
		ID: "folder-1", UserID: auth.DevIdentity.UserID, Name: "Work", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runStore.Start(ctx, chatrun.StartInput{
		Run: chatrun.Run{
			ID: "run-1", ConversationID: "chat-1", UserID: auth.DevIdentity.UserID,
			Status: chatrun.StatusRunning, StartedAt: now, LastActivityAt: now,
		},
		Conversation: convo.Conversation{
			ID: "chat-1", UserID: auth.DevIdentity.UserID, ChatType: "chat",
			FolderID: folder.ID, CreatedAt: now, UpdatedAt: now,
		},
		UserMessage: convo.Message{
			ID: "message-1", ConversationID: "chat-1", Role: "user", CreatedAt: now,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	api := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(store).
		WithChatRuns(runStore, RunConfig{})
	handler := api.Handler()

	move := httptest.NewRecorder()
	handler.ServeHTTP(move, httptest.NewRequest(http.MethodPut, "/v1/conversations/chat-1/folder", strings.NewReader(`{"folder_id":null}`)))
	assertRunInProgress(t, move)

	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, httptest.NewRequest(http.MethodDelete, "/v1/folders/folder-1", nil))
	assertRunInProgress(t, deleteRecorder)
	if _, err := store.GetFolder(ctx, folder.ID, auth.DevIdentity.UserID); err != nil {
		t.Fatalf("folder changed after rejected delete: %v", err)
	}
	if _, err := store.GetConversation(ctx, "chat-1", auth.DevIdentity.UserID); err != nil {
		t.Fatalf("conversation changed after rejected delete: %v", err)
	}
}

func TestFolderDeleteCascadesAndReleasesSessions(t *testing.T) {
	ctx := context.Background()
	store := convo.NewMemory()
	now := time.Now().UTC()
	folder, err := store.CreateFolder(ctx, convo.Folder{
		ID: "folder-1", UserID: auth.DevIdentity.UserID, Name: "Work", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertConversation(ctx, convo.Conversation{
		ID: "chat-1", UserID: auth.DevIdentity.UserID, ChatType: "chat",
		FolderID: folder.ID, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	releaser := &fakeReleaser{}
	handler := newConfiguredTestAPIWithConvo(
		&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must(), store,
	).WithAgentReleaser(releaser).Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/v1/folders/folder-1", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if len(releaser.calls) != 1 || releaser.calls[0] != auth.DevIdentity.UserID+":chat-1" {
		t.Fatalf("release calls = %v", releaser.calls)
	}
	if _, err := store.GetConversation(ctx, "chat-1", auth.DevIdentity.UserID); err != convo.ErrNotFound {
		t.Fatalf("deleted conversation = %v", err)
	}
}

func TestFolderDeleteFailureLeavesCleanupRetryable(t *testing.T) {
	ctx := context.Background()
	store := convo.NewMemory()
	now := time.Now().UTC()
	if _, err := store.CreateFolder(ctx, convo.Folder{
		ID: "folder-1", UserID: auth.DevIdentity.UserID, Name: "Work", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertConversation(ctx, convo.Conversation{
		ID: "chat-1", UserID: auth.DevIdentity.UserID, FolderID: "folder-1", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	releaser := &fakeReleaser{err: errors.New("storage unavailable")}
	handler := newConfiguredTestAPIWithConvo(
		&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must(), store,
	).WithAgentReleaser(releaser).Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/v1/folders/folder-1", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := store.GetFolder(ctx, "folder-1", auth.DevIdentity.UserID); !errors.Is(err, convo.ErrNotFound) {
		t.Fatalf("folder must remain deleted after cleanup failure: %v", err)
	}
}

func TestFolderDeleteReleasesMutationLockBeforeRemoteCleanup(t *testing.T) {
	ctx := context.Background()
	store := convo.NewMemory()
	runStore := chatrun.NewMemory(store)
	now := time.Now().UTC()
	if _, err := store.CreateFolder(ctx, convo.Folder{
		ID: "folder-1", UserID: auth.DevIdentity.UserID, Name: "Work", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertConversation(ctx, convo.Conversation{
		ID: "chat-1", UserID: auth.DevIdentity.UserID, FolderID: "folder-1", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	releaser := &blockingReleaser{started: make(chan struct{}), unblock: make(chan struct{})}
	api := newConfiguredTestAPIWithConvo(
		&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must(), store,
	).WithChatRuns(runStore, RunConfig{}).WithAgentReleaser(releaser)

	done := make(chan int, 1)
	go func() {
		recorder := httptest.NewRecorder()
		api.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/v1/folders/folder-1", nil))
		done <- recorder.Code
	}()
	select {
	case <-releaser.started:
	case <-time.After(time.Second):
		t.Fatal("remote cleanup did not start")
	}
	if !api.runs.mutationMu.TryLock() {
		t.Fatal("folder cleanup still holds the global run mutation lock")
	}
	api.runs.mutationMu.Unlock()
	close(releaser.unblock)
	select {
	case status := <-done:
		if status != http.StatusNoContent {
			t.Fatalf("delete status = %d", status)
		}
	case <-time.After(time.Second):
		t.Fatal("folder delete did not finish")
	}
}

func assertRunInProgress(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		ConversationID string `json:"conversation_id"`
		RunID          string `json:"run_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "RUN_IN_PROGRESS" || body.ConversationID != "chat-1" || body.RunID != "run-1" {
		t.Fatalf("run conflict body = %+v", body)
	}
}
