package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	feishuconnector "github.com/cocola-project/cocola/apps/gateway/internal/channel/feishu"
	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/apps/gateway/internal/memory"
	"github.com/cocola-project/cocola/apps/gateway/internal/project"
	"github.com/cocola-project/cocola/packages/go-common/logger"
)

type blockingFeishuCredentialStore struct {
	feishuconnector.Store
}

func (*blockingFeishuCredentialStore) GetConnectorByID(
	ctx context.Context,
	_ string,
) (feishuconnector.Connector, error) {
	<-ctx.Done()
	return feishuconnector.Connector{}, ctx.Err()
}

func TestResolveLarkRuntimeCredentialTimesOutWithoutBlockingChat(t *testing.T) {
	service, err := feishuconnector.NewService(
		context.Background(),
		&blockingFeishuCredentialStore{},
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	api := &API{feishu: service, log: logger.Must()}

	started := time.Now()
	credential := api.resolveLarkRuntimeCredential(
		context.Background(),
		auth.Identity{TenantID: "tenant", UserID: "user"},
		"connector-1",
		20*time.Millisecond,
	)
	if credential.Status != feishuconnector.RuntimeCredentialUnavailable {
		t.Fatalf("credential = %#v", credential)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("credential resolution blocked for %s", elapsed)
	}
}

func TestLiveRunMemoryRecallPublishesAndPersistsExactContext(t *testing.T) {
	events := make(chan agent.Event, 1)
	live := &liveRun{
		reducer: convo.NewReducer(),
		subs:    map[chan agent.Event]struct{}{events: {}},
	}
	contextText := "User profile:\nPrefers concise answers\n\nRelevant memory:\nUses Go"

	live.updateMemoryRecall(memory.RecallResult{
		Status: memory.RecallStatusHit, Count: 2, Context: contextText,
	})

	event := <-events
	if event.Data["content"] != contextText {
		t.Fatalf("published memory content = %q", event.Data["content"])
	}
	parts := live.parts()
	if len(parts) != 1 || parts[0].MemoryContent != contextText {
		t.Fatalf("persisted memory content = %+v", parts)
	}
}

func TestMeaningfulAssistantOutputIgnoresRuntimeScaffolding(t *testing.T) {
	scaffolding := []convo.Part{
		{Type: convo.PartEnvironment},
		{Type: convo.PartSessionStatus},
		{Type: convo.PartMemoryRecall},
		{Type: convo.PartProgress},
		{Type: convo.PartReasoning, Text: "internal reasoning"},
		{Type: convo.PartText, Text: "  \n"},
	}
	if hasMeaningfulAssistantOutput(scaffolding) {
		t.Fatal("runtime scaffolding was treated as a completed assistant answer")
	}
	for _, part := range []convo.Part{
		{Type: convo.PartText, Text: "answer"},
		{Type: convo.PartToolCall, ToolName: "Bash"},
		{Type: convo.PartFile, Filename: "result.txt"},
		{Type: convo.PartStructured},
	} {
		if !hasMeaningfulAssistantOutput([]convo.Part{part}) {
			t.Fatalf("meaningful part %q was rejected", part.Type)
		}
	}
}

type blockingChatStreamer struct {
	started  chan struct{}
	stopped  chan struct{}
	startOne sync.Once
	stopOne  sync.Once
}

type planModeStreamer struct {
	mu      sync.Mutex
	queries []agent.Query
}

type questionModeStreamer struct {
	mu               sync.Mutex
	queries          []agent.Query
	answerStartError error
}

type planWorkspaceStore struct {
	project.Store
	project   project.Project
	workspace project.Workspace
}

func (s *planWorkspaceStore) GetWorkspace(
	_ context.Context,
	_ project.Identity,
	conversationID string,
) (project.Workspace, project.Project, error) {
	if conversationID != s.workspace.ConversationID {
		return project.Workspace{}, project.Project{}, project.ErrNotFound
	}
	return s.workspace, s.project, nil
}

func (s *planWorkspaceStore) RevokeBrokerRun(
	_ context.Context,
	_ project.Identity,
	_ string,
	_ time.Time,
) error {
	return nil
}

func (s *planWorkspaceStore) ListActiveTokenLeasesForRun(
	_ context.Context,
	_ project.Identity,
	_ string,
	_ time.Time,
) ([]project.TokenLease, error) {
	return nil, nil
}

type blockingPlanWorkspaceStreamer struct {
	mu              sync.Mutex
	queries         []agent.Query
	inspectionStart chan struct{}
	releaseInspect  chan struct{}
	ordinaryStart   chan struct{}
	releaseExecute  chan struct{}
	inspectOnce     sync.Once
	ordinaryOnce    sync.Once
}

func newBlockingPlanWorkspaceStreamer() *blockingPlanWorkspaceStreamer {
	return &blockingPlanWorkspaceStreamer{
		inspectionStart: make(chan struct{}),
		releaseInspect:  make(chan struct{}),
		ordinaryStart:   make(chan struct{}),
		releaseExecute:  make(chan struct{}),
	}
}

func (s *blockingPlanWorkspaceStreamer) Stream(
	ctx context.Context,
	query agent.Query,
	onEvent func(agent.Event) error,
) error {
	s.mu.Lock()
	s.queries = append(s.queries, query)
	s.mu.Unlock()
	if query.InteractionMode == agent.InteractionModePlan {
		if err := onEvent(agent.Event{Kind: "plan_ready", Data: map[string]string{
			"content_markdown":   "## Plan\n\n- Implement the change",
			"workspace_revision": "revision-1",
		}}); err != nil {
			return err
		}
		return onEvent(agent.Event{Kind: "done"})
	}
	if query.Prompt == "ordinary change" {
		s.ordinaryOnce.Do(func() { close(s.ordinaryStart) })
	}
	select {
	case <-s.releaseExecute:
	case <-ctx.Done():
		return ctx.Err()
	}
	return onEvent(agent.Event{Kind: "done"})
}

func (s *blockingPlanWorkspaceStreamer) InspectWorkspaceGit(
	ctx context.Context,
	_ agent.InspectRequest,
) (agent.GitInspection, error) {
	s.inspectOnce.Do(func() { close(s.inspectionStart) })
	select {
	case <-s.releaseInspect:
	case <-ctx.Done():
		return agent.GitInspection{}, ctx.Err()
	}
	return agent.GitInspection{
		Snapshot: agent.GitSnapshot{WorkspaceRevision: "revision-1"},
	}, nil
}

func (s *planModeStreamer) Stream(_ context.Context, query agent.Query, onEvent func(agent.Event) error) error {
	s.mu.Lock()
	s.queries = append(s.queries, query)
	call := len(s.queries)
	s.mu.Unlock()
	if query.InteractionMode == agent.InteractionModePlan {
		content := "## Plan\n\n- Implement the change"
		if call > 1 {
			content = "## Revised plan\n\n- Add the requested tests"
		}
		if err := onEvent(agent.Event{Kind: "plan_ready", Data: map[string]string{
			"content_markdown": content,
		}}); err != nil {
			return err
		}
	} else {
		if err := onEvent(agent.Event{Kind: "text", Data: map[string]string{
			"text": "Implemented.",
		}}); err != nil {
			return err
		}
	}
	return onEvent(agent.Event{Kind: "done"})
}

func TestPlanRevisionIsPersistedAsUserTurnAndSupersedesAfterSuccess(t *testing.T) {
	streamer := &planModeStreamer{}
	conversations := convo.NewMemory()
	runs := chatrun.NewMemory(conversations)
	api := New(streamer, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(conversations).
		WithChatRuns(runs, RunConfig{
			PingEvery: time.Hour, MergeWindow: time.Millisecond, DraftInterval: time.Millisecond,
		})
	handler := api.Handler()

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
		`{"prompt":"plan the change","session_id":"conversation-plan-revision","client_request_id":"plan-request","runtime_id":"claude-code","model_route_id":"route-1","model_alias":"sonnet","interaction_mode":"plan"}`,
	)))
	if first.Code != http.StatusOK {
		t.Fatalf("initial plan response = %d %s", first.Code, first.Body.String())
	}
	messages, err := conversations.GetMessages(
		context.Background(), "conversation-plan-revision", auth.DevIdentity.UserID,
	)
	if err != nil || len(messages) != 2 || len(messages[1].Parts) != 1 {
		t.Fatalf("initial plan history = %+v, %v", messages, err)
	}
	initialPlan := messages[1].Parts[0]

	revision := httptest.NewRecorder()
	revisionBody := fmt.Sprintf(
		`{"prompt":"Please add browser tests","session_id":"conversation-plan-revision","client_request_id":"revision-request","runtime_id":"claude-code","model_route_id":"route-1","model_alias":"sonnet","interaction_mode":"plan","revision_of_plan_id":%q,"expected_plan_version":%d}`,
		initialPlan.PlanID,
		initialPlan.Version,
	)
	handler.ServeHTTP(revision, httptest.NewRequest(
		http.MethodPost, "/v1/chat", strings.NewReader(revisionBody),
	))
	if revision.Code != http.StatusOK || !strings.Contains(revision.Body.String(), `"kind":"plan_ready"`) {
		t.Fatalf("revision response = %d %s", revision.Code, revision.Body.String())
	}

	messages, err = conversations.GetMessages(
		context.Background(), "conversation-plan-revision", auth.DevIdentity.UserID,
	)
	if err != nil || len(messages) != 4 || len(messages[3].Parts) != 1 {
		t.Fatalf("revision history = %+v, %v", messages, err)
	}
	if messages[2].Role != "user" || len(messages[2].Parts) != 1 ||
		messages[2].Parts[0].Text != "Please add browser tests" {
		t.Fatalf("revision user turn = %+v", messages[2])
	}
	if messages[2].Metadata["revision_of_plan_id"] != initialPlan.PlanID ||
		messages[2].Metadata["expected_plan_version"] != initialPlan.Version {
		t.Fatalf("revision metadata = %#v", messages[2].Metadata)
	}
	if messages[1].Parts[0].Status != chatrun.PlanStatusSuperseded ||
		messages[3].Parts[0].Status != chatrun.PlanStatusReady ||
		messages[3].Parts[0].Version != 2 {
		t.Fatalf("versioned revision history = %+v", messages)
	}

	streamer.mu.Lock()
	queries := append([]agent.Query(nil), streamer.queries...)
	streamer.mu.Unlock()
	if len(queries) != 2 || queries[1].InteractionMode != agent.InteractionModePlan ||
		!strings.Contains(queries[1].Prompt, "Requested changes:\nPlease add browser tests") {
		t.Fatalf("revision query = %+v", queries)
	}
}

