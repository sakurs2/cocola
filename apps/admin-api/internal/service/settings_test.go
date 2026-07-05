package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

func TestSystemSettingsListRedactsSecretsAndReadsEnvDefaults(t *testing.T) {
	t.Setenv("COCOLA_AUTH_SECRET", "test-secret")
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
	secret := settingByKey(t, settings, "auth.secret")
	if secret.Source != "env" || !secret.Configured || secret.Value != nil {
		t.Fatalf("secret should be redacted but configured from env: %+v", secret)
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

func TestSystemSettingRejectsReadonlySecretAndTooSmallMinInterval(t *testing.T) {
	ctx := context.Background()
	svc := New(store.NewMemory(), nil, func() time.Time {
		return time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC)
	})

	if _, err := svc.UpdateSystemSetting(ctx, "auth.secret", SystemSettingUpdateInput{
		Value: "new-secret", Actor: "admin@example.com",
	}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("secret update should be denied, got %v", err)
	}
	if _, err := svc.UpdateSystemSetting(ctx, SettingSchedulerMinIntervalSecs, SystemSettingUpdateInput{
		Value: 1800, Actor: "admin@example.com",
	}); !errors.Is(err, ErrInvalidArg) {
		t.Fatalf("too small min interval should be invalid, got %v", err)
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
