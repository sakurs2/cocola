package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakePub records what the Mirror publishes and can be told to fail, so we can
// assert both the propagation and the best-effort error path without Redis.
type fakePub struct {
	revoked  []string
	setQ     []QuotaOverride
	deleted  []string
	failNext error
}

func (f *fakePub) Revoke(ctx context.Context, id string) error {
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return err
	}
	f.revoked = append(f.revoked, id)
	return nil
}

func (f *fakePub) SetQuota(ctx context.Context, scope, subject string, limit int64) error {
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return err
	}
	f.setQ = append(f.setQ, QuotaOverride{Scope: scope, Subject: subject, Limit: limit})
	return nil
}

func (f *fakePub) DeleteQuota(ctx context.Context, scope, subject string) error {
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return err
	}
	f.deleted = append(f.deleted, scope+"/"+subject)
	return nil
}

func (f *fakePub) Close() error { return nil }

func TestNewMirrorNilPublisherReturnsInner(t *testing.T) {
	inner := NewMemory()
	got := NewMirror(inner, nil)
	if got != Store(inner) {
		t.Fatalf("nil publisher should return the inner store unchanged")
	}
}

func TestMirrorPublishesRevokeAfterStoreWrite(t *testing.T) {
	ctx := context.Background()
	inner := NewMemory()
	if err := inner.CreateToken(ctx, TokenRecord{ID: "jti-1", UserID: "u"}); err != nil {
		t.Fatal(err)
	}
	pub := &fakePub{}
	m := NewMirror(inner, pub)

	if err := m.RevokeToken(ctx, "jti-1", time.Now()); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	// Authoritative write landed...
	rev, err := inner.IsRevoked(ctx, "jti-1")
	if err != nil || !rev {
		t.Fatalf("inner not revoked: rev=%v err=%v", rev, err)
	}
	// ...and was published.
	if len(pub.revoked) != 1 || pub.revoked[0] != "jti-1" {
		t.Fatalf("expected published revoke jti-1, got %v", pub.revoked)
	}
}

func TestMirrorDoesNotPublishWhenStoreWriteFails(t *testing.T) {
	ctx := context.Background()
	inner := NewMemory()
	pub := &fakePub{}
	m := NewMirror(inner, pub)

	// Revoking a non-existent token fails in the inner store; nothing publishes.
	if err := m.RevokeToken(ctx, "missing", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if len(pub.revoked) != 0 {
		t.Fatalf("must not publish when store write fails, got %v", pub.revoked)
	}
}

func TestMirrorPublishesQuotaSetAndDelete(t *testing.T) {
	ctx := context.Background()
	inner := NewMemory()
	pub := &fakePub{}
	m := NewMirror(inner, pub)

	q := QuotaOverride{Scope: "user", Subject: "emp-vip", Limit: 5}
	if err := m.SetQuota(ctx, q); err != nil {
		t.Fatal(err)
	}
	if len(pub.setQ) != 1 || pub.setQ[0].Limit != 5 || pub.setQ[0].Subject != "emp-vip" {
		t.Fatalf("expected published set, got %v", pub.setQ)
	}

	if err := m.DeleteQuota(ctx, "user", "emp-vip"); err != nil {
		t.Fatal(err)
	}
	if len(pub.deleted) != 1 || pub.deleted[0] != "user/emp-vip" {
		t.Fatalf("expected published delete, got %v", pub.deleted)
	}
}

func TestMirrorPublishErrorIsBestEffort(t *testing.T) {
	ctx := context.Background()
	inner := NewMemory()
	if err := inner.CreateToken(ctx, TokenRecord{ID: "jti-2", UserID: "u"}); err != nil {
		t.Fatal(err)
	}
	pub := &fakePub{failNext: errors.New("redis down")}
	var gotOp string
	var gotErr error
	mirror := NewMirror(inner, pub)
	mirror.(*Mirror).OnPublishError = func(op string, err error) { gotOp, gotErr = op, err }

	// The publish fails, but the admin op must still succeed (write already landed).
	if err := mirror.RevokeToken(ctx, "jti-2", time.Now()); err != nil {
		t.Fatalf("revoke must not fail on publish error: %v", err)
	}
	rev, _ := inner.IsRevoked(ctx, "jti-2")
	if !rev {
		t.Fatal("inner store should still be revoked")
	}
	if gotOp != "revoke" || gotErr == nil {
		t.Fatalf("expected OnPublishError(revoke, err), got (%q, %v)", gotOp, gotErr)
	}
}
