package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
)

// fakeThawer is a minimal containerThawer that records calls and replays the
// inspect result the test wires up. It lets us exercise thawIfPaused without a
// live Docker daemon.
type fakeThawer struct {
	state       *types.ContainerState
	inspectErr  error
	unpauseErr  error
	inspectCnt  int
	unpauseCnt  int
	unpausedIDs []string
}

func (f *fakeThawer) ContainerInspect(_ context.Context, _ string) (types.ContainerJSON, error) {
	f.inspectCnt++
	if f.inspectErr != nil {
		return types.ContainerJSON{}, f.inspectErr
	}
	return types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{State: f.state},
	}, nil
}

func (f *fakeThawer) ContainerUnpause(_ context.Context, id string) error {
	f.unpauseCnt++
	f.unpausedIDs = append(f.unpausedIDs, id)
	return f.unpauseErr
}

func TestThawIfPaused_UnpausesFrozenSandbox(t *testing.T) {
	f := &fakeThawer{state: &types.ContainerState{Paused: true}}
	if err := thawIfPaused(context.Background(), f, "cid-1", "sbx-1"); err != nil {
		t.Fatalf("thawIfPaused returned error: %v", err)
	}
	if f.unpauseCnt != 1 {
		t.Fatalf("expected exactly one unpause, got %d", f.unpauseCnt)
	}
	if len(f.unpausedIDs) != 1 || f.unpausedIDs[0] != "cid-1" {
		t.Fatalf("expected unpause of cid-1, got %v", f.unpausedIDs)
	}
}

func TestThawIfPaused_RunningSandboxIsNoop(t *testing.T) {
	f := &fakeThawer{state: &types.ContainerState{Paused: false}}
	if err := thawIfPaused(context.Background(), f, "cid-2", "sbx-2"); err != nil {
		t.Fatalf("thawIfPaused returned error: %v", err)
	}
	if f.unpauseCnt != 0 {
		t.Fatalf("running sandbox should not be unpaused, got %d calls", f.unpauseCnt)
	}
}

func TestThawIfPaused_NilStateIsNoop(t *testing.T) {
	f := &fakeThawer{state: nil}
	if err := thawIfPaused(context.Background(), f, "cid-3", "sbx-3"); err != nil {
		t.Fatalf("thawIfPaused returned error: %v", err)
	}
	if f.unpauseCnt != 0 {
		t.Fatalf("nil state should be a no-op, got %d unpause calls", f.unpauseCnt)
	}
}

func TestThawIfPaused_InspectErrorIsSwallowed(t *testing.T) {
	// Inspect failures must not abort exec: the downstream exec call owns the
	// authoritative error (e.g. no-such-container).
	f := &fakeThawer{inspectErr: errors.New("boom")}
	if err := thawIfPaused(context.Background(), f, "cid-4", "sbx-4"); err != nil {
		t.Fatalf("inspect error should be swallowed, got %v", err)
	}
	if f.unpauseCnt != 0 {
		t.Fatalf("no unpause expected when inspect fails, got %d", f.unpauseCnt)
	}
}

func TestThawIfPaused_UnpauseErrorIsReturned(t *testing.T) {
	f := &fakeThawer{state: &types.ContainerState{Paused: true}, unpauseErr: errors.New("freezer stuck")}
	err := thawIfPaused(context.Background(), f, "cid-5", "sbx-5")
	if err == nil {
		t.Fatal("expected unpause error to propagate, got nil")
	}
}

func hasEnv(env []string, key string) (string, bool) {
	for _, e := range env {
		if strings.HasPrefix(e, key+"=") {
			return strings.TrimPrefix(e, key+"="), true
		}
	}
	return "", false
}

func TestApplyEgressPolicy_NilAllowlistIsWideOpenKeepAlive(t *testing.T) {
	hc := &container.HostConfig{}
	env := []string{"FOO=bar"}
	cmd, execUser := applyEgressPolicy(hc, &env, provider.Networking{EgressAllowlist: nil})

	if len(cmd) == 0 || cmd[0] != "sh" {
		t.Fatalf("nil allowlist should keep the legacy keep-alive cmd, got %v", cmd)
	}
	if execUser != "" {
		t.Fatalf("nil allowlist must not pin an exec user, got %q", execUser)
	}
	if len(hc.CapAdd) != 0 {
		t.Fatalf("nil allowlist must not add caps, got %v", hc.CapAdd)
	}
	if len(hc.ExtraHosts) != 0 {
		t.Fatalf("nil allowlist must not add extra hosts, got %v", hc.ExtraHosts)
	}
	if _, ok := hasEnv(env, "COCOLA_EGRESS_ALLOWLIST"); ok {
		t.Fatalf("nil allowlist must not inject COCOLA_EGRESS_ALLOWLIST")
	}
	if hc.NetworkMode != "" {
		t.Fatalf("nil allowlist must not set NetworkMode, got %q", hc.NetworkMode)
	}
}

