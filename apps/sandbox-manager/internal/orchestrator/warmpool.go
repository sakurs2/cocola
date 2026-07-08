package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
)

// Warm-pool defaults. The user picked a default target of 10 pre-warmed
// sandboxes; the refill cadence is fast enough that a burst of cold-starts is
// replenished within a few seconds without hammering the backend.
const (
	// DefaultWarmPoolSize is the target inventory of pre-warmed sandboxes when
	// the pool is enabled and no admin/env override narrows it.
	DefaultWarmPoolSize = 10
	// DefaultWarmRefillEvery is how often the refill loop reconciles the pool
	// toward its target size.
	DefaultWarmRefillEvery = 5 * time.Second
	// warmCreateBudget caps how many warm sandboxes a single refill tick will
	// create, so a large deficit (e.g. right after startup) is filled over a few
	// ticks instead of one thundering herd against the backend.
	warmCreateBudget = 3
	// warmClaimVerify bounds the per-claim Health probe so a stuck backend can't
	// stall an Acquire that is trying to claim from the pool.
	warmClaimVerify = 3 * time.Second
)

// WarmConfig is the pre-warm pool's provisioning + sizing configuration. Sizing
// (Enabled/Size) is the admin-tunable part and can be overridden at runtime via
// a shared Redis key (see effectiveWarmSizing); Image/Env are process-level
// provisioning inputs the background loop needs to create session-agnostic
// sandboxes on its own (agent-runtime is not in the loop for warm creates, so
// sandbox-manager must carry the brain image + LLM credentials itself).
type WarmConfig struct {
	Enabled     bool
	Size        int
	RefillEvery time.Duration
	Image       string
	Env         map[string]string
}

// WarmConfigFromEnv reads the warm-pool provisioning + default sizing from the
// environment. Sizing defaults to enabled with DefaultWarmPoolSize so a stock
// deployment gets a warm pool out of the box; the admin config page (via the
// shared Redis override) can flip it off or resize it without a restart.
//
// The LLM credentials + brain image mirror what agent-runtime injects on the
// Acquire path (COCOLA_SANDBOX_IMAGE / COCOLA_SANDBOX_LLM_BASE_URL /
// COCOLA_SANDBOX_LLM_TOKEN / COCOLA_SANDBOX_MODEL_ALIAS). They are static and
// cluster-wide, so a warm sandbox created here is credential-identical to one a
// session would have cold-created — a later claim is transparent.
func WarmConfigFromEnv() WarmConfig {
	env := map[string]string{}
	if v := strings.TrimSpace(os.Getenv("COCOLA_SANDBOX_LLM_BASE_URL")); v != "" {
		env["ANTHROPIC_BASE_URL"] = v
	}
	if v := strings.TrimSpace(os.Getenv("COCOLA_SANDBOX_LLM_TOKEN")); v != "" {
		env["ANTHROPIC_AUTH_TOKEN"] = v
	}
	if v := strings.TrimSpace(os.Getenv("COCOLA_SANDBOX_MODEL_ALIAS")); v != "" {
		env["ANTHROPIC_MODEL"] = v
		env["ANTHROPIC_SMALL_FAST_MODEL"] = v
	}
	return WarmConfig{
		Enabled:     envBoolDefault("COCOLA_SANDBOX_WARM_POOL_ENABLED", true),
		Size:        envIntDefault("COCOLA_SANDBOX_WARM_POOL_SIZE", DefaultWarmPoolSize),
		RefillEvery: envSecs("COCOLA_SANDBOX_WARM_POOL_REFILL_SECS", DefaultWarmRefillEvery),
		Image:       strings.TrimSpace(os.Getenv("COCOLA_SANDBOX_IMAGE")),
		Env:         env,
	}
}

