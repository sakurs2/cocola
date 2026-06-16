package orchestrator

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

// fakeProvider is a minimal in-memory SandboxProvider for binder tests. It only
// implements the lifecycle methods the binder touches; the rest are stubs.
type fakeProvider struct {
	creates  atomic.Int64
	destroys atomic.Int64
	mu       sync.Mutex
	state    map[string]string // sandbox id -> "active"|"paused"|"destroyed"
}

func newFakeProvider() *fakeProvider { return &fakeProvider{state: map[string]string{}} }

func (f *fakeProvider) Create(ctx context.Context, spec provider.SandboxSpec) (*provider.Sandbox, error) {
	n := f.creates.Add(1)
	id := fmt.Sprintf("sbx-%d", n)
	f.mu.Lock()
	f.state[id] = "active"
	f.mu.Unlock()
	return &provider.Sandbox{ID: id, UserID: spec.UserID, SessionID: spec.SessionID}, nil
}
func (f *fakeProvider) Pause(ctx context.Context, sid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state[sid] = "paused"
	return nil
}
func (f *fakeProvider) Resume(ctx context.Context, sid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state[sid] = "active"
	return nil
}
func (f *fakeProvider) Destroy(ctx context.Context, sid string) error {
	f.destroys.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state[sid] = "destroyed"
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
func (f *fakeProvider) Health(ctx context.Context, sid string) (*provider.HealthStatus, error) {
	return &provider.HealthStatus{Healthy: true}, nil
}

func newTestBinder(t *testing.T) (*Binder, *fakeProvider) {
	t.Helper()
	kv := rds.NewFake()
	fp := newFakeProvider()
	b := NewBinder(kv, fp, Config{
		LeaseTTL:       2 * time.Second,
		HeartbeatEvery: time.Second,
		DestroyGrace:   2 * time.Second,
		LockTTL:        2 * time.Second,
		ReaperEvery:    200 * time.Millisecond,
		LockRetry:      5 * time.Millisecond,
	})
	return b, fp
}

// TestAcquireReusesSameSandbox: two sequential acquires for one session return
// the same sandbox and trigger exactly one create.
func TestAcquireReusesSameSandbox(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	sb1, err := b.Acquire(ctx, AcquireSpec{SessionID: "s1", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	sb2, err := b.Acquire(ctx, AcquireSpec{SessionID: "s1", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if sb1.ID != sb2.ID {
		t.Fatalf("expected same sandbox, got %s and %s", sb1.ID, sb2.ID)
	}
	if got := fp.creates.Load(); got != 1 {
		t.Fatalf("expected 1 create, got %d", got)
	}
}

// TestConcurrentAcquireConverges: K concurrent acquires for one new session
// converge on a single sandbox (one create).
func TestConcurrentAcquireConverges(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	const k = 16
	ids := make([]string, k)
	var wg sync.WaitGroup
	for i := 0; i < k; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sb, err := b.Acquire(ctx, AcquireSpec{SessionID: "race", UserID: "u"})
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = sb.ID
		}(i)
	}
	wg.Wait()
	for i := 1; i < k; i++ {
		if ids[i] != ids[0] {
			t.Fatalf("divergent sandbox ids: %s vs %s", ids[0], ids[i])
		}
	}
	if got := fp.creates.Load(); got != 1 {
		t.Fatalf("expected exactly 1 create under contention, got %d", got)
	}
}

// TestDistinctSessionsDistinctSandboxes: N sessions => N sandboxes.
func TestDistinctSessionsDistinctSandboxes(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	const n = 25
	seen := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sb, err := b.Acquire(ctx, AcquireSpec{SessionID: fmt.Sprintf("s-%d", i), UserID: "u"})
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			seen[sb.ID] = true
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if len(seen) != n {
		t.Fatalf("expected %d distinct sandboxes, got %d", n, len(seen))
	}
	if got := fp.creates.Load(); got != n {
		t.Fatalf("expected %d creates, got %d", n, got)
	}
}

// TestReaperPauseThenDestroy: an idle sandbox is first paused, then destroyed.
func TestReaperPauseThenDestroy(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	sb, err := b.Acquire(ctx, AcquireSpec{SessionID: "idle", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	id := sb.ID

	// Stage 1: after the lease lapses, a reap pass should pause it.
	deadline := time.Now().Add(8 * time.Second)
	sawPaused := false
	for time.Now().Before(deadline) {
		_ = b.reapOnce(ctx, time.Now())
		fp.mu.Lock()
		st := fp.state[id]
		fp.mu.Unlock()
		if st == "paused" {
			sawPaused = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !sawPaused {
		t.Fatal("sandbox was never paused (stage 1 failed)")
	}

	// Stage 2: after the destroy grace, a reap pass should destroy + unbind it.
	deadline = time.Now().Add(8 * time.Second)
	sawDestroyed := false
	for time.Now().Before(deadline) {
		_ = b.reapOnce(ctx, time.Now())
		fp.mu.Lock()
		st := fp.state[id]
		fp.mu.Unlock()
		if st == "destroyed" {
			sawDestroyed = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !sawDestroyed {
		t.Fatal("sandbox was never destroyed (stage 2 failed)")
	}

	// Mapping must be gone.
	if _, ok, _ := b.lookup(ctx, "idle"); ok {
		t.Fatal("expected mapping removed after destroy")
	}
}

// TestHeartbeatKeepsAlive: a heartbeated sandbox survives reaper passes.
func TestHeartbeatKeepsAlive(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	sb, err := b.Acquire(ctx, AcquireSpec{SessionID: "busy", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	id := sb.ID
	for i := 0; i < 10; i++ {
		if err := b.Heartbeat(ctx, id); err != nil {
			t.Fatal(err)
		}
		_ = b.reapOnce(ctx, time.Now())
		time.Sleep(150 * time.Millisecond)
	}
	fp.mu.Lock()
	st := fp.state[id]
	fp.mu.Unlock()
	if st != "active" {
		t.Fatalf("expected sandbox to stay active under heartbeat, got %s", st)
	}
}
