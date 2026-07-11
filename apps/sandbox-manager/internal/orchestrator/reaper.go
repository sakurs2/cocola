package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"time"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
	rds "github.com/cocola-project/cocola/packages/go-common/redis"
)

// Reaper implements the two-stage (Pause-then-Destroy) reclamation the user
// chose for M2:
//
//	stage 1 (idle):    an ACTIVE sandbox whose lease has lapsed (no heartbeat
//	                   within LeaseTTL) is Paused. Its workspace is preserved, so
//	                   a returning session resumes cheaply.
//	stage 2 (expired): a PAUSED sandbox that has sat idle for DestroyGrace beyond
//	                   its pause is Destroyed and fully unbound.
//
// Why two stages: Mira-style sessions are bursty — a user often returns within
// a minute or two. Pausing first turns that return into a fast Resume instead
// of a cold create, while still guaranteeing eventual teardown of truly dead
// sessions. The grace window is the knob between resource thrift (short) and
// resume hit-rate (long).
//
// Multi-replica safety: every sandbox is processed under a short per-sandbox
// reaper lock, so when several sandbox-manager replicas sweep concurrently each
// sandbox is acted on by exactly one of them per tick.
func (b *Binder) reapOnce(ctx context.Context, now time.Time) error {
	var active int64
	err := b.kv.ScanKeys(ctx, metaScanPattern(), 100, func(keys []string) error {
		for _, mk := range keys {
			n, err := b.reapMeta(ctx, mk, now)
			if err != nil {
				// Best-effort: one bad sandbox must not stall the whole sweep.
				continue
			}
			active += n
		}
		return nil
	})
	if b.metrics != nil {
		b.metrics.setActive(active)
	}
	return err
}

// reapMeta evaluates a single meta key. Returns 1 if the sandbox is counted as
// active after this pass, 0 otherwise.
func (b *Binder) reapMeta(ctx context.Context, metaK string, now time.Time) (int64, error) {
	raw, err := b.kv.Get(ctx, metaK)
	if errors.Is(err, rds.ErrNil) {
		return 0, nil // vanished mid-scan
	}
	if err != nil {
		return 0, err
	}
	var m meta
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return 0, err
	}

	switch m.State {
	case StateActive:
		// Active + lease still present => healthy, leave alone, count it.
		_, err := b.kv.Get(ctx, leaseKey(m.SandboxID))
		if err == nil {
			return 1, nil
		}
		if !errors.Is(err, rds.ErrNil) {
			return 0, err
		}
		// Lease lapsed -> stage 1: claim, Pause, mark paused.
		return 0, b.underReapLock(ctx, m.SandboxID, func() error {
			return b.pause(ctx, m, now)
		})

	case StatePaused:
		// A re-acquire/heartbeat would have flipped this back to active and
		// renewed the lease. If still paused past the grace window -> stage 2.
		if now.Unix()-m.PausedUnix >= int64(b.cfg.DestroyGrace.Seconds()) {
			return 0, b.underReapLock(ctx, m.SandboxID, func() error {
				return b.destroyPaused(ctx, m)
			})
		}
		return 0, nil // still within grace; not counted as active
	}
	return 0, nil
}

// pause performs stage-1 reclamation, re-checking state under the lock.
func (b *Binder) pause(ctx context.Context, m meta, now time.Time) error {
	// Re-read under lock: a racer may have renewed the lease just now.
	if _, err := b.kv.Get(ctx, leaseKey(m.SandboxID)); err == nil {
		return nil // lease came back; abort pause
	} else if !errors.Is(err, rds.ErrNil) {
		return err
	}
	b.checkpointBeforeReclaim(ctx, m)
	if err := b.p.Pause(ctx, m.SandboxID); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return b.unbind(ctx, m.SessionID, m.SandboxID)
		}
		return err
	}
	m.State = StatePaused
	m.PausedUnix = now.Unix()
	return b.putMeta(ctx, m)
}

// destroyPaused performs stage-2 reclamation: tear down the container and remove
// all binding keys.
func (b *Binder) destroyPaused(ctx context.Context, m meta) error {
	// Re-read under lock: a resume may have reactivated it.
	raw, err := b.kv.Get(ctx, metaKey(m.SandboxID))
	if errors.Is(err, rds.ErrNil) {
		return nil
	}
	if err != nil {
		return err
	}
	var cur meta
	if err := json.Unmarshal([]byte(raw), &cur); err != nil {
		return err
	}
	if cur.State != StatePaused {
		return nil // someone resumed it; leave alone
	}
	if err := b.p.Destroy(ctx, m.SandboxID); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return b.unbind(ctx, m.SessionID, m.SandboxID)
		}
		return err
	}
	return b.unbind(ctx, m.SessionID, m.SandboxID)
}

