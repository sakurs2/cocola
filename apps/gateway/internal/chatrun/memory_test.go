package chatrun

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
)

func testStartInput(runID, requestID, userID, conversationID string) StartInput {
	now := time.Now().UTC()
	return StartInput{
		Run: Run{
			ID: runID, ConversationID: conversationID, UserID: userID,
			Source: "interactive", ClientRequestID: requestID, Status: StatusRunning,
			StartedAt: now, LastActivityAt: now,
		},
		Conversation: convo.Conversation{
			ID: conversationID, UserID: userID, TenantID: "tenant",
			Title: "hello", ChatType: "chat", CreatedAt: now, UpdatedAt: now,
		},
		UserMessage: convo.Message{
			ID: runID + "-user", ConversationID: conversationID, Role: "user",
			Parts: []convo.Part{{Type: convo.PartText, Text: "hello"}}, CreatedAt: now,
		},
	}
}

func TestMemoryStartIsIdempotentAndSingleFlight(t *testing.T) {
	ctx := context.Background()
	conversations := convo.NewMemory()
	store := NewMemory(conversations)

	first, err := store.Start(ctx, testStartInput("run-1", "request-1", "user-1", "conversation-1"))
	if err != nil || !first.Created {
		t.Fatalf("first start = %+v, %v", first, err)
	}
	retry, err := store.Start(ctx, testStartInput("run-2", "request-1", "user-1", "conversation-1"))
	if err != nil || retry.Created || retry.Run.ID != first.Run.ID {
		t.Fatalf("idempotent retry = %+v, %v", retry, err)
	}
	conflict, err := store.Start(ctx, testStartInput("run-3", "request-2", "user-1", "conversation-1"))
	if !errors.Is(err, ErrConflict) || conflict.Run.ID != first.Run.ID {
		t.Fatalf("second request = %+v, %v", conflict, err)
	}
	messages, err := conversations.GetMessages(ctx, "conversation-1", "user-1")
	if err != nil || len(messages) != 1 || messages[0].ID != "run-1-user" {
		t.Fatalf("messages after retries = %+v, %v", messages, err)
	}
}