func TestApplyEgressPolicy_EmptyAllowlistInstallsBaselineFirewall(t *testing.T) {
	hc := &container.HostConfig{}
	env := []string{}
	cmd, execUser := applyEgressPolicy(hc, &env, provider.Networking{EgressAllowlist: []string{}})

	if len(cmd) != 1 || cmd[0] != firewallEntrypoint {
		t.Fatalf("empty (non-nil) allowlist should run the firewall entrypoint, got %v", cmd)
	}
	if execUser != sandboxUser {
		t.Fatalf("firewall path must pin exec to %q, got %q", sandboxUser, execUser)
	}
	if !slices.Contains(hc.CapAdd, "NET_ADMIN") {
		t.Fatalf("firewall needs NET_ADMIN, caps=%v", hc.CapAdd)
	}
	if hc.NetworkMode == "none" {
		t.Fatalf("empty allowlist must NOT use NetworkMode=none (would cut the gateway)")
	}
	v, ok := hasEnv(env, "COCOLA_EGRESS_ALLOWLIST")
	if !ok || v != "" {
		t.Fatalf("empty allowlist should inject an empty COCOLA_EGRESS_ALLOWLIST, got %q ok=%v", v, ok)
	}
}

func TestApplyEgressPolicy_NonEmptyAllowlistEnforced(t *testing.T) {
	hc := &container.HostConfig{}
	env := []string{}
	allow := []string{"api.example.com", "10.0.0.0/8", "host.docker.internal"}
	cmd, execUser := applyEgressPolicy(hc, &env, provider.Networking{EgressAllowlist: allow})

	if len(cmd) != 1 || cmd[0] != firewallEntrypoint {
		t.Fatalf("non-empty allowlist should run the firewall entrypoint, got %v", cmd)
	}
	if execUser != sandboxUser {
		t.Fatalf("firewall path must pin exec to %q, got %q", sandboxUser, execUser)
	}
	if !slices.Contains(hc.CapAdd, "NET_ADMIN") {
		t.Fatalf("firewall needs NET_ADMIN, caps=%v", hc.CapAdd)
	}
	if !slices.Contains(hc.ExtraHosts, "host.docker.internal:host-gateway") {
		t.Fatalf("gateway host must be mapped for in-container resolution, hosts=%v", hc.ExtraHosts)
	}
	v, ok := hasEnv(env, "COCOLA_EGRESS_ALLOWLIST")
	if !ok || v != "api.example.com,10.0.0.0/8,host.docker.internal" {
		t.Fatalf("allowlist env mismatch: %q ok=%v", v, ok)
	}
}

func TestSessionRootUsesUserAndSession(t *testing.T) {
	root := t.TempDir()
	p := &Provider{root: root}

	got := p.sessionRoot("User/1", "Sess..1")
	want := filepath.Join(root, "users", safePathSegment("User/1"), "sessions", safePathSegment("Sess..1"))
	if got != want {
		t.Fatalf("sessionRoot = %q, want %q", got, want)
	}
	if strings.Contains(got, "/User/1/") || strings.Contains(got, "/Sess..1/") {
		t.Fatalf("sessionRoot used raw unsafe ids: %q", got)
	}
}

func TestCleanupSessionStorageRemovesOnlySessionDir(t *testing.T) {
	root := t.TempDir()
	p := &Provider{root: root}
	sessionDir := p.sessionRoot("u1", "s1")
	otherDir := p.sessionRoot("u1", "s2")
	if err := os.MkdirAll(filepath.Join(sessionDir, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sessionDir, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(otherDir, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := p.CleanupSessionStorage(context.Background(), "u1", "s1"); err != nil {
		t.Fatalf("CleanupSessionStorage: %v", err)
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("session dir should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(otherDir); err != nil {
		t.Fatalf("other session dir should remain: %v", err)
	}
}
