package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	rds "github.com/cocola-project/cocola/packages/go-common/redis"
)

// Drain destroys every compute sandbox recorded in Redis while deliberately
// leaving Session Storage untouched. It is used only during process shutdown,
// after the gRPC server has stopped accepting new work. The caller supplies a
// bounded context so an unavailable provider cannot block container teardown.
func (b *Binder) Drain(ctx context.Context) (int, error) {
	released := 0
	var failures []error
	scanErr := b.kv.ScanKeys(ctx, metaScanPattern(), 100, func(keys []string) error {
		for _, key := range keys {
			if err := ctx.Err(); err != nil {
				return err
			}
			removed, err := b.drainMeta(ctx, key)
			if removed {
				released++
			}
			if err != nil {
				failures = append(failures, fmt.Errorf("drain %s: %w", key, err))
			}
		}
		return nil
	})
	if scanErr != nil {
		failures = append(failures, fmt.Errorf("scan sandbox registry: %w", scanErr))
	}
	return released, errors.Join(failures...)
}

func (b *Binder) drainMeta(ctx context.Context, key string) (bool, error) {
	raw, err := b.kv.Get(ctx, key)
	if errors.Is(err, rds.ErrNil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var initial meta
	if err := json.Unmarshal([]byte(raw), &initial); err != nil {
		return false, err
	}
	if initial.SessionID == "" || initial.SandboxID == "" || key != metaKey(initial.SandboxID) {
		return false, errors.New("orchestrator: incomplete sandbox meta")
	}

	// Drain shares the normal Session lock with Acquire, Release, Heartbeat and
	// the lease reaper. In-flight RPCs have already been given a chance to
	// finish, so contention should be brief and is bounded by ctx.
	lock, err := acquireLock(ctx, b.kv, initial.SessionID, b.cfg.LockTTL, b.cfg.LockRetry)
	if err != nil {
		return false, fmt.Errorf("acquire session lock: %w", err)
	}
	defer func() { _ = lock.release(ctx) }()

	// A second manager may have drained this record while we waited for the
	// distributed lock. Re-read it so only the current binding is destroyed.
	raw, err = b.kv.Get(ctx, key)
	if errors.Is(err, rds.ErrNil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var current meta
	if err := json.Unmarshal([]byte(raw), &current); err != nil {
		return false, err
	}
	if current.SessionID != initial.SessionID || current.SandboxID != initial.SandboxID {
		return false, errors.New("orchestrator: sandbox meta changed while draining")
	}

	if err := b.p.Destroy(ctx, current.SandboxID); err != nil && !errors.Is(err, fs.ErrNotExist) {
		// Keep the registry record so a later startup can still reconcile it.
		return false, fmt.Errorf("provider destroy: %w", err)
	}
	if err := b.unbind(ctx, current.SessionID, current.SandboxID); err != nil {
		return true, fmt.Errorf("remove binding: %w", err)
	}
	return true, nil
}