func (s *questionModeStreamer) Stream(
	_ context.Context,
	query agent.Query,
	onEvent func(agent.Event) error,
) error {
	s.mu.Lock()
	s.queries = append(s.queries, query)
	call := len(s.queries)
	s.mu.Unlock()
	if call == 1 {
		if err := onEvent(agent.Event{Kind: "question_required", Data: map[string]string{
			"question": "Which database?", "options": `["PostgreSQL","SQLite"]`,
		}}); err != nil {
			return err
		}
	} else {
		if s.answerStartError != nil {
			return s.answerStartError
		}
		if err := onEvent(agent.Event{Kind: "run_accepted"}); err != nil {
			return err
		}
		if err := onEvent(agent.Event{Kind: "text", Data: map[string]string{
			"text": "Continuing with PostgreSQL.",
		}}); err != nil {
			return err
		}
	}
	return onEvent(agent.Event{Kind: "done"})
}

func TestQuestionAnswerRestoresPendingWhenRuntimeRejectsStartup(t *testing.T) {
	streamer := &questionModeStreamer{answerStartError: errors.New("resume failed")}
	api, _, conversations := durableTestAPI(streamer)
	handler := api.Handler()

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
		`{"prompt":"create a service","session_id":"conversation-question-retry","client_request_id":"question-request","runtime_id":"claude-code","model_route_id":"route-1"}`,
	)))
	if first.Code != http.StatusOK {
		t.Fatalf("question response = %d %s", first.Code, first.Body.String())
	}
	messages, err := conversations.GetMessages(
		context.Background(), "conversation-question-retry", auth.DevIdentity.UserID,
	)
	if err != nil || len(messages) != 2 || len(messages[1].Parts) != 1 {
		t.Fatalf("question history = %+v, %v", messages, err)
	}
	question := messages[1].Parts[0]

	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, httptest.NewRequest(
		http.MethodPost,
		"/v1/conversations/conversation-question-retry/questions/"+question.QuestionID+"/answer",
		strings.NewReader(
			`{"expected_version":1,"answer":{"option_id":"option-1"},"client_request_id":"11111111-1111-4111-8111-111111111111"}`,
		),
	))
	if answer.Code != http.StatusOK ||
		!strings.Contains(answer.Body.String(), `"status":"pending"`) {
		t.Fatalf("rejected answer = %d %s", answer.Code, answer.Body.String())
	}
	messages, err = conversations.GetMessages(
		context.Background(), "conversation-question-retry", auth.DevIdentity.UserID,
	)
	if err != nil || messages[1].Parts[0].Status != chatrun.QuestionStatusPending ||
		messages[1].Parts[0].QuestionAnswer != nil {
		t.Fatalf("restored question history = %+v, %v", messages, err)
	}
}

