package project

import "testing"

func TestGitAuthorIdentityUsesCocolaEmail(t *testing.T) {
	name, email := gitAuthorIdentity(Identity{
		UserID: "user-1", Name: "Alice Example", Username: "alice", Email: "alice@example.com",
	})
	if name != "Alice Example" || email != "alice@example.com" {
		t.Fatalf("gitAuthorIdentity() = %q, %q", name, email)
	}
}

func TestGitAuthorIdentityFallsBackToCocolaUsername(t *testing.T) {
	name, email := gitAuthorIdentity(Identity{UserID: "user-1", Username: "alice"})
	if name != "alice" || email != "alice@localhost" {
		t.Fatalf("gitAuthorIdentity() = %q, %q", name, email)
	}
}
