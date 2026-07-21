package project

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func testSecretBox(t *testing.T) *secretBox {
	t.Helper()
	box, err := newSecretBox(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatalf("newSecretBox: %v", err)
	}
	return box
}

func TestSecretBoxBindsCiphertextToIdentityAndField(t *testing.T) {
	box := testSecretBox(t)
	identity := Identity{TenantID: "tenant-a", UserID: "user-a"}
	ciphertext, err := box.encrypt("github-token", tokenAAD(identity, "access_token"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	plain, err := box.decrypt(ciphertext, tokenAAD(identity, "access_token"))
	if err != nil || plain != "github-token" {
		t.Fatalf("decrypt = %q, %v", plain, err)
	}
	if _, err := box.decrypt(ciphertext, tokenAAD(Identity{TenantID: "tenant-a", UserID: "user-b"}, "access_token")); err == nil {
		t.Fatal("decrypt with another user unexpectedly succeeded")
	}
	if _, err := box.decrypt(ciphertext, tokenAAD(identity, "refresh_token")); err == nil {
		t.Fatal("decrypt with another field unexpectedly succeeded")
	}
}

func TestOAuthStateBindsUserExpiresAndSanitizesReturnPath(t *testing.T) {
	box := testSecretBox(t)
	identity := Identity{TenantID: "tenant-a", UserID: "user-a"}
	now := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)
	state, err := box.signState(identity, "https://evil.example/steal", now)
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	decoded, err := box.verifyState(state, identity, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("verifyState: %v", err)
	}
	if decoded.ReturnTo != "/projects/new" {
		t.Fatalf("returnTo = %q", decoded.ReturnTo)
	}
	if _, err := box.verifyState(state, Identity{TenantID: "tenant-a", UserID: "user-b"}, now); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("other user error = %v", err)
	}
	if _, err := box.verifyState(state, identity, now.Add(11*time.Minute)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expired state error = %v", err)
	}
}

func TestProjectConfigIsDisabledWhenEmptyAndRejectsPartialConfig(t *testing.T) {
	if err := (Config{}).validate(); err != nil {
		t.Fatalf("empty config: %v", err)
	}
	err := (Config{AppID: "123", MaxRepositoryMB: 512}).validate()
	if err == nil || !strings.Contains(err.Error(), "COCOLA_GITHUB_CLIENT_ID") || !strings.Contains(err.Error(), "COCOLA_SCM_SECRET_KEY") {
		t.Fatalf("partial config error = %v", err)
	}
	if err := (Config{ConfigurationPresent: true, MaxRepositoryMB: 512}).validate(); err == nil {
		t.Fatal("an unreadable or empty _FILE-only configuration was treated as disabled")
	}
}

func TestCreateValidationSeparatesCreateAndImportRepositoryIDs(t *testing.T) {
	base := CreateInput{
		ClientRequestID: "request-1", Name: "Project", RuntimeID: "claude-code",
		RepositoryName: "repo", Visibility: "private",
	}
	create := base
	create.Mode = "create"
	if err := validateCreate(create); err != nil {
		t.Fatalf("valid create: %v", err)
	}
	create.RepositoryID = 42
	if !errors.Is(validateCreate(create), ErrInvalidArgument) {
		t.Fatal("create accepted a client-supplied repository id")
	}
	importInput := base
	importInput.Mode = "import"
	if !errors.Is(validateCreate(importInput), ErrInvalidArgument) {
		t.Fatal("import accepted an empty repository id")
	}
	importInput.RepositoryID = 42
	if err := validateCreate(importInput); err != nil {
		t.Fatalf("valid import: %v", err)
	}
}
