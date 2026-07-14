package convo

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// TestPostgresParity runs the core lifecycle against a real Postgres, gated on
// COCOLA_TEST_PG_DSN (unset => skip, so `go test ./...` stays zero-dependency).
//
//	docker run --rm -d --name pgtest -e POSTGRES_USER=cocola \
//	  -e POSTGRES_PASSWORD=cocola_dev_pw -e POSTGRES_DB=cocola -p 5432:5432 \
//	  postgres:16-alpine
//	COCOLA_TEST_PG_DSN='postgres://cocola:cocola_dev_pw@localhost:5432/cocola?sslmode=disable' \
//	  go test ./internal/convo/ -run Parity -v
func TestPostgresParity(t *testing.T) {
	dsn := os.Getenv("COCOLA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("COCOLA_TEST_PG_DSN not set; skipping Postgres parity leg")
	}
	ctx := context.Background()
	if err := Migrate(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pg, err := NewPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pg.Close()
	// Clean slate.
	if _, err := pg.pool.Exec(ctx, "TRUNCATE messages, conversations, conversation_folders CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	t0 := time.Now().UTC().Truncate(time.Second)
	must(t, pg.UpsertConversation(ctx, Conversation{ID: "a", UserID: "u1", Title: "first", CreatedAt: t0, UpdatedAt: t0}))
	// Title preserved on conflict; updated_at refreshed.
	must(t, pg.UpsertConversation(ctx, Conversation{ID: "a", UserID: "u1", Title: "CHANGED", CreatedAt: t0, UpdatedAt: t0.Add(time.Minute)}))
	must(t, pg.InsertMessage(ctx, Message{ID: "turn-user", ConversationID: "a", Role: "user", Parts: []Part{{Type: PartText, Text: "hi"}}, CreatedAt: t0}))
	must(t, pg.InsertMessage(ctx, Message{ID: "turn-assistant", ConversationID: "a", Role: "assistant", Parts: []Part{
		{Type: PartToolCall, ToolCallID: "t1", ToolName: "bash", ArgsText: "{}", Result: str("ok"), IsError: false},
	}, CreatedAt: t0}))

	list, err := pg.ListConversations(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Title != "first" {
		t.Fatalf("title should be preserved, got %+v", list)
	}
	if !list[0].UpdatedAt.After(t0) {
		t.Fatalf("updated_at should have refreshed")
	}

	msgs, err := pg.GetMessages(ctx, "a", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" ||
		msgs[1].Parts[0].Result == nil || *msgs[1].Parts[0].Result != "ok" {
		t.Fatalf("bad messages roundtrip: %+v", msgs)
	}

	// Ownership gate on the real backend.
	if _, err := pg.GetMessages(ctx, "a", "intruder"); err != ErrNotFound {
		t.Fatalf("non-owner should get ErrNotFound, got %v", err)
	}

	folder, err := pg.CreateFolder(ctx, Folder{
		ID: "folder-a", UserID: "u1", Name: "  Research  ", CreatedAt: t0, UpdatedAt: t0,
	})
	if err != nil || folder.Name != "Research" {
		t.Fatalf("folder create = %+v, %v", folder, err)
	}
	if _, err := pg.CreateFolder(ctx, Folder{
		ID: "folder-b", UserID: "u1", Name: "research", CreatedAt: t0, UpdatedAt: t0,
	}); !errors.Is(err, ErrFolderNameConflict) {
		t.Fatalf("case-insensitive duplicate = %v, want ErrFolderNameConflict", err)
	}
	moved, err := pg.MoveConversation(ctx, "a", "u1", folder.ID, t0.Add(2*time.Minute))
	if err != nil || moved.FolderID != folder.ID {
		t.Fatalf("folder move = %+v, %v", moved, err)
	}
	deleted, err := pg.DeleteFolder(ctx, folder.ID, "u1")
	if err != nil || len(deleted) != 1 || deleted[0] != "a" {
		t.Fatalf("folder delete = %v, %v", deleted, err)
	}
	if _, err := pg.GetConversation(ctx, "a", "u1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("folder cascade left conversation: %v", err)
	}
}
