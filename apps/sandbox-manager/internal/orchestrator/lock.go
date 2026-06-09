package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	rds "github.com/cocola-project/cocola/packages/go-common/redis"
)

// ErrLockHeld is returned by tryLock when another holder owns the lock.
var ErrLockHeld = errors.New("orchestrator: lock held by another owner")

// luaUnlock is a compare-and-delete: only delete the lock if WE still own it
// (value matches our token). Prevents a slow holder from deleting a lock that a
// newer holder has since acquired after our TTL lapsed.
const luaUnlock = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
else
	return 0
end`

// distLock is a single acquired distributed lock instance. It carries the token
// so release can prove ownership.
type distLock struct {
	kv    rds.KV
	key   string
	token string
}

// newToken mints an unguessable lock owner token.
func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// tryLock attempts a single non-blocking SET NX EX. On contention it returns
// ErrLockHeld so the caller can decide whether to spin-wait or bail.
func tryLock(ctx context.Context, kv rds.KV, sessionID string, ttl time.Duration) (*distLock, error) {
	key := lockKey(sessionID)
	token := newToken()
	ok, err := kv.SetNX(ctx, key, token, ttl)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrLockHeld
	}
	return &distLock{kv: kv, key: key, token: token}, nil
}

// acquireLock spins on tryLock until success, ctx cancellation, or deadline.
// Used by Acquire so concurrent first-touch requests for the same session
// serialize on creation rather than racing to create duplicate sandboxes.
func acquireLock(ctx context.Context, kv rds.KV, sessionID string, ttl, retry time.Duration) (*distLock, error) {
	for {
		l, err := tryLock(ctx, kv, sessionID, ttl)
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, ErrLockHeld) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retry):
		}
	}
}

// release performs the CAS unlock. Safe to call even if the lock already
// expired — it simply no-ops when the stored token no longer matches.
func (l *distLock) release(ctx context.Context) error {
	_, err := l.kv.Eval(ctx, luaUnlock, []string{l.key}, l.token)
	return err
}
