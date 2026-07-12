package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/packages/go-common/logger"
)

type blockingChatStreamer struct {
	started  chan struct{}
	stopped  chan struct{}
	startOne sync.Once
	stopOne  sync.Once
}

type controlledFinalizeStore struct {
	chatrun.Store
	mu      sync.Mutex
	calls   int
	failAll bool
}

func (s *controlledFinalizeStore) Finalize(ctx context.Context, in chatrun.FinalizeInput) (chatrun.Run, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.failAll || in.AssistantMessage != nil {
		return chatrun.Run{}, errors.New("injected finalization failure")
	}
	return s.Store.Finalize(ctx, in)
}

func (s *controlledFinalizeStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func newBlockingChatStreamer() *blockingChatStreamer {
	return &blockingChatStreamer{started: make(chan struct{}), stopped: make(chan struct{})}
}

func (s *blockingChatStreamer) Stream(ctx context.Context, _ agent.Query, onEvent func(agent.Event) error) error {
	if err := onEvent(agent.Event{Kind: "text", Data: map[string]string{"text": "partial"}}); err != nil {
		return err
	}
	s.startOne.Do(func() { close(s.started) })
	<-ctx.Done()
	s.stopOne.Do(func() { close(s.stopped) })
	return ctx.Err()
}

func durableTestAPI(streamer agent.Streamer) (*API, *chatrun.Memory, *convo.Memory) {
	conversations := convo.NewMemory()
	runs := chatrun.NewMemory(conversations)
	api := New(streamer, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(conversations).
		WithChatRuns(runs, RunConfig{
			RunTimeout: time.Minute, PingEvery: time.Hour,
			MergeWindow: time.Millisecond, DraftInterval: time.Millisecond,
		})
	return api, runs, conversations
}

func waitForRunStatus(t *testing.T, store chatrun.Store, runID, want string) chatrun.Run {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, err := store.GetOwned(context.Background(), runID, auth.DevIdentity.UserID)
		if err == nil && run.Status == want {
			return run
		}
		time.Sleep(5 * time.Millisecond)
	}
	run, err := store.GetOwned(context.Background(), runID, auth.DevIdentity.UserID)
	t.Fatalf("run status = %+v, %v; want %s", run, err, want)
	return chatrun.Run{}
}

func TestDurableChatDisconnectDoesNotCancelRun(t *testing.T) {
	streamer := newBlockingChatStreamer()
	api, runs, conversations := durableTestAPI(streamer)
	handler := api.Handler()

	requestContext, disconnect := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
		`{"prompt":"hello","session_id":"conversation-1","client_request_id":"request-1"}`,
	)).WithContext(requestContext)
	recorder := httptest.NewRecorder()
	handlerDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(recorder, req)
		close(handlerDone)
	}()

	select {
	case <-streamer.started:
	case <-time.After(time.Second):
		t.Fatal("agent run did not start")
	}
	disconnect()
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("disconnected subscription did not return")
	}
	select {
	case <-streamer.stopped:
		t.Fatal("browser disconnect cancelled the background agent run")
	case <-time.After(30 * time.Millisecond):
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		messages, err := conversations.GetMessages(
			context.Background(), "conversation-1", auth.DevIdentity.UserID,
		)
		if err == nil && len(messages) == 2 && messages[1].Parts[0].Text == "partial" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	messages, err := conversations.GetMessages(
		context.Background(), "conversation-1", auth.DevIdentity.UserID,
	)
	if err != nil || len(messages) != 2 || messages[1].Parts[0].Text != "partial" {
		t.Fatalf("running draft was not persisted by the timer: %+v, %v", messages, err)
	}

	runID := recorder.Header().Get("x-cocola-run-id")
	if runID == "" {
		t.Fatal("missing run id response header")
	}
	active := httptest.NewRecorder()
	handler.ServeHTTP(active, httptest.NewRequest(http.MethodGet,
		"/v1/chat/runs/active?conversation_id=conversation-1", nil))
	if active.Code != http.StatusOK || !strings.Contains(active.Body.String(), runID) {
		t.Fatalf("active run response = %d %s", active.Code, active.Body.String())
	}

	cancel := httptest.NewRecorder()
	handler.ServeHTTP(cancel, httptest.NewRequest(http.MethodDelete, "/v1/chat/runs/"+runID, nil))
	if cancel.Code != http.StatusAccepted {
		t.Fatalf("cancel response = %d %s", cancel.Code, cancel.Body.String())
	}
	select {
	case <-streamer.stopped:
	case <-time.After(time.Second):
		t.Fatal("explicit cancel did not stop the agent run")
	}
	waitForRunStatus(t, runs, runID, chatrun.StatusCancelled)
	messages, err = conversations.GetMessages(context.Background(), "conversation-1", auth.DevIdentity.UserID)
	if err != nil || len(messages) != 2 || messages[1].Parts[0].Text != "partial" {
		t.Fatalf("cancelled partial output = %+v, %v", messages, err)
	}
}

