package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

func newMCPTestAdmin(mem *store.Memory) *Admin {
	return New(mem, nil, func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }).
		WithConfigSecretKey("mcp-test-secret")
}

func TestInvalidMCPConfigIsRejectedBeforePersistence(t *testing.T) {
	mem := store.NewMemory()
	admin := newMCPTestAdmin(mem)
	tests := []MCPServerInput{
		{ID: "bad-command", Name: "Bad command", Transport: MCPTransportStdio},
		{ID: "bad-url", Name: "Bad URL", Transport: MCPTransportHTTP, URL: "ftp://mcp.example.test"},
	}
	for _, input := range tests {
		if _, err := admin.CreateMCPServer(context.Background(), input); !errors.Is(err, ErrInvalidArg) {
			t.Fatalf("create %s error = %v", input.ID, err)
		}
		if _, err := mem.GetMCPServer(context.Background(), input.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("invalid MCP %s was persisted: %v", input.ID, err)
		}
	}
}

func TestMCPRemoteURLIsEncryptedBeforePersistence(t *testing.T) {
	mem := store.NewMemory()
	admin := newMCPTestAdmin(mem)
	rawURL := "https://user:password@mcp.example.test/api?token=super-secret#private"

	result, err := admin.CreateMCPServer(context.Background(), MCPServerInput{
		ID:        "remote",
		Name:      "Remote",
		Transport: MCPTransportHTTP,
		URL:       rawURL,
		Headers:   map[string]string{"Authorization": "Bearer header-secret"},
	})
	if err != nil {
		t.Fatalf("create MCP server: %v", err)
	}
	if result.Status != "configured" {
		t.Fatalf("status = %q, want configured", result.Status)
	}
	if result.URLHint != "https://mcp.example.test/api" {
		t.Fatalf("URL hint = %q", result.URLHint)
	}

	stored, err := mem.GetMCPServer(context.Background(), "remote")
	if err != nil {
		t.Fatalf("get stored MCP: %v", err)
	}
	if stored.URL != mcpRemoteURLTemplate {
		t.Fatalf("stored URL = %q", stored.URL)
	}
	if bytes.Contains(stored.URLVarCiphertextJSON, []byte("super-secret")) ||
		bytes.Contains(stored.URLVarCiphertextJSON, []byte("user:password")) {
		t.Fatalf("stored ciphertext contains plaintext URL: %s", stored.URLVarCiphertextJSON)
	}
	if bytes.Contains(stored.HeaderCiphertextJSON, []byte("header-secret")) {
		t.Fatalf("stored ciphertext contains plaintext header: %s", stored.HeaderCiphertextJSON)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{rawURL, "super-secret", "user:password", "header-secret"} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatalf("response leaked %q: %s", secret, encoded)
		}
	}
}

func TestMCPCreateAndUpdateOnlyPersistConfiguration(t *testing.T) {
	mem := store.NewMemory()
	admin := newMCPTestAdmin(mem)

	created, err := admin.CreateMCPServer(context.Background(), MCPServerInput{
		ID: "demo", Name: "Original", Transport: MCPTransportStdio, Command: "demo",
	})
	if err != nil {
		t.Fatalf("create MCP: %v", err)
	}
	if created.Status != "configured" {
		t.Fatalf("create status = %q", created.Status)
	}

	updated, err := admin.UpdateMCPServer(context.Background(), "demo", MCPServerInput{Name: "Changed"})
	if err != nil {
		t.Fatalf("update MCP: %v", err)
	}
	if updated.Name != "Changed" || updated.Status != "configured" {
		t.Fatalf("updated MCP = %+v", updated)
	}
}
