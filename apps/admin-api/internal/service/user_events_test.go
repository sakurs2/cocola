package service

import (
	"context"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

func TestMemoryUserEventBrokerDeliversEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	broker := NewMemoryUserEventBroker()
	ch, stop, err := broker.SubscribeUserEvents(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer stop()

	event := UserEvent{
		ID:     "evt-1",
		Type:   UserEventScheduledTaskRunStarted,
		UserID: "alice@example.com",
		Resource: UserEventResource{
			Kind: "conversation",
			ID:   "sched-task-1",
		},
	}
	if err := broker.PublishUserEvent(ctx, event); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case got := <-ch:
		if got.ID != event.ID || got.Resource.ID != event.Resource.ID {
			t.Fatalf("event mismatch: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestUserEventSnapshotIncludesRunningUserTask(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	mem := store.NewMemory()
	svc := New(mem, nil, func() time.Time { return now })
	if err := mem.CreateAuthUser(ctx, store.AuthUser{ID: "user-alice", Username: "alice", Email: "alice@example.com", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	task := store.ScheduledTask{
		ID:             "task-1",
		OwnerType:      "user",
		OwnerUserID:    "user-alice",
		ConversationID: "sched-task-1",
		Name:           "Time query",
		Status:         TaskStatusActive,
		ScheduleKind:   ScheduleOnce,
		ScheduleSpec:   []byte(`{"run_at":"2026-07-05T10:00:00Z"}`),
		Timezone:       "Asia/Shanghai",
		Prompt:         "what time is it",
		ModelAlias:     "claude-sonnet",
		MaxTurns:       defaultMaxTurns,
		ConfigJSON:     []byte(`{}`),
		NextRunAt:      now,
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now.Add(-time.Hour),
	}
	if err := mem.CreateScheduledTask(ctx, task, nil); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, ok, err := mem.TryStartScheduledTaskRun(ctx, task.ID, store.ScheduledTaskRun{
		ID:           "run-1",
		TaskID:       task.ID,
		ScheduledFor: now,
		Status:       "running",
		CreatedAt:    now,
		UpdatedAt:    now,
	}, time.Time{}); err != nil || !ok {
		t.Fatalf("start run ok=%v err=%v", ok, err)
	}

	events, err := svc.UserEventSnapshot(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d", len(events))
	}
	if events[0].Type != UserEventScheduledTaskRunStarted || events[0].Resource.ID != "sched-task-1" {
		t.Fatalf("bad snapshot event: %+v", events[0])
	}
}

func TestUserEventSnapshotIgnoresStaleRunningRunAfterTaskCompleted(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	mem := store.NewMemory()
	svc := New(mem, nil, func() time.Time { return now })
	if err := mem.CreateAuthUser(ctx, store.AuthUser{ID: "user-alice", Username: "alice", Email: "alice@example.com", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	task := store.ScheduledTask{
		ID:             "task-1",
		OwnerType:      "user",
		OwnerUserID:    "user-alice",
		ConversationID: "sched-task-1",
		Name:           "Time query",
		Status:         TaskStatusActive,
		ScheduleKind:   ScheduleHourly,
		ScheduleSpec:   []byte(`{"minute":0}`),
		Timezone:       "Asia/Shanghai",
		Prompt:         "what time is it",
		ModelAlias:     "claude-sonnet",
		MaxTurns:       defaultMaxTurns,
		ConfigJSON:     []byte(`{}`),
		NextRunAt:      now.Add(-10 * time.Minute),
		LastRunAt:      now.Add(-time.Minute),
		LastStatus:     "success",
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now.Add(-time.Minute),
	}
	if err := mem.CreateScheduledTask(ctx, task, nil); err != nil {
		t.Fatalf("create task: %v", err)
	}
	memRun := store.ScheduledTaskRun{
		ID:           "run-1",
		TaskID:       task.ID,
		ScheduledFor: now.Add(-10 * time.Minute),
		Status:       "running",
		StartedAt:    now.Add(-10 * time.Minute),
		CreatedAt:    now.Add(-10 * time.Minute),
		UpdatedAt:    now.Add(-10 * time.Minute),
	}
	if _, ok, err := mem.TryStartScheduledTaskRun(ctx, task.ID, memRun, now.Add(time.Hour)); err != nil || !ok {
		t.Fatalf("start run ok=%v err=%v", ok, err)
	}
	stored, err := mem.GetScheduledTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	stored.LastRunAt = task.LastRunAt
	stored.LastStatus = task.LastStatus
	if err := mem.UpdateScheduledTask(ctx, stored, false, nil); err != nil {
		t.Fatalf("restore terminal task fields: %v", err)
	}

	events, err := svc.UserEventSnapshot(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	for _, event := range events {
		if event.Type == UserEventScheduledTaskRunStarted {
			t.Fatalf("snapshot should not include stale running event: %+v", event)
		}
	}
}
