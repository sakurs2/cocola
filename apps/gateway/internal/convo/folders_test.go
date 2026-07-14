package convo

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryFolderLifecycleAndCascade(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	now := time.Now().UTC()

	folder, err := store.CreateFolder(ctx, Folder{
		ID: "folder-1", UserID: "u1", Name: "  Research  ", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if folder.Name != "Research" {
		t.Fatalf("trimmed folder name = %q", folder.Name)
	}
	if _, err := store.CreateFolder(ctx, Folder{
		ID: "folder-2", UserID: "u1", Name: "research", CreatedAt: now, UpdatedAt: now,
	}); !errors.Is(err, ErrFolderNameConflict) {
		t.Fatalf("case-insensitive duplicate = %v, want ErrFolderNameConflict", err)
	}
	if _, err := store.GetFolder(ctx, folder.ID, "u2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user folder read = %v, want ErrNotFound", err)
	}

	must(t, store.UpsertConversation(ctx, Conversation{
		ID: "chat-1", UserID: "u1", Title: "Chat", ChatType: "chat",
		FolderID: folder.ID, CreatedAt: now, UpdatedAt: now,
	}))
	must(t, store.InsertMessage(ctx, Message{
		ID: "message-1", ConversationID: "chat-1", Role: "user", CreatedAt: now,
	}))
	must(t, store.UpsertArtifact(ctx, Artifact{
		ID: "artifact-1", ConversationID: "chat-1", UserID: "u1",
	}))

	ids, err := store.DeleteFolder(ctx, folder.ID, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "chat-1" {
		t.Fatalf("deleted conversation IDs = %v", ids)
	}
	if _, err := store.GetConversation(ctx, "chat-1", "u1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cascaded conversation read = %v, want ErrNotFound", err)
	}
	if _, err := store.GetArtifact(ctx, "chat-1", "artifact-1", "u1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cascaded artifact read = %v, want ErrNotFound", err)
	}
}

func TestMemoryMoveConversationFolderRules(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	now := time.Now().UTC()
	_, err := store.CreateFolder(ctx, Folder{
		ID: "folder-1", UserID: "u1", Name: "Work", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	must(t, store.UpsertConversation(ctx, Conversation{
		ID: "chat-1", UserID: "u1", ChatType: "chat", CreatedAt: now, UpdatedAt: now,
	}))
	must(t, store.UpsertConversation(ctx, Conversation{
		ID: "task-1", UserID: "u1", ChatType: "scheduled_task", CreatedAt: now, UpdatedAt: now,
	}))

	moved, err := store.MoveConversation(ctx, "chat-1", "u1", "folder-1", now.Add(time.Second))
	if err != nil || moved.FolderID != "folder-1" {
		t.Fatalf("move to folder = %+v, %v", moved, err)
	}
	moved, err = store.MoveConversation(ctx, "chat-1", "u1", "", now.Add(2*time.Second))
	if err != nil || moved.FolderID != "" {
		t.Fatalf("move out of folder = %+v, %v", moved, err)
	}
	if _, err := store.MoveConversation(ctx, "task-1", "u1", "folder-1", now); !errors.Is(err, ErrUnsupportedChatType) {
		t.Fatalf("scheduled task move = %v, want ErrUnsupportedChatType", err)
	}
	if _, err := store.MoveConversation(ctx, "chat-1", "u1", "missing", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing destination = %v, want ErrNotFound", err)
	}
}
