package httpapi

import (
	"context"
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
	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/apps/gateway/internal/memory"
	"github.com/cocola-project/cocola/apps/gateway/internal/project"
	"github.com/cocola-project/cocola/packages/go-common/logger"
)

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
	if call == 1 {
		if err := onEvent(agent.Event{Kind: "plan_ready", Data: map[string]string{
			"content_markdown": "## Plan\n\n- Implement the change",
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

type controlledFinalizeStore struct {
	chatrun.Store
	mu      sync.Mutex
	calls   int
	failAll bool
}

func (s *controlledFinalizeStore) Finalize(ctx context.Context, in chatrun.FinalizeInput) (chatrun.FinalizeResult, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.failAll || in.AssistantMessage != nil {
		return chatrun.FinalizeResult{}, errors.New("injected finalization failure")
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
	store := &controlledFinalizeStore{Store: base}
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
		`{"prompt":"plan the change","session_id":"conversation-plan","client_request_id":"plan-request","runtime_id":"claude-code","model_route_id":"route-1","model_alias":"sonnet","interaction_mode":"plan"}`,
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
	if plan.Status != chatrun.PlanStatusReady || plan.PlanVersion != 1 {
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
		!strings.Contains(queries[1].Prompt, "<approved_cocola_plan>") {
		t.Fatalf("plan queries = %+v", queries)
	}
	if !queries[1].RequireSessionResume {
		t.Fatal("approved Plan execution did not require resuming the planning Session")
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
	close(streamer.releaseInspect)
	close(streamer.releaseExecute)
	approval := <-approvalDone
	ordinary := <-ordinaryDone

	if startedBeforeValidation {
		t.Fatal("normal run started while approved Plan workspace validation was still in progress")
	}
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
