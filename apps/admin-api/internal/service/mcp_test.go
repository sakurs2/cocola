package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

type recordingMCPVerifier struct {
	result MCPVerificationResult
	err    error
	config map[string]any
	calls  int
}

func (v *recordingMCPVerifier) Verify(_ context.Context, _ string, config map[string]any) (MCPVerificationResult, error) {
	v.calls++
	v.config = config
	return v.result, v.err
}

func TestInvalidMCPConfigIsRejectedBeforeVerification(t *testing.T) {
	mem := store.NewMemory()
	verifier := &recordingMCPVerifier{}
	admin := newMCPTestAdmin(mem, verifier)
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
	if verifier.calls != 0 {
		t.Fatalf("verifier calls = %d", verifier.calls)
	}
}

func newMCPTestAdmin(mem *store.Memory, verifier MCPVerifier) *Admin {
	return New(mem, nil, func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }).
		WithConfigSecretKey("mcp-test-secret").
		WithMCPVerifier(verifier)
}

func TestMCPRemoteURLIsEncryptedBeforePersistence(t *testing.T) {
	mem := store.NewMemory()
	verifier := &recordingMCPVerifier{result: MCPVerificationResult{ToolCount: 3}}
	admin := newMCPTestAdmin(mem, verifier)
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
	if verifier.config["url"] != rawURL {
		t.Fatalf("verifier URL = %#v", verifier.config["url"])
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

func TestMCPVerificationFailureDoesNotCreateOrOverwrite(t *testing.T) {
	mem := store.NewMemory()
	verifier := &recordingMCPVerifier{result: MCPVerificationResult{ToolCount: 1}}
	admin := newMCPTestAdmin(mem, verifier)
	_, err := admin.CreateMCPServer(context.Background(), MCPServerInput{
		ID: "demo", Name: "Original", Transport: MCPTransportStdio, Command: "demo",
	})
	if err != nil {
		t.Fatalf("seed MCP: %v", err)
	}

	verifier.err = errors.New("authentication failed")
	_, err = admin.CreateMCPServer(context.Background(), MCPServerInput{
		ID: "failed", Name: "Failed", Transport: MCPTransportStdio, Command: "bad",
	})
	if !errors.Is(err, ErrMCPVerification) {
		t.Fatalf("create error = %v", err)
	}
	if _, err := mem.GetMCPServer(context.Background(), "failed"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed MCP was persisted: %v", err)
	}

	_, err = admin.UpdateMCPServer(context.Background(), "demo", MCPServerInput{Name: "Changed"})
	if !errors.Is(err, ErrMCPVerification) {
		t.Fatalf("update error = %v", err)
	}
	stored, err := mem.GetMCPServer(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "Original" {
		t.Fatalf("verification failure overwrote record: %+v", stored)
	}
}

func TestMigrateMCPRemoteURLsIsIdempotent(t *testing.T) {
	mem := store.NewMemory()
	admin := newMCPTestAdmin(mem, &recordingMCPVerifier{})
	legacy := store.MCPServer{
		ID: "legacy", Name: "Legacy", Transport: MCPTransportSSE,
		URL:     "https://mcp.example.test/events?token=legacy-secret",
		Enabled: true, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
	}
	if err := mem.CreateMCPServer(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	if err := admin.MigrateMCPRemoteURLs(context.Background()); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	first, err := mem.GetMCPServer(context.Background(), "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if first.URL != mcpRemoteURLTemplate || bytes.Contains(first.URLVarCiphertextJSON, []byte("legacy-secret")) {
		t.Fatalf("legacy URL was not secured: %+v", first)
	}
	if err := admin.MigrateMCPRemoteURLs(context.Background()); err != nil {
		t.Fatalf("second migration: %v", err)
	}
	second, err := mem.GetMCPServer(context.Background(), "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.URLVarCiphertextJSON, second.URLVarCiphertextJSON) || !first.UpdatedAt.Equal(second.UpdatedAt) {
		t.Fatalf("idempotent migration changed the record")
	}
}

type fakeMCPSandboxRunner struct {
	stdout      []byte
	exitCode    int32
	execErr     error
	releaseCall int
}

func (f *fakeMCPSandboxRunner) Acquire(context.Context, string, string) (string, error) {
	return "sandbox-1", nil
}

func (f *fakeMCPSandboxRunner) Exec(context.Context, string, []byte, time.Duration) ([]byte, int32, error) {
	return f.stdout, f.exitCode, f.execErr
}

func (f *fakeMCPSandboxRunner) Release(context.Context, string) error {
	f.releaseCall++
	return nil
}

func TestSandboxMCPVerifierAlwaysReleases(t *testing.T) {
	tests := []struct {
		name        string
		ctx         func() context.Context
		stdout      string
		exitCode    int32
		execErr     error
		wantErrPart string
	}{
		{name: "success", ctx: context.Background, stdout: `{"status":"connected","tool_count":2}`},
		{name: "server error", ctx: context.Background, stdout: `{"status":"error","error":"authentication failed"}`, exitCode: 1, wantErrPart: "authentication failed"},
		{name: "sandbox error", ctx: context.Background, execErr: errors.New("exec failed"), wantErrPart: "sandbox verification failed"},
		{name: "timeout", ctx: func() context.Context {
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			cancel()
			return ctx
		}, execErr: context.DeadlineExceeded, wantErrPart: "timed out"},
		{name: "canceled", ctx: func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }, execErr: context.Canceled, wantErrPart: "sandbox verification failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeMCPSandboxRunner{stdout: []byte(tt.stdout), exitCode: tt.exitCode, execErr: tt.execErr}
			verifier := &SandboxMCPVerifier{runner: runner, image: "runtime:test"}
			_, err := verifier.Verify(tt.ctx(), "demo", map[string]any{"type": "stdio", "command": "demo"})
			if tt.wantErrPart == "" && err != nil {
				t.Fatalf("verify: %v", err)
			}
			if tt.wantErrPart != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErrPart)) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErrPart)
			}
			if runner.releaseCall != 1 {
				t.Fatalf("release calls = %d", runner.releaseCall)
			}
		})
	}
}
