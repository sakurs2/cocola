package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTokenLifecycle(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	rec := TokenRecord{ID: "tok-1", UserID: "emp-1", IssuedAt: time.Unix(1000, 0)}
	if err := m.CreateToken(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := m.CreateToken(ctx, rec); !errors.Is(err, ErrConflict) {
		t.Fatalf("dup create should conflict, got %v", err)
	}
	rev, err := m.IsRevoked(ctx, "tok-1")
	if err != nil || rev {
		t.Fatalf("fresh token should not be revoked: %v %v", err, rev)
	}
	if err := m.RevokeToken(ctx, "tok-1", time.Unix(2000, 0)); err != nil {
		t.Fatal(err)
	}
	rev, _ = m.IsRevoked(ctx, "tok-1")
	if !rev {
		t.Fatal("token should be revoked")
	}
	if _, err := m.GetToken(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing token should be NotFound, got %v", err)
	}
}

func TestListTokensFiltersByUser(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	_ = m.CreateToken(ctx, TokenRecord{ID: "a", UserID: "u1", IssuedAt: time.Unix(1, 0)})
	_ = m.CreateToken(ctx, TokenRecord{ID: "b", UserID: "u2", IssuedAt: time.Unix(2, 0)})
	_ = m.CreateToken(ctx, TokenRecord{ID: "c", UserID: "u1", IssuedAt: time.Unix(3, 0)})
	all, _ := m.ListTokens(ctx, "")
	if len(all) != 3 {
		t.Fatalf("want 3, got %d", len(all))
	}
	u1, _ := m.ListTokens(ctx, "u1")
	if len(u1) != 2 || u1[0].ID != "c" { // newest first
		t.Fatalf("filter/sort wrong: %+v", u1)
	}
}

func TestQuotaCRUD(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	_ = m.SetQuota(ctx, QuotaOverride{Scope: "user", Subject: "u1", Limit: 100})
	_ = m.SetQuota(ctx, QuotaOverride{Scope: "user", Subject: "u1", Limit: 200}) // upsert
	q, err := m.GetQuota(ctx, "user", "u1")
	if err != nil || q.Limit != 200 {
		t.Fatalf("upsert failed: %+v %v", q, err)
	}
	if err := m.DeleteQuota(ctx, "user", "u1"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetQuota(ctx, "user", "u1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted quota should be NotFound, got %v", err)
	}
}

func TestSkillCRUD(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	s := Skill{ID: "sk-1", Name: "Weather", Enabled: false}
	if err := m.CreateSkill(ctx, s); err != nil {
		t.Fatal(err)
	}
	if err := m.CreateSkill(ctx, s); !errors.Is(err, ErrConflict) {
		t.Fatalf("dup skill should conflict, got %v", err)
	}
	s.Enabled = true
	if err := m.UpdateSkill(ctx, s); err != nil {
		t.Fatal(err)
	}
	enabled, _ := m.ListSkills(ctx, true)
	if len(enabled) != 1 {
		t.Fatalf("want 1 enabled, got %d", len(enabled))
	}
	if err := m.DeleteSkill(ctx, "sk-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetSkill(ctx, "sk-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted skill should be NotFound, got %v", err)
	}
}

func TestAuditAppendAndListNewestFirst(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_ = m.AppendAudit(ctx, AuditEntry{Actor: "admin", Action: "x"})
	}
	got, _ := m.ListAudit(ctx, 3)
	if len(got) != 3 {
		t.Fatalf("limit not honored: %d", len(got))
	}
	if got[0].ID != 5 || got[1].ID != 4 || got[2].ID != 3 {
		t.Fatalf("not newest-first: %+v", got)
	}
}

func TestTryStartScheduledTaskRunBackfillsLegacyUserConversationID(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	task := ScheduledTask{
		ID:           "task-1",
		OwnerType:    "user",
		OwnerUserID:  "alice@example.com",
		Name:         "Time query",
		Status:       "active",
		ScheduleKind: "once",
		ScheduleSpec: []byte(`{"run_at":"2027-01-15T08:00:00Z"}`),
		Timezone:     "Asia/Shanghai",
		Prompt:       "what time is it",
		ModelAlias:   "claude-sonnet",
		MaxTurns:     30,
		ConfigJSON:   []byte(`{}`),
		NextRunAt:    now,
		CreatedAt:    now.Add(-time.Hour),
		UpdatedAt:    now.Add(-time.Hour),
	}
	if err := m.CreateScheduledTask(ctx, task, nil); err != nil {
		t.Fatalf("create legacy task: %v", err)
	}

	claimed, ok, err := m.TryStartScheduledTaskRun(ctx, task.ID, ScheduledTaskRun{
		ID:           "run-1",
		TaskID:       task.ID,
		ScheduledFor: now,
		Status:       "running",
		CreatedAt:    now,
		UpdatedAt:    now,
	}, time.Time{})
	if err != nil || !ok {
		t.Fatalf("try start: ok=%v err=%v", ok, err)
	}
	if claimed.ConversationID != "sched-task-1" {
		t.Fatalf("claimed conversation id = %q", claimed.ConversationID)
	}
	stored, err := m.GetScheduledTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if stored.ConversationID != "sched-task-1" {
		t.Fatalf("stored conversation id = %q", stored.ConversationID)
	}
}

