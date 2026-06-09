package store

import (
	"context"
	"time"
)

// Publisher propagates control-plane writes to a shared backend the data plane
// reads. The admin-api keeps the authoritative records in its Store; the gateway
// (a separate process) consults Redis on the hot path for two of them —
// revoked token ids and per-subject quota overrides. Mirror bridges the gap by
// pushing those writes to the same Redis keys the gateway reads, so a revoke or
// an override takes effect fleet-wide without a redeploy.
//
// The contract is intentionally narrow: only the two resources the gateway reads
// from Redis are published. Everything else (skills, audit, token metadata)
// stays in the Store alone until M7 makes the Store itself durable.
type Publisher interface {
	// Revoke adds a token id (jti) to the shared denylist set.
	Revoke(ctx context.Context, tokenID string) error
	// SetQuota upserts a per-subject override (limit 0 == explicitly unlimited).
	SetQuota(ctx context.Context, scope, subject string, limit int64) error
	// DeleteQuota removes a per-subject override (the subject reverts to the
	// gateway's static default).
	DeleteQuota(ctx context.Context, scope, subject string) error
	// Close releases the backend connection.
	Close() error
}

// Mirror wraps a Store and mirrors the two gateway-read resources (revocations,
// quota overrides) to a Publisher after the authoritative write succeeds.
//
// Publish is best-effort: the authoritative write already landed in the inner
// Store, so a Publisher failure must not fail the admin operation. Instead it is
// reported via OnPublishError (nil = ignore) so the caller can log it. This
// mirrors the codebase convention that propagation side-effects (like the audit
// log) never fail the underlying operation.
type Mirror struct {
	Store
	pub Publisher
	// OnPublishError, if set, is called when a mirror publish fails. The op is a
	// short tag ("revoke" / "quota.set" / "quota.delete") for the log line.
	OnPublishError func(op string, err error)
}

// NewMirror wraps inner so its revoke/quota writes also publish to pub. If pub
// is nil the original Store is returned unchanged (no-op wiring).
func NewMirror(inner Store, pub Publisher) Store {
	if pub == nil {
		return inner
	}
	return &Mirror{Store: inner, pub: pub}
}

var _ Store = (*Mirror)(nil)

func (m *Mirror) onErr(op string, err error) {
	if err != nil && m.OnPublishError != nil {
		m.OnPublishError(op, err)
	}
}

// RevokeToken revokes in the inner Store, then publishes the id to the shared
// denylist so every gateway replica rejects it.
func (m *Mirror) RevokeToken(ctx context.Context, id string, at time.Time) error {
	if err := m.Store.RevokeToken(ctx, id, at); err != nil {
		return err
	}
	m.onErr("revoke", m.pub.Revoke(ctx, id))
	return nil
}

// SetQuota upserts in the inner Store, then publishes the override fleet-wide.
func (m *Mirror) SetQuota(ctx context.Context, q QuotaOverride) error {
	if err := m.Store.SetQuota(ctx, q); err != nil {
		return err
	}
	m.onErr("quota.set", m.pub.SetQuota(ctx, q.Scope, q.Subject, q.Limit))
	return nil
}

// DeleteQuota removes from the inner Store, then clears the shared override.
func (m *Mirror) DeleteQuota(ctx context.Context, scope, subject string) error {
	if err := m.Store.DeleteQuota(ctx, scope, subject); err != nil {
		return err
	}
	m.onErr("quota.delete", m.pub.DeleteQuota(ctx, scope, subject))
	return nil
}