func TestMemoryStartDoesNotCrossConversationOwner(t *testing.T) {
	ctx := context.Background()
	conversations := convo.NewMemory()
	store := NewMemory(conversations)
	if _, err := store.Start(ctx, testStartInput("run-1", "request-1", "user-1", "shared-id")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Finalize(ctx, FinalizeInput{RunID: "run-1", UserID: "user-1", Status: StatusSuccess}); err != nil {
		t.Fatal(err)
	}
	_, err := store.Start(ctx, testStartInput("run-2", "request-2", "user-2", "shared-id"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner start error = %v, want not found", err)
	}
	if _, err := conversations.GetMessages(ctx, "shared-id", "user-2"); !errors.Is(err, convo.ErrNotFound) {
		t.Fatalf("cross-owner messages error = %v, want not found", err)
	}
}

func TestMemoryStartRejectsRuntimeChangeWithoutWrites(t *testing.T) {
	ctx := context.Background()
	conversations := convo.NewMemory()
	store := NewMemory(conversations)
	first := testStartInput("run-1", "request-1", "user-1", "conversation-1")
	first.Conversation.RuntimeID = "codex"
	if _, err := store.Start(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Finalize(ctx, FinalizeInput{
		RunID: "run-1", UserID: "user-1", Status: StatusSuccess,
	}); err != nil {
		t.Fatal(err)
	}

	mismatch := testStartInput("run-2", "request-2", "user-1", "conversation-1")
	mismatch.Conversation.RuntimeID = "claude-code"
	if _, err := store.Start(ctx, mismatch); !errors.Is(err, ErrRuntimeMismatch) {
		t.Fatalf("runtime change error = %v, want runtime mismatch", err)
	}
	messages, err := conversations.GetMessages(ctx, "conversation-1", "user-1")
	if err != nil || len(messages) != 1 || messages[0].ID != "run-1-user" {
		t.Fatalf("messages after runtime mismatch = %+v, %v", messages, err)
	}
	if _, err := store.GetOwned(ctx, "run-2", "user-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mismatched run exists: %v", err)
	}
}

func TestMemoryStartBindsFolderAndRejectsFolderChange(t *testing.T) {
	ctx := context.Background()
	conversations := convo.NewMemory()
	store := NewMemory(conversations)
	now := time.Now().UTC()
	for _, folder := range []convo.Folder{
		{ID: "folder-1", UserID: "user-1", Name: "One", CreatedAt: now, UpdatedAt: now},
		{ID: "folder-2", UserID: "user-1", Name: "Two", CreatedAt: now, UpdatedAt: now},
	} {
		if _, err := conversations.CreateFolder(ctx, folder); err != nil {
			t.Fatal(err)
		}
	}

	first := testStartInput("run-1", "request-1", "user-1", "conversation-1")
	first.Conversation.FolderID = "folder-1"
	result, err := store.Start(ctx, first)
	if err != nil || result.Conversation.FolderID != "folder-1" {
		t.Fatalf("folder start = %+v, %v", result, err)
	}
	if _, err := store.Finalize(ctx, FinalizeInput{
		RunID: "run-1", UserID: "user-1", Status: StatusSuccess,
	}); err != nil {
		t.Fatal(err)
	}

	// A transport retry is authoritative by request id even when an old client
	// accidentally repeats a stale folder hint.
	retry := testStartInput("run-retry", "request-1", "user-1", "conversation-1")
	retry.Conversation.FolderID = "folder-2"
	retried, err := store.Start(ctx, retry)
	if err != nil || retried.Created || retried.Run.ID != "run-1" {
		t.Fatalf("idempotent folder retry = %+v, %v", retried, err)
	}

	mismatch := testStartInput("run-2", "request-2", "user-1", "conversation-1")
	mismatch.Conversation.FolderID = "folder-2"
	if _, err := store.Start(ctx, mismatch); !errors.Is(err, ErrFolderMismatch) {
		t.Fatalf("folder change = %v, want ErrFolderMismatch", err)
	}
	messages, err := conversations.GetMessages(ctx, "conversation-1", "user-1")
	if err != nil || len(messages) != 1 {
		t.Fatalf("messages after folder mismatch = %+v, %v", messages, err)
	}

	missing := testStartInput("run-missing", "request-missing", "user-1", "conversation-missing")
	missing.Conversation.FolderID = "missing"
	if _, err := store.Start(ctx, missing); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("missing folder = %v, want ErrFolderNotFound", err)
	}
	if _, err := conversations.GetConversation(ctx, "conversation-missing", "user-1"); !errors.Is(err, convo.ErrNotFound) {
		t.Fatalf("invalid folder created conversation: %v", err)
	}
}

func TestMemoryDraftAndTerminalStateAreStable(t *testing.T) {
	ctx := context.Background()
	conversations := convo.NewMemory()
	store := NewMemory(conversations)
	if _, err := store.Start(ctx, testStartInput("run-1", "request-1", "user-1", "conversation-1")); err != nil {
		t.Fatal(err)
	}
	draft := convo.Message{
		ID: "run-1-assistant", ConversationID: "conversation-1", Role: "assistant",
		Parts:    []convo.Part{{Type: convo.PartText, Text: "partial"}},
		Metadata: map[string]any{"partial": true}, CreatedAt: time.Now().UTC(),
	}
	if err := store.SaveDraft(ctx, "run-1", "user-1", draft); err != nil {
		t.Fatal(err)
	}
	final := draft
	final.Parts = []convo.Part{{Type: convo.PartText, Text: "complete"}}
	final.Metadata = map[string]any{"partial": false}
	final.CreatedAt = draft.CreatedAt.Add(time.Second)
	run, err := store.Finalize(ctx, FinalizeInput{
		RunID: "run-1", UserID: "user-1", Status: StatusSuccess, AssistantMessage: &final,
	})
	if err != nil || run.Run.Status != StatusSuccess {
		t.Fatalf("finalize = %+v, %v", run, err)
	}
	run, err = store.Finalize(ctx, FinalizeInput{
		RunID: "run-1", UserID: "user-1", Status: StatusError, ErrorCode: "LATE_ERROR",
	})
	if err != nil || run.Run.Status != StatusSuccess || run.Run.ErrorCode != "" {
		t.Fatalf("late terminal overwrite = %+v, %v", run, err)
	}
	messages, err := conversations.GetMessages(ctx, "conversation-1", "user-1")
	if err != nil || len(messages) != 2 || messages[1].Parts[0].Text != "complete" ||
		!messages[1].CreatedAt.Equal(final.CreatedAt) {
		t.Fatalf("final messages = %+v, %v", messages, err)
	}
}

func TestMemoryPlanExecutionStopsAndContinuesWithoutUserMessage(t *testing.T) {
	ctx := context.Background()
	conversations := convo.NewMemory()
	store := NewMemory(conversations)
	planning := testStartInput("plan-run-1", "plan-request-1", "user-1", "conversation-1")
	planning.Run.InteractionMode = InteractionModePlan
	planning.Run.ModelRouteID = "route-1"
	planning.Run.ModelAlias = "sonnet"
	if _, err := store.Start(ctx, planning); err != nil {
		t.Fatal(err)
	}
	created, err := store.Finalize(ctx, FinalizeInput{
		RunID: "plan-run-1", UserID: "user-1", Status: StatusSuccess,
		PlanCandidate: &PlanCandidate{
			ID: "11111111-1111-4111-8111-111111111111", RuntimeID: "claude-code",
			ModelRouteID: "route-1", ModelAlias: "sonnet",
			ContentMarkdown: "## Plan\n\n- Implement",
		},
	})
	if err != nil || created.Plan == nil || created.Plan.Version != 1 ||
		created.Plan.Status != PlanStatusReady {
		t.Fatalf("created plan = %+v, %v", created, err)
	}

	executionStartedAt := time.Now().UTC()
	store.unavailableModels["route-1"] = true
	if _, err := store.StartPlanExecution(ctx, PlanExecutionInput{
		Run: Run{
			ID: "unavailable-model-run", RootSpanID: "unavailable-model-span",
			ClientRequestID: "unavailable-model-request", Status: StatusRunning,
			StartedAt: executionStartedAt, LastActivityAt: executionStartedAt,
		},
		ConversationID: "conversation-1", UserID: "user-1",
		ExpectedVersion: 1, PlanID: created.Plan.ID, ApprovedAt: executionStartedAt,
	}); !errors.Is(err, ErrPlanModelUnavailable) {
		t.Fatalf("unavailable model execution error = %v, want ErrPlanModelUnavailable", err)
	}
	delete(store.unavailableModels, "route-1")

	started, err := store.StartPlanExecution(ctx, PlanExecutionInput{
		Run: Run{
			ID: "execute-run-1", RootSpanID: "span-1", ClientRequestID: "execute-request-1",
			Status: StatusRunning, StartedAt: executionStartedAt, LastActivityAt: executionStartedAt,
		},
		ConversationID: "conversation-1", UserID: "user-1",
		ExpectedVersion: 1, PlanID: created.Plan.ID, ApprovedAt: executionStartedAt,
	})
	if err != nil || !started.Created || started.Plan.Status != PlanStatusExecuting ||
		started.Run.PlanID != created.Plan.ID {
		t.Fatalf("execution start = %+v, %v", started, err)
	}
	stopped, err := store.Finalize(ctx, FinalizeInput{
		RunID: "execute-run-1", UserID: "user-1", Status: StatusInterrupted,
	})
	if err != nil || stopped.Plan == nil || stopped.Plan.Status != PlanStatusStopped {
		t.Fatalf("stopped execution = %+v, %v", stopped, err)
	}

	continuedAt := executionStartedAt.Add(time.Second)
	continued, err := store.StartPlanExecution(ctx, PlanExecutionInput{
		Run: Run{
			ID: "execute-run-2", RootSpanID: "span-2", ClientRequestID: "execute-request-2",
			Status: StatusRunning, StartedAt: continuedAt, LastActivityAt: continuedAt,
		},
		ConversationID: "conversation-1", UserID: "user-1",
		ExpectedVersion: 1, PlanID: created.Plan.ID, ApprovedAt: continuedAt,
	})
	if err != nil || continued.Plan.Status != PlanStatusExecuting {
		t.Fatalf("continued execution = %+v, %v", continued, err)
	}
	completed, err := store.Finalize(ctx, FinalizeInput{
		RunID: "execute-run-2", UserID: "user-1", Status: StatusSuccess,
	})
	if err != nil || completed.Plan == nil || completed.Plan.Status != PlanStatusCompleted {
		t.Fatalf("completed execution = %+v, %v", completed, err)
	}

	messages, err := conversations.GetMessages(ctx, "conversation-1", "user-1")
	if err != nil || len(messages) != 2 || messages[1].Parts[0].Status != PlanStatusCompleted {
		t.Fatalf("plan messages = %+v, %v", messages, err)
	}
}

func TestMemoryNewPlanSupersedesOldVersion(t *testing.T) {
	ctx := context.Background()
	conversations := convo.NewMemory()
	store := NewMemory(conversations)
	createPlan := func(runID, requestID, planID string) FinalizeResult {
		t.Helper()
		input := testStartInput(runID, requestID, "user-1", "conversation-1")
		input.Run.InteractionMode = InteractionModePlan
		if _, err := store.Start(ctx, input); err != nil {
			t.Fatal(err)
		}
		result, err := store.Finalize(ctx, FinalizeInput{
			RunID: runID, UserID: "user-1", Status: StatusSuccess,
			PlanCandidate: &PlanCandidate{
				ID: planID, RuntimeID: "claude-code", ContentMarkdown: "## Plan\n\n- Review",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	first := createPlan(
		"plan-run-1", "plan-request-1", "11111111-1111-4111-8111-111111111111",
	)
	second := createPlan(
		"plan-run-2", "plan-request-2", "22222222-2222-4222-8222-222222222222",
	)
	if first.Plan == nil || second.Plan == nil || second.Plan.Version != 2 ||
		second.SupersededPlanID != first.Plan.ID {
		t.Fatalf("versioned plans = first %+v, second %+v", first, second)
	}
	old, err := store.GetPlan(ctx, "conversation-1", first.Plan.ID, "user-1")
	if err != nil || old.Status != PlanStatusSuperseded {
		t.Fatalf("old plan = %+v, %v", old, err)
	}
	if _, err := store.StartPlanExecution(ctx, PlanExecutionInput{
		Run: Run{
			ID: "execute-old", ClientRequestID: "execute-old-request", Status: StatusRunning,
			StartedAt: time.Now().UTC(), LastActivityAt: time.Now().UTC(),
		},
		ConversationID: "conversation-1", UserID: "user-1",
		ExpectedVersion: 1, PlanID: first.Plan.ID,
	}); !errors.Is(err, ErrPlanNotCurrent) {
		t.Fatalf("old plan execution error = %v, want ErrPlanNotCurrent", err)
	}
	cancelled, err := store.CancelPlan(
		ctx, "conversation-1", second.Plan.ID, "user-1", 2, time.Now().UTC(),
	)
	if err != nil || cancelled.Status != PlanStatusCancelled {
		t.Fatalf("cancelled plan = %+v, %v", cancelled, err)
	}
}
