package warmpool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
	rds "github.com/cocola-project/cocola/packages/go-common/redis"
)

// fakeProvider is a minimal in-memory SandboxProvider. It counts creates and
// destroys and lets a test inject a create error.
type fakeProvider struct {
	creates  atomic.Int64
	destroys atomic.Int64
	mu       sync.Mutex
	state    map[string]string // id -> "active"|"destroyed"
	failNext atomic.Bool
}

func newFakeProvider() *fakeProvider { return &fakeProvider{state: map[string]string{}} }

func (f *fakeProvider) Create(ctx context.Context, spec provider.SandboxSpec) (*provider.Sandbox, error) {
	if f.failNext.Swap(false) {
		return nil, fmt.Errorf("injected create failure")
	}
	n := f.creates.Add(1)
	id := fmt.Sprintf("sbx-%d", n)
	f.mu.Lock()
	f.state[id] = "active"
	f.mu.Unlock()
	return &provider.Sandbox{ID: id, UserID: spec.UserID, SessionID: spec.SessionID, Endpoint: "ep-" + id}, nil
}
func (f *fakeProvider) Destroy(ctx context.Context, sid string) error {
	f.destroys.Add(1)
	f.mu.Lock()
	f.state[sid] = "destroyed"
	f.mu.Unlock()
	return nil
}
func (f *fakeProvider) Exec(ctx context.Context, sid string, req provider.ExecRequest) (<-chan provider.ExecEvent, error) {
	return nil, nil
}
func (f *fakeProvider) WriteFile(ctx context.Context, sid, path string, data []byte) error {
	return nil
}
func (f *fakeProvider) ReadFile(ctx context.Context, sid, path string) ([]byte, error) {
	return nil, nil
}
func (f *fakeProvider) Pause(ctx context.Context, sid string) error  { return nil }
func (f *fakeProvider) Resume(ctx context.Context, sid string) error { return nil }
func (f *fakeProvider) Health(ctx context.Context, sid string) (*provider.HealthStatus, error) {
	return &provider.HealthStatus{Healthy: true}, nil
}

func newTestPool(t *testing.T, cfg Config) (*Pool, *fakeProvider, rds.KV) {
	t.Helper()
	kv := rds.NewFake()
	fp := newFakeProvider()
	cfg.Enabled = true
	if cfg.Image == "" {
		cfg.Image = "cocola/sandbox:test"
	}
	p := New(kv, fp, cfg)
	return p, fp, kv
}

// TestDisabledPoolIsNoop: a disabled pool never warms and always misses on
// checkout, so the binder always falls back to a normal create.
func TestDisabledPoolIsNoop(t *testing.T) {
	kv := rds.NewFake()
	fp := newFakeProvider()
	p := New(kv, fp, Config{Enabled: false, MinIdle: 3})
	if p.Enabled() {
		t.Fatal("pool should be disabled")
	}
	if err := p.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	// Run returns immediately when disabled.
	p.Run(context.Background())
	if got := fp.creates.Load(); got != 0 {
		t.Fatalf("disabled pool created %d sandboxes, want 0", got)
	}
	sb, ok, err := p.Checkout(context.Background())
	if err != nil || ok || sb != nil {
		t.Fatalf("disabled checkout = (%v,%v,%v), want (nil,false,nil)", sb, ok, err)
	}
}

// TestRefillConvergesToMinIdle: one tick warms exactly MinIdle sandboxes and a
// second tick is a no-op (already at target).
func TestRefillConvergesToMinIdle(t *testing.T) {
	ctx := context.Background()
	p, fp, _ := newTestPool(t, Config{MinIdle: 3, Max: 10})

	if err := p.tick(ctx); err != nil {
		t.Fatalf("tick1: %v", err)
	}
	if got := fp.creates.Load(); got != 3 {
		t.Fatalf("after tick1 creates=%d, want 3", got)
	}
	if n, _ := p.Size(ctx); n != 3 {
		t.Fatalf("idle size=%d, want 3", n)
	}
	// Second tick: already at MinIdle, no new creates.
	if err := p.tick(ctx); err != nil {
		t.Fatalf("tick2: %v", err)
	}
	if got := fp.creates.Load(); got != 3 {
		t.Fatalf("after tick2 creates=%d, want 3 (no overshoot)", got)
	}
}

// TestRefillRespectsMax: MinIdle clamps to Max so the pool never overshoots the
// hard cap.
func TestRefillRespectsMax(t *testing.T) {
	ctx := context.Background()
	p, fp, _ := newTestPool(t, Config{MinIdle: 8, Max: 4})
	if err := p.tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if got := fp.creates.Load(); got != 4 {
		t.Fatalf("creates=%d, want capped at Max=4", got)
	}
}