type controlledFinalizeStore struct {
	chatrun.Store
	mu          sync.Mutex
	calls       int
	failAll     bool
	failMessage bool
}

func (s *controlledFinalizeStore) Finalize(ctx context.Context, in chatrun.FinalizeInput) (chatrun.FinalizeResult, error) {
	s.mu.Lock()
	s.calls++
	failAll := s.failAll
	failMessage := s.failMessage
	s.mu.Unlock()
	if failAll || (failMessage && in.AssistantMessage != nil) {
		return chatrun.FinalizeResult{}, errors.New("injected finalization failure")
	}
	return s.Store.Finalize(ctx, in)
}

func (s *controlledFinalizeStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *controlledFinalizeStore) setFailAll(fail bool) {
	s.mu.Lock()
	s.failAll = fail
	s.mu.Unlock()
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
			PingEvery:   time.Hour,
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
	if err != nil || len(messages) != 2 ||
		!strings.Contains(messages[1].Parts[0].Text, "partial") ||
		!strings.Contains(messages[1].Parts[0].Text, "Run was cancelled.") {
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

	activeRun, err := runs.Active(context.Background(), "conversation-1", auth.DevIdentity.UserID)
	if err != nil {
		t.Fatalf("active run lookup failed: %v", err)
	}
	runID := activeRun.ID
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
	store := &controlledFinalizeStore{Store: base, failMessage: true}
	api := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithChatRuns(store, RunConfig{FinalizeRetry: time.Microsecond})

	run, ok := api.finalizeRun(chatrun.FinalizeInput{
		RunID: "run-1", UserID: auth.DevIdentity.UserID, Status: chatrun.StatusSuccess,
		AssistantMessage: &convo.Message{
			ID: "run-1-assistant", ConversationID: "conversation-1", Role: "assistant",
		},
	})
	if !ok || run.Run.Status != chatrun.StatusInterrupted || run.Run.ErrorCode != "FINALIZATION_FAILED" {
		t.Fatalf("fallback result = %+v, %v", run, ok)
	}
	if got, want := store.callCount(), finalizeMaxAttempts+1; got != want {
		t.Fatalf("finalize calls = %d, want %d", got, want)
	}
}

func TestExecuteLiveRunRetainsStateUntilFinalizationRecovers(t *testing.T) {
	conversations := convo.NewMemory()
	base := chatrun.NewMemory(conversations)
	store := &controlledFinalizeStore{Store: base, failAll: true}
	api := New(
		&fakeStreamer{script: []agent.Event{
			{Kind: "text", Data: map[string]string{"text": "completed"}},
			{Kind: "done"},
		}},
		auth.NewVerifier(auth.Config{}),
		logger.Must(),
	).WithConvoStore(conversations).
		WithChatRuns(store, RunConfig{
			PingEvery: time.Hour, MergeWindow: time.Millisecond,
			DraftInterval: time.Millisecond, FinalizeRetry: time.Millisecond,
		})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
		`{"prompt":"hello","session_id":"conversation-finalize-recovery","client_request_id":"request-finalize-recovery"}`,
	))
	requestDone := make(chan struct{})
	go func() {
		api.Handler().ServeHTTP(recorder, request)
		close(requestDone)
	}()

	deadline := time.Now().Add(time.Second)
	for store.callCount() < finalizeMaxAttempts+2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := store.callCount(); got < finalizeMaxAttempts+2 {
		t.Fatalf("fallback finalization did not enter recovery: calls=%d", got)
	}

	api.runs.mu.Lock()
	liveCount := len(api.runs.live)
	api.runs.mu.Unlock()
	if liveCount != 1 {
		t.Fatalf("live run count during finalization recovery = %d, want 1", liveCount)
	}

	store.setFailAll(false)
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("chat request did not finish after finalization storage recovered")
	}

	api.runs.mu.Lock()
	liveCount = len(api.runs.live)
	api.runs.mu.Unlock()
	if liveCount != 0 {
		t.Fatalf("live run count after durable finalization = %d, want 0", liveCount)
	}
	runID := recorder.Header().Get("x-cocola-run-id")
	run, err := base.GetOwned(context.Background(), runID, auth.DevIdentity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != chatrun.StatusSuccess || run.ErrorCode != "" {
		t.Fatalf("recovered run = %+v, want successful original finalization", run)
	}
}