func envBoolDefault(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func envIntDefault(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return def
}

// WithWarmPool attaches warm-pool provisioning + default sizing. Returns the
// binder for chaining. A warm pool with an empty Image is inert (the loop can't
// create session-agnostic sandboxes without a brain image), which keeps local
// single-process debugging free of surprise sandbox creation.
func (b *Binder) WithWarmPool(cfg WarmConfig) *Binder {
	if cfg.RefillEvery <= 0 {
		cfg.RefillEvery = DefaultWarmRefillEvery
	}
	b.warm = &cfg
	return b
}

// WarmEnabled reports whether a usable warm pool is configured (feature on and a
// brain image to create from).
func (b *Binder) WarmEnabled() bool {
	return b.warm != nil && b.warm.Image != ""
}

// warmSizing is the runtime-effective (enabled,size) after the admin override.
type warmSizing struct {
	Enabled bool `json:"enabled"`
	Size    int  `json:"size"`
}

// effectiveWarmSizing resolves the live warm-pool sizing: a shared Redis config
// key written by admin-api (hot-reload from the admin config page) wins over the
// process env/default baked into b.warm. A malformed/absent key falls back to
// the baseline so a bad write can never wedge the pool.
func (b *Binder) effectiveWarmSizing(ctx context.Context) warmSizing {
	base := warmSizing{Enabled: b.warm.Enabled, Size: b.warm.Size}
	raw, err := b.kv.Get(ctx, warmConfigKey)
	if err != nil || raw == "" {
		return base
	}
	var override warmSizing
	if err := json.Unmarshal([]byte(raw), &override); err != nil {
		return base
	}
	if override.Size < 0 {
		override.Size = 0
	}
	return override
}

// pruneDeadWarm probes every recorded warm sandbox and drops the Redis record of
// any that is unreachable/dead (issuing a best-effort Destroy for a half-dead
// pod), returning the ids that remain alive. It only touches records via an
// atomic DEL claim, so no racer double-drains and it is safe under concurrent
// replicas. This is the per-tick self-heal: it repairs the case where a warm
// key still exists but its pod was deleted out-of-band (e.g. an admin manual
// delete or a node drain), so the refill loop's count never counts a phantom.
func (b *Binder) pruneDeadWarm(ctx context.Context, ids []string) []string {
	alive := ids[:0:0]
	for _, id := range ids {
		hctx, cancel := context.WithTimeout(ctx, warmClaimVerify)
		_, herr := b.p.Health(hctx, id)
		cancel()
		if herr == nil {
			alive = append(alive, id)
			continue // still alive: keep it in the pool
		}
		// Dead or unreachable. Claim the key (atomic DEL returns 1 for exactly
		// one caller) so no racer double-drains, then best-effort tear down the
		// pod unless the provider already lost it.
		n, derr := b.kv.Del(ctx, warmKey(id))
		if derr != nil || n != 1 {
			continue
		}
		if !errors.Is(herr, fs.ErrNotExist) {
			_ = b.p.Destroy(ctx, id)
		}
	}
	return alive
}

// RunWarmPool reconciles the pre-warm inventory toward its target on
// RefillEvery until ctx is cancelled. Spawn one per process; a shared refill
// lock keeps concurrent replicas from each creating a full pool. The first
// reconcile runs immediately (synchronously via the initial call below) and
// doubles as the startup self-check: because every tick probes health and
// prunes dead warm records, a restart with stale warm keys is healed by the
// first tick — no separate startup reconcile pass is needed.
func (b *Binder) RunWarmPool(ctx context.Context) {
	if !b.WarmEnabled() {
		return
	}
	t := time.NewTicker(b.warm.RefillEvery)
	defer t.Stop()
	// Reconcile once immediately so the pool starts filling (and self-heals)
	// at boot rather than after the first tick.
	b.reconcileWarmOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.reconcileWarmOnce(ctx)
		}
	}
}

// reconcileWarmOnce is the per-tick self-heal + resize: under a short
// cross-replica lock it (1) probes every recorded warm sandbox and prunes dead
// ones (repairing warm keys whose pod was deleted out-of-band), then (2) brings
// the surviving inventory in line with the effective target — draining surplus
// (or everything, when disabled) and refilling a deficit. The health probe runs
// every tick (not just at startup) so a manually deleted / drained pod is
// reconciled within one RefillEvery instead of lingering as a phantom.
func (b *Binder) reconcileWarmOnce(ctx context.Context) {
	sizing := b.effectiveWarmSizing(ctx)
	target := sizing.Size
	if !sizing.Enabled {
		target = 0
	}

	// Only one replica mutates the pool per tick. We take the lock up front
	// (rather than after a count fast-path) because the per-tick health probe
	// must be serialized too — otherwise two replicas would double-probe and
	// race on pruning the same dead key.
	token := newToken()
	ok, err := b.kv.SetNX(ctx, warmRefillLockKey, token, 10*time.Second)
	if err != nil || !ok {
		return
	}
	defer func() { _, _ = b.kv.Eval(ctx, luaUnlock, []string{warmRefillLockKey}, token) }()

	ids, err := b.listWarm(ctx)
	if err != nil {
		return
	}
	// Self-heal: drop any warm record whose sandbox is gone/unreachable, so the
	// sizing step below counts only sandboxes that actually exist.
	ids = b.pruneDeadWarm(ctx, ids)

	switch {
	case len(ids) > target:
		b.drainWarm(ctx, ids[target:])
	case len(ids) < target:
		deficit := target - len(ids)
		if deficit > warmCreateBudget {
			deficit = warmCreateBudget
		}
		for i := 0; i < deficit; i++ {
			if err := b.createOneWarm(ctx); err != nil {
				// Capacity busy or backend error: stop this tick, retry next.
				return
			}
		}
	}
}

