package chatrun

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
)

func TestPostgresStartFinalizeParity(t *testing.T) {
	dsn := os.Getenv("COCOLA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("COCOLA_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	store, err := NewPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	conversationID := "run-test-" + uuid.NewString()
	runID := uuid.NewString()
	defer func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM conversation_runs WHERE conversation_id=$1`, conversationID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM conversations WHERE id=$1`, conversationID)
	}()

	input := testStartInput(runID, "request-1", "user-1", conversationID)
	started, err := store.Start(ctx, input)
	if err != nil || !started.Created {
		t.Fatalf("start = %+v, %v", started, err)
	}
	retryInput := testStartInput(uuid.NewString(), "request-1", "user-1", conversationID)
	retry, err := store.Start(ctx, retryInput)
	if err != nil || retry.Created || retry.Run.ID != runID {
		t.Fatalf("retry = %+v, %v", retry, err)
	}
	conflictInput := testStartInput(uuid.NewString(), "request-2", "user-1", conversationID)
	conflict, err := store.Start(ctx, conflictInput)
	if !errors.Is(err, ErrConflict) || conflict.Run.ID != runID {
		t.Fatalf("conflict = %+v, %v", conflict, err)
	}
	otherOwner := testStartInput(uuid.NewString(), "request-3", "user-2", conversationID)
	if _, err := store.Start(ctx, otherOwner); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner start error = %v", err)
	}

	draft := convo.Message{
		ID: runID + "-assistant", ConversationID: conversationID, Role: "assistant",
		Parts:    []convo.Part{{Type: convo.PartText, Text: "partial"}},
		Metadata: map[string]any{"partial": true}, CreatedAt: time.Now().UTC(),
	}
	if err := store.SaveDraft(ctx, runID, "user-1", draft); err != nil {
		t.Fatal(err)
	}
	final := draft
	final.Parts = []convo.Part{{Type: convo.PartText, Text: "complete"}}
	final.Metadata = map[string]any{"partial": false}
	final.CreatedAt = draft.CreatedAt.Add(time.Second)
	run, err := store.Finalize(ctx, FinalizeInput{
		RunID: runID, UserID: "user-1", Status: StatusSuccess, AssistantMessage: &final,
	})
	if err != nil || run.Status != StatusSuccess {
		t.Fatalf("finalize = %+v, %v", run, err)
	}
	run, err = store.Finalize(ctx, FinalizeInput{
		RunID: runID, UserID: "user-1", Status: StatusError, ErrorCode: "LATE_ERROR",
	})
	if err != nil || run.Status != StatusSuccess || run.ErrorCode != "" {
		t.Fatalf("late terminal overwrite = %+v, %v", run, err)
	}
	var assistantCreatedAt time.Time
	err = store.pool.QueryRow(ctx, `SELECT created_at FROM messages WHERE id=$1`, final.ID).
		Scan(&assistantCreatedAt)
	expectedCreatedAt := final.CreatedAt.Truncate(time.Microsecond)
	if err != nil || !assistantCreatedAt.Equal(expectedCreatedAt) {
		t.Fatalf("final assistant timestamp = %s, %v; want %s", assistantCreatedAt, err, expectedCreatedAt)
	}
}

func TestPostgresConcurrentIdempotentStart(t *testing.T) {
	dsn := os.Getenv("COCOLA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("COCOLA_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	store, err := NewPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	conversationID := "run-concurrent-test-" + uuid.NewString()
	defer func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM conversation_runs WHERE conversation_id=$1`, conversationID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM conversations WHERE id=$1`, conversationID)
	}()

	start := make(chan struct{})
	results := make(chan StartResult, 2)
	errorsOut := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, startErr := store.Start(ctx, testStartInput(
				uuid.NewString(), "same-request", "user-1", conversationID,
			))
			results <- result
			errorsOut <- startErr
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errorsOut)

	for startErr := range errorsOut {
		if startErr != nil {
			t.Fatalf("concurrent start error = %v", startErr)
		}
	}
	var runID string
	created := 0
	for result := range results {
		if runID == "" {
			runID = result.Run.ID
		} else if result.Run.ID != runID {
			t.Fatalf("concurrent starts returned different runs: %q and %q", runID, result.Run.ID)
		}
		if result.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created count = %d, want 1", created)
	}
}