// TestCheckoutClaimsAndShrinks: a checkout returns a warmed sandbox and removes
// it from the idle set.
func TestCheckoutClaimsAndShrinks(t *testing.T) {
	ctx := context.Background()
	p, _, _ := newTestPool(t, Config{MinIdle: 2, Max: 5})
	if err := p.tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	sb, ok, err := p.Checkout(ctx)
	if err != nil || !ok || sb == nil {
		t.Fatalf("checkout = (%v,%v,%v), want a hit", sb, ok, err)
	}
	if sb.ID == "" || sb.Endpoint == "" {
		t.Fatalf("checked-out sandbox missing fields: %+v", sb)
	}
	if n, _ := p.Size(ctx); n != 1 {
		t.Fatalf("idle size after checkout=%d, want 1", n)
	}
}

// TestCheckoutEmptyPoolMisses: checking out an empty (but enabled) pool returns
// a clean miss so the caller degrades to Create.
func TestCheckoutEmptyPoolMisses(t *testing.T) {
	ctx := context.Background()
	p, _, _ := newTestPool(t, Config{MinIdle: 2, Max: 5})
	sb, ok, err := p.Checkout(ctx)
	if err != nil || ok || sb != nil {
		t.Fatalf("empty checkout = (%v,%v,%v), want (nil,false,nil)", sb, ok, err)
	}
}

// TestConcurrentCheckoutNoDoubleHandout: N concurrent checkouts over a pool of M
// idle boxes hand out exactly min(N,M) DISTINCT sandboxes — never the same box
// twice (the Del-CAS guarantees a single winner per key).
func TestConcurrentCheckoutNoDoubleHandout(t *testing.T) {
	ctx := context.Background()
	p, _, _ := newTestPool(t, Config{MinIdle: 5, Max: 5})
	if err := p.tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	const racers = 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[string]int{}
	hits := atomic.Int64{}
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sb, ok, err := p.Checkout(ctx)
			if err != nil || !ok {
				return
			}
			hits.Add(1)
			mu.Lock()
			seen[sb.ID]++
			mu.Unlock()
		}()
	}
	wg.Wait()

	if hits.Load() != 5 {
		t.Fatalf("hits=%d, want 5 (pool size)", hits.Load())
	}
	for id, c := range seen {
		if c != 1 {
			t.Fatalf("sandbox %s handed out %d times, want exactly 1", id, c)
		}
	}
	if len(seen) != 5 {
		t.Fatalf("distinct sandboxes handed out=%d, want 5", len(seen))
	}
}

// TestAgeOutDestroysStale: idle boxes older than MaxLifetime are claimed and
// destroyed on a tick; fresh ones survive.
func TestAgeOutDestroysStale(t *testing.T) {
	ctx := context.Background()
	p, fp, _ := newTestPool(t, Config{MinIdle: 3, Max: 3, MaxLifetime: time.Hour})

	// Freeze clock 2h in the past, warm the pool -> all entries are "old".
	base := time.Now()
	p.clock = func() time.Time { return base.Add(-2 * time.Hour) }
	if err := p.refill(ctx); err != nil {
		t.Fatalf("refill: %v", err)
	}
	if n, _ := p.Size(ctx); n != 3 {
		t.Fatalf("pre-ageout size=%d, want 3", n)
	}

	// Advance clock to now: every entry now exceeds MaxLifetime=1h.
	p.clock = func() time.Time { return base }
	p.ageOut(ctx)
	if got := fp.destroys.Load(); got != 3 {
		t.Fatalf("destroys=%d, want 3 (all aged out)", got)
	}
	if n, _ := p.Size(ctx); n != 0 {
		t.Fatalf("post-ageout size=%d, want 0", n)
	}
}

// TestWarmFailureDoesNotLeak: a create failure surfaces from refill and leaves
// no idle marker behind (and a publish failure would destroy the orphan).
func TestWarmFailureDoesNotLeak(t *testing.T) {
	ctx := context.Background()
	p, fp, _ := newTestPool(t, Config{MinIdle: 2, Max: 2})
	fp.failNext.Store(true)
	if err := p.refill(ctx); err == nil {
		t.Fatal("refill should surface the injected create error")
	}
	// The failed create published nothing; a later tick recovers to MinIdle.
	if err := p.refill(ctx); err != nil {
		t.Fatalf("recovery refill: %v", err)
	}
	if n, _ := p.Size(ctx); n != 2 {
		t.Fatalf("post-recovery size=%d, want 2", n)
	}
}

// TestWithDefaults: zero-value config is filled with sane defaults and Max is
// never below MinIdle.
func TestWithDefaults(t *testing.T) {
	c := Config{MinIdle: 20, Max: 5}.withDefaults()
	if c.Max < c.MinIdle {
		t.Fatalf("Max=%d < MinIdle=%d, should be clamped up", c.Max, c.MinIdle)
	}
	d := Config{}.withDefaults()
	if d.MinIdle != DefaultMinIdle || d.Max != DefaultMax || d.RefillEvery != DefaultRefillEvery || d.MaxLifetime != DefaultMaxLifetime {
		t.Fatalf("zero config defaults wrong: %+v", d)
	}
}