// listWarm returns the sandbox ids currently in the warm inventory.
func (b *Binder) listWarm(ctx context.Context) ([]string, error) {
	var ids []string
	err := b.kv.ScanKeys(ctx, warmScanPattern(), 100, func(keys []string) error {
		for _, k := range keys {
			if id := strings.TrimPrefix(k, warmPrefix); id != "" {
				ids = append(ids, id)
			}
		}
		return nil
	})
	return ids, err
}

// createOneWarm provisions a single session-agnostic warm sandbox and records it
// in the inventory. Multi-node distribution reuses the capacity guard: each warm
// create picks the node with the most remaining capacity, and because the guard
// counts already-running sandbox pods, successive warm creates spread across
// nodes automatically. ErrCapacityBusy is surfaced so the caller stops filling.
func (b *Binder) createOneWarm(ctx context.Context) error {
	targetNode := ""
	if b.cap != nil {
		node, err := b.cap.SelectNode(ctx)
		if err != nil {
			return err
		}
		targetNode = node
	}
	sb, err := b.p.Create(ctx, provider.SandboxSpec{
		Image:          b.warm.Image,
		Env:            b.warm.Env,
		Networking:     b.net,
		TargetNodeName: targetNode,
		Warm:           true,
	})
	if err != nil {
		return err
	}
	wm := warmMeta{
		SandboxID:   sb.ID,
		Image:       b.warm.Image,
		NodeName:    targetNode,
		CreatedUnix: time.Now().Unix(),
	}
	raw, err := json.Marshal(wm)
	if err != nil {
		_ = b.p.Destroy(ctx, sb.ID)
		return err
	}
	if err := b.kv.Set(ctx, warmKey(sb.ID), string(raw), 0); err != nil {
		// Roll back so a create that we failed to record never leaks a sandbox.
		_ = b.p.Destroy(ctx, sb.ID)
		return err
	}
	return nil
}

// drainWarm removes surplus warm sandboxes: claim each key (atomic DEL) so no
// racer also destroys it, then tear the sandbox down. Best-effort per sandbox.
func (b *Binder) drainWarm(ctx context.Context, ids []string) {
	for _, id := range ids {
		n, err := b.kv.Del(ctx, warmKey(id))
		if err != nil || n != 1 {
			continue // someone else claimed/drained it
		}
		_ = b.p.Destroy(ctx, id)
	}
}

// claimWarm atomically removes one healthy sandbox from the warm inventory and
// returns it for immediate binding. The claim is a DEL that returns 1 for
// exactly one caller, so concurrent Acquires never hand the same warm sandbox to
// two sessions. A claimed-but-unhealthy sandbox is destroyed and the scan
// continues. Returns ok=false when the pool is empty or unusable.
func (b *Binder) claimWarm(ctx context.Context, spec AcquireSpec) (*provider.Sandbox, bool) {
	if !b.WarmEnabled() {
		return nil, false
	}
	ids, err := b.listWarm(ctx)
	if err != nil {
		return nil, false
	}
	for _, id := range ids {
		n, err := b.kv.Del(ctx, warmKey(id))
		if err != nil || n != 1 {
			continue // lost the race for this one
		}
		// We own it. Verify it is actually alive before handing it to a session.
		hctx, cancel := context.WithTimeout(ctx, warmClaimVerify)
		_, herr := b.p.Health(hctx, id)
		cancel()
		if herr != nil {
			if errors.Is(herr, fs.ErrNotExist) {
				// Provider already lost it; nothing to tear down.
				continue
			}
			_ = b.p.Destroy(ctx, id)
			continue
		}
		return &provider.Sandbox{
			ID:        id,
			UserID:    spec.UserID,
			SessionID: spec.SessionID,
		}, true
	}
	return nil, false
}
