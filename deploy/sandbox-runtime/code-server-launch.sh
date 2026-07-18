#!/usr/bin/env bash
# cocola resident code-server launcher (Route A).
#
# ADR-0009 reconciliation -- why this is NOT a violation of "a sandbox never
# binds a *host* port":
#   - code-server binds an IN-CONTAINER port (COCOLA_CODE_SERVER_PORT, default
#     39378) on 0.0.0.0. It is NEVER published to the host (`docker -p` / k8s
#     NodePort are never used for it).
#   - It is reached ONLY through the OpenSandbox server-proxy, exactly like execd
#     (sandbox-manager resolveEndpoint -> gateway Preview Proxy -> browser). The
#     egress firewall's INPUT DROP default still holds; init-firewall.sh opens
#     just this one service port for the proxy's inbound connection.
#   - Auth is enforced by the cocola chain (web runtime token -> gateway
#     verifier), so code-server itself runs with --auth none: whoever the proxy
#     lets through is already an authenticated cocola user for this session.
#
# The container's main process (firewall-entrypoint.sh) runs as root to install
# the firewall; code-server must run as the non-root brain user (cocola, uid
# 10001) so the editor's file operations match the user the shim/agent exec as.
set -uo pipefail

CODE_SERVER_PORT="${COCOLA_CODE_SERVER_PORT:-39378}"
CODE_SERVER_BIN="${COCOLA_CODE_SERVER_BIN:-/usr/local/bin/code-server}"
CODE_SERVER_USER="${COCOLA_CODE_SERVER_USER:-cocola}"
CODE_SERVER_DIR="${COCOLA_CODE_SERVER_DIR:-/workspace}"

log() { echo "[code-server] $*"; }

# Opt-out hook: operators can disable the resident editor without rebuilding.
if [ "${COCOLA_CODE_SERVER_ENABLED:-1}" = "0" ]; then
  log "disabled via COCOLA_CODE_SERVER_ENABLED=0; not starting"
  exit 0
fi

if [ ! -x "$CODE_SERVER_BIN" ]; then
  # Pre-baked at build time; a missing binary means a broken image, but we must
  # not take the whole container down -- exec/agent workloads still work.
  log "WARNING: $CODE_SERVER_BIN not found or not executable; editor unavailable" >&2
  exit 0
fi

# runuser drops to the brain user with a login-like env (HOME=/home/cocola), so
# code-server's user-data/extensions default under the writable brain home.
start_one() {
  runuser -u "$CODE_SERVER_USER" -- \
    env HOME="/home/${CODE_SERVER_USER}" \
    "$CODE_SERVER_BIN" \
    --bind-addr "0.0.0.0:${CODE_SERVER_PORT}" \
    --auth none \
    --disable-telemetry \
    --disable-update-check \
    --disable-workspace-trust \
    "$CODE_SERVER_DIR"
}

# Resident supervision: code-server is meant to stay up for the session's life,
# so a crash is retried with a short backoff rather than left dead. The loop is
# backgrounded by the caller (firewall-entrypoint.sh) so the container's
# keep-alive wait is never blocked.
log "starting resident code-server on 0.0.0.0:${CODE_SERVER_PORT} as ${CODE_SERVER_USER} (root=${CODE_SERVER_DIR})"
while true; do
  start_one || log "code-server exited ($?); restarting in 2s" >&2
  sleep 2
done
