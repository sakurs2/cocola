package config

import (
	"os"
	"strings"
)

// SecretFromEnv resolves a secret value using the industry-standard "_FILE"
// indirection convention (as used by the Postgres official image, Docker
// secrets, and Vault Agent template rendering): if "<name>_FILE" is set, the
// secret is read from that file path; otherwise it falls back to the "<name>"
// env var; otherwise it is empty.
//
// This is the only seam cocola needs to be Vault-ready without taking a Vault
// SDK dependency (ADR-0008 §5): a Vault Agent Sidecar renders the secret to a
// file (e.g. /vault/secrets/auth_secret) and the operator points "<name>_FILE"
// at it, so the application reads a file and stays oblivious to Vault. The dev
// .env flow is unchanged: with no "_FILE" set, behavior is identical to a plain
// os.Getenv.
//
// A trailing newline is trimmed from file contents (templating tools commonly
// append one). On a read error the file path is treated as absent and the
// "<name>" env fallback applies, so a transient mount gap degrades to env
// rather than crashing.
func SecretFromEnv(name string) string {
	if path := strings.TrimSpace(os.Getenv(name + "_FILE")); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			return strings.TrimRight(string(data), "\r\n")
		}
	}
	return os.Getenv(name)
}