func TestDurableChatBusinessErrorCannotBecomeSuccess(t *testing.T) {
	streamer := &fakeStreamer{script: []agent.Event{
		{Kind: "text", Data: map[string]string{"text": "before error"}},
		{Kind: "error", Data: map[string]string{"error": "tool failed"}},
		{Kind: "done", Data: map[string]string{"reason": "complete"}},
	}}
	api, runs, conversations := durableTestAPI(streamer)
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
	if !strings.Contains(recorder.Body.String(), `"duration_ms":"`) {
		t.Fatalf("terminal SSE did not report duration: %s", recorder.Body.String())
	}
	messages, err := conversations.GetMessages(context.Background(), "conversation-1", auth.DevIdentity.UserID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("saved messages = %+v, %v", messages, err)
	}
	duration, ok := messages[1].Metadata["duration_ms"].(int64)
	if !ok || duration < 0 {
		t.Fatalf("assistant duration metadata = %#v", messages[1].Metadata["duration_ms"])
	}
	if !strings.Contains(recorder.Body.String(), `"duration_ms":"`+fmt.Sprint(duration)+`"`) {
		t.Fatalf("SSE and metadata durations differ: metadata=%d body=%s", duration, recorder.Body.String())
	}
}

func TestDurableChatEmptyAgentResponseBecomesError(t *testing.T) {
	api, runs, conversations := durableTestAPI(
		&fakeStreamer{script: []agent.Event{{Kind: "done"}}},
	)
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
		`{"prompt":"hello","session_id":"empty-agent-response","client_request_id":"empty-response-request"}`,
	)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("chat response = %d %s", recorder.Code, recorder.Body.String())
	}
	runID := recorder.Header().Get("x-cocola-run-id")
	run := waitForRunStatus(t, runs, runID, chatrun.StatusError)
	if run.ErrorCode != emptyAgentResponse {
		t.Fatalf("error code = %q, want %q", run.ErrorCode, emptyAgentResponse)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"EMPTY_AGENT_RESPONSE"`) ||
		!strings.Contains(recorder.Body.String(), "completed without returning an answer") {
		t.Fatalf("empty response error was not streamed: %s", recorder.Body.String())
	}
	messages, err := conversations.GetMessages(
		context.Background(), "empty-agent-response", auth.DevIdentity.UserID,
	)
	if err != nil || len(messages) != 2 || len(messages[1].Parts) != 1 ||
		!strings.Contains(messages[1].Parts[0].Text, "completed without returning an answer") {
		t.Fatalf("saved empty response error = %+v, %v", messages, err)
	}
}

func TestPlanModeCreatesAndExecutesDurablePlanWithoutUserMessage(t *testing.T) {
	streamer := &planModeStreamer{}
	conversations := convo.NewMemory()
	runs := chatrun.NewMemory(conversations)
	api := New(streamer, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(conversations).
		WithChatRuns(runs, RunConfig{
			PingEvery: time.Hour, MergeWindow: time.Millisecond, DraftInterval: time.Millisecond,
		})
	handler := api.Handler()

	planning := httptest.NewRecorder()
	handler.ServeHTTP(planning, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
		`{"prompt":"plan the change","session_id":"conversation-plan","client_request_id":"plan-request","runtime_id":"claude-code","model_route_id":"route-1","model_alias":"sonnet","reasoning_effort":"max","interaction_mode":"plan"}`,
	)))
	if planning.Code != http.StatusOK || !strings.Contains(planning.Body.String(), `"kind":"plan_ready"`) {
		t.Fatalf("plan response = %d %s", planning.Code, planning.Body.String())
	}
	messages, err := conversations.GetMessages(
		context.Background(), "conversation-plan", auth.DevIdentity.UserID,
	)
	if err != nil || len(messages) != 2 || len(messages[1].Parts) != 1 ||
		messages[1].Parts[0].Type != convo.PartPlan {
		t.Fatalf("planned messages = %+v, %v", messages, err)
	}
	plan := messages[1].Parts[0]
	if plan.Status != chatrun.PlanStatusReady || plan.Version != 1 {
		t.Fatalf("created plan part = %+v", plan)
	}

	execution := httptest.NewRecorder()
	handler.ServeHTTP(execution, httptest.NewRequest(
		http.MethodPost,
		"/v1/conversations/conversation-plan/plans/"+plan.PlanID+"/execute",
		strings.NewReader(
			`{"expected_version":1,"client_request_id":"11111111-1111-4111-8111-111111111111"}`,
		),
	))
	if execution.Code != http.StatusOK ||
		!strings.Contains(execution.Body.String(), `"status":"completed"`) {
		t.Fatalf("execution response = %d %s", execution.Code, execution.Body.String())
	}
	executionRunID := execution.Header().Get("x-cocola-run-id")
	replayedExecution := httptest.NewRecorder()
	handler.ServeHTTP(replayedExecution, httptest.NewRequest(
		http.MethodPost,
		"/v1/conversations/conversation-plan/plans/"+plan.PlanID+"/execute",
		strings.NewReader(
			`{"expected_version":1,"client_request_id":"11111111-1111-4111-8111-111111111111"}`,
		),
	))
	if replayedExecution.Code != http.StatusOK ||
		replayedExecution.Header().Get("x-cocola-run-id") != executionRunID {
		t.Fatalf(
			"idempotent execution response = %d run %q, want %q",
			replayedExecution.Code,
			replayedExecution.Header().Get("x-cocola-run-id"),
			executionRunID,
		)
	}
	streamer.mu.Lock()
	queries := append([]agent.Query(nil), streamer.queries...)
	streamer.mu.Unlock()
	if len(queries) != 2 || queries[0].InteractionMode != agent.InteractionModePlan ||
		queries[1].InteractionMode != agent.InteractionModeExecute ||
		queries[1].SessionID != queries[0].SessionID ||
		queries[1].ModelRouteID != "route-1" ||
		queries[0].ReasoningEffort != "max" || queries[1].ReasoningEffort != "max" ||
		!strings.Contains(queries[1].Prompt, `"content_markdown":"## Plan`) {
		t.Fatalf("plan queries = %+v", queries)
	}
	if !queries[1].RequireSessionResume {
		t.Fatal("approved Plan execution did not require resuming the planning Session")
	}
	promptParts := strings.SplitN(queries[1].Prompt, "\n\n", 2)
	var approvedPayload struct {
		PlanID          string `json:"plan_id"`
		Version         int    `json:"version"`
		ContentMarkdown string `json:"content_markdown"`
	}
	if len(promptParts) != 2 {
		t.Fatalf("approved Plan prompt does not contain a JSON payload: %q", queries[1].Prompt)
	}
	if err := json.Unmarshal([]byte(promptParts[1]), &approvedPayload); err != nil {
		t.Fatalf("decode approved Plan payload: %v", err)
	}
	if approvedPayload.PlanID != plan.PlanID ||
		approvedPayload.Version != plan.Version ||
		approvedPayload.ContentMarkdown != plan.PlanContentMarkdown {
		t.Fatalf("approved Plan payload = %+v, want %+v", approvedPayload, plan)
	}
	messages, err = conversations.GetMessages(
		context.Background(), "conversation-plan", auth.DevIdentity.UserID,
	)
	if err != nil || len(messages) != 3 {
		t.Fatalf("executed messages = %+v, %v", messages, err)
	}
	if messages[1].Parts[0].Status != chatrun.PlanStatusCompleted ||
		messages[2].Role != "assistant" {
		t.Fatalf("completed plan history = %+v", messages)
	}

	stalePlanMessage := messages[1]
	stalePlanMessage.Parts[0].Status = chatrun.PlanStatusReady
	if err := conversations.UpsertMessage(context.Background(), stalePlanMessage); err != nil {
		t.Fatal(err)
	}
	history := httptest.NewRecorder()
	handler.ServeHTTP(history, httptest.NewRequest(
		http.MethodGet,
		"/v1/conversations/conversation-plan/messages",
		nil,
	))
	if history.Code != http.StatusOK ||
		!strings.Contains(history.Body.String(), `"status":"completed"`) {
		t.Fatalf("authoritative plan history = %d %s", history.Code, history.Body.String())
	}
}

