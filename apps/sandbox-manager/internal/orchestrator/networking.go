package orchestrator

import (
	"net/url"
	"os"
	"strings"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
)

// NetworkingFromEnv builds the sandbox egress policy from environment config.
//
// COCOLA_SANDBOX_EGRESS_ALLOWLIST is a comma-separated list of domains/CIDRs the
// sandbox may reach *in addition* to the always-on baseline (DNS + llm-gateway),
// which each provider enforces unconditionally. The llm-gateway host is parsed
// from COCOLA_SANDBOX_LLM_BASE_URL and folded into the allowlist so operators
// need not repeat it.
//
// Returned value semantics (see provider.Networking and ADR-0009):
//   - non-empty allowlist -> baseline + the listed targets; everything else denied.
//   - nil/empty allowlist  -> nothing configured; providers fall back to their
//     own default (Docker: legacy wide-open; K8s: deny-all). The secure default
//     posture is tightened in the provider layer (S2/S3), not here.
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
