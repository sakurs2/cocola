package store

// Postgres/Memory parity test. The same scenario runs against both backends so
// the durable store is verified to behave identically to the in-memory one the
// rest of the suite already trusts.
//
// The Postgres leg is gated on COCOLA_TEST_PG_DSN: when unset the test skips
// (so `go test ./...` stays zero-dependency). To run it locally:
//
//	docker run --rm -d --name pgtest -e POSTGRES_USER=cocola \
//	  -e POSTGRES_PASSWORD=cocola_dev_pw -e POSTGRES_DB=cocola -p 5432:5432 \
//	  postgres:16-alpine
//	COCOLA_TEST_PG_DSN='postgres://cocola:cocola_dev_pw@localhost:5432/cocola?sslmode=disable' \
//	  go test ./internal/store/ -run Parity -v

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// newParityPostgres applies migrations, truncates all tables for a clean slate,
// and returns a connected Postgres store. It skips the test if no DSN is set.
func newParityPostgres(t *testing.T) *Postgres {
	t.Helper()
	dsn := os.Getenv("COCOLA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("COCOLA_TEST_PG_DSN not set; skipping Postgres parity leg")
	}
	ctx := context.Background()
	if err := Migrate(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pg, err := NewPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// Clean slate: truncate every table this suite touches.
	_, err = pg.pool.Exec(ctx,
		`TRUNCATE auth_user_identifiers, auth_users, token_records, quota_overrides, skill_entries, audit_log, audit_events RESTART IDENTITY`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
	t.Cleanup(pg.Close)
	return pg
}

// runStoreContract exercises the full Store contract. It is backend-agnostic so
// the same assertions cover Memory and Postgres.
func runStoreContract(t *testing.T, st Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()

	// ----- auth users -----
	user := AuthUser{
		ID:              "user-1",
		Username:        "alice",
		Email:           "alice@example.com",
		Name:            "Alice",
		TenantID:        "team-a",
		Role:            "user",
		Enabled:         true,
		PasswordHash:    "$2a$10$012345678901234567890u012345678901234567890123456789012",
		CreatedAt:       now,
		UpdatedAt:       now,
		CreatedBy:       "admin",
		UpdatedBy:       "admin",
		PasswordUpdated: now,
	}
	if err := st.CreateAuthUser(ctx, user); err != nil {
		t.Fatalf("CreateAuthUser: %v", err)
	}
	if err := st.CreateAuthUser(ctx, AuthUser{ID: "user-2", Email: user.Email}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate auth email want ErrConflict, got %v", err)
	}
	if err := st.CreateAuthUser(ctx, AuthUser{ID: "user-3", Username: user.Username, Email: "other@example.com"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate auth username want ErrConflict, got %v", err)
	}
	gotUser, err := st.GetAuthUserByEmail(ctx, "alice@example.com")
	if err != nil || gotUser.ID != "user-1" || gotUser.PasswordHash == "" {
		t.Fatalf("GetAuthUserByEmail roundtrip: %+v %v", gotUser, err)
	}
	if gotUser.TenantID != "team-a" {
		t.Fatalf("tenant not persisted: %+v", gotUser)
	}
	gotUser, err = st.GetAuthUserByIdentifier(ctx, "alice")
	if err != nil || gotUser.ID != "user-1" || gotUser.PasswordHash == "" {
		t.Fatalf("GetAuthUserByIdentifier username roundtrip: %+v %v", gotUser, err)
	}
	gotUser, err = st.GetAuthUserByIdentifier(ctx, "alice@example.com")
	if err != nil || gotUser.ID != "user-1" || gotUser.PasswordHash == "" {
		t.Fatalf("GetAuthUserByIdentifier email roundtrip: %+v %v", gotUser, err)
	}
	gotUser.Role = "admin"
	gotUser.Enabled = false
	gotUser.TenantID = "team-b"
	gotUser.UpdatedAt = now.Add(time.Hour)
	if err := st.UpdateAuthUser(ctx, gotUser); err != nil {
		t.Fatalf("UpdateAuthUser: %v", err)
	}
	updated, _ := st.GetAuthUser(ctx, "user-1")
	if updated.Role != "admin" || updated.Enabled || updated.TenantID != "team-b" {
		t.Fatalf("auth user update not persisted: %+v", updated)
	}
	if err := st.TouchAuthUserLogin(ctx, "user-1", now.Add(2*time.Hour)); err != nil {
		t.Fatalf("TouchAuthUserLogin: %v", err)
	}
	touched, _ := st.GetAuthUser(ctx, "user-1")
	if touched.LastLoginAt.IsZero() {
		t.Fatalf("last login not updated: %+v", touched)
	}
	users, _ := st.ListAuthUsers(ctx)
	if len(users) != 1 || users[0].Email != "alice@example.com" {
		t.Fatalf("ListAuthUsers: %+v", users)
	}
	if err := st.DeleteAuthUser(ctx, "user-1", "admin", now.Add(3*time.Hour)); err != nil {
		t.Fatalf("DeleteAuthUser: %v", err)
	}
	users, _ = st.ListAuthUsers(ctx)
	if len(users) != 0 {
		t.Fatalf("deleted auth user should be hidden from list: %+v", users)
	}
	deletedUser, err := st.GetAuthUserByIdentifier(ctx, "alice")
	if err != nil || deletedUser.ID != "user-1" || deletedUser.Enabled {
		t.Fatalf("deleted auth user lookup should remain reserved and disabled: %+v %v", deletedUser, err)
	}
	if deletedUser.DeletedAt.IsZero() || deletedUser.DeletedBy != "admin" {
		t.Fatalf("deleted auth user metadata missing: %+v", deletedUser)
	}
	if err := st.DeleteAuthUser(ctx, "user-1", "admin", now.Add(4*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteAuthUser already deleted want ErrNotFound, got %v", err)
	}

	// ----- tokens -----
	tok := TokenRecord{ID: "tok-1", UserID: "u1", TenantID: "t1", Issuer: "cocola", IssuedAt: now, CreatedBy: "admin"}
	if err := st.CreateToken(ctx, tok); err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if err := st.CreateToken(ctx, tok); !errors.Is(err, ErrConflict) {
		t.Fatalf("dup CreateToken want ErrConflict, got %v", err)
	}
	got, err := st.GetToken(ctx, "tok-1")
	if err != nil || got.UserID != "u1" || got.CreatedBy != "admin" {
		t.Fatalf("GetToken roundtrip: %+v %v", got, err)
	}
	if _, err := st.GetToken(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetToken missing want ErrNotFound, got %v", err)
	}
	rev, err := st.IsRevoked(ctx, "tok-1")
	if err != nil || rev {
		t.Fatalf("fresh token revoked? %v %v", rev, err)
	}
	revAt := now.Add(time.Hour)
	if err := st.RevokeToken(ctx, "tok-1", revAt); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if err := st.RevokeToken(ctx, "ghost", revAt); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke missing want ErrNotFound, got %v", err)
	}
	rev, _ = st.IsRevoked(ctx, "tok-1")
	if !rev {
		t.Fatal("token should be revoked")
	}
	// list filter + newest-first
	_ = st.CreateToken(ctx, TokenRecord{ID: "tok-2", UserID: "u1", IssuedAt: now.Add(2 * time.Hour)})
	_ = st.CreateToken(ctx, TokenRecord{ID: "tok-3", UserID: "u2", IssuedAt: now.Add(3 * time.Hour)})
	u1, _ := st.ListTokens(ctx, "u1")
	if len(u1) != 2 || u1[0].ID != "tok-2" {
		t.Fatalf("ListTokens(u1) filter/sort wrong: %+v", u1)
	}
	all, _ := st.ListTokens(ctx, "")
	if len(all) != 3 {
		t.Fatalf("ListTokens(all) want 3, got %d", len(all))
	}

	// ----- quota -----
	_ = st.SetQuota(ctx, QuotaOverride{Scope: "user", Subject: "u1", Limit: 100, UpdatedAt: now, UpdatedBy: "admin"})
	_ = st.SetQuota(ctx, QuotaOverride{Scope: "user", Subject: "u1", Limit: 200, UpdatedAt: now, UpdatedBy: "admin2"}) // upsert
	q, err := st.GetQuota(ctx, "user", "u1")
	if err != nil || q.Limit != 200 || q.UpdatedBy != "admin2" {
		t.Fatalf("quota upsert: %+v %v", q, err)
	}
	_ = st.SetQuota(ctx, QuotaOverride{Scope: "tenant", Subject: "t1", Limit: 50, UpdatedAt: now})
	qs, _ := st.ListQuotas(ctx)
	if len(qs) != 2 || qs[0].Scope != "tenant" {
		t.Fatalf("ListQuotas sort/count: %+v", qs)
	}
	if err := st.DeleteQuota(ctx, "user", "u1"); err != nil {
		t.Fatalf("DeleteQuota: %v", err)
	}
	if err := st.DeleteQuota(ctx, "user", "u1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing quota want ErrNotFound, got %v", err)
	}

	// ----- skills -----
	sk := Skill{ID: "sk-1", Name: "Weather", Version: "1.0", Entrypoint: "m.weather", Enabled: false, CreatedAt: now, UpdatedAt: now}
	if err := st.CreateSkill(ctx, sk); err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	if err := st.CreateSkill(ctx, sk); !errors.Is(err, ErrConflict) {
		t.Fatalf("dup skill want ErrConflict, got %v", err)
	}
	sk.Enabled = true
	sk.UpdatedAt = now.Add(time.Hour)
	if err := st.UpdateSkill(ctx, sk); err != nil {
		t.Fatalf("UpdateSkill: %v", err)
	}
	if err := st.UpdateSkill(ctx, Skill{ID: "ghost"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing skill want ErrNotFound, got %v", err)
	}
	enabled, _ := st.ListSkills(ctx, true)
	if len(enabled) != 1 || !enabled[0].Enabled {
		t.Fatalf("ListSkills(enabled): %+v", enabled)
	}
	if err := st.DeleteSkill(ctx, "sk-1"); err != nil {
		t.Fatalf("DeleteSkill: %v", err)
	}
	if _, err := st.GetSkill(ctx, "sk-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted skill want ErrNotFound, got %v", err)
	}

	// ----- audit -----
	for i := 0; i < 5; i++ {
		if err := st.AppendAudit(ctx, AuditEntry{At: now, Actor: "admin", Action: "x", Resource: "r"}); err != nil {
			t.Fatalf("AppendAudit: %v", err)
		}
	}
	a, _ := st.ListAudit(ctx, 3)
	if len(a) != 3 {
		t.Fatalf("ListAudit limit: %d", len(a))
	}
	// newest-first: ids strictly descending
	if !(a[0].ID > a[1].ID && a[1].ID > a[2].ID) {
		t.Fatalf("ListAudit not newest-first: %+v", a)
	}
}

func TestStoreContract_Memory(t *testing.T) {
	runStoreContract(t, NewMemory())
}

func TestStoreContract_Postgres_Parity(t *testing.T) {
	runStoreContract(t, newParityPostgres(t))
}
