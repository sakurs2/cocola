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
	t.Setenv("COCOLA_SESSION_VOLUME_SIZE", "4Gi")
	t.Setenv("COCOLA_AGENT_TOOL_STEP_TIMEOUT_SECS", "900")

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
	if len(settings) != 9 {
		t.Fatalf("settings count = %d, want 9 runtime settings", len(settings))
	}
	expected := map[string]bool{
		SettingAgentMaxTurns:             true,
		SettingToolStepTimeoutSecs:       true,
		SettingSchedulerEnabled:          true,
		SettingSchedulerPollSecs:         true,
		SettingSchedulerRunTimeoutSecs:   true,
		SettingSchedulerHeartbeatSecs:    true,
		SettingSchedulerLeaseTimeoutSecs: true,
		SettingSessionVolumeDefaultSize:  true,
		SettingTraceRetentionDays:        true,
	}
	volume := settingByKey(t, settings, SettingSessionVolumeDefaultSize)
	if volume.Source != "env" || volume.Value != "4Gi" || !volume.Editable {
		t.Fatalf("session volume setting = %+v, want editable env 4Gi", volume)
	}
	toolTimeout := settingByKey(t, settings, SettingToolStepTimeoutSecs)
	if toolTimeout.Source != "env" || toolTimeout.Value != 900 || !toolTimeout.Editable {
		t.Fatalf("tool timeout setting = %+v, want editable env 900", toolTimeout)
	}
	for _, setting := range settings {
		if !expected[setting.Key] {
			t.Fatalf("startup-only setting leaked into runtime settings: %s", setting.Key)
		}
	}
}

func TestSessionVolumeSettingNormalizesQuantity(t *testing.T) {
	ctx := context.Background()
	svc := New(store.NewMemory(), nil, time.Now)

	updated, err := svc.UpdateSystemSetting(ctx, SettingSessionVolumeDefaultSize, SystemSettingUpdateInput{
		Value: "2048Mi", Actor: "admin@example.com",
	})
	if err != nil {
		t.Fatalf("update session volume size: %v", err)
	}
	if updated.Source != "db" || updated.Value != "2Gi" {
		t.Fatalf("normalized setting = %+v, want db 2Gi", updated)
	}
	if _, err := svc.UpdateSystemSetting(ctx, SettingSessionVolumeDefaultSize, SystemSettingUpdateInput{
		Value: "0Gi", ExpectedVersion: updated.Version,
	}); !errors.Is(err, ErrInvalidArg) {
		t.Fatalf("zero quantity error = %v, want invalid argument", err)
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
