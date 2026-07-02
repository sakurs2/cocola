package convo

import (
	"context"
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
	if _, err := pg.pool.Exec(ctx, "TRUNCATE messages, conversations"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	t0 := time.Now().UTC().Truncate(time.Second)
	must(t, pg.UpsertConversation(ctx, Conversation{ID: "a", UserID: "u1", Title: "first", CreatedAt: t0, UpdatedAt: t0}))
	// Title preserved on conflict; updated_at refreshed.
	must(t, pg.UpsertConversation(ctx, Conversation{ID: "a", UserID: "u1", Title: "CHANGED", CreatedAt: t0, UpdatedAt: t0.Add(time.Minute)}))
	must(t, pg.InsertMessage(ctx, Message{ID: "m1", ConversationID: "a", Role: "user", Parts: []Part{{Type: PartText, Text: "hi"}}, CreatedAt: t0}))
	must(t, pg.InsertMessage(ctx, Message{ID: "m2", ConversationID: "a", Role: "assistant", Parts: []Part{
		{Type: PartToolCall, ToolCallID: "t1", ToolName: "bash", ArgsText: "{}", Result: str("ok"), IsError: false},
	}, CreatedAt: t0.Add(time.Second)}))

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
	if len(msgs) != 2 || msgs[1].Parts[0].Result == nil || *msgs[1].Parts[0].Result != "ok" {
		t.Fatalf("bad messages roundtrip: %+v", msgs)
	}

	// Ownership gate on the real backend.
	if _, err := pg.GetMessages(ctx, "a", "intruder"); err != ErrNotFound {
		t.Fatalf("non-owner should get ErrNotFound, got %v", err)
	}
}
