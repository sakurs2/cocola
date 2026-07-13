package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
	"github.com/cocola-project/cocola/packages/go-common/token"
	"golang.org/x/crypto/bcrypt"
)

func authTestClock() time.Time { return time.Unix(1_700_000_000, 0).UTC() }

func boolPtr(v bool) *bool { return &v }

func stringPtr(v string) *string { return &v }

func TestBootstrapAdminAuthenticateAndReset(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	svc := New(st, token.NewIssuer("secret", "cocola", time.Hour), authTestClock)

	if err := svc.BootstrapAdmin(ctx, BootstrapAdminInput{
		Username: "Admin",
		Email:    "Admin@Example.COM",
		Password: "first-password",
		Actor:    "test",
	}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	u, err := st.GetAuthUserByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("get bootstrap admin: %v", err)
	}
	if u.Username != "admin" || u.Role != RoleAdmin || !u.Enabled {
		t.Fatalf("bootstrap user role/enabled wrong: %+v", u)
	}
	if strings.Contains(u.PasswordHash, "first-password") {
		t.Fatalf("password stored in plaintext: %q", u.PasswordHash)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("first-password")); err != nil {
		t.Fatalf("bootstrap password hash mismatch: %v", err)
	}

	if _, err := svc.Authenticate(ctx, "admin@example.com", "first-password"); err != nil {
		t.Fatalf("authenticate bootstrap admin: %v", err)
	}
	if _, err := svc.Authenticate(ctx, "admin", "first-password"); err != nil {
		t.Fatalf("authenticate bootstrap admin by username: %v", err)
	}

	// Idempotent bootstrap does not overwrite an existing admin unless reset is explicit.
	if err := svc.BootstrapAdmin(ctx, BootstrapAdminInput{
		Email:    "admin@example.com",
		Password: "second-password",
		Actor:    "test",
	}); err != nil {
		t.Fatalf("idempotent bootstrap: %v", err)
	}
	if _, err := svc.Authenticate(ctx, "admin@example.com", "second-password"); err == nil {
		t.Fatalf("bootstrap without reset should not replace the password")
	}

	if err := svc.BootstrapAdmin(ctx, BootstrapAdminInput{
		Email:    "admin@example.com",
		Password: "second-password",
		Reset:    true,
		Actor:    "test",
	}); err != nil {
		t.Fatalf("bootstrap reset: %v", err)
	}
	if _, err := svc.Authenticate(ctx, "admin@example.com", "second-password"); err != nil {
		t.Fatalf("authenticate after reset: %v", err)
	}
	u, err = st.GetAuthUserByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("get protected bootstrap admin: %v", err)
	}
	if u.CreatedBy != bootstrapActor {
		t.Fatalf("bootstrap admin should be marked protected: %+v", u)
	}
	if _, err := svc.SetAuthUser(ctx, u.ID, AuthUserInput{
		Role:  RoleUser,
		Actor: "admin",
	}); !errors.Is(err, ErrProtectedAdmin) {
		t.Fatalf("bootstrap admin downgrade want ErrProtectedAdmin, got %v", err)
	}
	if _, err := svc.SetAuthUser(ctx, u.ID, AuthUserInput{
		Enabled: boolPtr(false),
		Actor:   "admin",
	}); !errors.Is(err, ErrProtectedAdmin) {
		t.Fatalf("bootstrap admin disable want ErrProtectedAdmin, got %v", err)
	}
	if err := svc.DeleteAuthUser(ctx, u.ID, "admin"); !errors.Is(err, ErrProtectedAdmin) {
		t.Fatalf("bootstrap admin delete want ErrProtectedAdmin, got %v", err)
	}
}

