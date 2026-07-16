package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
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
	creates    atomic.Int64
	destroys   atomic.Int64
	mu         sync.Mutex
	state      map[string]string // sandbox id -> "active"|"paused"|"destroyed"
	pauseErr   map[string]error
	resumeErr  map[string]error
	destroyErr map[string]error
	createErr  error
	cleanups   []string
	lastSpec   provider.SandboxSpec
	createHook func(int64)
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{
		state:      map[string]string{},
		pauseErr:   map[string]error{},
		resumeErr:  map[string]error{},
		destroyErr: map[string]error{},
	}
}

type fakeSessionStorage struct {
	mu         sync.Mutex
	binding    *SessionStorageBinding
	creates    int
	prepared   int
	resets     int
	commitErr  error
	commitHook func()
	ensures    []string
	discards   []string
	deletes    []string
}

func (f *fakeSessionStorage) Get(_ context.Context, userID, sessionID string) (SessionStorageBinding, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.binding == nil || f.binding.SessionID != sessionID {
		return SessionStorageBinding{}, false, nil
	}
	if f.binding.UserID != userID {
		return SessionStorageBinding{}, false, ErrSessionStorageOwnerMismatch
	}
	return *f.binding, true, nil
}

func (f *fakeSessionStorage) Create(_ context.Context, userID, sessionID, nodeName string) (SessionStorageBinding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	binding := SessionStorageBinding{
		StorageID: "storage-1", SessionID: sessionID, UserID: userID,
		PVCNamespace: "opensandbox", PVCName: "cocola-sv-new", NodeName: nodeName,
		Generation: 1, RequestedBytes: 2 * 1024 * 1024 * 1024,
	}
	f.binding = &binding
	f.ensures = append(f.ensures, binding.PVCName)
	return binding, nil
}

func (f *fakeSessionStorage) PrepareReset(_ context.Context, current SessionStorageBinding, nodeName, reason string) (SessionStorageBinding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepared++
	next := current
	next.PVCName = "cocola-sv-reset"
	next.NodeName = nodeName
	next.Generation++
	next.LastResetReason = reason
	f.ensures = append(f.ensures, next.PVCName)
	return next, nil
}

func (f *fakeSessionStorage) CommitReset(_ context.Context, current, next SessionStorageBinding) (SessionStorageBinding, error) {
	if f.commitHook != nil {
		f.commitHook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.commitErr != nil {
		return SessionStorageBinding{}, f.commitErr
	}
	if f.binding == nil || f.binding.PVCName != current.PVCName || f.binding.Generation != current.Generation {
		return SessionStorageBinding{}, errors.New("session storage changed concurrently")
	}
	f.resets++
	f.binding = &next
	return next, nil
}

func (f *fakeSessionStorage) DiscardReset(_ context.Context, next SessionStorageBinding) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discards = append(f.discards, next.PVCName)
	return nil
}

func (f *fakeSessionStorage) EnsurePVC(_ context.Context, binding SessionStorageBinding) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensures = append(f.ensures, binding.PVCName)
	return nil
}

func (f *fakeSessionStorage) NodeRequestedBytes(context.Context) (map[string]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int64{}
	if f.binding != nil {
		out[f.binding.NodeName] = f.binding.RequestedBytes
	}
	return out, nil
}

func (f *fakeSessionStorage) Delete(_ context.Context, userID, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.binding != nil && f.binding.UserID != userID {
		return ErrSessionStorageOwnerMismatch
	}
	f.deletes = append(f.deletes, userID+"/"+sessionID)
	f.binding = nil
	return nil
}

func (*fakeSessionStorage) Close() {}

