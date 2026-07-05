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
