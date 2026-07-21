package orchestrator

import (
	"context"
	"testing"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
	rds "github.com/cocola-project/cocola/packages/go-common/redis"
)

func TestNetworkingFromEnv(t *testing.T) {
	cases := []struct {
		name      string
		allowlist string
		baseURL   string
		want      []string
	}{
		{
			name: "empty yields nil allowlist",
			want: nil,
		},
		{
			name:      "comma separated trimmed and deduped",
			allowlist: " api.example.com , 10.0.0.0/8 ,api.example.com,",
			want:      []string{"api.example.com", "10.0.0.0/8"},
		},
		{
			name:      "gateway host folded in from base url",
			allowlist: "api.example.com",
			baseURL:   "http://host.docker.internal:18091",
			want:      []string{"api.example.com", "host.docker.internal"},
		},
		{
			name:    "empty allowlist keeps public access despite gateway url",
			baseURL: "https://llm-gateway.cocola.svc.cluster.local:8080",
			want:    nil,
		},
		{
			name:      "gateway host not duplicated if already listed",
			allowlist: "host.docker.internal",
			baseURL:   "http://host.docker.internal:18091",
			want:      []string{"host.docker.internal"},
		},
		{
			name:    "unparseable base url ignored",
			baseURL: "://not a url",
			want:    nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("COCOLA_SANDBOX_EGRESS_ALLOWLIST", tc.allowlist)
			t.Setenv("COCOLA_SANDBOX_LLM_BASE_URL", tc.baseURL)
			got := NetworkingFromEnv()
			if !equalStrings(got.EgressAllowlist, tc.want) {
				t.Fatalf("EgressAllowlist = %v, want %v", got.EgressAllowlist, tc.want)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// capturingProvider records the last SandboxSpec it received so tests can assert
// the binder forwards the configured Networking policy.
type capturingProvider struct {
	*fakeProvider
	last provider.SandboxSpec
}

func (c *capturingProvider) Create(ctx context.Context, spec provider.SandboxSpec) (*provider.Sandbox, error) {
	c.last = spec
	return c.fakeProvider.Create(ctx, spec)
}

func TestBinderForwardsNetworking(t *testing.T) {
	kv := rds.NewFake()
	cp := &capturingProvider{fakeProvider: newFakeProvider()}
	net := provider.Networking{EgressAllowlist: []string{"api.example.com", "host.docker.internal"}}
	b := NewBinder(kv, cp, Config{}).WithNetworking(net)

	if _, err := b.Acquire(context.Background(), AcquireSpec{
		SessionID: "s1",
		UserID:    "u1",
		Image:     "img",
	}); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !equalStrings(cp.last.Networking.EgressAllowlist, net.EgressAllowlist) {
		t.Fatalf("forwarded allowlist = %v, want %v",
			cp.last.Networking.EgressAllowlist, net.EgressAllowlist)
	}
}

func TestMergeSessionNetworkingExpandsConfiguredPolicy(t *testing.T) {
	base := provider.Networking{EgressAllowlist: []string{"llm.internal", "github.com"}}
	got := mergeSessionNetworking(base, []string{" github.com ", "api.github.com"})
	want := []string{"llm.internal", "github.com", "api.github.com"}
	if !equalStrings(got.EgressAllowlist, want) {
		t.Fatalf("allowlist = %v, want %v", got.EgressAllowlist, want)
	}
}

func TestMergeSessionNetworkingPreservesUnrestrictedPolicy(t *testing.T) {
	got := mergeSessionNetworking(provider.Networking{}, []string{"github.com"})
	if got.EgressAllowlist != nil {
		t.Fatalf("allowlist = %v, want nil public policy", got.EgressAllowlist)
	}
}