func TestQuestionModePersistsAnswerAndResumesTheSameSession(t *testing.T) {
	streamer := &questionModeStreamer{}
	api, _, conversations := durableTestAPI(streamer)
	handler := api.Handler()

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
		`{"prompt":"create a service","session_id":"conversation-question","client_request_id":"question-request","runtime_id":"claude-code","model_route_id":"route-1","model_alias":"sonnet","reasoning_effort":"high","skill_id":"backend","interaction_mode":"execute"}`,
	)))
	if first.Code != http.StatusOK ||
		!strings.Contains(first.Body.String(), `"kind":"question_ready"`) ||
		!strings.Contains(first.Body.String(), `"status":"waiting_input"`) {
		t.Fatalf("question response = %d %s", first.Code, first.Body.String())
	}
	messages, err := conversations.GetMessages(
		context.Background(), "conversation-question", auth.DevIdentity.UserID,
	)
	if err != nil || len(messages) != 2 || len(messages[1].Parts) != 1 ||
		messages[1].Parts[0].Type != convo.PartQuestion {
		t.Fatalf("question history = %+v, %v", messages, err)
	}
	question := messages[1].Parts[0]

	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
		`{"prompt":"another run","session_id":"conversation-question","client_request_id":"blocked-request"}`,
	)))
	if blocked.Code != http.StatusConflict ||
		!strings.Contains(blocked.Body.String(), `"code":"QUESTION_PENDING"`) {
		t.Fatalf("pending question did not block chat = %d %s", blocked.Code, blocked.Body.String())
	}

	answer := httptest.NewRecorder()
	answerPath := "/v1/conversations/conversation-question/questions/" + question.QuestionID + "/answer"
	answerBody := `{"expected_version":1,"answer":{"option_id":"option-1"},"client_request_id":"11111111-1111-4111-8111-111111111111"}`
	handler.ServeHTTP(answer, httptest.NewRequest(
		http.MethodPost, answerPath, strings.NewReader(answerBody),
	))
	if answer.Code != http.StatusOK ||
		!strings.Contains(answer.Body.String(), `"status":"answered"`) {
		t.Fatalf("question answer = %d %s", answer.Code, answer.Body.String())
	}
	answerRunID := answer.Header().Get("x-cocola-run-id")
	retry := httptest.NewRecorder()
	handler.ServeHTTP(retry, httptest.NewRequest(
		http.MethodPost, answerPath, strings.NewReader(answerBody),
	))
	if retry.Code != http.StatusOK ||
		retry.Header().Get("x-cocola-run-id") != answerRunID {
		t.Fatalf("idempotent answer = %d run %q, want %q",
			retry.Code, retry.Header().Get("x-cocola-run-id"), answerRunID)
	}

	streamer.mu.Lock()
	queries := append([]agent.Query(nil), streamer.queries...)
	streamer.mu.Unlock()
	if len(queries) != 2 || !queries[1].RequireSessionResume ||
		queries[1].SessionID != queries[0].SessionID ||
		queries[1].RuntimeID != queries[0].RuntimeID ||
		queries[1].ModelRouteID != queries[0].ModelRouteID ||
		queries[0].ReasoningEffort != "high" || queries[1].ReasoningEffort != "high" ||
		queries[1].SkillID != "backend" ||
		queries[1].InteractionMode != queries[0].InteractionMode {
		t.Fatalf("question continuation queries = %+v", queries)
	}
	messages, err = conversations.GetMessages(
		context.Background(), "conversation-question", auth.DevIdentity.UserID,
	)
	if err != nil || len(messages) != 4 || messages[2].Role != "user" ||
		messages[2].Parts[0].Text != "PostgreSQL" ||
		messages[1].Parts[0].Status != chatrun.QuestionStatusAnswered {
		t.Fatalf("answered question history = %+v, %v", messages, err)
	}
}

