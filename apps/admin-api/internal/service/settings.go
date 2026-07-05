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
)

type SystemSettingDefinition struct {
	Key             string `json:"key"`
	Group           string `json:"group"`
	Label           string `json:"label"`
	Description     string `json:"description"`
	Kind            string `json:"kind"`
	Env             string `json:"env,omitempty"`
	Default         any    `json:"default"`
	Editable        bool   `json:"editable"`
	HotReload       bool   `json:"hot_reload"`
	RestartRequired bool   `json:"restart_required"`
	Sensitive       bool   `json:"sensitive"`
	Min             int    `json:"min,omitempty"`
	Max             int    `json:"max,omitempty"`
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
			Kind:        "bool", Env: "COCOLA_SCHEDULER_ENABLED", Default: true, Editable: true, HotReload: true,
		},
		{
			Key: SettingSchedulerPollSecs, Group: "Scheduler", Label: "Poll Interval",
			Description: "Seconds between due-task scans.",
			Kind:        "int", Env: "COCOLA_SCHEDULER_POLL_SECS", Default: 60, Editable: true, HotReload: true, Min: 1, Max: 3600,
		},
		{
			Key: SettingSchedulerRunTimeoutSecs, Group: "Scheduler", Label: "Run Timeout",
			Description: "Maximum seconds allowed for a scheduled task run.",
			Kind:        "int", Env: "COCOLA_SCHEDULER_RUN_TIMEOUT_SECS", Default: 3600, Editable: true, HotReload: true, Min: 60, Max: 86400,
		},
		{
			Key: SettingSchedulerHeartbeatSecs, Group: "Scheduler", Label: "Heartbeat Interval",
			Description: "Seconds between running-task lease heartbeats.",
			Kind:        "int", Env: "COCOLA_SCHEDULER_HEARTBEAT_SECS", Default: 30, Editable: true, HotReload: true, Min: 1, Max: 3600,
		},
		{
			Key: SettingSchedulerLeaseTimeoutSecs, Group: "Scheduler", Label: "Lease Timeout",
			Description: "Seconds after which a running task without heartbeat is marked expired.",
			Kind:        "int", Env: "COCOLA_SCHEDULER_LEASE_TIMEOUT_SECS", Default: 300, Editable: true, HotReload: true, Min: 60, Max: 86400,
		},
		{
			Key: SettingSchedulerMinIntervalSecs, Group: "Scheduler", Label: "Minimum Schedule Interval",
			Description: "Minimum allowed task interval in seconds. Values below one hour are rejected.",
			Kind:        "int", Env: "COCOLA_SCHEDULER_MIN_INTERVAL_SECS", Default: 3600, Editable: true, HotReload: true, Min: 3600, Max: 86400,
		},
		{
			Key: "auth.token_ttl_secs", Group: "Auth", Label: "Token Default TTL",
			Description: "Default admin-minted token lifetime. Applies after restart in the current issuer implementation.",
			Kind:        "int", Env: "COCOLA_AUTH_TOKEN_TTL_SECS", Default: 30 * 24 * 3600, RestartRequired: true, Min: 0, Max: 365 * 24 * 3600,
		},
		{
			Key: "auth.secret", Group: "Auth", Label: "Runtime Auth Secret",
			Description: "Whether the shared HS256 auth secret is configured.",
			Kind:        "secret", Env: "COCOLA_AUTH_SECRET", Sensitive: true, RestartRequired: true,
		},
		{
			Key: "admin.key", Group: "Auth", Label: "Admin API Key",
			Description: "Whether static admin bearer authentication is configured.",
			Kind:        "secret", Env: "COCOLA_ADMIN_KEY", Sensitive: true, RestartRequired: true,
		},
		{
			Key: "infra.postgres_dsn", Group: "Storage / Infra", Label: "Postgres DSN",
			Description: "Postgres persistence DSN configuration status.",
			Kind:        "secret", Env: "COCOLA_PG_DSN", Sensitive: true, RestartRequired: true,
		},
		{
			Key: "infra.redis_addr", Group: "Storage / Infra", Label: "Redis Address",
			Description: "Shared Redis address for revocations, quota propagation, sandbox metadata, and user events.",
			Kind:        "string", Env: "COCOLA_REDIS_ADDR", Default: "", RestartRequired: true,
		},
		{
			Key: "gateway.url", Group: "AI Runtime", Label: "Gateway URL",
			Description: "Gateway URL used by admin-api for user scheduled task runs.",
			Kind:        "string", Env: "COCOLA_GATEWAY_URL", Default: "http://127.0.0.1:8080", RestartRequired: true,
		},
		{
			Key: "agent.addr", Group: "AI Runtime", Label: "Agent Runtime Address",
			Description: "agent-runtime gRPC address used by system scheduled tasks.",
			Kind:        "string", Env: "COCOLA_AGENT_ADDR", Default: "127.0.0.1:50061", RestartRequired: true,
		},
		{
			Key: "observability.metrics_addr", Group: "Observability", Label: "Metrics Address",
			Description: "admin-api metrics listen address. Empty disables the metrics server.",
			Kind:        "string", Env: "COCOLA_METRICS_ADDR", Default: ":9093", RestartRequired: true,
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
	if !def.Editable || def.Sensitive || def.RestartRequired {
		return SystemSettingView{}, ErrPermissionDenied
	}
	if def.Key == SettingSchedulerEnabled && !a.schedulerStarted.Load() {
		return SystemSettingView{}, ErrPermissionDenied
	}
	value, raw, err := normalizeSettingValue(def, in.Value)
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
	a.audit(ctx, in.Actor, "setting.update", key, "value="+settingAuditValue(def, value))
	return a.settingView(def, setting), nil
}

func (a *Admin) ResetSystemSetting(ctx context.Context, key string, expectedVersion int64, actor string) error {
	def, ok := settingDefinitionByKey(key)
	if !ok {
		return store.ErrNotFound
	}
	if !def.Editable || def.Sensitive || def.RestartRequired {
		return ErrPermissionDenied
	}
	if def.Key == SettingSchedulerEnabled && !a.schedulerStarted.Load() {
		return ErrPermissionDenied
	}
	if err := a.store.DeleteSystemSetting(ctx, key, expectedVersion); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	a.audit(ctx, actor, "setting.reset", key, "")
	return nil
}

func (a *Admin) settingView(def SystemSettingDefinition, override store.SystemSetting) SystemSettingView {
	value, source, configured := effectiveSettingValue(def, override)
	editable := def.Editable
	if def.Key == SettingSchedulerEnabled && !a.schedulerStarted.Load() {
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
	if def.Sensitive {
		view.Value = nil
	}
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
	if def.Sensitive {
		return nil, "default", false
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
	case "secret":
		return nil, nil
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

func settingAuditValue(def SystemSettingDefinition, value any) string {
	if def.Sensitive {
		return "<redacted>"
	}
	b, err := json.Marshal(value)
	if err != nil {
		return "<invalid>"
	}
	return string(b)
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

func secondsDuration(n int) time.Duration {
	return time.Duration(n) * time.Second
}
