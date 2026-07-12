package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

const (
	SettingSchedulerEnabled          = "scheduler.enabled"
	SettingSchedulerPollSecs         = "scheduler.poll_secs"
	SettingSchedulerRunTimeoutSecs   = "scheduler.run_timeout_secs"
	SettingSchedulerHeartbeatSecs    = "scheduler.heartbeat_secs"
	SettingSchedulerLeaseTimeoutSecs = "scheduler.lease_timeout_secs"
	SettingSchedulerMinIntervalSecs  = "scheduler.min_interval_secs"

	SettingWarmPoolEnabled    = "sandbox.warm_pool_enabled"
	SettingWarmPoolSize       = "sandbox.warm_pool_size"
	SettingTraceRetentionDays = "observability.trace_retention_days"
)

type SystemSettingDefinition struct {
	Key         string `json:"key"`
	Group       string `json:"group"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Env         string `json:"env,omitempty"`
	Default     any    `json:"default"`
	Editable    bool   `json:"editable"`
	Min         int    `json:"min,omitempty"`
	Max         int    `json:"max,omitempty"`
}

type SystemSettingView struct {
	SystemSettingDefinition
	Value      any       `json:"value,omitempty"`
	Source     string    `json:"source"`
	Configured bool      `json:"configured"`
	Version    int64     `json:"version"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
	UpdatedBy  string    `json:"updated_by,omitempty"`
}

type SystemSettingUpdateInput struct {
	Value           any
	ExpectedVersion int64
	Actor           string
}

func settingDefinitions() []SystemSettingDefinition {
	return []SystemSettingDefinition{
		{
			Key: SettingSchedulerEnabled, Group: "Scheduler", Label: "Scheduler Enabled",
			Description: "Pause or resume due-task execution while the scheduler worker is running.",
			Kind:        "bool", Env: "COCOLA_SCHEDULER_ENABLED", Default: true, Editable: true,
		},
		{
			Key: SettingSchedulerPollSecs, Group: "Scheduler", Label: "Poll Interval",
			Description: "Seconds between due-task scans.",
			Kind:        "int", Env: "COCOLA_SCHEDULER_POLL_SECS", Default: 60, Editable: true, Min: 1, Max: 3600,
		},
		{
			Key: SettingSchedulerRunTimeoutSecs, Group: "Scheduler", Label: "Run Timeout",
			Description: "Maximum seconds allowed for a scheduled task run.",
			Kind:        "int", Env: "COCOLA_SCHEDULER_RUN_TIMEOUT_SECS", Default: 3600, Editable: true, Min: 60, Max: 86400,
		},
		{
			Key: SettingSchedulerHeartbeatSecs, Group: "Scheduler", Label: "Heartbeat Interval",
			Description: "Seconds between running-task lease heartbeats.",
			Kind:        "int", Env: "COCOLA_SCHEDULER_HEARTBEAT_SECS", Default: 30, Editable: true, Min: 1, Max: 3600,
		},
		{
			Key: SettingSchedulerLeaseTimeoutSecs, Group: "Scheduler", Label: "Lease Timeout",
			Description: "Seconds after which a running task without heartbeat is marked expired.",
			Kind:        "int", Env: "COCOLA_SCHEDULER_LEASE_TIMEOUT_SECS", Default: 300, Editable: true, Min: 60, Max: 86400,
		},
		{
			Key: SettingSchedulerMinIntervalSecs, Group: "Scheduler", Label: "Minimum Schedule Interval",
			Description: "Minimum interval retained for legacy custom schedules; new tasks use simple calendar frequencies.",
			Kind:        "int", Env: "COCOLA_SCHEDULER_MIN_INTERVAL_SECS", Default: 3600, Editable: true, Min: 3600, Max: 86400,
		},
		{
			Key: SettingWarmPoolEnabled, Group: "Sandbox", Label: "Warm Pool Enabled",
			Description: "Create session-agnostic sandboxes ahead of demand. Applied without restarting sandbox-manager.",
			Kind:        "bool", Env: "COCOLA_SANDBOX_WARM_POOL_ENABLED", Default: true, Editable: true,
		},
		{
			Key: SettingWarmPoolSize, Group: "Sandbox", Label: "Warm Idle Target",
			Description: "Target number of idle pre-warmed sandboxes, excluding sandboxes already claimed by active sessions.",
			Kind:        "int", Env: "COCOLA_SANDBOX_WARM_POOL_SIZE", Default: 10, Editable: true, Min: 0, Max: 500,
		},
		{
			Key: SettingTraceRetentionDays, Group: "Observability", Label: "Trace Retention",
			Description: "Days to retain detailed conversation spans. Conversation audit summaries are kept.",
			Kind:        "int", Env: "COCOLA_TRACE_RETENTION_DAYS", Default: 30, Editable: true, Min: 1, Max: 365,
		},
	}
}

