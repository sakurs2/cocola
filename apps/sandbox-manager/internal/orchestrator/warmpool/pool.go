// Package warmpool keeps a small background pool of ready-to-bind sandboxes so a
// new session can adopt one instead of paying the cold-create cost (image pull +
// container/Pod boot) on the request path. It mirrors the well-worn
// "min-idle + max-pool + async refill + age-out" shape used by connection pools
// (HikariCP) and sandbox/serverless pre-warmers (E2B, Knative scale-from-zero),
// per ADR-0008 §3 "Warm pool".
//
// Design notes:
//   - Provider stays untouched (ADR-0002): the pool only calls the same
//     SandboxProvider.Create/Destroy the binder uses, with a USER-AGNOSTIC spec
//     (no UserID / placeholder SessionID). The caller "adopts" the checked-out
//     sandbox for a real session afterwards.
//   - State lives in Redis behind the go-common KV so multiple sandbox-manager
//     replicas share one pool. Each idle sandbox is its own scannable key; a
//     checkout claims it with Get-then-Del where Del's removed-count==1 is the
//     atomic CAS winner (works identically on real Redis and the Fake; no new
//     Lua needed).
//   - Pure optimisation: if the pool is empty or errors, Checkout returns
//     (nil,false,nil) and the binder falls back to a normal Create. The pool can
//     never become a new failure mode.
package warmpool

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
	rds "github.com/cocola-project/cocola/packages/go-common/redis"
)

// Key namespace. Idle markers share the orchestrator's "cocola:sb:" root so a
// shared Redis stays collision-free, under a dedicated "warm:" sub-namespace.
const keyPrefix = "cocola:sb:warm:"

func idleKey(sandboxID string) string { return keyPrefix + "idle:" + sandboxID }
func idleScanPattern() string         { return keyPrefix + "idle:*" }

// Default tunables. Conservative: the pool is OFF unless explicitly enabled, and
// the starting params mirror the "reference practice" ADR-0008 §3 cites.
const (
	DefaultMinIdle     = 2
	DefaultMax         = 16
	DefaultRefillEvery = 2 * time.Second
	DefaultMaxLifetime = 48 * time.Hour // hard cap so no warm box lingers forever
	DefaultIdleTTL     = 30 * time.Minute
)

// entry is the durable record for one idle, ready-to-adopt sandbox.
type entry struct {
	SandboxID   string `json:"sandbox_id"`
	Endpoint    string `json:"endpoint"`
	CreatedUnix int64  `json:"created_unix"`
}

// Config tunes the pool. Zero values fall back to the package Default* via
// withDefaults, so callers set only what they care about.
type Config struct {
	Enabled     bool
	MinIdle     int
	Max         int // hard cap on idle + in-flight, guards against runaway warming
	Image       string
	RefillEvery time.Duration
	MaxLifetime time.Duration
	IdleTTL     time.Duration // safety TTL on an idle marker; 0 = no expiry
}

func (c Config) withDefaults() Config {
	if c.MinIdle <= 0 {
		c.MinIdle = DefaultMinIdle
	}
	if c.Max <= 0 {
		c.Max = DefaultMax
	}
	if c.MinIdle > c.Max {
		// Max is the absolute ceiling; you cannot keep more idle than the cap.
		c.MinIdle = c.Max
	}
	if c.RefillEvery <= 0 {
		c.RefillEvery = DefaultRefillEvery
	}
	if c.MaxLifetime <= 0 {
		c.MaxLifetime = DefaultMaxLifetime
	}
	// IdleTTL may legitimately be 0 (no expiry); leave as-is.
	return c
}

// ConfigFromEnv reads COCOLA_WARMPOOL_* overrides. The pool is OFF unless
// COCOLA_WARMPOOL_ENABLED is truthy, so existing deployments see no behaviour
// change. Sizes are plain ints; durations are seconds.
func ConfigFromEnv(image string) Config {
	return Config{
		Enabled:     envBool("COCOLA_WARMPOOL_ENABLED", false),
		MinIdle:     envInt("COCOLA_WARMPOOL_MIN_IDLE", DefaultMinIdle),
		Max:         envInt("COCOLA_WARMPOOL_MAX", DefaultMax),
		Image:       image,
		RefillEvery: envSecs("COCOLA_WARMPOOL_REFILL_SECS", DefaultRefillEvery),
		MaxLifetime: envSecs("COCOLA_WARMPOOL_MAX_LIFETIME_SECS", DefaultMaxLifetime),
		IdleTTL:     envSecs("COCOLA_WARMPOOL_IDLE_TTL_SECS", DefaultIdleTTL),
	}
}

func envBool(k string, def bool) bool {
	switch os.Getenv(k) {
	case "1", "true", "TRUE", "yes", "on":
		return true
	case "0", "false", "FALSE", "no", "off":
		return false
	default:
		return def
	}
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return def
}

func envSecs(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return def
}

// Pool maintains the idle set and serves checkouts. Safe for concurrent use.
type Pool struct {
	kv  rds.KV
	p   provider.SandboxProvider
	cfg Config
	net provider.Networking // egress injected into every warmed sandbox

	// inflight counts creates this replica has started but not yet published as
	// idle, so a burst of refill ticks doesn't overshoot Max.
	inflight atomic.Int64

	clock func() time.Time // injectable for tests
}

// New constructs a Pool. The provider is the same abstraction the binder uses,
// so warming works identically against Docker today and K8s+gVisor later.
func New(kv rds.KV, p provider.SandboxProvider, cfg Config) *Pool {
	return &Pool{kv: kv, p: p, cfg: cfg.withDefaults(), clock: time.Now}
}

