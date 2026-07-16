package redis

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Fake is an in-memory KV implementation for tests. It supports TTL expiry and
// a tiny, purpose-built Lua interpreter covering only the script shapes the
// orchestrator uses (CAS-unlock and the bind dual-write). It is NOT a general
// Redis emulator — keep the supported surface minimal and explicit.
//
// Using a fake here keeps binder/reaper unit tests hermetic (no Redis process),
// while the e2e bench exercises the real go-redis client against real Redis.
type Fake struct {
	mu   sync.Mutex
	data map[string]fakeVal
}

type fakeVal struct {
	val string
	exp time.Time // zero = no expiry
}

// NewFake returns an empty in-memory KV.
func NewFake() *Fake { return &Fake{data: map[string]fakeVal{}} }

var _ KV = (*Fake)(nil)

func (f *Fake) now() time.Time { return time.Now() }

// liveLocked returns the value if present and unexpired; caller holds the lock.
func (f *Fake) liveLocked(key string) (string, bool) {
	v, ok := f.data[key]
	if !ok {
		return "", false
	}
	if !v.exp.IsZero() && f.now().After(v.exp) {
		delete(f.data, key)
		return "", false
	}
	return v.val, true
}

func (f *Fake) Get(ctx context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, ok := f.liveLocked(key); ok {
		return v, nil
	}
	return "", ErrNil
}

func (f *Fake) Set(ctx context.Context, key, val string, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setLocked(key, val, ttl)
	return nil
}

func (f *Fake) setLocked(key, val string, ttl time.Duration) {
	var exp time.Time
	if ttl > 0 {
		exp = f.now().Add(ttl)
	}
	f.data[key] = fakeVal{val: val, exp: exp}
}

func (f *Fake) SetNX(ctx context.Context, key, val string, ttl time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.liveLocked(key); ok {
		return false, nil
	}
	f.setLocked(key, val, ttl)
	return true, nil
}

func (f *Fake) Del(ctx context.Context, keys ...string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, k := range keys {
		if _, ok := f.data[k]; ok {
			delete(f.data, k)
			n++
		}
	}
	return n, nil
}

func (f *Fake) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, ok := f.liveLocked(key); ok {
		f.setLocked(key, v, ttl)
		return true, nil
	}
	return false, nil
}

// Eval implements just enough Lua to run the orchestrator's two scripts. It
// dispatches on recognizable substrings rather than interpreting Lua.
func (f *Fake) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	// Conditional unbind: remove the forward key only if it still points at the
	// sandbox being removed, then always remove that sandbox's own keys.
	case len(keys) == 4 && strings.Contains(script, `redis.call("GET", KEYS[1]) == ARGV[1]`) &&
		strings.Contains(script, `KEYS[2], KEYS[3], KEYS[4]`):
		cur, ok := f.liveLocked(keys[0])
		if ok && cur == toStr(args[0]) {
			delete(f.data, keys[0])
		}
		var removed int64
		for _, key := range keys[1:] {
			if _, exists := f.data[key]; exists {
				delete(f.data, key)
				removed++
			}
		}
		return removed, nil

	// CAS unlock: delete KEYS[1] iff its value == ARGV[1].
	case strings.Contains(script, `redis.call("DEL", KEYS[1])`) &&
		strings.Contains(script, `== ARGV[1]`):
		cur, ok := f.liveLocked(keys[0])
		if ok && cur == toStr(args[0]) {
			delete(f.data, keys[0])
			return int64(1), nil
		}
		return int64(0), nil

	// CAS bind: an existing forward pointer wins; otherwise write the complete
	// mapping and lease atomically.
	case strings.Contains(script, `redis.call("SET", KEYS[4], "1", "EX"`):
		lockToken, lockOK := f.liveLocked(keys[4])
		if !lockOK || lockToken != toStr(args[4]) {
			return int64(-1), nil
		}
		if _, ok := f.liveLocked(keys[0]); ok {
			return int64(0), nil
		}
		f.setLocked(keys[0], toStr(args[0]), 0)
		f.setLocked(keys[1], toStr(args[1]), 0)
		f.setLocked(keys[2], toStr(args[2]), 0)
		ttl, _ := strconv.Atoi(toStr(args[3]))
		f.setLocked(keys[3], "1", time.Duration(ttl)*time.Second)
		if len(args) > 5 {
			lockTTL, _ := strconv.Atoi(toStr(args[5]))
			f.setLocked(keys[4], lockToken, time.Duration(lockTTL)*time.Millisecond)
		}
		return int64(1), nil
	}
	// Unknown script: surface clearly so a new script isn't silently ignored.
	return nil, ErrNil
}

func (f *Fake) ScanKeys(ctx context.Context, pattern string, batch int64, fn func(keys []string) error) error {
	f.mu.Lock()
	// snapshot matching keys under lock, then release before invoking fn.
	prefix := strings.TrimSuffix(pattern, "*")
	var matched []string
	for k := range f.data {
		if _, ok := f.liveLocked(k); !ok {
			continue
		}
		if strings.HasPrefix(k, prefix) {
			matched = append(matched, k)
		}
	}
	f.mu.Unlock()
	if len(matched) == 0 {
		return nil
	}
	return fn(matched)
}

func (f *Fake) Ping(ctx context.Context) error { return nil }
func (f *Fake) Close() error                   { return nil }

func toStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		return ""
	}
}