func settingDefinitionByKey(key string) (SystemSettingDefinition, bool) {
	for _, def := range settingDefinitions() {
		if def.Key == key {
			return def, true
		}
	}
	return SystemSettingDefinition{}, false
}

func (a *Admin) ListSystemSettings(ctx context.Context) ([]SystemSettingView, error) {
	stored, err := a.store.ListSystemSettings(ctx)
	if err != nil {
		return nil, err
	}
	overrides := map[string]store.SystemSetting{}
	for _, setting := range stored {
		overrides[setting.Key] = setting
	}
	out := make([]SystemSettingView, 0, len(settingDefinitions()))
	for _, def := range settingDefinitions() {
		out = append(out, a.settingView(def, overrides[def.Key]))
	}
	return out, nil
}

func (a *Admin) UpdateSystemSetting(ctx context.Context, key string, in SystemSettingUpdateInput) (SystemSettingView, error) {
	def, ok := settingDefinitionByKey(key)
	if !ok {
		return SystemSettingView{}, store.ErrNotFound
	}
	if !def.Editable {
		return SystemSettingView{}, ErrPermissionDenied
	}
	if def.Key == SettingSchedulerEnabled && !a.schedulerStarted.Load() {
		return SystemSettingView{}, ErrPermissionDenied
	}
	if isWarmPoolSetting(key) && a.warmPool == nil {
		return SystemSettingView{}, ErrNotConfigured
	}
	_, raw, err := normalizeSettingValue(def, in.Value)
	if err != nil {
		return SystemSettingView{}, err
	}
	now := a.now().UTC()
	setting, err := a.store.SetSystemSetting(ctx, store.SystemSetting{
		Key:       key,
		ValueJSON: raw,
		UpdatedAt: now,
		UpdatedBy: in.Actor,
	}, in.ExpectedVersion)
	if err != nil {
		return SystemSettingView{}, err
	}
	return a.settingView(def, setting), nil
}

func (a *Admin) ResetSystemSetting(ctx context.Context, key string, expectedVersion int64, actor string) error {
	def, ok := settingDefinitionByKey(key)
	if !ok {
		return store.ErrNotFound
	}
	if !def.Editable {
		return ErrPermissionDenied
	}
	if def.Key == SettingSchedulerEnabled && !a.schedulerStarted.Load() {
		return ErrPermissionDenied
	}
	if isWarmPoolSetting(key) && a.warmPool == nil {
		return ErrNotConfigured
	}
	if err := a.store.DeleteSystemSetting(ctx, key, expectedVersion); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	return nil
}

func (a *Admin) settingView(def SystemSettingDefinition, override store.SystemSetting) SystemSettingView {
	value, source, configured := effectiveSettingValue(def, override)
	editable := def.Editable
	if def.Key == SettingSchedulerEnabled && !a.schedulerStarted.Load() {
		editable = false
	}
	if isWarmPoolSetting(def.Key) && a.warmPool == nil {
		editable = false
	}
	view := SystemSettingView{
		SystemSettingDefinition: def,
		Value:                   value,
		Source:                  source,
		Configured:              configured,
		Version:                 override.Version,
		UpdatedAt:               override.UpdatedAt,
		UpdatedBy:               override.UpdatedBy,
	}
	view.Editable = editable
	return view
}

func effectiveSettingValue(def SystemSettingDefinition, override store.SystemSetting) (any, string, bool) {
	if override.Key != "" {
		value, err := decodeSettingValue(def, override.ValueJSON)
		if err == nil {
			return value, "db", true
		}
	}
	if def.Env != "" {
		if raw, ok := os.LookupEnv(def.Env); ok {
			value, err := parseSettingString(def, raw)
			if err == nil {
				return value, "env", raw != ""
			}
		}
	}
	return def.Default, "default", def.Default != nil
}

func normalizeSettingValue(def SystemSettingDefinition, value any) (any, json.RawMessage, error) {
	normalized, err := coerceSettingValue(def, value)
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return nil, nil, err
	}
	return normalized, raw, nil
}

func decodeSettingValue(def SystemSettingDefinition, raw json.RawMessage) (any, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, ErrInvalidArg
	}
	return coerceSettingValue(def, value)
}

