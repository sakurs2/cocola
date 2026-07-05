package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

func TestComputeNextScheduledRunRejectsTooFrequentInterval(t *testing.T) {
	_, err := computeNextScheduledRun(
		ScheduleInterval,
		json.RawMessage(`{"every_seconds":1800}`),
		"Asia/Shanghai",
		time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC),
		time.Hour,
	)
	if !errors.Is(err, ErrScheduleTooFrequent) {
		t.Fatalf("expected ErrScheduleTooFrequent, got %v", err)
	}
}

func TestComputeNextScheduledRunAllowsHourlyInterval(t *testing.T) {
	after := time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC)
	got, err := computeNextScheduledRun(
		ScheduleInterval,
		json.RawMessage(`{"every_seconds":3600}`),
		"Asia/Shanghai",
		after,
		time.Hour,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Sub(after) != time.Hour {
		t.Fatalf("next run = %s, want exactly one hour after %s", got, after)
	}
}

func TestComputeNextScheduledRunRejectsTooFrequentCron(t *testing.T) {
	_, err := computeNextScheduledRun(
		ScheduleCron,
		json.RawMessage(`{"expression":"*/30 * * * *"}`),
		"Asia/Shanghai",
		time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC),
		time.Hour,
	)
	if !errors.Is(err, ErrScheduleTooFrequent) {
		t.Fatalf("expected ErrScheduleTooFrequent, got %v", err)
	}
}

func TestComputeNextScheduledRunAllowsHourlyCron(t *testing.T) {
	got, err := computeNextScheduledRun(
		ScheduleCron,
		json.RawMessage(`{"expression":"0 * * * *"}`),
		"Asia/Shanghai",
		time.Date(2026, 7, 5, 8, 13, 0, 0, time.UTC),
		time.Hour,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.IsZero() {
		t.Fatal("next run is zero")
	}
}

func TestComputeNextScheduledRunRejectsPastOnce(t *testing.T) {
	_, err := computeNextScheduledRun(
		ScheduleOnce,
		json.RawMessage(`{"run_at":"2026-07-05T07:59:00Z"}`),
		"Asia/Shanghai",
		time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC),
		time.Hour,
	)
	if !errors.Is(err, ErrScheduleInPast) {
		t.Fatalf("expected ErrScheduleInPast, got %v", err)
	}
}

func TestCreateUserScheduledTaskSetsOwnerAndConversation(t *testing.T) {
	ctx := context.Background()
	svc := New(store.NewMemory(), nil, func() time.Time {
		return time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC)
	})

	out, err := svc.CreateUserScheduledTask(ctx, "alice@example.com", ScheduledTaskInput{
		Name:         "Hourly report",
		ScheduleKind: ScheduleInterval,
		ScheduleSpec: json.RawMessage(`{"every_seconds":3600}`),
		Prompt:       "summarize",
		ModelAlias:   "claude-sonnet",
	})
	if err != nil {
		t.Fatalf("create user task: %v", err)
	}
	if out.OwnerType != "user" || out.OwnerUserID != "alice@example.com" {
		t.Fatalf("owner not set: %+v", out.ScheduledTask)
	}
	if out.ConversationID == "" || out.ConversationID == out.ID {
		t.Fatalf("conversation id not generated: %+v", out.ScheduledTask)
	}
	if out.MaxTurns != defaultMaxTurns {
		t.Fatalf("max turns = %d, want %d", out.MaxTurns, defaultMaxTurns)
	}
	if _, err := svc.GetUserScheduledTask(ctx, out.ID, "bob@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-user lookup should miss, got %v", err)
	}
}
