package orchestrator

import (
	"net/url"
	"os"
	"strings"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
)

// NetworkingFromEnv builds the sandbox egress policy from environment config.
//
// COCOLA_SANDBOX_EGRESS_ALLOWLIST is a comma-separated list of domains/CIDRs.
// Leaving it empty does not configure an egress policy, so the provider keeps
// its default public-network access. When operators opt into an allowlist, the
// llm-gateway host is parsed from COCOLA_SANDBOX_LLM_BASE_URL and folded into
// the list so the restricted sandbox can still reach the model gateway.
//
// Returned value semantics (see provider.Networking and ADR-0009):
//   - non-empty allowlist -> gateway + the listed targets; everything else denied.
//   - nil allowlist       -> no egress policy; public network access stays open.
func NetworkingFromEnv() provider.Networking {
	var allow []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		allow = append(allow, s)
	}
	for _, item := range strings.Split(os.Getenv("COCOLA_SANDBOX_EGRESS_ALLOWLIST"), ",") {
		add(item)
	}
	if len(allow) == 0 {
		return provider.Networking{}
	}
	if h := gatewayHost(os.Getenv("COCOLA_SANDBOX_LLM_BASE_URL")); h != "" {
		add(h)
	}
	return provider.Networking{EgressAllowlist: allow}
}

// gatewayHost extracts the bare hostname (no scheme/port) from a base URL.
// Returns "" when the URL is empty or cannot be parsed into a host.
func gatewayHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Hostname()
}