func TestQuestionAnswerRejectsUnavailableRuntimeBeforeStartingRun(t *testing.T) {
	streamer := &questionModeStreamer{}
	api, _, conversations := durableTestAPI(streamer)
	handler := api.Handler()

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
		`{"prompt":"create a service","session_id":"conversation-question-runtime","client_request_id":"question-runtime-request","runtime_id":"claude-code","model_route_id":"route-1"}`,
	)))
	if first.Code != http.StatusOK {
		t.Fatalf("question response = %d %s", first.Code, first.Body.String())
	}
	messages, err := conversations.GetMessages(
		context.Background(), "conversation-question-runtime", auth.DevIdentity.UserID,
	)
	if err != nil || len(messages) != 2 || len(messages[1].Parts) != 1 {
		t.Fatalf("question history = %+v, %v", messages, err)
	}
	question := messages[1].Parts[0]
	api.WithAgentRuntimes(nil)

	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, httptest.NewRequest(
		http.MethodPost,
		"/v1/conversations/conversation-question-runtime/questions/"+question.QuestionID+"/answer",
		strings.NewReader(
			`{"expected_version":1,"answer":{"option_id":"option-1"},"client_request_id":"11111111-1111-4111-8111-111111111111"}`,
		),
	))
	if answer.Code != http.StatusConflict ||
		!strings.Contains(answer.Body.String(), `"code":"QUESTION_RUNTIME_UNAVAILABLE"`) {
		t.Fatalf("unavailable runtime answer = %d %s", answer.Code, answer.Body.String())
	}
	streamer.mu.Lock()
	queryCount := len(streamer.queries)
	streamer.mu.Unlock()
	if queryCount != 1 {
		t.Fatalf("runtime query count = %d, want 1", queryCount)
	}
}

