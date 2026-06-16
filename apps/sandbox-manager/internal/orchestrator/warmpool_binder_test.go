package orchestrator

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/orchestrator/warmpool"
	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
	rds "github.com/cocola-project/cocola/packages/go-common/redis"
)

// adoptableProvider wraps fakeProvider with the optional provider.Adopter
// capability so we can exercise the binder's warm-pool adopt path. adopts counts
// successful re-targets; failAdopt forces a single adopt failure.
type adoptableProvider struct {
	*fakeProvider
	adopts    atomic.Int64
	failAdopt atomic.Bool
}

func newAdoptable() *adoptableProvider {
	return &adoptableProvider{fakeProvider: newFakeProvider()}
}

func (a *adoptableProvider) Adopt(ctx context.Context, sid string, spec provider.SandboxSpec) error {
	if a.failAdopt.Swap(false) {
		return context.DeadlineExceeded
	}
	a.adopts.Add(1)
	return nil
}

func newPooledBinder(t *testing.T, p provider.SandboxProvider, poolCfg warmpool.Config) (*Binder, *warmpool.Pool) {
	t.Helper()
	kv := rds.NewFake()
	b := NewBinder(kv, p, Config{
		LeaseTTL:       2 * time.Second,
		HeartbeatEvery: time.Second,
		DestroyGrace:   2 * time.Second,
		LockTTL:        2 * time.Second,
		ReaperEvery:    200 * time.Millisecond,
		LockRetry:      5 * time.Millisecond,
	}).WithMetrics(NewMetrics())
	pool := warmpool.New(kv, p, poolCfg)
	b.WithWarmPool(pool)
	return b, pool
}

// TestPoolDisabledSameAsCold: with the pool disabled the miss path cold-creates
// exactly as before — one create per new session, zero adopts.
func TestPoolDisabledSameAsCold(t *testing.T) {
	ctx := context.Background()
	ap := newAdoptable()
	b, _ := newPooledBinder(t, ap, warmpool.Config{Enabled: false})

	out, err := b.AcquireWithOutcome(ctx, AcquireSpec{SessionID: "s1", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Reused {
		t.Fatal("first acquire should not be a reuse")
	}
	if ap.creates.Load() != 1 || ap.adopts.Load() != 0 {
		t.Fatalf("disabled pool: creates=%d adopts=%d, want 1/0", ap.creates.Load(), ap.adopts.Load())
	}
	if snap := b.metrics.Snapshot(); snap.PooledCount != 0 {
		t.Fatalf("pooled count=%d, want 0 when disabled", snap.PooledCount)
	}
}

// TestPoolAdoptOnMiss: with a warmed pool and an adopter-capable provider, a new
// session adopts a pre-warmed box instead of cold-creating. The only Create
// calls are the pool's own warming, and the box is adopted (not freshly made on
// the request path).
func TestPoolAdoptOnMiss(t *testing.T) {
	ctx := context.Background()
	ap := newAdoptable()
	b, pool := newPooledBinder(t, ap, warmpool.Config{Enabled: true, MinIdle: 2, Max: 4, Image: "img"})

	// Warm the pool: 2 creates happen here, off the request path.
	if err := pool.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	createsAfterWarm := ap.creates.Load()
	if createsAfterWarm != 2 {
		t.Fatalf("warm creates=%d, want 2", createsAfterWarm)
	}

	out, err := b.AcquireWithOutcome(ctx, AcquireSpec{SessionID: "s1", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Reused {
		t.Fatal("adopt is a fresh bind, Reused must be false")
	}
	// No NEW create on the request path; exactly one adopt.
	if ap.creates.Load() != createsAfterWarm {
		t.Fatalf("request-path create happened: creates went %d -> %d", createsAfterWarm, ap.creates.Load())
	}
	if ap.adopts.Load() != 1 {
		t.Fatalf("adopts=%d, want 1", ap.adopts.Load())
	}
	if out.Sandbox.UserID != "u" || out.Sandbox.SessionID != "s1" {
		t.Fatalf("adopted box not re-targeted: %+v", out.Sandbox)
	}
	if snap := b.metrics.Snapshot(); snap.PooledCount != 1 {
		t.Fatalf("pooled count=%d, want 1", snap.PooledCount)
	}
	// The adopted box is now bound: a second acquire for the same session reuses it.
	out2, err := b.AcquireWithOutcome(ctx, AcquireSpec{SessionID: "s1", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if !out2.Reused || out2.Sandbox.ID != out.Sandbox.ID {
		t.Fatalf("second acquire should reuse the adopted box, got %+v", out2)
	}
}

// TestPoolEmptyDegradesToCold: an enabled-but-empty pool falls back to a normal
// cold Create with no error surfaced to the caller.
func TestPoolEmptyDegradesToCold(t *testing.T) {
	ctx := context.Background()
	ap := newAdoptable()
	b, _ := newPooledBinder(t, ap, warmpool.Config{Enabled: true, MinIdle: 2, Max: 4, Image: "img"})

	out, err := b.AcquireWithOutcome(ctx, AcquireSpec{SessionID: "s1", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Sandbox == nil || ap.creates.Load() != 1 || ap.adopts.Load() != 0 {
		t.Fatalf("empty pool should cold-create: creates=%d adopts=%d", ap.creates.Load(), ap.adopts.Load())
	}
}

// TestAdoptFailureDegradesToCold: if Adopt fails after checkout, the warm box is
// destroyed and the request falls back to a clean cold Create — no leak, no error.
func TestAdoptFailureDegradesToCold(t *testing.T) {
	ctx := context.Background()
	ap := newAdoptable()
	b, pool := newPooledBinder(t, ap, warmpool.Config{Enabled: true, MinIdle: 1, Max: 2, Image: "img"})
	if err := pool.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	warmCreates := ap.creates.Load()
	ap.failAdopt.Store(true)

	out, err := b.AcquireWithOutcome(ctx, AcquireSpec{SessionID: "s1", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Sandbox == nil {
		t.Fatal("expected a sandbox from cold fallback")
	}
	// The orphaned warm box was destroyed, and a cold Create produced the bound box.
	if ap.destroys.Load() != 1 {
		t.Fatalf("destroys=%d, want 1 (orphan cleaned)", ap.destroys.Load())
	}
	if ap.creates.Load() != warmCreates+1 {
		t.Fatalf("expected one cold create after adopt failure, creates=%d", ap.creates.Load())
	}
	if snap := b.metrics.Snapshot(); snap.PooledCount != 0 {
		t.Fatalf("pooled count=%d, want 0 (adopt failed)", snap.PooledCount)
	}
}

// TestNonAdopterProviderSkipsPool: a provider WITHOUT the Adopter capability
// never takes from the pool — warm boxes aren't adoptable, so every miss
// cold-creates. (fakeProvider does not implement provider.Adopter.)
func TestNonAdopterProviderSkipsPool(t *testing.T) {
	ctx := context.Background()
	fp := newFakeProvider()
	b, pool := newPooledBinder(t, fp, warmpool.Config{Enabled: true, MinIdle: 2, Max: 4, Image: "img"})
	if err := pool.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	warmCreates := fp.creates.Load()

	out, err := b.AcquireWithOutcome(ctx, AcquireSpec{SessionID: "s1", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Sandbox == nil || fp.creates.Load() != warmCreates+1 {
		t.Fatalf("non-adopter must cold-create: creates=%d (warm was %d)", fp.creates.Load(), warmCreates)
	}
	if snap := b.metrics.Snapshot(); snap.PooledCount != 0 {
		t.Fatalf("pooled count=%d, want 0 for non-adopter provider", snap.PooledCount)
	}
}