func TestDeleteConversationRejectsActiveRun(t *testing.T) {
	streamer := newBlockingChatStreamer()
	api, runs, conversations := durableTestAPI(streamer)
	handler := api.Handler()

	chat := httptest.NewRecorder()
	chatDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(chat, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
			`{"prompt":"hello","session_id":"conversation-1","client_request_id":"request-1"}`,
		)))
		close(chatDone)
	}()
	select {
	case <-streamer.started:
	case <-time.After(time.Second):
		t.Fatal("agent run did not start")
	}

	deleteWhileRunning := httptest.NewRecorder()
	handler.ServeHTTP(deleteWhileRunning, httptest.NewRequest(
		http.MethodDelete, "/v1/conversations/conversation-1", nil,
	))
	if deleteWhileRunning.Code != http.StatusConflict ||
		!strings.Contains(deleteWhileRunning.Body.String(), `"code":"RUN_IN_PROGRESS"`) {
		t.Fatalf("active delete response = %d %s", deleteWhileRunning.Code, deleteWhileRunning.Body.String())
	}
	if _, err := conversations.GetConversation(
		context.Background(), "conversation-1", auth.DevIdentity.UserID,
	); err != nil {
		t.Fatalf("active conversation was deleted: %v", err)
	}

	runID := chat.Header().Get("x-cocola-run-id")
	cancel := httptest.NewRecorder()
	handler.ServeHTTP(cancel, httptest.NewRequest(http.MethodDelete, "/v1/chat/runs/"+runID, nil))
	if cancel.Code != http.StatusAccepted {
		t.Fatalf("cancel response = %d %s", cancel.Code, cancel.Body.String())
	}
	waitForRunStatus(t, runs, runID, chatrun.StatusCancelled)
	select {
	case <-chatDone:
	case <-time.After(time.Second):
		t.Fatal("chat subscription did not finish after cancellation")
	}

	deleteFinished := httptest.NewRecorder()
	handler.ServeHTTP(deleteFinished, httptest.NewRequest(
		http.MethodDelete, "/v1/conversations/conversation-1", nil,
	))
	if deleteFinished.Code != http.StatusNoContent {
		t.Fatalf("terminal delete response = %d %s", deleteFinished.Code, deleteFinished.Body.String())
	}
}