func TestScheduledTaskRunHeartbeatAndExpire(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	task := ScheduledTask{
		ID:           "task-1",
		OwnerType:    "user",
		OwnerUserID:  "alice@example.com",
		Name:         "Hourly report",
		Status:       "active",
		ScheduleKind: "interval",
		ScheduleSpec: []byte(`{"every_seconds":3600}`),
		Timezone:     "Asia/Shanghai",
		Prompt:       "summarize",
		ModelAlias:   "claude-sonnet",
		MaxTurns:     30,
		ConfigJSON:   []byte(`{}`),
		NextRunAt:    now.Add(-time.Hour),
		CreatedAt:    now.Add(-2 * time.Hour),
		UpdatedAt:    now.Add(-2 * time.Hour),
	}
	if err := m.CreateScheduledTask(ctx, task, nil); err != nil {
		t.Fatalf("create task: %v", err)
	}
	run := ScheduledTaskRun{
		ID:           "run-1",
		TaskID:       task.ID,
		ScheduledFor: now.Add(-time.Hour),
		Status:       "running",
		WorkerID:     "worker-a",
		StartedAt:    now.Add(-time.Hour),
		CreatedAt:    now.Add(-time.Hour),
		UpdatedAt:    now.Add(-time.Hour),
	}
	if _, ok, err := m.TryStartScheduledTaskRun(ctx, task.ID, run, now); err != nil || !ok {
		t.Fatalf("try start: ok=%v err=%v", ok, err)
	}
	if ok, err := m.HeartbeatScheduledTaskRun(ctx, run.ID, "worker-a", now.Add(-time.Minute)); err != nil || !ok {
		t.Fatalf("heartbeat: ok=%v err=%v", ok, err)
	}
	expired, err := m.ExpireStaleScheduledTaskRuns(ctx, now.Add(-2*time.Minute), now, "expired", 10)
	if err != nil {
		t.Fatalf("expire fresh heartbeat: %v", err)
	}
	if len(expired) != 0 {
		t.Fatalf("fresh heartbeat should not expire: %+v", expired)
	}
	expired, err = m.ExpireStaleScheduledTaskRuns(ctx, now.Add(30*time.Second), now, "expired", 10)
	if err != nil {
		t.Fatalf("expire stale: %v", err)
	}
	if len(expired) != 1 || expired[0].Status != "error" || expired[0].Error != "expired" {
		t.Fatalf("bad expired run: %+v", expired)
	}
	storedRun, err := m.GetScheduledTaskRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if storedRun.Status != "error" {
		t.Fatalf("run status = %q", storedRun.Status)
	}
	storedTask, err := m.GetScheduledTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if storedTask.RunCount != 1 || storedTask.LastStatus != "error" || !storedTask.NextRunAt.Equal(now) {
		t.Fatalf("bad task after expire: %+v", storedTask)
	}
	nextRun := ScheduledTaskRun{
		ID:           "run-2",
		TaskID:       task.ID,
		ScheduledFor: now,
		Status:       "running",
		WorkerID:     "worker-b",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if _, ok, err := m.TryStartScheduledTaskRun(ctx, task.ID, nextRun, now.Add(time.Hour)); err != nil || !ok {
		t.Fatalf("next claim after expire: ok=%v err=%v", ok, err)
	}
}

func TestExpireOnceScheduledTaskRunCompletesTask(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	task := ScheduledTask{
		ID:           "task-1",
		OwnerType:    "user",
		OwnerUserID:  "alice@example.com",
		Name:         "One-shot",
		Status:       "active",
		ScheduleKind: "once",
		ScheduleSpec: []byte(`{"run_at":"2027-01-15T08:00:00Z"}`),
		Timezone:     "Asia/Shanghai",
		Prompt:       "run once",
		ModelAlias:   "claude-sonnet",
		MaxTurns:     30,
		ConfigJSON:   []byte(`{}`),
		NextRunAt:    now.Add(-time.Hour),
		CreatedAt:    now.Add(-2 * time.Hour),
		UpdatedAt:    now.Add(-2 * time.Hour),
	}
	if err := m.CreateScheduledTask(ctx, task, nil); err != nil {
		t.Fatalf("create task: %v", err)
	}
	run := ScheduledTaskRun{
		ID:           "run-1",
		TaskID:       task.ID,
		ScheduledFor: now.Add(-time.Hour),
		Status:       "running",
		WorkerID:     "worker-a",
		CreatedAt:    now.Add(-time.Hour),
		UpdatedAt:    now.Add(-time.Hour),
	}
	if _, ok, err := m.TryStartScheduledTaskRun(ctx, task.ID, run, time.Time{}); err != nil || !ok {
		t.Fatalf("try start: ok=%v err=%v", ok, err)
	}
	if _, err := m.ExpireStaleScheduledTaskRuns(ctx, now.Add(-time.Minute), now, "expired", 10); err != nil {
		t.Fatalf("expire stale: %v", err)
	}
	storedTask, err := m.GetScheduledTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if storedTask.Status != "completed" || !storedTask.NextRunAt.IsZero() {
		t.Fatalf("once task should be completed without retry: %+v", storedTask)
	}
}
