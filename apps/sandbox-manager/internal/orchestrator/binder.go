package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"time"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
	rds "github.com/cocola-project/cocola/packages/go-common/redis"
)

// Config tunes the binder's lifecycle behaviour. Zero values fall back to the
// package Default* constants, so callers can set only what they care about.
type Config struct {
	LeaseTTL       time.Duration
	HeartbeatEvery time.Duration
	DestroyGrace   time.Duration
	LockTTL        time.Duration
	ReaperEvery    time.Duration
	LockRetry      time.Duration // spin interval while waiting on a held lock
}

func (c Config) withDefaults() Config {
	if c.LeaseTTL == 0 {
		c.LeaseTTL = DefaultLeaseTTL
	}
	if c.HeartbeatEvery == 0 {
		c.HeartbeatEvery = DefaultHeartbeatEvery
	}
	if c.DestroyGrace == 0 {
		c.DestroyGrace = DefaultDestroyGrace
	}
	if c.LockTTL == 0 {
		c.LockTTL = DefaultLockTTL
	}
	if c.ReaperEvery == 0 {
		c.ReaperEvery = DefaultReaperEvery
	}
	if c.LockRetry == 0 {
		c.LockRetry = 50 * time.Millisecond
	}
	return c
}

// ConfigFromEnv reads COCOLA_SANDBOX_* lifecycle overrides (seconds). Any unset
// or invalid var falls back to the package default via withDefaults().
func ConfigFromEnv() Config {
	return Config{
		LeaseTTL:       envSecs("COCOLA_SANDBOX_LEASE_TTL_SECS", DefaultLeaseTTL),
		HeartbeatEvery: envSecs("COCOLA_SANDBOX_HEARTBEAT_SECS", DefaultHeartbeatEvery),
		DestroyGrace:   envSecs("COCOLA_SANDBOX_DESTROY_GRACE_SECS", DefaultDestroyGrace),
		LockTTL:        envSecs("COCOLA_SANDBOX_LOCK_TTL_SECS", DefaultLockTTL),
		ReaperEvery:    envSecs("COCOLA_SANDBOX_REAPER_SECS", DefaultReaperEvery),
	}
}

func envSecs(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return def
}

// Binder maps sessions to sandboxes over a KV store, creating/reusing/resuming
// sandboxes through the provider. It is safe for concurrent use and holds no
// per-session state in memory — everything authoritative lives in the KV.
type Binder struct {
	kv  rds.KV
	p   provider.SandboxProvider
	cfg Config

	// net is the egress policy injected into every provider.Create. The zero
	// value (nil allowlist) leaves each provider on its own default; callers
	// tighten it via WithNetworking (see NetworkingFromEnv).
	net provider.Networking

	// metrics is optional; nil means "don't record".
	metrics *Metrics
}

// NewBinder constructs a Binder. The provider is the same abstraction the gRPC
// server uses, so the binder works identically against Docker today and
// K8s+gVisor later with no changes here.
func NewBinder(kv rds.KV, p provider.SandboxProvider, cfg Config) *Binder {
	return &Binder{kv: kv, p: p, cfg: cfg.withDefaults()}
}

// EffectiveConfig returns the binder's config after defaults have been applied.
func (b *Binder) EffectiveConfig() Config { return b.cfg }

// WithMetrics attaches a metrics sink. Returns the binder for chaining.
func (b *Binder) WithMetrics(m *Metrics) *Binder {
	b.metrics = m
	return b
}

// WithNetworking sets the egress policy injected into every sandbox the binder
// creates. Returns the binder for chaining.
func (b *Binder) WithNetworking(n provider.Networking) *Binder {
	b.net = n
	return b
}

// AcquireSpec is what a caller needs to bind a session to a sandbox.
type AcquireSpec struct {
	SessionID string
	UserID    string
	Image     string
	Env       map[string]string
}

// Outcome reports the result of an Acquire: the bound sandbox and whether it
// was reused (hit) or freshly created (miss).
type Outcome struct {
	Sandbox *provider.Sandbox
	Reused  bool
}

