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
	if err != nil || run.Status != StatusSuccess {
		t.Fatalf("finalize = %+v, %v", run, err)
	}
	run, err = store.Finalize(ctx, FinalizeInput{
		RunID: "run-1", UserID: "user-1", Status: StatusError, ErrorCode: "LATE_ERROR",
	})
	if err != nil || run.Status != StatusSuccess || run.ErrorCode != "" {
		t.Fatalf("late terminal overwrite = %+v, %v", run, err)
	}
	messages, err := conversations.GetMessages(ctx, "conversation-1", "user-1")
	if err != nil || len(messages) != 2 || messages[1].Parts[0].Text != "complete" ||
		!messages[1].CreatedAt.Equal(final.CreatedAt) {
		t.Fatalf("final messages = %+v, %v", messages, err)
	}
}