func TestAuthUserLifecycleAndRuntimeToken(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	svc := New(st, token.NewIssuer("secret", "cocola", time.Hour), authTestClock)

	created, err := svc.CreateAuthUser(ctx, AuthUserInput{
		Username: "WJH",
		Email:    "test@email",
		Role:     RoleUser,
		Enabled:  boolPtr(true),
		Password: "user-password",
		Actor:    "admin",
	})
	if err != nil {
		t.Fatalf("create auth user: %v", err)
	}
	if created.PasswordHash == "user-password" || created.PasswordHash == "" {
		t.Fatalf("password hash not stored safely: %q", created.PasswordHash)
	}

	login, err := svc.Authenticate(ctx, "test@email", "user-password")
	if err != nil {
		t.Fatalf("authenticate user: %v", err)
	}
	if login.ID != created.ID || login.Username != "wjh" || login.Role != RoleUser {
		t.Fatalf("login payload wrong: %+v", login)
	}
	if _, err := svc.Authenticate(ctx, "wjh", "user-password"); err != nil {
		t.Fatalf("authenticate user by username: %v", err)
	}
	if _, err := svc.Authenticate(ctx, "test", "user-password"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("email local-part should not authenticate as username, got %v", err)
	}
	if _, err := svc.CreateAuthUser(ctx, AuthUserInput{
		Username: "test@email",
		Email:    "other@email",
		Password: "other-password",
		Actor:    "admin",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("identifier reused across kinds should conflict, got %v", err)
	}

	tok, err := svc.IssueRuntimeToken(ctx, created.Email, 10*time.Minute)
	if err != nil {
		t.Fatalf("runtime token: %v", err)
	}
	claims, err := token.Decode(tok, "secret", authTestClock().Unix())
	if err != nil {
		t.Fatalf("decode runtime token: %v", err)
	}
	if claims.Subject != "test@email" || claims.Tenant != "" {
		t.Fatalf("runtime claims wrong: %+v", claims)
	}

	disabled, err := svc.SetAuthUser(ctx, created.ID, AuthUserInput{
		Enabled: boolPtr(false),
		Actor:   "admin",
	})
	if err != nil {
		t.Fatalf("disable auth user: %v", err)
	}
	if disabled.Enabled {
		t.Fatalf("expected disabled user: %+v", disabled)
	}
	if _, err := svc.Authenticate(ctx, "test@email", "user-password"); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("disabled login want ErrAccountDisabled, got %v", err)
	}
	if _, err := svc.IssueRuntimeToken(ctx, created.Email, 10*time.Minute); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("disabled runtime token want ErrAccountDisabled, got %v", err)
	}
	reenabled, err := svc.SetAuthUser(ctx, created.ID, AuthUserInput{
		Enabled: boolPtr(true),
		Actor:   "admin",
	})
	if err != nil {
		t.Fatalf("reenable auth user: %v", err)
	}
	if err := svc.DeleteAuthUser(ctx, reenabled.ID, "admin"); err != nil {
		t.Fatalf("delete auth user: %v", err)
	}
	listed, err := svc.ListAuthUsers(ctx)
	if err != nil {
		t.Fatalf("list auth users: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("deleted user should be hidden from list: %+v", listed)
	}
	if _, err := svc.Authenticate(ctx, "test@email", "user-password"); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("deleted login want ErrAccountDisabled, got %v", err)
	}
	if _, err := svc.CreateAuthUser(ctx, AuthUserInput{
		Username: "new-wjh",
		Email:    "test@email",
		Password: "new-password",
		Actor:    "admin",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("deleted user's email should still conflict, got %v", err)
	}
	if err := svc.DeleteAuthUser(ctx, reenabled.ID, "admin"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted user cannot be restored/redeleted, got %v", err)
	}
}

func TestAdminCannotChangeOwnPermissions(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	svc := New(st, token.NewIssuer("secret", "cocola", time.Hour), authTestClock)

	admin, err := svc.CreateAuthUser(ctx, AuthUserInput{
		Username: "self-admin",
		Email:    "self@example.com",
		Role:     RoleAdmin,
		Enabled:  boolPtr(true),
		Password: "admin-password",
		Actor:    "owner@example.com",
	})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if _, err := svc.SetAuthUser(ctx, admin.ID, AuthUserInput{
		Role:  RoleUser,
		Actor: "self@example.com",
	}); !errors.Is(err, ErrSelfPermission) {
		t.Fatalf("self role change want ErrSelfPermission, got %v", err)
	}
	if _, err := svc.SetAuthUser(ctx, admin.ID, AuthUserInput{
		Enabled: boolPtr(false),
		Actor:   "SELF@example.com",
	}); !errors.Is(err, ErrSelfPermission) {
		t.Fatalf("self disable want ErrSelfPermission, got %v", err)
	}
	if err := svc.DeleteAuthUser(ctx, admin.ID, "self@example.com"); !errors.Is(err, ErrSelfPermission) {
		t.Fatalf("self delete want ErrSelfPermission, got %v", err)
	}
	if _, err := svc.SetAuthUser(ctx, admin.ID, AuthUserInput{
		Role:  RoleUser,
		Actor: "other-admin@example.com",
	}); err != nil {
		t.Fatalf("different admin should be able to change role: %v", err)
	}
}

func TestRuntimeTokenTenantFromUserRecord(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	svc := New(st, token.NewIssuer("secret", "cocola", time.Hour), authTestClock)

	created, err := svc.CreateAuthUser(ctx, AuthUserInput{
		Username: "member",
		Email:    "member@example.com",
		Tenant:   stringPtr("team-alpha"),
		Role:     RoleUser,
		Enabled:  boolPtr(true),
		Password: "member-password",
		Actor:    "admin",
	})
	if err != nil {
		t.Fatalf("create auth user: %v", err)
	}
	if created.TenantID != "team-alpha" {
		t.Fatalf("tenant not stored on create: %+v", created)
	}

	// The persisted tenant is authoritative even when the caller passes nothing.
	tok, err := svc.IssueRuntimeToken(ctx, created.Email, 10*time.Minute)
	if err != nil {
		t.Fatalf("runtime token: %v", err)
	}
	claims, err := token.Decode(tok, "secret", authTestClock().Unix())
	if err != nil {
		t.Fatalf("decode runtime token: %v", err)
	}
	if claims.Tenant != "team-alpha" {
		t.Fatalf("runtime token tenant want team-alpha, got %q", claims.Tenant)
	}

	// Reassigning the team updates subsequent tokens.
	if _, err := svc.SetAuthUser(ctx, created.ID, AuthUserInput{
		Tenant: stringPtr("team-beta"),
		Actor:  "admin",
	}); err != nil {
		t.Fatalf("update tenant: %v", err)
	}
	tok3, err := svc.IssueRuntimeToken(ctx, created.Email, 10*time.Minute)
	if err != nil {
		t.Fatalf("runtime token 3: %v", err)
	}
	claims3, err := token.Decode(tok3, "secret", authTestClock().Unix())
	if err != nil {
		t.Fatalf("decode runtime token 3: %v", err)
	}
	if claims3.Tenant != "team-beta" {
		t.Fatalf("runtime token tenant want team-beta, got %q", claims3.Tenant)
	}
}