// Acquire returns the sandbox bound to spec.SessionID, creating one if none
// exists. Convenience wrapper over AcquireWithOutcome for callers that don't
// care about the hit/miss signal.
func (b *Binder) Acquire(ctx context.Context, spec AcquireSpec) (*provider.Sandbox, error) {
	out, err := b.AcquireWithOutcome(ctx, spec)
	if err != nil {
		return nil, err
	}
	return out.Sandbox, nil
}

// AcquireWithOutcome is the heart of M2's "same session reuses same sandbox"
// guarantee.
//
// Fast path: a forward mapping already points at a live sandbox -> renew lease
// and return it (no lock taken). Reused=true.
//
// Slow path: no mapping. Take the per-session lock, re-check (another racer may
// have bound while we waited), then either resume a paused sandbox or create a
// fresh one, write the bidirectional mapping + lease atomically, and return.
// Reused=false only when this call actually created the sandbox.
func (b *Binder) AcquireWithOutcome(ctx context.Context, spec AcquireSpec) (Outcome, error) {
	if spec.SessionID == "" {
		return Outcome{}, errors.New("orchestrator: session id required")
	}

	// --- Fast path: existing binding -------------------------------------
	if sb, ok, err := b.lookup(ctx, spec.SessionID); err != nil {
		return Outcome{}, err
	} else if ok {
		if err := b.renewLease(ctx, sb.ID); err != nil {
			return Outcome{}, err
		}
		b.recordHit()
		return Outcome{Sandbox: sb, Reused: true}, nil
	}

	// --- Slow path: serialize creation on the per-session lock -----------
	lock, err := acquireLock(ctx, b.kv, spec.SessionID, b.cfg.LockTTL, b.cfg.LockRetry)
	if err != nil {
		return Outcome{}, fmt.Errorf("acquire lock: %w", err)
	}
	defer func() { _ = lock.release(ctx) }()

	// Double-check under lock: a racer may have bound while we waited.
	if sb, ok, err := b.lookup(ctx, spec.SessionID); err != nil {
		return Outcome{}, err
	} else if ok {
		if err := b.renewLease(ctx, sb.ID); err != nil {
			return Outcome{}, err
		}
		b.recordHit()
		return Outcome{Sandbox: sb, Reused: true}, nil
	}

	start := time.Now()
	sb, err := b.p.Create(ctx, provider.SandboxSpec{
		UserID:     spec.UserID,
		SessionID:  spec.SessionID,
		Image:      spec.Image,
		Env:        spec.Env,
		Networking: b.net,
	})
	if err != nil {
		return Outcome{}, fmt.Errorf("provider create: %w", err)
	}

	m := meta{
		SandboxID:   sb.ID,
		SessionID:   spec.SessionID,
		UserID:      spec.UserID,
		Image:       spec.Image,
		State:       StateActive,
		CreatedUnix: time.Now().Unix(),
	}
	if err := b.bind(ctx, m); err != nil {
		// Roll back the orphaned sandbox so a failed bind never leaks a container.
		_ = b.p.Destroy(ctx, sb.ID)
		return Outcome{}, fmt.Errorf("bind: %w", err)
	}

	b.recordMiss(time.Since(start))
	return Outcome{Sandbox: sb, Reused: false}, nil
}

