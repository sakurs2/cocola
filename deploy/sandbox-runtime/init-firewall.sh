#!/usr/bin/env bash
# cocola Route-A sandbox egress firewall (ADR-0009 hardening; see
# docs/plan/hardening-sandbox-egress-allowlist.md).
#
# Reuses the well-trodden iptables + ipset "default-DROP + allowlist" pattern
# (same shape as Anthropic's Claude Code devcontainer init-firewall.sh, a
# natural fit since Route A runs Claude Code *inside* this sandbox). Runs ONCE as
# root at container start, before any user exec lands. User code execs as the
# non-root cocola user without NET_ADMIN, so it cannot undo these rules.
#
# Fail-closed ordering: DNS/loopback/established are allowed first, THEN the
# OUTPUT default is flipped to DROP, THEN the allowlist is resolved (DNS still
# works). If the script dies after the flip, the posture is deny, not open.
#
# Posture (Plan 3.1):
#   - always allow: loopback, established/related, DNS.
#   - allow: every domain/CIDR in COCOLA_EGRESS_ALLOWLIST (the llm-gateway host
#     is folded in by the orchestrator, so Route A's lifeline is covered).
#   - default DROP everything else.
#
# Empty/unset COCOLA_EGRESS_ALLOWLIST still installs the baseline (DNS only):
# secure-by-default, never wide-open.
set -euo pipefail

log() { echo "[init-firewall] $*"; }

ALLOWLIST="${COCOLA_EGRESS_ALLOWLIST:-}"

# --- reset -----------------------------------------------------------------
iptables -F
iptables -X 2>&1 || true
iptables -t nat -F 2>&1 || true

# --- input side: deny early (nothing should reach the sandbox) -------------
iptables -P INPUT   DROP
iptables -P FORWARD DROP
iptables -A INPUT -i lo -j ACCEPT
iptables -A INPUT -m state --state ESTABLISHED,RELATED -j ACCEPT

# In-container service ports the OpenSandbox server-proxy connects to (execd for
# exec/file ops, code-server for the resident editor). These are reached ONLY
# via the server-proxy, never published to the host, so allowing their inbound
# SYN keeps INPUT default-DROP for everything else. code-server's port is
# overridable via COCOLA_CODE_SERVER_PORT (kept in sync with the launcher).
COCOLA_EXECD_PORT="${COCOLA_EXECD_PORT:-44772}"
COCOLA_CODE_SERVER_PORT="${COCOLA_CODE_SERVER_PORT:-39378}"
for svc_port in "$COCOLA_EXECD_PORT" "$COCOLA_CODE_SERVER_PORT"; do
  [ -n "$svc_port" ] && iptables -A INPUT -p tcp --dport "$svc_port" -j ACCEPT
done

# --- output baseline: loopback + established + DNS -------------------------
iptables -A OUTPUT -o lo -j ACCEPT
iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT
# DNS (incl. Docker embedded resolver 127.0.0.11) -- required to resolve the
# allowlist below and for user workloads.
iptables -A OUTPUT -p udp --dport 53 -j ACCEPT
iptables -A OUTPUT -p tcp --dport 53 -j ACCEPT

# --- allowlist ipset + match rule (populated after the DROP flip) ----------
ipset create cocola-allow hash:net family inet -exist
ipset flush cocola-allow
iptables -A OUTPUT -m set --match-set cocola-allow dst -j ACCEPT

# --- flip default to DROP (fail-closed from here on) -----------------------
iptables -P OUTPUT DROP

# --- resolve + populate the allowlist (DNS already permitted) --------------
add_net() { # $1 = CIDR or bare IP
  ipset add cocola-allow "$1" -exist && log "allow net $1"
}

resolve_host() { # $1 = hostname -> emit resolved IPv4 addrs (best-effort)
  getent ahostsv4 "$1" 2>&1 | awk '{print $1}' | sort -u || true
}

IFS=', ' read -r -a ENTRIES <<< "$ALLOWLIST"
for raw in "${ENTRIES[@]:-}"; do
  entry="$(echo "$raw" | tr -d '[:space:]')"
  [ -z "$entry" ] && continue
  if [[ "$entry" == */* ]]; then
    add_net "$entry"                       # CIDR
  elif [[ "$entry" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    add_net "$entry/32"                    # bare IPv4
  else
    ips="$(resolve_host "$entry")"
    if [ -z "$ips" ]; then
      log "WARN: could not resolve '$entry'; skipped"
      continue
    fi
    while read -r ip; do
      [ -n "$ip" ] && add_net "$ip/32"
    done <<< "$ips"
  fi
done

log "egress firewall active (allowlist entries: ${#ENTRIES[@]})"
