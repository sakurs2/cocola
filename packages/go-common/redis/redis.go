// Package redis is a thin wrapper around go-redis exposing only the primitive
// surface the sandbox orchestrator needs: atomic SET-NX, scripted (Lua) ops,
// key scanning, and TTL management.
//
// Why a wrapper and not raw go-redis everywhere: the orchestrator's binding
// state (session<->sandbox mappings, locks, leases) is the kind of thing a
// second-developer may want to back with something other than Redis. By
// funnelling all access through the KV interface here, swapping the backend
// later is additive — implement KV, no orchestrator changes.
package redis

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// ErrNil mirrors a "key does not exist" result without leaking the go-redis type.
var ErrNil = errors.New("redis: nil")

// KV is the minimal contract the orchestrator depends on. Anything that can
// satisfy these methods (Redis, an in-memory fake for tests, a future
// alternative store) is a drop-in backend.
type KV interface {
	// Get returns ErrNil if the key is absent.
	Get(ctx context.Context, key string) (string, error)
	// Set writes a value with an optional TTL (0 = no expiry).
	Set(ctx context.Context, key, val string, ttl time.Duration) error
	// SetNX sets the key only if absent, with a TTL. Returns true if it won.
	SetNX(ctx context.Context, key, val string, ttl time.Duration) (bool, error)
	// Del removes keys, returning the count removed.
	Del(ctx context.Context, keys ...string) (int64, error)
	// Expire (re)sets a key's TTL. Returns false if the key is gone.
	Expire(ctx context.Context, key string, ttl time.Duration) (bool, error)
	// Eval runs a Lua script atomically (used for CAS release & dual-write).
	Eval(ctx context.Context, script string, keys []string, args ...any) (any, error)
	// ScanKeys iterates keys matching pattern, invoking fn per batch. The
	// callback must not retain the slice beyond the call.
	ScanKeys(ctx context.Context, pattern string, batch int64, fn func(keys []string) error) error
	// Ping verifies connectivity.
	Ping(ctx context.Context) error
	// Close releases the underlying connection pool.
	Close() error
}

// Config describes how to reach Redis. Populate via ConfigFromEnv or by hand.
type Config struct {
	Addr     string // host:port
	Password string
	DB       int
	PoolSize int
}

// ConfigFromEnv reads COCOLA_REDIS_* with Mira-friendly defaults.
func ConfigFromEnv() Config {
	return Config{
		Addr:     env("COCOLA_REDIS_ADDR", "localhost:6379"),
		Password: env("COCOLA_REDIS_PASSWORD", ""),
		DB:       envInt("COCOLA_REDIS_DB", 0),
		PoolSize: envInt("COCOLA_REDIS_POOL_SIZE", 10),
	}
}

// Client is the Redis-backed KV implementation.
type Client struct {
	rdb *goredis.Client
}

// compile-time assertion that Client satisfies KV.
var _ KV = (*Client)(nil)

// New dials Redis and verifies the connection with a Ping.
func New(ctx context.Context, cfg Config) (*Client, error) {
	rdb := goredis.NewClient(&goredis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: cfg.PoolSize,
	})
	c := &Client{rdb: rdb}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.Ping(pctx); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) Get(ctx context.Context, key string) (string, error) {
	v, err := c.rdb.Get(ctx, key).Result()
	if errors.Is(err, goredis.Nil) {
		return "", ErrNil
	}
	return v, err
}

func (c *Client) Set(ctx context.Context, key, val string, ttl time.Duration) error {
	return c.rdb.Set(ctx, key, val, ttl).Err()
}

func (c *Client) SetNX(ctx context.Context, key, val string, ttl time.Duration) (bool, error) {
	return c.rdb.SetNX(ctx, key, val, ttl).Result()
}

func (c *Client) Del(ctx context.Context, keys ...string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	return c.rdb.Del(ctx, keys...).Result()
}

func (c *Client) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return c.rdb.Expire(ctx, key, ttl).Result()
}

func (c *Client) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	res, err := c.rdb.Eval(ctx, script, keys, args...).Result()
	if errors.Is(err, goredis.Nil) {
		return nil, ErrNil
	}
	return res, err
}

func (c *Client) ScanKeys(ctx context.Context, pattern string, batch int64, fn func(keys []string) error) error {
	var cursor uint64
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, pattern, batch).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := fn(keys); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

func (c *Client) Close() error {
	return c.rdb.Close()
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
