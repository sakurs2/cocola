package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

func TestSystemSettingsListOnlyIncludesRuntimeSettingsAndReadsEnvDefaults(t *testing.T) {
	t.Setenv("COCOLA_SCHEDULER_POLL_SECS", "45")

	svc := New(store.NewMemory(), nil, func() time.Time {
		return time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC)
	})
	settings, err := svc.ListSystemSettings(context.Background())
	if err != nil {
		t.Fatalf("list settings: %v", err)
	}

	poll := settingByKey(t, settings, SettingSchedulerPollSecs)
	if poll.Source != "env" || poll.Value != 45 {
		t.Fatalf("poll setting = source %q value %#v, want env 45", poll.Source, poll.Value)
	}
	if len(settings) != 8 {
		t.Fatalf("settings count = %d, want 8 runtime settings", len(settings))
	}
	expected := map[string]bool{
		SettingSchedulerEnabled:          true,
		SettingSchedulerPollSecs:         true,
		SettingSchedulerRunTimeoutSecs:   true,
		SettingSchedulerHeartbeatSecs:    true,
		SettingSchedulerLeaseTimeoutSecs: true,
		SettingWarmPoolEnabled:           true,
		SettingWarmPoolSize:              true,
		SettingTraceRetentionDays:        true,
	}
	warm := settingByKey(t, settings, SettingWarmPoolSize)
	if warm.Value != 10 || warm.Editable {
		t.Fatalf("warm setting without Redis writer = %+v, want visible read-only default", warm)
	}
	for _, setting := range settings {
		if !expected[setting.Key] {
			t.Fatalf("startup-only setting leaked into runtime settings: %s", setting.Key)
		}
	}
}

type warmPoolRecorder struct {
	enabled bool
	size    int
	calls   int
	err     error
}

func (w *warmPoolRecorder) SetWarmPoolConfig(_ context.Context, enabled bool, size int) error {
	w.enabled = enabled
	w.size = size
	w.calls++
	return w.err
}

func TestWarmPoolSettingsPublishAndRecoverAfterTransientFailure(t *testing.T) {
	ctx := context.Background()
	writer := &warmPoolRecorder{err: errors.New("redis unavailable")}
	svc := New(store.NewMemory(), nil, time.Now).WithWarmPoolConfigWriter(writer)

	if _, err := svc.UpdateSystemSetting(ctx, SettingWarmPoolSize, SystemSettingUpdateInput{
		Value: 25, Actor: "admin@example.com",
	}); err != nil {
		t.Fatalf("durable warm-pool update: %v", err)
	}
	stored, err := svc.store.GetSystemSetting(ctx, SettingWarmPoolSize)
	if err != nil || stored.Version != 1 {
		t.Fatalf("durable desired value was not saved: %+v, %v", stored, err)
	}

	if err := svc.PublishWarmPoolConfig(ctx); err == nil {
		t.Fatal("reconciliation should surface Redis failure")
	}
	writer.err = nil
	if err := svc.PublishWarmPoolConfig(ctx); err != nil {
		t.Fatalf("reconcile warm-pool config: %v", err)
	}
	if !writer.enabled || writer.size != 25 || writer.calls != 2 {
		t.Fatalf("published warm config = enabled %v size %d calls %d", writer.enabled, writer.size, writer.calls)
	}
	settings, err := svc.ListSystemSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !settingByKey(t, settings, SettingWarmPoolSize).Editable {
		t.Fatal("warm-pool size should be editable when propagation is configured")
	}
}

func TestSystemSettingUpdateResetAndVersionConflict(t *testing.T) {
	ctx := context.Background()
	svc := New(store.NewMemory(), nil, func() time.Time {
		return time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC)
	})

	updated, err := svc.UpdateSystemSetting(ctx, SettingSchedulerPollSecs, SystemSettingUpdateInput{
		Value: 10, Actor: "admin@example.com",
	})
	if err != nil {
		t.Fatalf("update setting: %v", err)
	}
	if updated.Source != "db" || updated.Value != 10 || updated.Version != 1 {
		t.Fatalf("bad first update: %+v", updated)
	}

	updated, err = svc.UpdateSystemSetting(ctx, SettingSchedulerPollSecs, SystemSettingUpdateInput{
		Value: 20, ExpectedVersion: updated.Version, Actor: "admin@example.com",
	})
	if err != nil {
		t.Fatalf("second update: %v", err)
	}
	if updated.Value != 20 || updated.Version != 2 {
		t.Fatalf("bad second update: %+v", updated)
	}
	if _, err := svc.UpdateSystemSetting(ctx, SettingSchedulerPollSecs, SystemSettingUpdateInput{
		Value: 30, ExpectedVersion: 1, Actor: "admin@example.com",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale update should conflict, got %v", err)
	}

	if err := svc.ResetSystemSetting(ctx, SettingSchedulerPollSecs, updated.Version, "admin@example.com"); err != nil {
		t.Fatalf("reset setting: %v", err)
	}
	settings, err := svc.ListSystemSettings(ctx)
	if err != nil {
		t.Fatalf("list after reset: %v", err)
	}
	poll := settingByKey(t, settings, SettingSchedulerPollSecs)
	if poll.Source != "default" || poll.Version != 0 || poll.Value != 60 {
		t.Fatalf("bad reset state: %+v", poll)
	}
}

func TestSystemSettingRejectsUnknownAndRequiresStartedScheduler(t *testing.T) {
	ctx := context.Background()
	svc := New(store.NewMemory(), nil, func() time.Time {
		return time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC)
	})

	if _, err := svc.UpdateSystemSetting(ctx, "auth.secret", SystemSettingUpdateInput{
		Value: "new-secret", Actor: "admin@example.com",
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("removed startup setting should be unknown, got %v", err)
	}
	if _, err := svc.UpdateSystemSetting(ctx, SettingSchedulerEnabled, SystemSettingUpdateInput{
		Value: false, Actor: "admin@example.com",
	}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("scheduler enabled should be read-only before worker start, got %v", err)
	}
	svc.schedulerStarted.Store(true)
	if _, err := svc.UpdateSystemSetting(ctx, SettingSchedulerEnabled, SystemSettingUpdateInput{
		Value: false, Actor: "admin@example.com",
	}); err != nil {
		t.Fatalf("scheduler enabled update after worker start: %v", err)
	}
}

func settingByKey(t *testing.T, settings []SystemSettingView, key string) SystemSettingView {
	t.Helper()
	for _, setting := range settings {
		if setting.Key == key {
			return setting
		}
	}
	t.Fatalf("setting %q not found", key)
	return SystemSettingView{}
}
