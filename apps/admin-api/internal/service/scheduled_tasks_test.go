package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
	"github.com/cocola-project/cocola/packages/go-common/token"
)

func TestComputeNextScheduledRunRejectsPastOnce(t *testing.T) {
	_, err := computeNextScheduledRun(
		ScheduleOnce,
		json.RawMessage(`{"run_at":"2026-07-05T07:59:00Z"}`),
		"Asia/Shanghai",
		time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC),
	)
	if !errors.Is(err, ErrScheduleInPast) {
		t.Fatalf("expected ErrScheduleInPast, got %v", err)
	}
}

func TestComputeNextScheduledRunCalendarSchedules(t *testing.T) {
	tests := []struct {
		name  string
		kind  string
		spec  string
		after time.Time
		want  time.Time
	}{
		{
			name:  "hourly",
			kind:  ScheduleHourly,
			spec:  `{"minute":15}`,
			after: time.Date(2026, 7, 5, 8, 20, 0, 0, time.UTC),
			want:  time.Date(2026, 7, 5, 9, 15, 0, 0, time.UTC),
		},
		{
			name:  "daily in timezone",
			kind:  ScheduleDaily,
			spec:  `{"hour":9,"minute":0}`,
			after: time.Date(2026, 7, 5, 1, 1, 0, 0, time.UTC),
			want:  time.Date(2026, 7, 6, 1, 0, 0, 0, time.UTC),
		},
		{
			name:  "weekly ISO weekday",
			kind:  ScheduleWeekly,
			spec:  `{"weekday":1,"hour":9,"minute":30}`,
			after: time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC),
			want:  time.Date(2026, 7, 6, 1, 30, 0, 0, time.UTC),
		},
		{
			name:  "monthly clamps to last day",
			kind:  ScheduleMonthly,
			spec:  `{"day":31,"hour":9,"minute":0}`,
			after: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
			want:  time.Date(2026, 2, 28, 1, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := computeNextScheduledRun(tt.kind, json.RawMessage(tt.spec), "Asia/Shanghai", tt.after)
			if err != nil {
				t.Fatalf("compute next run: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("next run = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestCreateUserScheduledTaskSetsOwnerAndConversation(t *testing.T) {
	ctx := context.Background()
	memory := store.NewMemory()
	if err := memory.CreateAuthUser(ctx, store.AuthUser{ID: "user-alice", Username: "alice", Email: "alice@example.com", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	svc := New(memory, nil, func() time.Time {
		return time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC)
	})

	out, err := svc.CreateUserScheduledTask(ctx, "alice@example.com", ScheduledTaskInput{
		Name:         "Hourly report",
		ScheduleKind: ScheduleHourly,
		ScheduleSpec: json.RawMessage(`{"minute":0}`),
		Prompt:       "summarize",
		ModelAlias:   "claude-sonnet",
	})
	if err != nil {
		t.Fatalf("create user task: %v", err)
	}
	if out.OwnerType != "user" || out.OwnerUserID != "user-alice" {
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

func TestScheduledTaskExpirationValidationAndSweep(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC)
	memory := store.NewMemory()
	if err := memory.CreateAuthUser(ctx, store.AuthUser{ID: "alice", Email: "alice@example.com", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	svc := New(memory, nil, func() time.Time { return now })
	_, err := svc.CreateUserScheduledTask(ctx, "alice", ScheduledTaskInput{
		Name: "too late", ScheduleKind: ScheduleDaily, ScheduleSpec: json.RawMessage(`{"hour":17,"minute":0}`),
		Prompt: "work", ModelAlias: "model", ExpiresAt: now.Add(30 * time.Minute), ReplaceExpiresAt: true,
	})
	if !errors.Is(err, ErrScheduleExpiration) {
		t.Fatalf("expected ErrScheduleExpiration, got %v", err)
	}

	task := store.ScheduledTask{ID: "expired", OwnerUserID: "alice", Status: TaskStatusActive, ExpiresAt: now.Add(-time.Second), NextRunAt: now, CreatedAt: now}
	if err := memory.CreateScheduledTask(ctx, task, nil); err != nil {
		t.Fatal(err)
	}
	expired, err := memory.ExpireScheduledTasks(ctx, now, 10)
	if err != nil || len(expired) != 1 || expired[0].Status != TaskStatusExpired || !expired[0].NextRunAt.IsZero() {
		t.Fatalf("expired tasks = %+v, err = %v", expired, err)
	}

	finalTask := store.ScheduledTask{ID: "final-run", OwnerUserID: "alice", Status: TaskStatusActive, ScheduleKind: ScheduleHourly, ExpiresAt: now.Add(30 * time.Minute), NextRunAt: now, CreatedAt: now}
	if err := memory.CreateScheduledTask(ctx, finalTask, nil); err != nil {
		t.Fatal(err)
	}
	run := store.ScheduledTaskRun{ID: "final-run-1", TaskID: finalTask.ID, Status: "running", ScheduledFor: now, CreatedAt: now, UpdatedAt: now}
	if _, ok, err := memory.TryStartScheduledTaskRun(ctx, finalTask.ID, run, time.Time{}); err != nil || !ok {
		t.Fatalf("claim final run: ok=%v err=%v", ok, err)
	}
	run.Status = "success"
	run.FinishedAt = now.Add(time.Minute)
	run.UpdatedAt = run.FinishedAt
	if err := memory.UpdateScheduledTaskRun(ctx, run, time.Time{}, true); err != nil {
		t.Fatal(err)
	}
	stored, err := memory.GetScheduledTask(ctx, finalTask.ID)
	if err != nil || stored.Status != TaskStatusExpired {
		t.Fatalf("final recurring task status = %q, err=%v", stored.Status, err)
	}
}

func TestDisablingOwnerPausesActiveTasks(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC)
	memory := store.NewMemory()
	user := store.AuthUser{ID: "alice", Username: "alice", Email: "alice@example.com", Role: RoleUser, Enabled: true}
	if err := memory.CreateAuthUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := memory.CreateScheduledTask(ctx, store.ScheduledTask{
		ID: "task", OwnerUserID: user.ID, Status: TaskStatusActive, NextRunAt: now.Add(time.Hour), CreatedAt: now,
	}, nil); err != nil {
		t.Fatal(err)
	}
	svc := New(memory, nil, func() time.Time { return now })
	disabled := false
	if _, err := svc.SetAuthUser(ctx, user.ID, AuthUserInput{Enabled: &disabled, Actor: "admin@example.com"}); err != nil {
		t.Fatal(err)
	}
	task, err := memory.GetScheduledTask(ctx, "task")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != TaskStatusPaused || !task.NextRunAt.IsZero() || task.LastError != "Owner disabled" {
		t.Fatalf("task was not paused: %+v", task)
	}
}

func TestGatewayTaskRunnerUsesOwnerIdentityAndStableConversation(t *testing.T) {
	ctx := context.Background()
	memory := store.NewMemory()
	owner := store.AuthUser{ID: "user-alice", Username: "alice", Email: "alice@example.com", Enabled: true}
	if err := memory.CreateAuthUser(ctx, owner); err != nil {
		t.Fatal(err)
	}
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat" || !strings.HasPrefix(r.Header.Get("authorization"), "Bearer ") {
			t.Errorf("unexpected gateway request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("event: text\ndata: {\"kind\":\"text\",\"data\":{\"text\":\"done\"}}\n\n"))
	}))
	defer server.Close()

	svc := New(memory, token.NewIssuer("secret", "cocola", time.Hour), time.Now)
	runner := gatewayTaskRunner{admin: svc, gatewayURL: server.URL, httpClient: server.Client()}
	sessionID, err := runner.Run(ctx, store.ScheduledTask{
		ID: "task", OwnerUserID: owner.ID, ConversationID: "sched-task", Name: "Daily report", Prompt: "summarize", ModelAlias: "model", MaxTurns: 30,
	}, nil, func(string, map[string]string) {})
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != "sched-task" || requestBody["session_id"] != "sched-task" || requestBody["conversation_type"] != "scheduled_task" {
		t.Fatalf("unexpected gateway payload: session=%q body=%+v", sessionID, requestBody)
	}
}

type recordingTaskRunner struct{ called bool }

func (r *recordingTaskRunner) Run(context.Context, store.ScheduledTask, []store.ScheduledTaskAttachment, func(string, map[string]string)) (string, error) {
	r.called = true
	return "", nil
}

func TestWorkerSkipsTaskThatExpiredAfterDueQuery(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 5, 8, 0, 1, 0, time.UTC)
	memory := store.NewMemory()
	task := store.ScheduledTask{
		ID: "expired-between-query-and-claim", OwnerUserID: "user-alice", Status: TaskStatusActive,
		ScheduleKind: ScheduleHourly, ExpiresAt: now.Add(-time.Second), NextRunAt: now.Add(-2 * time.Second), CreatedAt: now.Add(-time.Hour),
	}
	if err := memory.CreateScheduledTask(ctx, task, nil); err != nil {
		t.Fatal(err)
	}
	runner := &recordingTaskRunner{}
	svc := New(memory, nil, func() time.Time { return now })
	svc.executeDueTask(ctx, SchedulerConfig{WorkerID: "test", RunTimeout: time.Minute}, runner, task)
	stored, err := memory.GetScheduledTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if runner.called || stored.Status != TaskStatusExpired || !stored.NextRunAt.IsZero() {
		t.Fatalf("expired task was not skipped: called=%v task=%+v", runner.called, stored)
	}
}

type finishFailStore struct {
	store.Store
	err error
}

func (s *finishFailStore) UpdateScheduledTaskRun(context.Context, store.ScheduledTaskRun, time.Time, bool) error {
	return s.err
}

func TestFinishRunReturnsPersistenceError(t *testing.T) {
	want := errors.New("persist terminal state")
	svc := New(&finishFailStore{Store: store.NewMemory(), err: want}, nil, func() time.Time {
		return time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	})
	err := svc.finishRun(context.Background(), store.ScheduledTaskRun{ID: "run", TaskID: "task"}, time.Time{}, "success", "done", "")
	if !errors.Is(err, want) {
		t.Fatalf("finishRun error = %v, want %v", err, want)
	}
}
