package orchestrator

import (
	"context"
	"encoding/json"
	"errors"

	rds "github.com/cocola-project/cocola/packages/go-common/redis"
)

// Heartbeat is a per-sandbox liveness pulse driven by the *caller* (the
// agent-runtime, via a Heartbeat RPC) for sandboxes it is actively using.
//
// Design note — why caller-driven and not a blanket scan:
// The binder renews the lease on every Acquire, which covers request-driven
// traffic. But a long-running agent task may hold a sandbox for minutes between
// Acquire calls; without an explicit pulse the lease would lapse and the reaper
// would pause a sandbox that is actually busy. The Heartbeat RPC lets the active
// holder keep exactly its sandbox alive — O(active) work, no global scan, and it
// naturally stops when the holder dies (so idle sandboxes do get reclaimed).
func (b *Binder) Heartbeat(ctx context.Context, sandboxID string) error {
	if sandboxID == "" {
		return errors.New("orchestrator: sandbox id required")
	}
	// Only renew if the sandbox still has a durable meta record; renewing a
	// lease for an already-destroyed sandbox would resurrect a ghost lease.
	raw, err := b.kv.Get(ctx, metaKey(sandboxID))
	if errors.Is(err, rds.ErrNil) {
		return ErrUnknownSandbox
	}
	if err != nil {
		return err
	}
	var meta meta
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return err
	}
	if meta.SessionID == "" || meta.SandboxID != sandboxID {
		return ErrUnknownSandbox
	}

	// Use the same Session lock as Acquire, Release and Reaper. Heartbeat is a
	// single attempt: contention is surfaced as a transient error instead of
	// waiting in a new retry loop or falsely reporting a successful renewal.
	lock, err := tryLock(ctx, b.kv, meta.SessionID, b.cfg.LockTTL)
	if err != nil {
		return err
	}
	defer func() { _ = lock.release(ctx) }()

	// A reaper or release may have removed the sandbox before this lock was won.
	raw, err = b.kv.Get(ctx, metaKey(sandboxID))
	if errors.Is(err, rds.ErrNil) {
		return ErrUnknownSandbox
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return err
	}
	if meta.SessionID == "" || meta.SandboxID != sandboxID {
		return ErrUnknownSandbox
	}
	return b.renewLease(ctx, sandboxID)
}

// ErrUnknownSandbox is returned when an operation targets a sandbox that has no
// binding record (never existed, or already reclaimed).
var ErrUnknownSandbox = errors.New("orchestrator: unknown sandbox")