// underReapLock runs fn while holding a short-lived per-sandbox lock, so only
// one replica acts on a given sandbox per tick. Contention => skip this tick.
func (b *Binder) underReapLock(ctx context.Context, sandboxID string, fn func() error) error {
	key := keyPrefix + "reaplock:" + sandboxID
	token := newToken()
	ok, err := b.kv.SetNX(ctx, key, token, 5*time.Second)
	if err != nil || !ok {
		return err // another replica owns this sandbox this tick
	}
	defer func() { _, _ = b.kv.Eval(ctx, luaUnlock, []string{key}, token) }()
	return fn()
}

// CheckpointFailure identifies one active session whose checkpoint did not complete.
type CheckpointFailure struct {
	SessionID string
	SandboxID string
	Err       error
}

// CheckpointSummary makes a best-effort checkpoint sweep observable to shutdown callers.
type CheckpointSummary struct {
	Scanned   int
	Succeeded int
	Skipped   int
	Failures  []CheckpointFailure
	ScanError error
}

// CheckpointAllActive snapshots every ACTIVE bound sandbox once, best-effort.
//
// This is the graceful-teardown safety net (plan A): reclaim-time checkpointing
// only fires when the reaper Pauses an idle sandbox or a conversation is
// deleted, so a sandbox that dies abruptly (Ctrl+C, SIGTERM on rollout/drain/
// scale-in, pod eviction) between reclaims would lose every turn since its last
// clean reclaim. Draining a checkpoint for each live session on the way out
// closes that gap without touching the per-turn hot path.
//
// It is deliberately parallel to reapOnce: same meta scan, same per-sandbox
// reaper lock (so across replicas each sandbox is checkpointed exactly once),
// but instead of Pausing it just archives. PAUSED sandboxes are skipped -- they
// were already checkpointed at Pause time and archiving one would need a thaw.
// The whole sweep is bounded by ctx; a caller passes a deadline so a slow
// backend cannot wedge the exit path.
func (b *Binder) CheckpointAllActive(ctx context.Context) CheckpointSummary {
	var summary CheckpointSummary
	checkpointer, ok := b.p.(provider.SessionCheckpointer)
	if !ok {
		return summary // provider has no checkpointing configured; nothing to do
	}
	summary.ScanError = b.kv.ScanKeys(ctx, metaScanPattern(), 100, func(keys []string) error {
		for _, mk := range keys {
			if ctx.Err() != nil {
				return ctx.Err() // teardown budget exhausted; stop scanning
			}
			summary.Scanned++
			attempted, failure := b.checkpointActiveMeta(ctx, checkpointer, mk)
			if failure != nil {
				summary.Failures = append(summary.Failures, *failure)
				continue
			}
			if attempted {
				summary.Succeeded++
			} else {
				summary.Skipped++
			}
		}
		return nil
	})
	return summary
}

// checkpointActiveMeta archives a single meta key if it is ACTIVE, under the
// shared per-sandbox reaper lock so concurrent replicas do not double-archive.
func (b *Binder) checkpointActiveMeta(
	ctx context.Context, checkpointer provider.SessionCheckpointer, metaK string,
) (bool, *CheckpointFailure) {
	failure := CheckpointFailure{SandboxID: metaK[len(metaPrefix):]}
	raw, err := b.kv.Get(ctx, metaK)
	if err != nil {
		failure.Err = err
		return false, &failure
	}
	var m meta
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		failure.Err = err
		return false, &failure
	}
	failure.SessionID = m.SessionID
	failure.SandboxID = m.SandboxID
	if m.State != StateActive {
		return false, nil // paused sandboxes were already checkpointed at Pause time
	}
	attempted := false
	err = b.underReapLock(ctx, m.SandboxID, func() error {
		// Re-read under lock: a racer may have paused/destroyed it meanwhile.
		cur, cerr := b.kv.Get(ctx, metaK)
		if cerr != nil {
			return cerr
		}
		var cm meta
		if jerr := json.Unmarshal([]byte(cur), &cm); jerr != nil {
			return jerr
		}
		if cm.State != StateActive {
			return nil
		}
		attempted = true
		return checkpointer.CheckpointSession(ctx, cm.UserID, cm.SessionID, cm.SandboxID)
	})
	if err != nil {
		failure.Err = err
		return attempted, &failure
	}
	return attempted, nil
}

// RunReaper drives reapOnce on ReaperEvery until ctx is cancelled. Spawn one per
// process; the per-sandbox locks make concurrent reapers across replicas safe.
func (b *Binder) RunReaper(ctx context.Context) {
	t := time.NewTicker(b.cfg.ReaperEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			_ = b.reapOnce(ctx, now)
		}
	}
}
