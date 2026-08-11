package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	sandboxIdleTimeoutSettingKey = "execution.sandbox_idle_timeout_minutes"
	minSandboxIdleTimeoutMinutes = 5
	maxSandboxIdleTimeoutMinutes = 24 * 60
	runtimeSettingsPollInterval  = 30 * time.Second
)

// RuntimeSettings keeps the small set of Admin-owned Sandbox settings in
// process-local atomics. Admin API is the only writer to system_settings;
// sandbox-manager polls once per interval so heartbeats never issue SQL.
type RuntimeSettings struct {
	pool             *pgxpool.Pool
	fallbackLeaseTTL time.Duration
	leaseTTLNanos    atomic.Int64
}

// NewRuntimeSettingsFromEnv returns nil when PostgreSQL is not configured so
// standalone provider deployments continue to use environment defaults.
func NewRuntimeSettingsFromEnv(ctx context.Context, fallbackLeaseTTL time.Duration) (*RuntimeSettings, error) {
	dsn := strings.TrimSpace(os.Getenv("COCOLA_PG_DSN"))
	if dsn == "" {
		return nil, nil
	}
	if fallbackLeaseTTL <= 0 {
		fallbackLeaseTTL = DefaultLeaseTTL
	}
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("runtime settings postgres config: %w", err)
	}
	// Runtime settings issue one short indexed query per poll interval. A
	// single lazy connection is sufficient and avoids reserving the default
	// pool size in every sandbox-manager replica.
	poolConfig.MinConns = 0
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("runtime settings postgres: %w", err)
	}
	settings := &RuntimeSettings{pool: pool, fallbackLeaseTTL: fallbackLeaseTTL}
	settings.leaseTTLNanos.Store(int64(fallbackLeaseTTL))
	return settings, nil
}

// Refresh loads one authoritative setting row. Missing rows restore the
// environment/default value; malformed rows preserve the last valid value and
// return a diagnosable error.
func (s *RuntimeSettings) Refresh(ctx context.Context) error {
	var raw json.RawMessage
	err := s.pool.QueryRow(ctx, `SELECT value_json FROM system_settings WHERE key=$1`, sandboxIdleTimeoutSettingKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		s.leaseTTLNanos.Store(int64(s.fallbackLeaseTTL))
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", sandboxIdleTimeoutSettingKey, err)
	}
	ttl, err := decodeSandboxIdleTimeout(raw)
	if err != nil {
		return err
	}
	s.leaseTTLNanos.Store(int64(ttl))
	return nil
}

func decodeSandboxIdleTimeout(raw json.RawMessage) (time.Duration, error) {
	var minutes int
	if err := json.Unmarshal(raw, &minutes); err != nil {
		return 0, fmt.Errorf("decode %s: %w", sandboxIdleTimeoutSettingKey, err)
	}
	if minutes < minSandboxIdleTimeoutMinutes || minutes > maxSandboxIdleTimeoutMinutes {
		return 0, fmt.Errorf(
			"invalid %s value %d (want %d-%d minutes)",
			sandboxIdleTimeoutSettingKey,
			minutes,
			minSandboxIdleTimeoutMinutes,
			maxSandboxIdleTimeoutMinutes,
		)
	}
	return time.Duration(minutes) * time.Minute, nil
}

// Run refreshes the atomics on a bounded cadence. A failed refresh leaves the
// last valid value active and is surfaced through onError for operator logs.
func (s *RuntimeSettings) Run(ctx context.Context, onError func(error)) {
	ticker := time.NewTicker(runtimeSettingsPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := s.Refresh(refreshCtx)
			cancel()
			if err != nil && onError != nil {
				onError(err)
			}
		}
	}
}

func (s *RuntimeSettings) SandboxLeaseTTL() time.Duration {
	return time.Duration(s.leaseTTLNanos.Load())
}

func (s *RuntimeSettings) Close() { s.pool.Close() }