// lookup resolves the forward mapping to a provider.Sandbox. The second return
// is false when the session has no binding. A dangling forward key (sandbox
// meta gone) is treated as "no binding" and cleaned opportunistically.
func (b *Binder) lookup(ctx context.Context, sessionID string) (*provider.Sandbox, bool, error) {
	sid, err := b.kv.Get(ctx, convKey(sessionID))
	if errors.Is(err, rds.ErrNil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	raw, err := b.kv.Get(ctx, metaKey(sid))
	if errors.Is(err, rds.ErrNil) {
		// Forward pointer survived but the sandbox record is gone — stale. Drop it.
		_, _ = b.kv.Del(ctx, convKey(sessionID))
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var m meta
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, false, err
	}
	// If the sandbox was paused (stage-1 reclaim), resurrect it so the session
	// resumes on its existing workspace rather than cold-creating.
	if m.State == StatePaused {
		if err := b.p.Resume(ctx, sid); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// The provider has already forgotten the paused sandbox (for
				// example after an OpenSandbox/Docker restart) while our durable
				// binding still points at it. Drop the stale binding and let
				// Acquire create a fresh sandbox for the same session.
				if unbindErr := b.unbind(ctx, m.SessionID, m.SandboxID); unbindErr != nil {
					return nil, false, unbindErr
				}
				return nil, false, nil
			}
			return nil, false, fmt.Errorf("resume paused sandbox: %w", err)
		}
		m.State = StateActive
		m.PausedUnix = 0
		if err := b.putMeta(ctx, m); err != nil {
			return nil, false, err
		}
	}
	return &provider.Sandbox{
		ID:        m.SandboxID,
		UserID:    m.UserID,
		SessionID: m.SessionID,
	}, true, nil
}

// bind atomically writes the bidirectional mapping + meta + lease for a freshly
// created sandbox. Uses a Lua script so a crash mid-write can't leave half a
// mapping (e.g. forward without reverse).
func (b *Binder) bind(ctx context.Context, m meta) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	leaseSecs := int(b.cfg.LeaseTTL.Seconds())
	// KEYS: conv, rev, meta, lease   ARGV: sandboxID, sessionID, metaJSON, leaseTTL
	_, err = b.kv.Eval(ctx, luaBind,
		[]string{convKey(m.SessionID), revKey(m.SandboxID), metaKey(m.SandboxID), leaseKey(m.SandboxID)},
		m.SandboxID, m.SessionID, string(raw), leaseSecs,
	)
	return err
}

// luaBind writes all four binding keys in one round trip. conv/rev/meta are
// durable (no TTL); only the lease expires.
const luaBind = `
redis.call("SET", KEYS[1], ARGV[1])
redis.call("SET", KEYS[2], ARGV[2])
redis.call("SET", KEYS[3], ARGV[3])
redis.call("SET", KEYS[4], "1", "EX", tonumber(ARGV[4]))
return 1`

// putMeta overwrites the durable meta record (used on state transitions).
func (b *Binder) putMeta(ctx context.Context, m meta) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return b.kv.Set(ctx, metaKey(m.SandboxID), string(raw), 0)
}

// renewLease pushes a sandbox's lease TTL forward. Called on every Acquire hit
// and by the heartbeat worker.
func (b *Binder) renewLease(ctx context.Context, sandboxID string) error {
	// Re-create rather than EXPIRE: a sandbox briefly past its lease (but not
	// yet reaped) must still be renewable on a fresh request.
	return b.kv.Set(ctx, leaseKey(sandboxID), "1", b.cfg.LeaseTTL)
}

// Release explicitly unbinds and destroys a session's sandbox immediately,
// bypassing the lease grace period. Used when a session ends cleanly.
func (b *Binder) Release(ctx context.Context, sessionID string) error {
	sid, err := b.kv.Get(ctx, convKey(sessionID))
	if errors.Is(err, rds.ErrNil) {
		return nil // nothing bound
	}
	if err != nil {
		return err
	}
	// Destroy the sandbox first; even if mapping cleanup fails afterwards the
	// reaper will mop up the now-dangling record.
	if err := b.p.Destroy(ctx, sid); err != nil {
		return fmt.Errorf("provider destroy: %w", err)
	}
	return b.unbind(ctx, sessionID, sid)
}

// unbind removes all four keys for a (session, sandbox) pair atomically.
func (b *Binder) unbind(ctx context.Context, sessionID, sandboxID string) error {
	_, err := b.kv.Del(ctx,
		convKey(sessionID), revKey(sandboxID), metaKey(sandboxID), leaseKey(sandboxID))
	return err
}

func (b *Binder) recordHit() {
	if b.metrics != nil {
		b.metrics.recordHit()
	}
}
func (b *Binder) recordMiss(d time.Duration) {
	if b.metrics != nil {
		b.metrics.recordMiss(d)
	}
}
