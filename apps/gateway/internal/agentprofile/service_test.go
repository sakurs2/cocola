package agentprofile

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestServiceLifecycleAndOwnership(t *testing.T) {
	t.Parallel()
	service := NewService(NewMemory())
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	owner := Identity{TenantID: "tenant-a", UserID: "user-a"}
	value, err := service.Create(context.Background(), owner, CreateInput{
		Name: " Research ", Description: " docs ", Instructions: "Be precise.",
		RuntimeID: "claude-code", ModelRouteID: "sonnet", ModelAlias: "sonnet",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if value.Name != "Research" || value.AvatarKey != "sparkle" ||
		value.AvatarColor != "blue" || value.Version != 1 {
		t.Fatalf("unexpected created Agent: %+v", value)
	}
	if _, err := service.Get(context.Background(),
		Identity{TenantID: "tenant-a", UserID: "user-b"}, value.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user Get error = %v, want ErrNotFound", err)
	}

	updated, err := service.Update(context.Background(), owner, value.ID, UpdateInput{
		Name: "Research", Description: "Updated", Instructions: value.Instructions,
		AvatarKey: "search", AvatarColor: "violet", RuntimeID: value.RuntimeID,
		ModelRouteID: value.ModelRouteID, ModelAlias: value.ModelAlias, Version: value.Version,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Version != 2 || updated.AvatarKey != "search" {
		t.Fatalf("unexpected updated Agent: %+v", updated)
	}
	if _, err := service.Update(context.Background(), owner, value.ID, UpdateInput{
		Name: "Research", AvatarKey: "search", AvatarColor: "violet",
		RuntimeID: value.RuntimeID, ModelRouteID: value.ModelRouteID,
		ModelAlias: value.ModelAlias, Version: value.Version,
	}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale Update error = %v, want ErrVersionConflict", err)
	}

	archived, err := service.Archive(context.Background(), owner, value.ID, updated.Version)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if archived.Status != StatusArchived || archived.Version != 3 {
		t.Fatalf("unexpected archived Agent: %+v", archived)
	}
	if list, err := service.List(context.Background(), owner); err != nil || len(list) != 0 {
		t.Fatalf("List after archive = %+v, %v", list, err)
	}
	if _, err := service.GetActive(context.Background(), owner, value.ID); !errors.Is(err, ErrArchived) {
		t.Fatalf("GetActive error = %v, want ErrArchived", err)
	}
}

func TestServiceValidationAndNameConflict(t *testing.T) {
	t.Parallel()
	service := NewService(NewMemory())
	id := Identity{UserID: "user-a"}
	valid := CreateInput{
		Name: "Research", RuntimeID: "claude-code",
		ModelRouteID: "sonnet", ModelAlias: "sonnet",
	}
	if _, err := service.Create(context.Background(), id, valid); err != nil {
		t.Fatalf("Create valid: %v", err)
	}
	valid.Name = "research"
	if _, err := service.Create(context.Background(), id, valid); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name error = %v, want ErrConflict", err)
	}
	invalid := []CreateInput{
		{Name: "", RuntimeID: "claude-code", ModelRouteID: "a", ModelAlias: "a"},
		{Name: strings.Repeat("a", 101), RuntimeID: "claude-code", ModelRouteID: "a", ModelAlias: "a"},
		{Name: "A", Instructions: strings.Repeat("a", MaxInstructionsBytes+1), RuntimeID: "claude-code", ModelRouteID: "a", ModelAlias: "a"},
		{Name: "A", AvatarKey: "custom", RuntimeID: "claude-code", ModelRouteID: "a", ModelAlias: "a"},
		{Name: "A", RuntimeID: "bad\nruntime", ModelRouteID: "a", ModelAlias: "a"},
	}
	for index, input := range invalid {
		if _, err := service.Create(context.Background(), id, input); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("invalid[%d] error = %v, want ErrInvalidArgument", index, err)
		}
	}
}
