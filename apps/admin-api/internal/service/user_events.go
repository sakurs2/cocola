package service

import (
	"context"
	"sync"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

const (
	UserEventScheduledTaskRunStarted  = "scheduled_task.run.started"
	UserEventScheduledTaskRunFinished = "scheduled_task.run.finished"
	UserEventScheduledTaskRunFailed   = "scheduled_task.run.failed"
)

type UserEventResource struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type UserEvent struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	UserID     string            `json:"user_id"`
	TenantID   string            `json:"tenant_id,omitempty"`
	OccurredAt time.Time         `json:"occurred_at"`
	Resource   UserEventResource `json:"resource"`
	Data       map[string]any    `json:"data,omitempty"`
}

type UserEventBroker interface {
	PublishUserEvent(ctx context.Context, event UserEvent) error
	SubscribeUserEvents(ctx context.Context) (<-chan UserEvent, func(), error)
}

type MemoryUserEventBroker struct {
	mu   sync.RWMutex
	subs map[chan UserEvent]struct{}
}

func NewMemoryUserEventBroker() *MemoryUserEventBroker {
	return &MemoryUserEventBroker{subs: map[chan UserEvent]struct{}{}}
}

func (b *MemoryUserEventBroker) PublishUserEvent(ctx context.Context, event UserEvent) error {
	b.mu.RLock()
	subs := make([]chan UserEvent, 0, len(b.subs))
	for ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- event:
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return nil
}

func (b *MemoryUserEventBroker) SubscribeUserEvents(ctx context.Context) (<-chan UserEvent, func(), error) {
	ch := make(chan UserEvent, 32)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	cancel := func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
	go func() {
		<-ctx.Done()
		cancel()
	}()
	return ch, cancel, nil
}

func (a *Admin) WithUserEventBroker(b UserEventBroker) *Admin {
	a.userEvents = b
	return a
}

func (a *Admin) PublishUserEvent(ctx context.Context, event UserEvent) error {
	if a.userEvents == nil {
		return nil
	}
	if event.ID == "" {
		event.ID = newID()
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = a.now().UTC()
	}
	return a.userEvents.PublishUserEvent(ctx, event)
}

func (a *Admin) SubscribeUserEvents(ctx context.Context) (<-chan UserEvent, func(), error) {
	if a.userEvents == nil {
		return nil, func() {}, ErrNotConfigured
	}
	return a.userEvents.SubscribeUserEvents(ctx)
}

func (a *Admin) UserEventSnapshot(ctx context.Context, ownerUserID string) ([]UserEvent, error) {
	tasks, err := a.store.ListScheduledTasksForOwner(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	now := a.now().UTC()
	recentCutoff := now.Add(-10 * time.Minute)
	events := make([]UserEvent, 0)
	for _, task := range tasks {
		convID := task.ConversationID
		if convID == "" {
			convID = "sched-" + task.ID
		}
		running, err := a.store.ListScheduledTaskRuns(ctx, task.ID, "running", 1)
		if err != nil {
			return nil, err
		}
		if len(running) > 0 && scheduledTaskRunStillActive(task, running[0], now) {
			occurredAt := scheduledTaskRunStartedAt(running[0])
			if occurredAt.IsZero() {
				occurredAt = now
			}
			events = append(events, scheduledTaskUserEvent(
				UserEventScheduledTaskRunStarted,
				task,
				running[0],
				"running",
				"",
				occurredAt,
			))
			continue
		}
		if !task.LastRunAt.IsZero() && task.LastRunAt.After(recentCutoff) {
			eventType := UserEventScheduledTaskRunFinished
			if task.LastStatus == "error" {
				eventType = UserEventScheduledTaskRunFailed
			}
			events = append(events, scheduledTaskUserEvent(
				eventType,
				task,
				store.ScheduledTaskRun{TaskID: task.ID, Status: task.LastStatus},
				task.LastStatus,
				task.LastError,
				task.LastRunAt,
			))
		}
	}
	return events, nil
}

func scheduledTaskRunStillActive(task store.ScheduledTask, run store.ScheduledTaskRun, now time.Time) bool {
	if !task.LastRunAt.IsZero() {
		runStartedAt := scheduledTaskRunStartedAt(run)
		if runStartedAt.IsZero() || !task.LastRunAt.Before(runStartedAt) {
			return false
		}
	}
	runUpdatedAt := run.UpdatedAt
	if runUpdatedAt.IsZero() {
		runUpdatedAt = run.CreatedAt
	}
	return runUpdatedAt.IsZero() || now.Sub(runUpdatedAt) <= 2*time.Hour
}

func scheduledTaskRunStartedAt(run store.ScheduledTaskRun) time.Time {
	if !run.StartedAt.IsZero() {
		return run.StartedAt
	}
	return run.CreatedAt
}

func scheduledTaskUserEvent(eventType string, task store.ScheduledTask, run store.ScheduledTaskRun, status, errText string, occurredAt time.Time) UserEvent {
	convID := task.ConversationID
	if convID == "" {
		convID = "sched-" + task.ID
	}
	data := map[string]any{
		"task_id":         task.ID,
		"run_id":          run.ID,
		"conversation_id": convID,
		"title":           task.Name,
		"status":          status,
		"chat_type":       "scheduled_task",
	}
	if startedAt := scheduledTaskRunStartedAt(run); !startedAt.IsZero() {
		data["run_started_at"] = startedAt.UTC().Format(time.RFC3339Nano)
	}
	if !run.UpdatedAt.IsZero() {
		data["run_updated_at"] = run.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !run.FinishedAt.IsZero() {
		data["run_finished_at"] = run.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	if errText != "" {
		data["error"] = errText
	}
	return UserEvent{
		ID:         newID(),
		Type:       eventType,
		UserID:     task.OwnerUserID,
		OccurredAt: occurredAt,
		Resource: UserEventResource{
			Kind: "conversation",
			ID:   convID,
		},
		Data: data,
	}
}
