package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecretFromEnv_FilePreferredOverEnv(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "auth_secret")
	if err := os.WriteFile(p, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_AUTH_SECRET", "from-env")
	t.Setenv("COCOLA_AUTH_SECRET_FILE", p)

	if got := SecretFromEnv("COCOLA_AUTH_SECRET"); got != "from-file" {
		t.Fatalf("want file value (trailing newline trimmed), got %q", got)
	}
}

func TestSecretFromEnv_EnvFallbackWhenNoFile(t *testing.T) {
	t.Setenv("COCOLA_AUTH_SECRET", "from-env")
	os.Unsetenv("COCOLA_AUTH_SECRET_FILE")

	if got := SecretFromEnv("COCOLA_AUTH_SECRET"); got != "from-env" {
		t.Fatalf("want env value, got %q", got)
	}
}

func TestSecretFromEnv_EnvFallbackWhenFileUnreadable(t *testing.T) {
	t.Setenv("COCOLA_AUTH_SECRET", "from-env")
	t.Setenv("COCOLA_AUTH_SECRET_FILE", "/nonexistent/cocola/secret")

	if got := SecretFromEnv("COCOLA_AUTH_SECRET"); got != "from-env" {
		t.Fatalf("unreadable file should degrade to env, got %q", got)
	}
}

func TestSecretFromEnv_EmptyWhenNeitherSet(t *testing.T) {
	os.Unsetenv("COCOLA_AUTH_SECRET")
	os.Unsetenv("COCOLA_AUTH_SECRET_FILE")

	if got := SecretFromEnv("COCOLA_AUTH_SECRET"); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestSecretFromEnv_TrimsTrailingNewlineOnly(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "k")
	// leading/internal whitespace preserved; only trailing CR/LF trimmed.
	if err := os.WriteFile(p, []byte("  pa ss \r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCOLA_X_FILE", p)
	if got := SecretFromEnv("COCOLA_X"); got != "  pa ss " {
		t.Fatalf("want trailing newline trimmed only, got %q", got)
	}
}
