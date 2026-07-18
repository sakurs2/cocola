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

func TestMigrateLegacyMCPSecrets(t *testing.T) {
	mem := store.NewMemory()
	defaultEnabled := true
	legacy := New(mem, nil, func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }).
		WithModelSecretKey("legacy-model-secret").
		WithConfigSecretKey("legacy-model-secret")
	_, err := legacy.CreateMCPServer(context.Background(), MCPServerInput{
		ID:             "remote",
		Name:           "Remote",
		Transport:      MCPTransportHTTP,
		URL:            "https://mcp.example.test/api?token=secret",
		Headers:        map[string]string{"Authorization": "Bearer secret"},
		DefaultEnabled: &defaultEnabled,
	})
	if err != nil {
		t.Fatalf("create legacy MCP: %v", err)
	}

	current := New(mem, nil, func() time.Time { return time.Unix(1_800_000_000, 0).UTC() }).
		WithModelSecretKey("legacy-model-secret").
		WithConfigSecretKey("current-config-secret")
	if _, err := current.ListMCPServers(context.Background(), false); err == nil {
		t.Fatal("legacy ciphertext unexpectedly decrypted with current key")
	}

	if err := current.MigrateLegacyMCPSecrets(context.Background()); err != nil {
		t.Fatalf("migrate legacy MCP secrets: %v", err)
	}
	servers, err := current.ListMCPServers(context.Background(), false)
	if err != nil {
		t.Fatalf("list migrated MCP servers: %v", err)
	}
	if len(servers) != 1 || servers[0].URLHint != "https://mcp.example.test/api" {
		t.Fatalf("migrated MCP servers = %+v", servers)
	}
	stored, err := mem.GetMCPServer(context.Background(), "remote")
	if err != nil {
		t.Fatalf("get migrated MCP: %v", err)
	}
	runtimeConfig, err := current.mcpServerRuntimeConfig(stored)
	if err != nil {
		t.Fatalf("build migrated runtime config: %v", err)
	}
	if runtimeConfig["url"] != "https://mcp.example.test/api?token=secret" {
		t.Fatalf("migrated runtime URL = %v", runtimeConfig["url"])
	}

	firstCiphertext := append([]byte(nil), stored.URLVarCiphertextJSON...)
	if err := current.MigrateLegacyMCPSecrets(context.Background()); err != nil {
		t.Fatalf("repeat legacy MCP migration: %v", err)
	}
	stored, err = mem.GetMCPServer(context.Background(), "remote")
	if err != nil {
		t.Fatalf("get repeatedly migrated MCP: %v", err)
	}
	if !bytes.Equal(firstCiphertext, stored.URLVarCiphertextJSON) {
		t.Fatal("idempotent migration rewrote current ciphertext")
	}
}

func TestAggregateMCPHubReflectsEffectiveSelection(t *testing.T) {
	mem := store.NewMemory()
	admin := newMCPTestAdmin(mem)
	ctx := context.Background()
	defaultOn := true

	// Two admin-published servers: one default-on (stdio), one default-off (http).
	if _, err := admin.CreateMCPServer(ctx, MCPServerInput{
		ID: "fs", Name: "Filesystem", Transport: MCPTransportStdio, Command: "mcp-fs",
		DefaultEnabled: &defaultOn,
	}); err != nil {
		t.Fatalf("create fs: %v", err)
	}
	if _, err := admin.CreateMCPServer(ctx, MCPServerInput{
		ID: "search", Name: "Search", Transport: MCPTransportHTTP,
		URL: "https://mcp.example.test/search",
	}); err != nil {
		t.Fatalf("create search: %v", err)
	}

	const user = "user-1"

	// Before any preference: default-on server is effective, default-off is not.
	hub, err := admin.AggregateMCPHub(ctx, user)
	if err != nil {
		t.Fatalf("aggregate hub: %v", err)
	}
	if hub.TotalPublished != 2 {
		t.Fatalf("TotalPublished = %d, want 2", hub.TotalPublished)
	}
	if hub.TotalEffective != 1 {
		t.Fatalf("TotalEffective = %d, want 1", hub.TotalEffective)
	}
	if hub.Transports[MCPTransportStdio] != 1 || hub.Transports[MCPTransportHTTP] != 0 {
		t.Fatalf("Transports = %+v", hub.Transports)
	}

	// The aggregate must agree with the runtime config agent-runtime loads.
	cfg, err := admin.ListEffectiveMCPRuntimeConfig(ctx, user)
	if err != nil {
		t.Fatalf("effective runtime config: %v", err)
	}
	if len(cfg.MCPServers) != hub.TotalEffective {
		t.Fatalf("runtime config count %d != hub effective %d", len(cfg.MCPServers), hub.TotalEffective)
	}

	// User opts into the default-off server: hub now reports 2 effective.
	if err := admin.SetUserMCPEnabled(ctx, user, "search", true); err != nil {
		t.Fatalf("opt into search: %v", err)
	}
	hub, err = admin.AggregateMCPHub(ctx, user)
	if err != nil {
		t.Fatalf("aggregate hub after opt-in: %v", err)
	}
	if hub.TotalEffective != 2 {
		t.Fatalf("TotalEffective after opt-in = %d, want 2", hub.TotalEffective)
	}
	var search MCPHubEntry
	var found bool
	for _, entry := range hub.Servers {
		if entry.ID == "search" {
			search = entry
			found = true
		}
	}
	if !found {
		t.Fatal("search entry missing from hub")
	}
	if !search.Effective || !search.PreferenceSet || search.DefaultOn {
		t.Fatalf("search entry = %+v", search)
	}
	// The hub must never leak secrets: only a sanitized URL hint.
	if search.URLHint != "https://mcp.example.test/search" {
		t.Fatalf("search URLHint = %q", search.URLHint)
	}
}

func TestAggregateMCPHubRequiresUser(t *testing.T) {
	admin := newMCPTestAdmin(store.NewMemory())
	if _, err := admin.AggregateMCPHub(context.Background(), "  "); !errors.Is(err, ErrInvalidArg) {
		t.Fatalf("empty user error = %v, want ErrInvalidArg", err)
	}
}