func coerceSettingValue(def SystemSettingDefinition, value any) (any, error) {
	switch def.Kind {
	case "bool":
		b, ok := value.(bool)
		if !ok {
			return nil, ErrInvalidArg
		}
		return b, nil
	case "int":
		var n int
		switch v := value.(type) {
		case float64:
			if v != float64(int(v)) {
				return nil, ErrInvalidArg
			}
			n = int(v)
		case int:
			n = v
		default:
			return nil, ErrInvalidArg
		}
		if def.Min != 0 && n < def.Min {
			return nil, ErrInvalidArg
		}
		if def.Max != 0 && n > def.Max {
			return nil, ErrInvalidArg
		}
		return n, nil
	case "string":
		s, ok := value.(string)
		if !ok {
			return nil, ErrInvalidArg
		}
		return strings.TrimSpace(s), nil
	default:
		return nil, ErrPermissionDenied
	}
}

func parseSettingString(def SystemSettingDefinition, raw string) (any, error) {
	switch def.Kind {
	case "bool":
		if def.Key == SettingSchedulerEnabled {
			return !envBoolFalseValue(raw), nil
		}
		return parseBoolSetting(raw)
	case "int":
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return def.Default, nil
		}
		return coerceSettingValue(def, n)
	case "string":
		return strings.TrimSpace(raw), nil
	default:
		return def.Default, nil
	}
}

func parseBoolSetting(raw string) (bool, error) {
	switch strings.TrimSpace(raw) {
	case "1", "true", "TRUE", "True", "yes", "YES", "on", "ON":
		return true, nil
	case "0", "false", "FALSE", "False", "no", "NO", "off", "OFF":
		return false, nil
	default:
		return false, ErrInvalidArg
	}
}

func envBoolFalseValue(v string) bool {
	switch strings.TrimSpace(v) {
	case "0", "false", "FALSE", "False", "no", "NO", "off", "OFF":
		return true
	default:
		return false
	}
}

func (a *Admin) settingInt(ctx context.Context, key string, fallback int) int {
	def, ok := settingDefinitionByKey(key)
	if !ok {
		return fallback
	}
	setting, err := a.store.GetSystemSetting(ctx, key)
	if err == nil {
		value, err := decodeSettingValue(def, setting.ValueJSON)
		if err == nil {
			if n, ok := value.(int); ok {
				return n
			}
		}
	}
	value, _, _ := effectiveSettingValue(def, store.SystemSetting{})
	if n, ok := value.(int); ok {
		return n
	}
	return fallback
}

func (a *Admin) settingBool(ctx context.Context, key string, fallback bool) bool {
	def, ok := settingDefinitionByKey(key)
	if !ok {
		return fallback
	}
	setting, err := a.store.GetSystemSetting(ctx, key)
	if err == nil {
		value, err := decodeSettingValue(def, setting.ValueJSON)
		if err == nil {
			if b, ok := value.(bool); ok {
				return b
			}
		}
	}
	value, _, _ := effectiveSettingValue(def, store.SystemSetting{})
	if b, ok := value.(bool); ok {
		return b
	}
	return fallback
}

// WarmPoolConfigWriter propagates the durable desired warm-pool size to the
// Redis key sandbox-manager reads on every reconciliation tick.
type WarmPoolConfigWriter interface {
	SetWarmPoolConfig(ctx context.Context, enabled bool, size int) error
}

// WithWarmPoolConfigWriter enables hot warm-pool sizing. Without the writer the
// settings remain visible but read-only, because changing them could not affect
// sandbox-manager.
func (a *Admin) WithWarmPoolConfigWriter(w WarmPoolConfigWriter) *Admin {
	a.warmPool = w
	return a
}

// PublishWarmPoolConfig reconciles the Redis delivery value from the durable
// DB override (or its env/default fallback). Callers retry this operation so a
// transient Redis failure cannot leave the runtime stale indefinitely.
func (a *Admin) PublishWarmPoolConfig(ctx context.Context) error {
	if a.warmPool == nil {
		return ErrNotConfigured
	}
	return a.warmPool.SetWarmPoolConfig(
		ctx,
		a.settingBool(ctx, SettingWarmPoolEnabled, true),
		a.settingInt(ctx, SettingWarmPoolSize, 10),
	)
}

func isWarmPoolSetting(key string) bool {
	return key == SettingWarmPoolEnabled || key == SettingWarmPoolSize
}

func secondsDuration(n int) time.Duration {
	return time.Duration(n) * time.Second
}