func TestFinalizeRunRetriesAreBoundedAndFallbackToInterrupted(t *testing.T) {
	conversations := convo.NewMemory()
	base := chatrun.NewMemory(conversations)
	startedAt := time.Now().UTC()
	_, err := base.Start(context.Background(), chatrun.StartInput{
		Run: chatrun.Run{
			ID: "run-1", ConversationID: "conversation-1", UserID: auth.DevIdentity.UserID,
			Status: chatrun.StatusRunning, StartedAt: startedAt, LastActivityAt: startedAt,
		},
		Conversation: convo.Conversation{
			ID: "conversation-1", UserID: auth.DevIdentity.UserID,
			CreatedAt: startedAt, UpdatedAt: startedAt,
		},
		UserMessage: convo.Message{
			ID: "run-1-user", ConversationID: "conversation-1", Role: "user", CreatedAt: startedAt,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &controlledFinalizeStore{Store: base}
	api := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithChatRuns(store, RunConfig{FinalizeRetry: time.Microsecond})

	run, ok := api.finalizeRun(chatrun.FinalizeInput{
		RunID: "run-1", UserID: auth.DevIdentity.UserID, Status: chatrun.StatusSuccess,
		AssistantMessage: &convo.Message{
			ID: "run-1-assistant", ConversationID: "conversation-1", Role: "assistant",
		},
	})
	if !ok || run.Status != chatrun.StatusInterrupted || run.ErrorCode != "FINALIZATION_FAILED" {
		t.Fatalf("fallback result = %+v, %v", run, ok)
	}
	if got, want := store.callCount(), finalizeMaxAttempts+1; got != want {
		t.Fatalf("finalize calls = %d, want %d", got, want)
	}

	alwaysFail := &controlledFinalizeStore{Store: base, failAll: true}
	api = New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithChatRuns(alwaysFail, RunConfig{FinalizeRetry: time.Microsecond})
	if _, ok := api.finalizeRun(chatrun.FinalizeInput{
		RunID: "run-2", UserID: auth.DevIdentity.UserID, Status: chatrun.StatusSuccess,
	}); ok {
		t.Fatal("permanent failure unexpectedly finalized")
	}
	if got, want := alwaysFail.callCount(), finalizeMaxAttempts+1; got != want {
		t.Fatalf("permanent failure calls = %d, want %d", got, want)
	}
}

func TestDurableChatBusinessErrorCannotBecomeSuccess(t *testing.T) {
	streamer := &fakeStreamer{script: []agent.Event{
		{Kind: "text", Data: map[string]string{"text": "before error"}},
		{Kind: "error", Data: map[string]string{"error": "tool failed"}},
		{Kind: "done", Data: map[string]string{"reason": "complete"}},
	}}
	api, runs, _ := durableTestAPI(streamer)
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
		`{"prompt":"hello","session_id":"conversation-1","client_request_id":"request-1"}`,
	)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("chat response = %d %s", recorder.Code, recorder.Body.String())
	}
	runID := recorder.Header().Get("x-cocola-run-id")
	run := waitForRunStatus(t, runs, runID, chatrun.StatusError)
	if run.ErrorCode != "AGENT_ERROR" {
		t.Fatalf("error code = %q", run.ErrorCode)
	}
	if !strings.Contains(recorder.Body.String(), `"status":"error"`) {
		t.Fatalf("terminal SSE did not report error: %s", recorder.Body.String())
	}
}

func TestReconnectSnapshotDoesNotDuplicateBufferedText(t *testing.T) {
	streamer := newBlockingChatStreamer()
	conversations := convo.NewMemory()
	runs := chatrun.NewMemory(conversations)
	api := New(streamer, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(conversations).
		WithChatRuns(runs, RunConfig{
			RunTimeout: time.Minute, PingEvery: time.Hour,
			MergeWindow: 500 * time.Millisecond, DraftInterval: time.Hour,
		})
	handler := api.Handler()

	postContext, disconnect := context.WithCancel(context.Background())
	post := httptest.NewRecorder()
	postDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
			`{"prompt":"hello","session_id":"conversation-1","client_request_id":"request-1"}`,
		)).WithContext(postContext))
		close(postDone)
	}()
	select {
	case <-streamer.started:
	case <-time.After(time.Second):
		t.Fatal("agent run did not start")
	}
	disconnect()
	<-postDone
	runID := post.Header().Get("x-cocola-run-id")

	replay := httptest.NewRecorder()
	replayDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(replay, httptest.NewRequest(http.MethodGet, "/v1/chat/runs/"+runID, nil))
		close(replayDone)
	}()
	time.Sleep(600 * time.Millisecond)
	cancel := httptest.NewRecorder()
	handler.ServeHTTP(cancel, httptest.NewRequest(http.MethodDelete, "/v1/chat/runs/"+runID, nil))
	select {
	case <-replayDone:
	case <-time.After(time.Second):
		t.Fatal("reconnected stream did not terminate")
	}
	if count := strings.Count(replay.Body.String(), `"text":"partial"`); count != 1 {
		t.Fatalf("reconnected text count = %d, want 1; body=%s", count, replay.Body.String())
	}
}

func TestStoredRunReplayIncludesSavedAssistantSnapshot(t *testing.T) {
	streamer := &fakeStreamer{script: []agent.Event{
		{Kind: "text", Data: map[string]string{"text": "saved answer"}},
		{Kind: "done", Data: map[string]string{"reason": "complete"}},
	}}
	api, runs, _ := durableTestAPI(streamer)
	handler := api.Handler()
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
		`{"prompt":"hello","session_id":"conversation-1","client_request_id":"request-1"}`,
	)))
	runID := first.Header().Get("x-cocola-run-id")
	waitForRunStatus(t, runs, runID, chatrun.StatusSuccess)

	replay := httptest.NewRecorder()
	handler.ServeHTTP(replay, httptest.NewRequest(http.MethodGet, "/v1/chat/runs/"+runID, nil))
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), "saved answer") {
		t.Fatalf("stored replay = %d %s", replay.Code, replay.Body.String())
	}
	assertNoActiveRun(t, handler, "conversation-1")
}

func assertNoActiveRun(t *testing.T, handler http.Handler, conversationID string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
		"/v1/chat/runs/active?conversation_id="+conversationID, nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("active completed run response = %d %s", recorder.Code, recorder.Body.String())
	}
}
