package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
	creates       atomic.Int64
	destroys      atomic.Int64
	mu            sync.Mutex
	state         map[string]string // sandbox id -> "active"|"paused"|"destroyed"
	pauseErr      map[string]error
	resumeErr     map[string]error
	destroyErr    map[string]error
	checkpointErr map[string]error
	cleanups      []string
	checkpoints   []string
	lastSpec      provider.SandboxSpec
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{
		state:         map[string]string{},
		pauseErr:      map[string]error{},
		resumeErr:     map[string]error{},
		destroyErr:    map[string]error{},
		checkpointErr: map[string]error{},
	}
}

func (f *fakeProvider) Create(ctx context.Context, spec provider.SandboxSpec) (*provider.Sandbox, error) {
	n := f.creates.Add(1)
	id := fmt.Sprintf("sbx-%d", n)
	f.mu.Lock()
	f.state[id] = "active"
	f.lastSpec = spec
	f.mu.Unlock()
	return &provider.Sandbox{ID: id, UserID: spec.UserID, SessionID: spec.SessionID}, nil
}
func (f *fakeProvider) Pause(ctx context.Context, sid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.pauseErr[sid]; err != nil {
		return err
	}
	f.state[sid] = "paused"
	return nil
}
func (f *fakeProvider) Resume(ctx context.Context, sid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.resumeErr[sid]; err != nil {
		return err
	}
	f.state[sid] = "active"
	return nil
}
func (f *fakeProvider) Destroy(ctx context.Context, sid string) error {
	f.destroys.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.destroyErr[sid]; err != nil {
		return err
	}
	f.state[sid] = "destroyed"
	return nil
}
func (f *fakeProvider) CleanupSessionStorage(ctx context.Context, userID, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanups = append(f.cleanups, userID+"/"+sessionID)
	return nil
}
func (f *fakeProvider) CheckpointSession(ctx context.Context, userID, sessionID, sandboxID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkpoints = append(f.checkpoints, userID+"/"+sessionID+"/"+sandboxID)
	return f.checkpointErr[sandboxID]
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
	f.mu.Lock()
	defer f.mu.Unlock()
	state := f.state[sid]
	if state == "" || state == "destroyed" {
		return nil, fs.ErrNotExist
	}
	return &provider.HealthStatus{
		Healthy: state == "active", Transitional: state == "pending", Detail: state,
	}, nil
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

func TestDrainWarmPoolLeavesClaimedSandboxRunning(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	b.WithWarmPool(WarmConfig{Image: "sandbox-image"})
	for i := 0; i < 3; i++ {
		if err := b.createOneWarm(ctx); err != nil {
			t.Fatalf("create warm sandbox: %v", err)
		}
	}
	claimed, ok := b.claimWarm(ctx, AcquireSpec{SessionID: "session-1", UserID: "user-1"})
	if !ok {
		t.Fatal("expected one warm sandbox to be claimed")
	}

	drained, err := b.DrainWarmPool(ctx)
	if err != nil {
		t.Fatalf("drain warm pool: %v", err)
	}
	if drained != 2 {
		t.Fatalf("drained = %d, want 2 unclaimed sandboxes", drained)
	}
	if got := fp.destroys.Load(); got != 2 {
		t.Fatalf("provider destroys = %d, want 2", got)
	}
	fp.mu.Lock()
	claimedState := fp.state[claimed.ID]
	fp.mu.Unlock()
	if claimedState != "active" {
		t.Fatalf("claimed sandbox state = %q, want active", claimedState)
	}
	ids, err := b.listWarm(ctx)
	if err != nil || len(ids) != 0 {
		t.Fatalf("remaining warm inventory = %v, %v", ids, err)
	}
}

func TestClaimWarmRejectsUnhealthySandbox(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	b.WithWarmPool(WarmConfig{Image: "sandbox-image"})
	if err := b.createOneWarm(ctx); err != nil {
		t.Fatal(err)
	}
	ids, err := b.listWarm(ctx)
	if err != nil || len(ids) != 1 {
		t.Fatalf("warm inventory = %v, %v", ids, err)
	}
	fp.mu.Lock()
	fp.state[ids[0]] = "pending"
	fp.mu.Unlock()

	if claimed, ok := b.claimWarm(ctx, AcquireSpec{SessionID: "session-1", UserID: "user-1"}); ok {
		t.Fatalf("claimed unhealthy sandbox %+v", claimed)
	}
	remaining, err := b.listWarm(ctx)
	if err != nil || len(remaining) != 1 || remaining[0] != ids[0] {
		t.Fatalf("pending warm inventory = %v, %v; want %s retained", remaining, err, ids[0])
	}
}

func TestPruneWarmKeepsStartingAndRemovesFailedSandboxes(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	b.WithWarmPool(WarmConfig{Image: "sandbox-image"})
	for range 2 {
		if err := b.createOneWarm(ctx); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := b.listWarm(ctx)
	if err != nil || len(ids) != 2 {
		t.Fatalf("warm inventory = %v, %v", ids, err)
	}
	fp.mu.Lock()
	fp.state[ids[0]] = "pending"
	fp.state[ids[1]] = "failed"
	fp.mu.Unlock()

	alive := b.pruneDeadWarm(ctx, ids)
	if len(alive) != 1 || alive[0] != ids[0] {
		t.Fatalf("alive warm sandboxes = %v, want pending %s", alive, ids[0])
	}
	remaining, err := b.listWarm(ctx)
	if err != nil || len(remaining) != 1 || remaining[0] != ids[0] {
		t.Fatalf("remaining warm inventory = %v, %v", remaining, err)
	}
}

func TestWarmPoolSizingUsesHotRuntimeOverride(t *testing.T) {
	b, _ := newTestBinder(t)
	ctx := context.Background()
	b.WithWarmPool(WarmConfig{Enabled: true, Size: 10, Image: "sandbox-image"})

	if err := b.kv.Set(ctx, warmConfigKey, `{"enabled":true,"size":3}`, 0); err != nil {
		t.Fatal(err)
	}
	sizing, ok := b.effectiveWarmSizing(ctx)
	if !ok {
		t.Fatal("runtime warm sizing should be available")
	}
	if !sizing.Enabled || sizing.Size != 3 {
		t.Fatalf("effective warm sizing = %+v, want enabled size 3", sizing)
	}
}

func TestWarmPoolProvisioningNeverBakesStaticAuthToken(t *testing.T) {
	t.Setenv("COCOLA_SANDBOX_IMAGE", "registry/runtime:v1")
	t.Setenv("COCOLA_SANDBOX_LLM_BASE_URL", "http://llm-gateway:8080")
	t.Setenv("COCOLA_SANDBOX_MODEL_ALIAS", "cocola-default")
	t.Setenv("COCOLA_SANDBOX_LLM_TOKEN", "legacy-shared-token")

	cfg := WarmConfigFromEnv()
	if cfg.Env["ANTHROPIC_AUTH_TOKEN"] != "" {
		t.Fatalf("warm sandbox contains a static auth token: %q", cfg.Env["ANTHROPIC_AUTH_TOKEN"])
	}
	if cfg.Env["ANTHROPIC_BASE_URL"] != "http://llm-gateway:8080" ||
		cfg.Env["COCOLA_LLM_BASE_URL"] != "http://llm-gateway:8080" ||
		cfg.Env["ANTHROPIC_MODEL"] != "cocola-default" {
		t.Fatalf("warm routing env = %#v", cfg.Env)
	}
}

func TestWarmPoolSizingWaitsForRuntimeDelivery(t *testing.T) {
	b, _ := newTestBinder(t)
	b.WithWarmPool(WarmConfig{Enabled: true, Size: 10, Image: "sandbox-image"})

	if _, ok := b.effectiveWarmSizing(context.Background()); ok {
		t.Fatal("missing runtime warm sizing must not fall back to startup defaults")
	}
}

func TestDrainWarmPoolRetainsInventoryWhenDestroyFails(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	b.WithWarmPool(WarmConfig{Image: "sandbox-image"})
	if err := b.createOneWarm(ctx); err != nil {
		t.Fatalf("create warm sandbox: %v", err)
	}
	ids, err := b.listWarm(ctx)
	if err != nil || len(ids) != 1 {
		t.Fatalf("warm inventory = %v, %v", ids, err)
	}
	fp.mu.Lock()
	fp.destroyErr[ids[0]] = errors.New("provider unavailable")
	fp.mu.Unlock()

	drained, err := b.DrainWarmPool(ctx)
	if drained != 0 || err == nil {
		t.Fatalf("drain result = %d, %v; want failure", drained, err)
	}
	remaining, listErr := b.listWarm(ctx)
	if listErr != nil || len(remaining) != 1 || remaining[0] != ids[0] {
		t.Fatalf("failed sandbox inventory = %v, %v; want %s retained", remaining, listErr, ids[0])
	}
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

func TestAcquireReplacesUnhealthyBoundSandbox(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	first, err := b.Acquire(ctx, AcquireSpec{SessionID: "unhealthy", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	fp.mu.Lock()
	fp.state[first.ID] = "failed"
	fp.mu.Unlock()

	replacement, err := b.Acquire(ctx, AcquireSpec{SessionID: "unhealthy", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.ID == first.ID {
		t.Fatalf("reused unhealthy sandbox %s", first.ID)
	}
	if got := fp.creates.Load(); got != 2 {
		t.Fatalf("creates = %d, want 2", got)
	}
}

func TestAcquireRejectsSessionOwnedByAnotherUser(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	first, err := b.Acquire(ctx, AcquireSpec{SessionID: "shared", UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Acquire(ctx, AcquireSpec{SessionID: "shared", UserID: "u2"}); !errors.Is(err, ErrSessionOwnerMismatch) {
		t.Fatalf("cross-owner acquire error = %v, want owner mismatch", err)
	}
	if got := fp.creates.Load(); got != 1 {
		t.Fatalf("creates = %d, want 1", got)
	}
	if state := fp.state[first.ID]; state != "active" {
		t.Fatalf("original sandbox state = %q, want active", state)
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
	if _, ok, _ := b.lookup(ctx, "idle", "u"); ok {
		t.Fatal("expected mapping removed after destroy")
	}
	fp.mu.Lock()
	cleanups := append([]string(nil), fp.cleanups...)
	checkpoints := append([]string(nil), fp.checkpoints...)
	fp.mu.Unlock()
	if len(cleanups) != 0 {
		t.Fatalf("idle reaper must not clean session storage, got %v", cleanups)
	}
	wantCheckpoint := "u/idle/" + id
	if len(checkpoints) != 1 || checkpoints[0] != wantCheckpoint {
		t.Fatalf("checkpoints = %v, want [%s]", checkpoints, wantCheckpoint)
	}
}

func TestReaperUnbindsMissingActiveSandbox(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	sb, err := b.Acquire(ctx, AcquireSpec{SessionID: "missing-active", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	fp.mu.Lock()
	fp.pauseErr[sb.ID] = fs.ErrNotExist
	fp.mu.Unlock()

	if _, err := b.kv.Del(ctx, leaseKey(sb.ID)); err != nil {
		t.Fatalf("delete lease: %v", err)
	}
	if err := b.reapOnce(ctx, time.Now()); err != nil {
		t.Fatalf("reapOnce: %v", err)
	}
	if _, ok, err := b.lookup(ctx, "missing-active", "u"); err != nil || ok {
		t.Fatalf("lookup after reap ok=%v err=%v, want unbound", ok, err)
	}
}

func TestReaperUnbindsMissingPausedSandbox(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	sb, err := b.Acquire(ctx, AcquireSpec{SessionID: "missing-paused", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	m := meta{
		SandboxID:   sb.ID,
		SessionID:   "missing-paused",
		UserID:      "u",
		State:       StatePaused,
		CreatedUnix: time.Now().Unix(),
		PausedUnix:  time.Now().Add(-time.Hour).Unix(),
	}
	if err := b.putMeta(ctx, m); err != nil {
		t.Fatalf("put meta: %v", err)
	}
	fp.mu.Lock()
	fp.destroyErr[sb.ID] = fs.ErrNotExist
	fp.mu.Unlock()

	if err := b.reapOnce(ctx, time.Now()); err != nil {
		t.Fatalf("reapOnce: %v", err)
	}
	if _, ok, err := b.lookup(ctx, "missing-paused", "u"); err != nil || ok {
		t.Fatalf("lookup after reap ok=%v err=%v, want unbound", ok, err)
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

func TestReleaseDestroysUnbindsAndCleansSessionStorage(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	sb, err := b.Acquire(ctx, AcquireSpec{SessionID: "s1", UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Release(ctx, "s1"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if got := fp.destroys.Load(); got != 1 {
		t.Fatalf("destroys = %d, want 1", got)
	}
	if _, ok, _ := b.lookup(ctx, "s1", "u1"); ok {
		t.Fatal("expected mapping removed after release")
	}
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if fp.state[sb.ID] != "destroyed" {
		t.Fatalf("sandbox state = %q, want destroyed", fp.state[sb.ID])
	}
	if len(fp.cleanups) != 1 || fp.cleanups[0] != "u1/s1" {
		t.Fatalf("cleanups = %v, want [u1/s1]", fp.cleanups)
	}
	if len(fp.checkpoints) != 0 {
		t.Fatalf("explicit release must not checkpoint deleted conversation, got %v", fp.checkpoints)
	}
}

func TestAcquireRecreatesWhenPausedSandboxDisappeared(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	spec := AcquireSpec{SessionID: "gone", UserID: "u"}

	sb1, err := b.Acquire(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}

	fp.mu.Lock()
	fp.state[sb1.ID] = "destroyed"
	fp.resumeErr[sb1.ID] = fs.ErrNotExist
	fp.mu.Unlock()

	if err := b.putMeta(ctx, meta{
		SandboxID:   sb1.ID,
		SessionID:   spec.SessionID,
		UserID:      spec.UserID,
		State:       StatePaused,
		CreatedUnix: time.Now().Unix(),
		PausedUnix:  time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	out, err := b.AcquireWithOutcome(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if out.Reused {
		t.Fatal("expected fresh sandbox after stale paused binding, got reused")
	}
	if out.Sandbox.ID == sb1.ID {
		t.Fatalf("expected new sandbox id, got stale %s", out.Sandbox.ID)
	}
	if got := fp.creates.Load(); got != 2 {
		t.Fatalf("expected 2 creates, got %d", got)
	}
	if sid, err := b.kv.Get(ctx, convKey(spec.SessionID)); err != nil || sid != out.Sandbox.ID {
		t.Fatalf("forward binding = %q, %v; want %q", sid, err, out.Sandbox.ID)
	}
}

// TestAcquireRecreatesWhenPausedSandboxNotResumable covers the state-divergence
// bug: our durable binding says StatePaused but the provider has driven the
// sandbox to a terminal phase and discarded the paused checkpoint, so Resume
// returns provider.ErrSandboxNotResumable (the 409 INVALID_STATE case). Acquire
// must self-heal — drop the stale binding and cold-create — rather than fail.
func TestAcquireRecreatesWhenPausedSandboxNotResumable(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	spec := AcquireSpec{SessionID: "terminal", UserID: "u"}

	sb1, err := b.Acquire(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}

	fp.mu.Lock()
	fp.resumeErr[sb1.ID] = provider.ErrSandboxNotResumable
	fp.mu.Unlock()

	if err := b.putMeta(ctx, meta{
		SandboxID:   sb1.ID,
		SessionID:   spec.SessionID,
		UserID:      spec.UserID,
		State:       StatePaused,
		CreatedUnix: time.Now().Unix(),
		PausedUnix:  time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	out, err := b.AcquireWithOutcome(ctx, spec)
	if err != nil {
		t.Fatalf("acquire must self-heal a non-resumable paused sandbox, got err: %v", err)
	}
	if out.Reused {
		t.Fatal("expected fresh sandbox after non-resumable paused binding, got reused")
	}
	if out.Sandbox.ID == sb1.ID {
		t.Fatalf("expected new sandbox id, got stale %s", out.Sandbox.ID)
	}
	if got := fp.creates.Load(); got != 2 {
		t.Fatalf("expected 2 creates, got %d", got)
	}
	if sid, err := b.kv.Get(ctx, convKey(spec.SessionID)); err != nil || sid != out.Sandbox.ID {
		t.Fatalf("forward binding = %q, %v; want %q", sid, err, out.Sandbox.ID)
	}
}

func TestAcquireRecreatesWhenActiveSandboxDisappeared(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	spec := AcquireSpec{SessionID: "active-gone", UserID: "u"}

	sb1, err := b.Acquire(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}

	fp.mu.Lock()
	fp.state[sb1.ID] = "destroyed"
	fp.mu.Unlock()

	out, err := b.AcquireWithOutcome(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if out.Reused {
		t.Fatal("expected fresh sandbox after stale active binding, got reused")
	}
	if out.Sandbox.ID == sb1.ID {
		t.Fatalf("expected new sandbox id, got stale %s", out.Sandbox.ID)
	}
	if got := fp.creates.Load(); got != 2 {
		t.Fatalf("expected 2 creates, got %d", got)
	}
	if sid, err := b.kv.Get(ctx, convKey(spec.SessionID)); err != nil || sid != out.Sandbox.ID {
		t.Fatalf("forward binding = %q, %v; want %q", sid, err, out.Sandbox.ID)
	}
}

// TestCheckpointAllActiveArchivesOnlyActive: the graceful-teardown drain sweep
// checkpoints every ACTIVE bound sandbox exactly once and skips PAUSED ones
// (those were already archived at Pause time; archiving one would need a thaw).
func TestCheckpointAllActiveArchivesOnlyActive(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()

	sb1, err := b.Acquire(ctx, AcquireSpec{SessionID: "live-1", UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	sb2, err := b.Acquire(ctx, AcquireSpec{SessionID: "live-2", UserID: "u2"})
	if err != nil {
		t.Fatal(err)
	}

	pausedID := "sbx-paused"
	if err := b.putMeta(ctx, meta{
		SandboxID:   pausedID,
		SessionID:   "dozing",
		UserID:      "u3",
		State:       StatePaused,
		CreatedUnix: time.Now().Unix(),
		PausedUnix:  time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	summary := b.CheckpointAllActive(ctx)

	fp.mu.Lock()
	got := map[string]bool{}
	for _, c := range fp.checkpoints {
		got[c] = true
	}
	fp.mu.Unlock()

	if len(got) != 2 {
		t.Fatalf("expected exactly 2 checkpoints, got %v", got)
	}
	if !got["u1/live-1/"+sb1.ID] {
		t.Fatalf("missing checkpoint for live-1; got %v", got)
	}
	if !got["u2/live-2/"+sb2.ID] {
		t.Fatalf("missing checkpoint for live-2; got %v", got)
	}
	if got["u3/dozing/"+pausedID] {
		t.Fatalf("paused sandbox must not be checkpointed; got %v", got)
	}
	if summary.Scanned != 3 || summary.Succeeded != 2 || summary.Skipped != 1 || len(summary.Failures) != 0 {
		t.Fatalf("unexpected checkpoint summary: %+v", summary)
	}
}

func TestCheckpointAllActiveReportsProviderFailure(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()

	sb, err := b.Acquire(ctx, AcquireSpec{SessionID: "live-fail", UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	fp.mu.Lock()
	fp.checkpointErr[sb.ID] = errors.New("minio upload unavailable")
	fp.mu.Unlock()

	summary := b.CheckpointAllActive(ctx)
	if summary.Scanned != 1 || summary.Succeeded != 0 || len(summary.Failures) != 1 {
		t.Fatalf("unexpected checkpoint summary: %+v", summary)
	}
	failure := summary.Failures[0]
	if failure.SessionID != "live-fail" || failure.SandboxID != sb.ID {
		t.Fatalf("unexpected failure identity: %+v", failure)
	}
	if failure.Err == nil || failure.Err.Error() != "minio upload unavailable" {
		t.Fatalf("unexpected failure error: %v", failure.Err)
	}
}

// TestCheckpointAllActiveStopsWhenBudgetExhausted: a cancelled context (the
// teardown budget elapsed) halts the sweep without archiving anything.
func TestCheckpointAllActiveStopsWhenBudgetExhausted(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()

	if _, err := b.Acquire(ctx, AcquireSpec{SessionID: "live-1", UserID: "u1"}); err != nil {
		t.Fatal(err)
	}

	cctx, cancel := context.WithCancel(ctx)
	cancel() // budget already exhausted before the sweep starts

	summary := b.CheckpointAllActive(cctx)

	fp.mu.Lock()
	n := len(fp.checkpoints)
	fp.mu.Unlock()
	if n != 0 {
		t.Fatalf("expected no checkpoints under an exhausted budget, got %d", n)
	}
	if !errors.Is(summary.ScanError, context.Canceled) {
		t.Fatalf("scan error = %v, want context.Canceled", summary.ScanError)
	}
}