func TestPlanApprovalSerializesWorkspaceValidationWithNormalRunStart(t *testing.T) {
	const (
		projectID      = "11111111-1111-4111-8111-111111111111"
		conversationID = "conversation-plan-workspace"
	)
	streamer := newBlockingPlanWorkspaceStreamer()
	conversations := convo.NewMemory()
	runs := chatrun.NewMemory(conversations)
	projectStore := &planWorkspaceStore{
		project: project.Project{
			ID: projectID, Status: project.ProjectReady, RepositoryProvider: project.ProviderLocal,
			DefaultBranch: "main", RuntimeID: "claude-code",
		},
		workspace: project.Workspace{
			ConversationID: conversationID, ProjectID: projectID, BaseRef: "main",
		},
	}
	projectService, err := project.New(projectStore, project.Config{
		MaxRepositoryMB: 512, DisableGitHubConnector: true, DisableGitHubAgentWrite: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	api := New(streamer, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(conversations).
		WithProjects(projectService).
		WithChatRuns(runs, RunConfig{
			PingEvery: time.Hour, MergeWindow: time.Millisecond, DraftInterval: time.Millisecond,
		})
	handler := api.Handler()

	planning := httptest.NewRecorder()
	handler.ServeHTTP(planning, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
		`{"prompt":"plan the change","session_id":"`+conversationID+`","project_id":"`+
			projectID+`","runtime_id":"claude-code","model_route_id":"route-1","interaction_mode":"plan"}`,
	)))
	if planning.Code != http.StatusOK {
		t.Fatalf("plan response = %d %s", planning.Code, planning.Body.String())
	}
	messages, err := conversations.GetMessages(context.Background(), conversationID, auth.DevIdentity.UserID)
	if err != nil || len(messages) != 2 || len(messages[1].Parts) != 1 {
		t.Fatalf("planned messages = %+v, %v", messages, err)
	}
	plan := messages[1].Parts[0]

	approvalDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(
			http.MethodPost,
			"/v1/conversations/"+conversationID+"/plans/"+plan.PlanID+"/execute",
			strings.NewReader(
				`{"expected_version":1,"client_request_id":"22222222-2222-4222-8222-222222222222"}`,
			),
		))
		approvalDone <- recorder
	}()
	select {
	case <-streamer.inspectionStart:
	case <-time.After(time.Second):
		t.Fatal("plan workspace validation did not start")
	}

	ordinaryDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(
			`{"prompt":"ordinary change","session_id":"`+conversationID+`","project_id":"`+
				projectID+`","runtime_id":"claude-code"}`,
		)))
		ordinaryDone <- recorder
	}()

	startedBeforeValidation := false
	select {
	case <-streamer.ordinaryStart:
		startedBeforeValidation = true
	case <-time.After(100 * time.Millisecond):
	}
	if startedBeforeValidation {
		t.Fatal("normal run started while approved Plan workspace validation was still in progress")
	}
	close(streamer.releaseInspect)
	var ordinary *httptest.ResponseRecorder
	select {
	case ordinary = <-ordinaryDone:
	case <-time.After(time.Second):
		close(streamer.releaseExecute)
		t.Fatal("normal run did not resolve while approved Plan execution was active")
	}
	close(streamer.releaseExecute)
	approval := <-approvalDone
	if approval.Code != http.StatusOK {
		t.Fatalf("approval response = %d %s", approval.Code, approval.Body.String())
	}
	if ordinary.Code != http.StatusConflict ||
		!strings.Contains(ordinary.Body.String(), `"code":"RUN_IN_PROGRESS"`) {
		t.Fatalf("ordinary response = %d %s", ordinary.Code, ordinary.Body.String())
	}
}

