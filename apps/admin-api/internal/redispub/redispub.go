// Package redispub is the admin-api's Redis-backed store.Publisher: it pushes
// the two control-plane resources the llm-gateway reads on its hot path —
// revoked token ids and per-subject quota overrides — to the exact Redis keys
// and shapes the gateway's Python stores expect.
//
// Key compatibility is the whole point; these MUST match the gateway side:
//
//	revocations : SET  "cocola:revoked"            members are token ids (jti)
//	              -> RedisRevocationStore (SISMEMBER) in auth/revocation.py
//	overrides   : HASH "cocola:quota:override"     field "scope/subject" -> limit
//	              -> RedisOverrideStore (HGET) in quota/overrides.py
//
// The admin-api owns the authoritative records (store.Store); this publisher is
// the propagation seam (wrapped by store.Mirror) so a revoke/override the admin
// performs is visible to every gateway replica without a redeploy. The shared
// Redis keys outlive an admin-api restart; durable authoritative storage (and
// boot-time reconciliation) lands with the M7 Store backend.
package redispub

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/cocola-project/cocola/apps/admin-api/internal/service"
	goredis "github.com/redis/go-redis/v9"
)

// Key names + the field separator, kept identical to the gateway's Python side.
const (
	RevokedKey   = "cocola:revoked"
	OverrideKey  = "cocola:quota:override"
	UserEventsCh = "cocola:user-events"
	fieldSepRune = "/"
)

func overrideField(scope, subject string) string { return scope + fieldSepRune + subject }

// Config describes how to reach Redis. Mirrors go-common/redis.ConfigFromEnv so
// the admin-api and the orchestrator share one set of COCOLA_REDIS_* vars.
type Config struct {
	Addr     string
	Password string
	DB       int
	PoolSize int
}

// Publisher is the Redis-backed store.Publisher implementation.
type Publisher struct {
	rdb         *goredis.Client
	revokedKey  string
	overrideKey string
	eventsCh    string
}

// New dials Redis, verifies the connection with a Ping, and returns a Publisher.
func New(ctx context.Context, cfg Config) (*Publisher, error) {
	rdb := goredis.NewClient(&goredis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: cfg.PoolSize,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	return &Publisher{rdb: rdb, revokedKey: RevokedKey, overrideKey: OverrideKey, eventsCh: UserEventsCh}, nil
}

// Revoke adds a token id to the shared denylist set (SADD). Idempotent.
func (p *Publisher) Revoke(ctx context.Context, tokenID string) error {
	if tokenID == "" {
		return nil
	}
	return p.rdb.SAdd(ctx, p.revokedKey, tokenID).Err()
}

// SetQuota upserts a per-subject override into the shared hash (HSET). A limit of
// 0 is stored verbatim and means "explicitly unlimited" on the gateway side.
func (p *Publisher) SetQuota(ctx context.Context, scope, subject string, limit int64) error {
	if subject == "" {
		return nil
	}
	return p.rdb.HSet(ctx, p.overrideKey, overrideField(scope, subject), strconv.FormatInt(limit, 10)).Err()
}

// DeleteQuota removes a per-subject override from the shared hash (HDEL).
func (p *Publisher) DeleteQuota(ctx context.Context, scope, subject string) error {
	if subject == "" {
		return nil
	}
	return p.rdb.HDel(ctx, p.overrideKey, overrideField(scope, subject)).Err()
}

func (p *Publisher) PublishUserEvent(ctx context.Context, event service.UserEvent) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return p.rdb.Publish(ctx, p.eventsCh, raw).Err()
}

func (p *Publisher) SubscribeUserEvents(ctx context.Context) (<-chan service.UserEvent, func(), error) {
	sub := p.rdb.Subscribe(ctx, p.eventsCh)
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, func() {}, err
	}
	out := make(chan service.UserEvent, 32)
	done := make(chan struct{})
	go func() {
		defer close(out)
		ch := sub.Channel()
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var event service.UserEvent
				if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
					continue
				}
				select {
				case out <- event:
				case <-done:
					return
				case <-ctx.Done():
					return
				}
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	cancel := func() {
		select {
		case <-done:
		default:
			close(done)
			_ = sub.Close()
		}
	}
	return out, cancel, nil
}

// Close releases the connection pool.
func (p *Publisher) Close() error { return p.rdb.Close() }
