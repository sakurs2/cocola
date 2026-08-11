package orchestrator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	rds "github.com/cocola-project/cocola/packages/go-common/redis"
)

func TestRuntimeSettingsUsesSingleLazyPostgresConnection(t *testing.T) {
	t.Setenv("COCOLA_PG_DSN", "postgres://cocola:secret@127.0.0.1:5432/cocola")
	settings, err := NewRuntimeSettingsFromEnv(context.Background(), 30*time.Minute)
	if err != nil {
		t.Fatalf("new runtime settings: %v", err)
	}
	defer settings.Close()

	config := settings.pool.Config()
	if config.MinConns != 0 || config.MaxConns != 1 {
		t.Fatalf("runtime settings pool connections = %d-%d, want 0-1", config.MinConns, config.MaxConns)
	}
}

func TestDecodeSandboxIdleTimeout(t *testing.T) {
	if DefaultLeaseTTL != 30*time.Minute {
		t.Fatalf("default lease TTL = %s, want 30m", DefaultLeaseTTL)
	}
	for _, test := range []struct {
		name string
		raw  string
		want time.Duration
		ok   bool
	}{
		{name: "default", raw: `30`, want: 30 * time.Minute, ok: true},
		{name: "minimum", raw: `5`, want: 5 * time.Minute, ok: true},
		{name: "maximum", raw: `1440`, want: 24 * time.Hour, ok: true},
		{name: "below minimum", raw: `4`},
		{name: "above maximum", raw: `1441`},
		{name: "not an integer", raw: `"30"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := decodeSandboxIdleTimeout(json.RawMessage(test.raw))
			if test.ok {
				if err != nil || got != test.want {
					t.Fatalf("decode = %s, %v; want %s", got, err, test.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("decode = %s, want validation error", got)
			}
		})
	}
}

func TestBinderUsesRuntimeLeaseTTLSource(t *testing.T) {
	binder := NewBinder(rds.NewFake(), newFakeProvider(), Config{LeaseTTL: 10 * time.Minute}).
		WithLeaseTTLSource(func() time.Duration { return 30 * time.Minute })
	if got := binder.EffectiveConfig().LeaseTTL; got != 30*time.Minute {
		t.Fatalf("effective lease TTL = %s, want 30m", got)
	}

	binder.WithLeaseTTLSource(func() time.Duration { return 0 })
	if got := binder.EffectiveConfig().LeaseTTL; got != 10*time.Minute {
		t.Fatalf("invalid runtime lease TTL fallback = %s, want configured 10m", got)
	}
}

func TestConfigFromEnvUsesSandboxIdleTimeoutMinutes(t *testing.T) {
	t.Setenv("COCOLA_SANDBOX_IDLE_TIMEOUT_MINUTES", "45")
	t.Setenv("COCOLA_SANDBOX_LEASE_TTL_SECS", "600")
	if got := ConfigFromEnv().LeaseTTL; got != 45*time.Minute {
		t.Fatalf("lease TTL = %s, want the minutes-based 45m override", got)
	}

	t.Setenv("COCOLA_SANDBOX_IDLE_TIMEOUT_MINUTES", "invalid")
	if got := ConfigFromEnv().LeaseTTL; got != DefaultLeaseTTL {
		t.Fatalf("invalid minutes override fallback = %s, want %s", got, DefaultLeaseTTL)
	}
}
