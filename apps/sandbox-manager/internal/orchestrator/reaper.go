package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"time"

	rds "github.com/cocola-project/cocola/packages/go-common/redis"
)

// reapOnce destroys idle compute while leaving Session Storage untouched.
// The existing lease scan is a sandbox lifecycle concern; storage provisioning
// and cleanup remain entirely request-driven.
func (b *Binder) reapOnce(ctx context.Context, _ time.Time) error {
	var active int64
	err := b.kv.ScanKeys(ctx, metaScanPattern(), 100, func(keys []string) error {
		for _, metaKey := range keys {
			count, err := b.reapMeta(ctx, metaKey)
			if err == nil {
				active += count
			}
		}
		return nil
	})
	if b.metrics != nil {
		b.metrics.setActive(active)
	}
	return err
}

func (b *Binder) reapMeta(ctx context.Context, key string) (int64, error) {
	raw, err := b.kv.Get(ctx, key)
	if errors.Is(err, rds.ErrNil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var meta meta
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return 0, err
	}
	if meta.SessionID == "" || meta.SandboxID == "" {
		return 0, errors.New("orchestrator: incomplete sandbox meta")
	}
	lockSessionID, lockSandboxID := meta.SessionID, meta.SandboxID
	if _, err := b.kv.Get(ctx, leaseKey(meta.SandboxID)); err == nil {
		return 1, nil
	} else if !errors.Is(err, rds.ErrNil) {
		return 0, err
	}

	// Reaper, Acquire, Release and Heartbeat must make their final lifecycle
	// decision under the same Session lock. On contention this scan simply skips
	// the sandbox; a later existing reaper pass will reconsider it.
	lock, err := tryLock(ctx, b.kv, meta.SessionID, b.cfg.LockTTL)
	if errors.Is(err, ErrLockHeld) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	defer func() { _ = lock.release(ctx) }()

	// Re-read both records after locking: another holder may have renewed or
	// removed the binding between the initial scan and lock acquisition.
	raw, err = b.kv.Get(ctx, key)
	if errors.Is(err, rds.ErrNil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return 0, err
	}
	if meta.SessionID != lockSessionID || meta.SandboxID != lockSandboxID {
		return 0, errors.New("orchestrator: sandbox meta changed while reaping")
	}
	if _, err := b.kv.Get(ctx, leaseKey(meta.SandboxID)); err == nil {
		return 1, nil
	} else if !errors.Is(err, rds.ErrNil) {
		return 0, err
	}
	if err := b.p.Destroy(ctx, meta.SandboxID); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	return 0, b.unbind(ctx, meta.SessionID, meta.SandboxID)
}

// RunReaper drives the existing sandbox lease lifecycle. It never scans or
// reconciles PVCs and cannot delete Session Storage.
func (b *Binder) RunReaper(ctx context.Context) {
	ticker := time.NewTicker(b.cfg.ReaperEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			_ = b.reapOnce(ctx, now)
		}
	}
}