func TestPlanModeRejectsCodexAndScheduledTasks(t *testing.T) {
	streamer := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	api, _, _ := durableTestAPI(streamer)
	api.WithAgentRuntimes([]agent.Runtime{
		{ID: "claude-code", Label: "Claude Code", ModelProtocol: "anthropic-messages", IsDefault: true},
		{ID: "codex", Label: "Codex", ModelProtocol: "openai-responses"},
	})
	handler := api.Handler()
	for name, body := range map[string]string{
		"codex":     `{"prompt":"plan","session_id":"codex-plan","runtime_id":"codex","interaction_mode":"plan"}`,
		"scheduled": `{"prompt":"plan","session_id":"scheduled-plan","conversation_type":"scheduled_task","interaction_mode":"plan"}`,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(body)))
			if recorder.Code != http.StatusConflict {
				t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestTerminalRunDataOmitsInvalidDuration(t *testing.T) {
	startedAt := time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(118 * time.Second)
	data := terminalRunData(chatrun.Run{
		Status: chatrun.StatusSuccess, StartedAt: startedAt, CompletedAt: &completedAt,
	})
	if data["duration_ms"] != "118000" {
		t.Fatalf("duration_ms = %q, want 118000", data["duration_ms"])
	}

	invalid := startedAt.Add(-time.Second)
	data = terminalRunData(chatrun.Run{
		Status: chatrun.StatusError, StartedAt: startedAt, CompletedAt: &invalid,
	})
	if _, exists := data["duration_ms"]; exists {
		t.Fatalf("invalid duration was included: %+v", data)
	}
}

func TestReconnectSnapshotDoesNotDuplicateBufferedText(t *testing.T) {
	streamer := newBlockingChatStreamer()
	conversations := convo.NewMemory()
	runs := chatrun.NewMemory(conversations)
	api := New(streamer, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithConvoStore(conversations).
		WithChatRuns(runs, RunConfig{
			PingEvery:   time.Hour,
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
	if !strings.Contains(replay.Body.String(), `"duration_ms":"`) {
		t.Fatalf("stored replay did not report duration: %s", replay.Body.String())
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
