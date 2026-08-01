package orchestrator

import (
	"context"
	"errors"
	"testing"

	rds "github.com/cocola-project/cocola/packages/go-common/redis"
)

func TestDrainDestroysRegisteredComputeAndPreservesSessionStorage(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	first, err := b.Acquire(ctx, AcquireSpec{SessionID: "session-1", UserID: "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.Acquire(ctx, AcquireSpec{SessionID: "session-2", UserID: "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	storage := &fakeSessionStorage{}
	b.storage = storage

	released, err := b.Drain(ctx)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if released != 2 {
		t.Fatalf("released = %d, want 2", released)
	}
	for _, sandboxID := range []string{first.ID, second.ID} {
		if fp.state[sandboxID] != "destroyed" {
			t.Fatalf("sandbox %s state = %q, want destroyed", sandboxID, fp.state[sandboxID])
		}
		if _, err := b.kv.Get(ctx, metaKey(sandboxID)); !errors.Is(err, rds.ErrNil) {
			t.Fatalf("meta %s remains after drain: %v", sandboxID, err)
		}
	}
	if len(storage.deletes) != 0 || len(fp.cleanups) != 0 {
		t.Fatalf("drain removed session storage: manager=%v provider=%v", storage.deletes, fp.cleanups)
	}

	released, err = b.Drain(ctx)
	if err != nil || released != 0 {
		t.Fatalf("second Drain = (%d, %v), want (0, nil)", released, err)
	}
}

func TestDrainContinuesAfterProviderFailureAndKeepsFailedBinding(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	first, err := b.Acquire(ctx, AcquireSpec{SessionID: "session-1", UserID: "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.Acquire(ctx, AcquireSpec{SessionID: "session-2", UserID: "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	fp.destroyErr[first.ID] = errors.New("provider unavailable")

	released, err := b.Drain(ctx)
	if err == nil || released != 1 {
		t.Fatalf("Drain = (%d, %v), want one release plus error", released, err)
	}
	if _, getErr := b.kv.Get(ctx, metaKey(first.ID)); getErr != nil {
		t.Fatalf("failed sandbox binding was removed: %v", getErr)
	}
	if _, getErr := b.kv.Get(ctx, metaKey(second.ID)); !errors.Is(getErr, rds.ErrNil) {
		t.Fatalf("successful sandbox binding remains: %v", getErr)
	}
}