// WithNetworking sets the egress policy stamped onto every warmed sandbox so a
// pooled box is never created without its allowlist. Returns the pool for chaining.
func (p *Pool) WithNetworking(n provider.Networking) *Pool { p.net = n; return p }

// Enabled reports whether warming is active.
func (p *Pool) Enabled() bool { return p.cfg.Enabled }

// EffectiveConfig returns the config after defaults were applied.
func (p *Pool) EffectiveConfig() Config { return p.cfg }

// Checkout tries to claim one ready sandbox. On an empty pool (or any error) it
// returns (nil, false, nil): the caller MUST fall back to a normal Create.
// Claiming is Get-then-Del: only the racer whose Del removes the key (count==1)
// owns the sandbox, so concurrent checkouts never hand out the same box.
func (p *Pool) Checkout(ctx context.Context) (*provider.Sandbox, bool, error) {
	if !p.cfg.Enabled {
		return nil, false, nil
	}
	var claimed *provider.Sandbox
	err := p.kv.ScanKeys(ctx, idleScanPattern(), 100, func(keys []string) error {
		for _, k := range keys {
			if claimed != nil {
				return nil
			}
			raw, err := p.kv.Get(ctx, k)
			if err != nil {
				continue // vanished or transient; try the next candidate
			}
			n, err := p.kv.Del(ctx, k)
			if err != nil || n != 1 {
				continue // another replica won this one
			}
			var e entry
			if json.Unmarshal([]byte(raw), &e) != nil {
				continue
			}
			claimed = &provider.Sandbox{ID: e.SandboxID, Endpoint: e.Endpoint}
		}
		return nil
	})
	if err != nil {
		return nil, false, nil // pure optimisation: degrade silently to Create
	}
	if claimed == nil {
		return nil, false, nil
	}
	return claimed, true, nil
}

// Size returns the current number of idle sandboxes (best-effort gauge).
func (p *Pool) Size(ctx context.Context) (int, error) {
	n := 0
	err := p.kv.ScanKeys(ctx, idleScanPattern(), 100, func(keys []string) error {
		n += len(keys)
		return nil
	})
	return n, err
}

// Run drives the background refill + age-out loop until ctx is cancelled. It is
// a no-op when the pool is disabled, so callers can start it unconditionally.
func (p *Pool) Run(ctx context.Context) {
	if !p.cfg.Enabled {
		return
	}
	t := time.NewTicker(p.cfg.RefillEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = p.tick(ctx)
		}
	}
}

// Tick runs one maintenance pass synchronously: age out stale boxes, then top up
// toward MinIdle (bounded by Max). The background Run loop calls it on a ticker;
// callers may also invoke it directly to pre-warm before serving traffic. No-op
// when the pool is disabled. Safe for concurrent use.
func (p *Pool) Tick(ctx context.Context) error { return p.tick(ctx) }

// tick runs one maintenance pass: age out stale boxes, then top up toward
// MinIdle (bounded by Max).
func (p *Pool) tick(ctx context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}
	p.ageOut(ctx)
	return p.refill(ctx)
}

// refill creates sandboxes until idle+inflight reaches MinIdle, never letting
// idle+inflight exceed Max. Each new box is published as an idle marker.
func (p *Pool) refill(ctx context.Context) error {
	for {
		idle, err := p.Size(ctx)
		if err != nil {
			return err
		}
		total := idle + int(p.inflight.Load())
		if total >= p.cfg.MinIdle || total >= p.cfg.Max {
			return nil
		}
		if err := p.warmOne(ctx); err != nil {
			return err // surface; loop will retry next tick
		}
	}
}

// warmOne creates a single user-agnostic sandbox and publishes it as idle. The
// inflight counter brackets the create so concurrent ticks can't overshoot Max.
func (p *Pool) warmOne(ctx context.Context) error {
	p.inflight.Add(1)
	defer p.inflight.Add(-1)

	sb, err := p.p.Create(ctx, provider.SandboxSpec{
		// USER-AGNOSTIC: no UserID; a placeholder session keeps providers that
		// key on session id happy. The binder rebinds these on adopt.
		SessionID:  "warm-" + newToken(),
		Image:      p.cfg.Image,
		Networking: p.net,
	})
	if err != nil {
		return err
	}
	e := entry{SandboxID: sb.ID, Endpoint: sb.Endpoint, CreatedUnix: p.clock().Unix()}
	blob, _ := json.Marshal(e)
	if err := p.kv.Set(ctx, idleKey(sb.ID), string(blob), p.cfg.IdleTTL); err != nil {
		// Couldn't publish — destroy so we don't leak an unreferenced box.
		_ = p.p.Destroy(ctx, sb.ID)
		return err
	}
	return nil
}

// ageOut claims and destroys idle boxes older than MaxLifetime, so the pool
// never serves a stale sandbox. It uses the same Del-CAS claim as Checkout.
func (p *Pool) ageOut(ctx context.Context) {
	cutoff := p.clock().Add(-p.cfg.MaxLifetime).Unix()
	_ = p.kv.ScanKeys(ctx, idleScanPattern(), 100, func(keys []string) error {
		for _, k := range keys {
			raw, err := p.kv.Get(ctx, k)
			if err != nil {
				continue
			}
			var e entry
			if json.Unmarshal([]byte(raw), &e) != nil {
				continue
			}
			if e.CreatedUnix > cutoff {
				continue // still fresh
			}
			if n, err := p.kv.Del(ctx, k); err != nil || n != 1 {
				continue // lost the claim race; let the winner handle it
			}
			_ = p.p.Destroy(ctx, e.SandboxID)
		}
		return nil
	})
}

// newToken mints an unguessable suffix for warm session placeholders.
func newToken() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
