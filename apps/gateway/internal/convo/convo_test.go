package convo

import (
	"context"
	"testing"
	"time"
)

func str(s string) *string { return &s }

// TestMemoryConversationLifecycle covers upsert (insert + updated_at refresh
// with title preservation), message append, ordering, and ownership scoping.
func TestMemoryConversationLifecycle(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()

	t0 := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	// Two conversations for user "u1", one for "u2".
	must(t, m.UpsertConversation(ctx, Conversation{ID: "a", UserID: "u1", Title: "first", CreatedAt: t0, UpdatedAt: t0}))
	must(t, m.UpsertConversation(ctx, Conversation{ID: "b", UserID: "u1", Title: "second", CreatedAt: t0.Add(time.Minute), UpdatedAt: t0.Add(time.Minute)}))
	must(t, m.UpsertConversation(ctx, Conversation{ID: "c", UserID: "u2", Title: "other", CreatedAt: t0, UpdatedAt: t0}))

	// Upsert existing "a" with a later updated_at + a DIFFERENT title: updated_at
	// must refresh, title must be preserved (MVP: never overwrite).
	must(t, m.UpsertConversation(ctx, Conversation{ID: "a", UserID: "u1", Title: "CHANGED", CreatedAt: t0, UpdatedAt: t0.Add(2 * time.Minute)}))

	list, err := m.ListConversations(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 conversations for u1, got %d", len(list))
	}
	// Most-recently-updated first: "a" (t0+2m) then "b" (t0+1m).
	if list[0].ID != "a" || list[1].ID != "b" {
		t.Fatalf("bad order: %s,%s", list[0].ID, list[1].ID)
	}
	if list[0].Title != "first" {
		t.Fatalf("title should be preserved on upsert, got %q", list[0].Title)
	}

	// Messages.
	must(t, m.InsertMessage(ctx, Message{ID: "m1", ConversationID: "a", Role: "user", Parts: []Part{{Type: PartText, Text: "hi"}}, CreatedAt: t0}))
	must(t, m.InsertMessage(ctx, Message{ID: "m2", ConversationID: "a", Role: "assistant", Parts: []Part{
		{Type: PartReasoning, Text: "think"},
		{Type: PartText, Text: "hello"},
		{Type: PartToolCall, ToolCallID: "t1", ToolName: "bash", ArgsText: "{}", Result: str("ok")},
	}, CreatedAt: t0.Add(time.Second)}))

	msgs, err := m.GetMessages(ctx, "a", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].ID != "m1" || msgs[1].ID != "m2" {
		t.Fatalf("bad messages: %+v", msgs)
	}
	if len(msgs[1].Parts) != 3 || msgs[1].Parts[2].Result == nil || *msgs[1].Parts[2].Result != "ok" {
		t.Fatalf("bad assistant parts: %+v", msgs[1].Parts)
	}
}

// TestMemoryOwnershipGate: a non-owner (or missing conv) gets ErrNotFound.
func TestMemoryOwnershipGate(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	must(t, m.UpsertConversation(ctx, Conversation{ID: "a", UserID: "u1", CreatedAt: time.Now(), UpdatedAt: time.Now()}))

	if _, err := m.GetMessages(ctx, "a", "u2"); err != ErrNotFound {
		t.Fatalf("non-owner should get ErrNotFound, got %v", err)
	}
	if _, err := m.GetMessages(ctx, "missing", "u1"); err != ErrNotFound {
		t.Fatalf("missing conv should get ErrNotFound, got %v", err)
	}
}

func TestMemoryRenameAndDeleteConversation(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	now := time.Now()
	must(t, m.UpsertConversation(ctx, Conversation{ID: "a", UserID: "u1", Title: "old", CreatedAt: now, UpdatedAt: now}))
	must(t, m.InsertMessage(ctx, Message{ID: "m1", ConversationID: "a", Role: "user", CreatedAt: now}))
	must(t, m.UpsertArtifact(ctx, Artifact{ID: "art-1", ConversationID: "a", UserID: "u1"}))

	renamed, err := m.RenameConversation(ctx, "a", "u1", "new")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Title != "new" {
		t.Fatalf("title = %q", renamed.Title)
	}
	if _, err := m.RenameConversation(ctx, "a", "u2", "bad"); err != ErrNotFound {
		t.Fatalf("non-owner rename should get ErrNotFound, got %v", err)
	}

	if err := m.DeleteConversation(ctx, "a", "u1"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetConversation(ctx, "a", "u1"); err != ErrNotFound {
		t.Fatalf("deleted conversation should be gone, got %v", err)
	}
	if _, err := m.GetMessages(ctx, "a", "u1"); err != ErrNotFound {
		t.Fatalf("deleted messages should be inaccessible, got %v", err)
	}
	if _, err := m.GetArtifact(ctx, "a", "art-1", "u1"); err != ErrNotFound {
		t.Fatalf("deleted artifact metadata should be inaccessible, got %v", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