func (f *fakeProvider) Create(ctx context.Context, spec provider.SandboxSpec) (*provider.Sandbox, error) {
	n := f.creates.Add(1)
	if f.createHook != nil {
		f.createHook(n)
	}
	id := fmt.Sprintf("sbx-%d", n)
	f.mu.Lock()
	if f.createErr != nil {
		err := f.createErr
		f.mu.Unlock()
		return nil, err
	}
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
		LeaseTTL:    2 * time.Second,
		LockTTL:     2 * time.Second,
		ReaperEvery: 200 * time.Millisecond,
		LockRetry:   5 * time.Millisecond,
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

func TestAcquireCreatesAndRemountsNodeLocalSessionStorage(t *testing.T) {
	b, fp := newTestBinder(t)
	storage := &fakeSessionStorage{}
	b.WithCapacityGuard(staticNodeGuard("node-a")).WithSessionStorage(storage)
	ctx := context.Background()

	first, err := b.AcquireWithOutcome(ctx, AcquireSpec{SessionID: "persisted", UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if first.WorkspaceState != provider.WorkspaceFresh || first.WorkspaceNode != "node-a" {
		t.Fatalf("first workspace = state %q node %q", first.WorkspaceState, first.WorkspaceNode)
	}
	fp.mu.Lock()
	firstSpec := fp.lastSpec
	fp.mu.Unlock()
	if firstSpec.SessionClaim != "cocola-sv-new" || firstSpec.TargetNodeName != "node-a" {
		t.Fatalf("first provider spec = %+v", firstSpec)
	}

	if _, err := b.kv.Del(ctx, leaseKey(first.Sandbox.ID)); err != nil {
		t.Fatal(err)
	}
	if err := b.reapOnce(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	second, err := b.AcquireWithOutcome(ctx, AcquireSpec{SessionID: "persisted", UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if second.WorkspaceState != provider.WorkspacePreserved || second.WorkspaceNode != "node-a" {
		t.Fatalf("restored workspace = state %q node %q", second.WorkspaceState, second.WorkspaceNode)
	}
	storage.mu.Lock()
	creates := storage.creates
	storage.mu.Unlock()
	if creates != 1 || fp.creates.Load() != 2 {
		t.Fatalf("storage creates=%d sandbox creates=%d, want 1 and 2", creates, fp.creates.Load())
	}
}

func TestAcquireRejectsRedisBindingForUncommittedWorkspaceGeneration(t *testing.T) {
	b, fp := newTestBinder(t)
	storage := &fakeSessionStorage{}
	b.WithCapacityGuard(staticNodeGuard("node-a")).WithSessionStorage(storage)
	ctx := context.Background()

	first, err := b.AcquireWithOutcome(ctx, AcquireSpec{SessionID: "persisted", UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.putMeta(ctx, meta{
		SandboxID: first.Sandbox.ID, SessionID: "persisted", UserID: "u1",
		State: StateActive, NodeName: "node-b", StorageID: "storage-1",
		PVCNamespace: "opensandbox", SessionClaim: "cocola-sv-reset", StorageGeneration: 2,
	}); err != nil {
		t.Fatal(err)
	}

	second, err := b.AcquireWithOutcome(ctx, AcquireSpec{SessionID: "persisted", UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Reused || second.Sandbox.ID == first.Sandbox.ID {
		t.Fatalf("stale storage binding was reused: first=%+v second=%+v", first, second)
	}
	storage.mu.Lock()
	discards := append([]string(nil), storage.discards...)
	storage.mu.Unlock()
	if len(discards) != 1 || discards[0] != "cocola-sv-reset" {
		t.Fatalf("discarded claims = %v, want reset candidate", discards)
	}
	fp.mu.Lock()
	claim := fp.lastSpec.SessionClaim
	fp.mu.Unlock()
	if claim != "cocola-sv-new" {
		t.Fatalf("replacement claim = %q, want PostgreSQL binding", claim)
	}
}

func TestAcquireRequiresExplicitResetWhenWorkspaceNodeUnavailable(t *testing.T) {
	b, fp := newTestBinder(t)
	storage := &fakeSessionStorage{binding: &SessionStorageBinding{
		StorageID: "storage-1", SessionID: "persisted", UserID: "u1",
		PVCNamespace: "opensandbox", PVCName: "cocola-sv-old", NodeName: "node-a",
		Generation: 1, RequestedBytes: 2 * 1024 * 1024 * 1024,
	}}
	b.WithCapacityGuard(unavailablePreferredGuard{}).WithSessionStorage(storage)

	if _, err := b.AcquireWithOutcome(context.Background(), AcquireSpec{
		SessionID: "persisted", UserID: "u1",
	}); !errors.Is(err, ErrWorkspaceNodeUnavailable) {
		t.Fatalf("acquire error = %v, want workspace node unavailable", err)
	}
	if fp.creates.Load() != 0 {
		t.Fatal("unavailable workspace must not create a blank sandbox")
	}

	out, err := b.AcquireWithOutcome(context.Background(), AcquireSpec{
		SessionID: "persisted", UserID: "u1", AllowWorkspaceReset: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.WorkspaceState != provider.WorkspaceReset || out.WorkspaceNode != "node-b" ||
		out.PreviousWorkspaceNode != "node-a" {
		t.Fatalf("reset outcome = %+v", out)
	}
	storage.mu.Lock()
	binding := *storage.binding
	resets := storage.resets
	storage.mu.Unlock()
	if resets != 1 || binding.Generation != 2 || binding.PVCName != "cocola-sv-reset" {
		t.Fatalf("reset binding = %+v, resets=%d", binding, resets)
	}
}

func TestWorkspaceResetProviderCreateFailurePreservesBinding(t *testing.T) {
	b, fp := newTestBinder(t)
	storage := &fakeSessionStorage{binding: &SessionStorageBinding{
		StorageID: "storage-1", SessionID: "persisted", UserID: "u1",
		PVCNamespace: "opensandbox", PVCName: "cocola-sv-old", NodeName: "node-a",
		Generation: 1, RequestedBytes: 2 * 1024 * 1024 * 1024,
	}}
	fp.createErr = errors.New("create failed")
	b.WithCapacityGuard(unavailablePreferredGuard{}).WithSessionStorage(storage)

	_, err := b.AcquireWithOutcome(context.Background(), AcquireSpec{
		SessionID: "persisted", UserID: "u1", AllowWorkspaceReset: true,
	})
	if err == nil || !strings.Contains(err.Error(), "provider create") {
		t.Fatalf("reset error = %v, want provider create failure", err)
	}
	storage.mu.Lock()
	defer storage.mu.Unlock()
	if storage.binding.Generation != 1 || storage.binding.PVCName != "cocola-sv-old" || storage.resets != 0 {
		t.Fatalf("binding committed before sandbox success: %+v, resets=%d", *storage.binding, storage.resets)
	}
	if storage.prepared != 1 || len(storage.discards) != 1 || storage.discards[0] != "cocola-sv-reset" {
		t.Fatalf("prepared=%d discards=%v", storage.prepared, storage.discards)
	}
}

func TestWorkspaceResetCommitFailureDestroysCandidateAndPreservesBinding(t *testing.T) {
	b, fp := newTestBinder(t)
	storage := &fakeSessionStorage{
		binding: &SessionStorageBinding{
			StorageID: "storage-1", SessionID: "persisted", UserID: "u1",
			PVCNamespace: "opensandbox", PVCName: "cocola-sv-old", NodeName: "node-a",
			Generation: 1, RequestedBytes: 2 * 1024 * 1024 * 1024,
		},
		commitErr: errors.New("database unavailable"),
	}
	b.WithCapacityGuard(unavailablePreferredGuard{}).WithSessionStorage(storage)

	_, err := b.AcquireWithOutcome(context.Background(), AcquireSpec{
		SessionID: "persisted", UserID: "u1", AllowWorkspaceReset: true,
	})
	if err == nil || !strings.Contains(err.Error(), "commit workspace reset") {
		t.Fatalf("reset error = %v, want commit failure", err)
	}
	if fp.destroys.Load() != 1 {
		t.Fatalf("candidate destroys = %d, want 1", fp.destroys.Load())
	}
	if _, ok, lookupErr := b.lookup(context.Background(), "persisted", "u1"); lookupErr != nil || ok {
		t.Fatalf("candidate remained bound: ok=%v err=%v", ok, lookupErr)
	}
	storage.mu.Lock()
	defer storage.mu.Unlock()
	if storage.binding.Generation != 1 || storage.binding.PVCName != "cocola-sv-old" || storage.resets != 0 {
		t.Fatalf("binding changed after failed commit: %+v, resets=%d", *storage.binding, storage.resets)
	}
	if len(storage.discards) != 1 || storage.discards[0] != "cocola-sv-reset" {
		t.Fatalf("discarded reset claims = %v", storage.discards)
	}
}

func TestWorkspaceResetExtendsCreateLockThroughCommit(t *testing.T) {
	type acquireResult struct {
		sandbox *provider.Sandbox
		err     error
	}
	b, fp := newTestBinder(t)
	b.cfg.LockTTL = 500 * time.Millisecond
	commitStarted := make(chan struct{})
	allowCommit := make(chan struct{})
	storage := &fakeSessionStorage{
		binding: &SessionStorageBinding{
			StorageID: "storage-1", SessionID: "persisted", UserID: "u1",
			PVCNamespace: "opensandbox", PVCName: "cocola-sv-old", NodeName: "node-a",
			Generation: 1, RequestedBytes: 2 * 1024 * 1024 * 1024,
		},
		commitHook: func() {
			close(commitStarted)
			<-allowCommit
		},
	}
	fp.createHook = func(number int64) {
		if number == 1 {
			time.Sleep(400 * time.Millisecond)
		}
	}
	b.WithCapacityGuard(unavailablePreferredGuard{}).WithSessionStorage(storage)

	firstDone := make(chan acquireResult, 1)
	go func() {
		out, err := b.AcquireWithOutcome(context.Background(), AcquireSpec{
			SessionID: "persisted", UserID: "u1", AllowWorkspaceReset: true,
		})
		firstDone <- acquireResult{sandbox: out.Sandbox, err: err}
	}()
	<-commitStarted
	time.Sleep(150 * time.Millisecond) // the original pre-create lock TTL has elapsed

	secondDone := make(chan acquireResult, 1)
	go func() {
		out, err := b.AcquireWithOutcome(context.Background(), AcquireSpec{
			SessionID: "persisted", UserID: "u1",
		})
		secondDone <- acquireResult{sandbox: out.Sandbox, err: err}
	}()
	time.Sleep(50 * time.Millisecond)
	if got := fp.destroys.Load(); got != 0 {
		t.Fatalf("reset candidate destroyed before commit completed: %d", got)
	}
	close(allowCommit)

	first := <-firstDone
	second := <-secondDone
	if first.err != nil || second.err != nil {
		t.Fatalf("acquire errors: first=%v second=%v", first.err, second.err)
	}
	if first.sandbox.ID != second.sandbox.ID {
		t.Fatalf("reset acquire did not converge: %s vs %s", first.sandbox.ID, second.sandbox.ID)
	}
}

func TestWorkspaceResetCleanupKeepsBindingWhenDestroyFails(t *testing.T) {
	b, fp := newTestBinder(t)
	storage := &fakeSessionStorage{
		binding: &SessionStorageBinding{
			StorageID: "storage-1", SessionID: "persisted", UserID: "u1",
			PVCNamespace: "opensandbox", PVCName: "cocola-sv-old", NodeName: "node-a",
			Generation: 1, RequestedBytes: 2 * 1024 * 1024 * 1024,
		},
		commitErr: errors.New("database unavailable"),
	}
	fp.destroyErr["sbx-1"] = errors.New("control plane unavailable")
	b.WithCapacityGuard(unavailablePreferredGuard{}).WithSessionStorage(storage)

	_, err := b.AcquireWithOutcome(context.Background(), AcquireSpec{
		SessionID: "persisted", UserID: "u1", AllowWorkspaceReset: true,
	})
	if err == nil || !strings.Contains(err.Error(), "control plane unavailable") {
		t.Fatalf("reset error = %v, want destroy failure", err)
	}
	if sid, getErr := b.kv.Get(context.Background(), convKey("persisted")); getErr != nil || sid != "sbx-1" {
		t.Fatalf("running candidate binding = %q, %v; want retained", sid, getErr)
	}
	storage.mu.Lock()
	discards := append([]string(nil), storage.discards...)
	storage.mu.Unlock()
	if len(discards) != 0 {
		t.Fatalf("candidate PVC discarded while sandbox may still be running: %v", discards)
	}
}

type unavailablePreferredGuard struct{}

func (unavailablePreferredGuard) SelectNode(_ context.Context, preferred, excluded string, _ map[string]int64) (string, error) {
	if preferred != "" {
		return "", ErrWorkspaceNodeUnavailable
	}
	if excluded == "node-a" {
		return "node-b", nil
	}
	return "", ErrCapacityBusy
}

type switchableNodeGuard struct{ unavailable bool }

func (g *switchableNodeGuard) SelectNode(_ context.Context, preferred, excluded string, _ map[string]int64) (string, error) {
	if preferred != "" {
		if g.unavailable {
			return "", ErrWorkspaceNodeUnavailable
		}
		return preferred, nil
	}
	if excluded == "node-a" {
		return "node-b", nil
	}
	return "node-a", nil
}

func (g *switchableNodeGuard) NodeAvailable(_ context.Context, nodeName string) (bool, error) {
	return nodeName != "node-a" || !g.unavailable, nil
}

func TestAcquireDoesNotRenewSandboxOnUnavailableWorkspaceNode(t *testing.T) {
	b, fp := newTestBinder(t)
	storage := &fakeSessionStorage{}
	guard := &switchableNodeGuard{}
	b.WithCapacityGuard(guard).WithSessionStorage(storage)

	first, err := b.AcquireWithOutcome(context.Background(), AcquireSpec{SessionID: "active", UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	guard.unavailable = true
	if _, err := b.AcquireWithOutcome(context.Background(), AcquireSpec{
		SessionID: "active", UserID: "u1",
	}); !errors.Is(err, ErrWorkspaceNodeUnavailable) {
		t.Fatalf("acquire error = %v, want workspace node unavailable", err)
	}
	if fp.creates.Load() != 1 {
		t.Fatal("unavailable running sandbox must not be replaced without confirmation")
	}

	reset, err := b.AcquireWithOutcome(context.Background(), AcquireSpec{
		SessionID: "active", UserID: "u1", AllowWorkspaceReset: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reset.Sandbox.ID == first.Sandbox.ID || reset.WorkspaceState != provider.WorkspaceReset ||
		reset.WorkspaceNode != "node-b" || reset.PreviousWorkspaceNode != "node-a" {
		t.Fatalf("reset outcome = %+v, first = %+v", reset, first)
	}
}

func TestWorkspaceResetStopsWhenOldSandboxDeletionFails(t *testing.T) {
	b, fp := newTestBinder(t)
	storage := &fakeSessionStorage{}
	guard := &switchableNodeGuard{}
	b.WithCapacityGuard(guard).WithSessionStorage(storage)

	first, err := b.AcquireWithOutcome(context.Background(), AcquireSpec{
		SessionID: "reset-delete-failure", UserID: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	guard.unavailable = true
	fp.mu.Lock()
	fp.destroyErr[first.Sandbox.ID] = errors.New("control plane unavailable")
	fp.mu.Unlock()

	_, err = b.AcquireWithOutcome(context.Background(), AcquireSpec{
		SessionID: "reset-delete-failure", UserID: "u1", AllowWorkspaceReset: true,
	})
	if err == nil || !strings.Contains(err.Error(), "destroy sandbox before workspace reset") {
		t.Fatalf("reset error = %v, want old sandbox deletion failure", err)
	}
	storage.mu.Lock()
	resets := storage.resets
	generation := storage.binding.Generation
	storage.mu.Unlock()
	if resets != 0 || generation != 1 {
		t.Fatalf("storage changed after failed destroy: resets=%d generation=%d", resets, generation)
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

func TestExpiredCreateLockStillConvergesThroughCASBind(t *testing.T) {
	b, fp := newTestBinder(t)
	b.cfg.LockTTL = 25 * time.Millisecond
	firstCreateStarted := make(chan struct{})
	releaseFirstCreate := make(chan struct{})
	fp.createHook = func(number int64) {
		if number == 1 {
			close(firstCreateStarted)
			<-releaseFirstCreate
		}
	}

	type result struct {
		sandbox *provider.Sandbox
		err     error
	}
	firstResult := make(chan result, 1)
	go func() {
		sandbox, err := b.Acquire(context.Background(), AcquireSpec{SessionID: "slow", UserID: "u"})
		firstResult <- result{sandbox: sandbox, err: err}
	}()
	<-firstCreateStarted
	time.Sleep(2 * b.cfg.LockTTL)

	second, err := b.Acquire(context.Background(), AcquireSpec{SessionID: "slow", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	close(releaseFirstCreate)
	first := <-firstResult
	if first.err != nil {
		t.Fatal(first.err)
	}
	if first.sandbox.ID != second.ID {
		t.Fatalf("CAS bind returned different winners: %s and %s", first.sandbox.ID, second.ID)
	}
	if got := fp.creates.Load(); got != 2 {
		t.Fatalf("creates = %d, want the expired-lock race to create two candidates", got)
	}
	if got := fp.destroys.Load(); got != 1 {
		t.Fatalf("destroys = %d, want exactly one loser cleanup", got)
	}
}

func TestExpiredAcquireCannotRebindAfterRelease(t *testing.T) {
	b, fp := newTestBinder(t)
	b.cfg.LockTTL = 25 * time.Millisecond
	storage := &fakeSessionStorage{}
	b.WithCapacityGuard(staticNodeGuard("node-a")).WithSessionStorage(storage)
	createStarted := make(chan struct{})
	allowCreate := make(chan struct{})
	fp.createHook = func(number int64) {
		if number == 1 {
			close(createStarted)
			<-allowCreate
		}
	}

	acquireDone := make(chan error, 1)
	go func() {
		_, err := b.Acquire(context.Background(), AcquireSpec{
			SessionID: "deleted-during-create", UserID: "u",
		})
		acquireDone <- err
	}()
	<-createStarted
	time.Sleep(2 * b.cfg.LockTTL)
	if err := b.Release(context.Background(), "u", "deleted-during-create"); err != nil {
		t.Fatal(err)
	}
	close(allowCreate)
	if err := <-acquireDone; !errors.Is(err, ErrSessionLockLost) {
		t.Fatalf("acquire error = %v, want lock-lost fencing", err)
	}
	if _, err := b.kv.Get(context.Background(), convKey("deleted-during-create")); !errors.Is(err, rds.ErrNil) {
		t.Fatalf("deleted Session was rebound: %v", err)
	}
	storage.mu.Lock()
	binding := storage.binding
	storage.mu.Unlock()
	if binding != nil {
		t.Fatalf("storage binding survived Release: %+v", binding)
	}
	if got := fp.destroys.Load(); got != 1 {
		t.Fatalf("destroys = %d, want late candidate cleanup", got)
	}
}

func TestReleaseWaitsForAcquireSessionLock(t *testing.T) {
	b, fp := newTestBinder(t)
	createStarted := make(chan struct{})
	allowCreate := make(chan struct{})
	fp.createHook = func(number int64) {
		if number == 1 {
			close(createStarted)
			<-allowCreate
		}
	}

	acquireDone := make(chan error, 1)
	go func() {
		_, err := b.Acquire(context.Background(), AcquireSpec{SessionID: "release-race", UserID: "u"})
		acquireDone <- err
	}()
	<-createStarted
	releaseDone := make(chan error, 1)
	go func() { releaseDone <- b.Release(context.Background(), "u", "release-race") }()

	select {
	case err := <-releaseDone:
		t.Fatalf("release completed before acquire released the Session lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(allowCreate)
	if err := <-acquireDone; err != nil {
		t.Fatal(err)
	}
	if err := <-releaseDone; err != nil {
		t.Fatal(err)
	}
	if _, err := b.kv.Get(context.Background(), convKey("release-race")); !errors.Is(err, rds.ErrNil) {
		t.Fatalf("forward binding survived release: %v", err)
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

func TestReaperDestroysIdleSandboxWithoutCleaningSessionStorage(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	sb, err := b.Acquire(ctx, AcquireSpec{SessionID: "idle", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	id := sb.ID

	if _, err := b.kv.Del(ctx, leaseKey(id)); err != nil {
		t.Fatal(err)
	}
	if err := b.reapOnce(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}

	// Mapping must be gone.
	if _, ok, _ := b.lookup(ctx, "idle", "u"); ok {
		t.Fatal("expected mapping removed after destroy")
	}
	fp.mu.Lock()
	cleanups := append([]string(nil), fp.cleanups...)
	fp.mu.Unlock()
	if len(cleanups) != 0 {
		t.Fatalf("idle reaper must not clean session storage, got %v", cleanups)
	}
	fp.mu.Lock()
	state := fp.state[id]
	fp.mu.Unlock()
	if state != "destroyed" {
		t.Fatalf("sandbox state = %q, want destroyed", state)
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
	fp.destroyErr[sb.ID] = fs.ErrNotExist
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

func TestReaperSkipsSandboxWhileSessionLockIsHeld(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	sb, err := b.Acquire(ctx, AcquireSpec{SessionID: "locked", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.kv.Del(ctx, leaseKey(sb.ID)); err != nil {
		t.Fatal(err)
	}
	lock, err := tryLock(ctx, b.kv, "locked", b.cfg.LockTTL)
	if err != nil {
		t.Fatal(err)
	}
	count, err := b.reapMeta(ctx, metaKey(sb.ID))
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || fp.destroys.Load() != 0 {
		t.Fatalf("locked reap count=%d destroys=%d", count, fp.destroys.Load())
	}
	if err := lock.release(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := b.reapMeta(ctx, metaKey(sb.ID)); err != nil {
		t.Fatal(err)
	}
	if fp.destroys.Load() != 1 {
		t.Fatalf("unlocked destroys=%d, want 1", fp.destroys.Load())
	}
}

func TestHeartbeatDoesNotReportSuccessDuringSessionLockContention(t *testing.T) {
	b, _ := newTestBinder(t)
	ctx := context.Background()
	sb, err := b.Acquire(ctx, AcquireSpec{SessionID: "locked-heartbeat", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.kv.Del(ctx, leaseKey(sb.ID)); err != nil {
		t.Fatal(err)
	}
	lock, err := tryLock(ctx, b.kv, "locked-heartbeat", b.cfg.LockTTL)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Heartbeat(ctx, sb.ID); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("heartbeat error = %v, want ErrLockHeld", err)
	}
	if _, err := b.kv.Get(ctx, leaseKey(sb.ID)); !errors.Is(err, rds.ErrNil) {
		t.Fatalf("heartbeat renewed lease under contention: %v", err)
	}
	if err := lock.release(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Heartbeat(ctx, sb.ID); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseDestroysUnbindsAndCleansSessionStorage(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	sb, err := b.Acquire(ctx, AcquireSpec{SessionID: "s1", UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Release(ctx, "u1", "s1"); err != nil {
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
