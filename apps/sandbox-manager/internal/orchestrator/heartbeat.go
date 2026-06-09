package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"time"

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
	var m meta
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return err
	}
	// A heartbeat on a paused sandbox means the holder came back: resume it and
	// flip state to active before renewing.
	if m.State == StatePaused {
		if err := b.p.Resume(ctx, sandboxID); err != nil {
			return err
		}
		m.State = StateActive
		m.PausedUnix = 0
		if err := b.putMeta(ctx, m); err != nil {
			return err
		}
	}
	return b.renewLease(ctx, sandboxID)
}

// ErrUnknownSandbox is returned when an operation targets a sandbox that has no
// binding record (never existed, or already reclaimed).
var ErrUnknownSandbox = errors.New("orchestrator: unknown sandbox")

// HeartbeatLoop is a convenience driver for in-process callers (e.g. tests or a
// co-located runtime) that want a sandbox kept alive on a fixed cadence until
// ctx is cancelled. Network callers use the Heartbeat RPC instead.
func (b *Binder) HeartbeatLoop(ctx context.Context, sandboxID string) {
	t := time.NewTicker(b.cfg.HeartbeatEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := b.Heartbeat(ctx, sandboxID); err != nil {
				// Sandbox is gone — nothing left to pulse.
				if errors.Is(err, ErrUnknownSandbox) {
					return
				}
			}
		}
	}
}
