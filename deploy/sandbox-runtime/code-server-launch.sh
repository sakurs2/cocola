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
# Supervisor executes this root-owned launcher as root, so the privilege-drop
# target must not be caller-configurable. The binary and workspace arguments
# are evaluated only after runuser has switched to this fixed guest identity.
CODE_SERVER_USER="cocola"
CODE_SERVER_DIR="${COCOLA_CODE_SERVER_DIR:-/workspace}"
# Platform extensions are immutable runtime assets. Keeping this path fixed
# prevents a sandbox request from replacing the operator-approved extension
# set or moving updates into the persisted user home.
CODE_SERVER_EXTENSIONS_DIR="/opt/cocola/code-server/extensions"
# Keep mutable Workbench state out of HOME and separate from the immutable
# extension set. The path is fixed so a request cannot redirect the root-owned
# launcher into changing ownership of an arbitrary location.
CODE_SERVER_STATE_DIR="/session/runtime/cocola/code-server"
CODE_SERVER_CONFIG_DIR="$CODE_SERVER_STATE_DIR/config"
CODE_SERVER_USER_DATA_DIR="$CODE_SERVER_STATE_DIR/user-data"
CODE_SERVER_TRUSTED_ORIGINS="${COCOLA_CODE_SERVER_TRUSTED_ORIGINS:-}"

log() { echo "[code-server] $*"; }

# Opt-out hook: operators can disable the resident editor without rebuilding.
if [ "${COCOLA_CODE_SERVER_ENABLED:-1}" = "0" ]; then
  log "disabled via COCOLA_CODE_SERVER_ENABLED=0; not starting"
  exit 0
fi

if [ ! -x "$CODE_SERVER_BIN" ]; then
  # Pre-baked at build time; a missing binary means a broken image, but we must
  # not take the whole container down -- supervisor records this optional
  # service as failed while exec/agent workloads remain available.
  log "WARNING: $CODE_SERVER_BIN not found or not executable; editor unavailable" >&2
  exit 127
fi
if [ ! -d "$CODE_SERVER_EXTENSIONS_DIR" ]; then
  log "WARNING: platform extension directory missing: $CODE_SERVER_EXTENSIONS_DIR" >&2
  exit 127
fi

# Supervisor invokes this launcher as root. Prepare only the fixed editor state
# directories before dropping privileges; extension assets remain root-owned.
if ! install -d -o "$CODE_SERVER_USER" -g "$CODE_SERVER_USER" -m 0750 \
  "$CODE_SERVER_CONFIG_DIR" "$CODE_SERVER_USER_DATA_DIR"; then
  log "WARNING: could not prepare Code Server state under $CODE_SERVER_STATE_DIR" >&2
  exit 1
fi

# sandbox-manager derives this host-only list from COCOLA_PUBLIC_ORIGINS and
# owns the environment key, so an Agent request cannot weaken the policy. Keep
# a second validation layer here because the image may also be run directly.
trusted_origin_args=()
IFS=',' read -r -a trusted_origins <<< "$CODE_SERVER_TRUSTED_ORIGINS"
for origin in "${trusted_origins[@]}"; do
  # Trim only surrounding whitespace. Internal whitespace remains invalid so a
  # malformed value cannot be silently rewritten into a different host.
  origin="${origin#"${origin%%[![:space:]]*}"}"
  origin="${origin%"${origin##*[![:space:]]}"}"
  [ -z "$origin" ] && continue
  if [[ "$origin" == *[[:space:]]* || "$origin" == *"*"* || "$origin" == *"://"* || "$origin" == *"/"* ||
        "$origin" == *"?"* || "$origin" == *"#"* || "$origin" == *"@"* ]]; then
    log "ERROR: invalid trusted origin host: $origin" >&2
    exit 1
  fi
  trusted_origin_args+=(--trusted-origins "$origin")
done
if [ "${#trusted_origin_args[@]}" -eq 0 ]; then
  log "WARNING: no trusted origins configured; browser WebSocket upgrades will remain blocked" >&2
fi

# Mutable config and Workbench data are session-scoped, while HOME remains the
# guest's normal home for terminals and language servers spawned by Workbench.
log "starting resident code-server on 0.0.0.0:${CODE_SERVER_PORT} as ${CODE_SERVER_USER} (root=${CODE_SERVER_DIR})"
exec runuser -u "$CODE_SERVER_USER" -- \
  env HOME="/home/${CODE_SERVER_USER}" \
  XDG_CONFIG_HOME="$CODE_SERVER_CONFIG_DIR" \
  "$CODE_SERVER_BIN" \
  --bind-addr "0.0.0.0:${CODE_SERVER_PORT}" \
  --auth none \
  --disable-telemetry \
  --disable-update-check \
  --disable-workspace-trust \
  --user-data-dir "$CODE_SERVER_USER_DATA_DIR" \
  --extensions-dir "$CODE_SERVER_EXTENSIONS_DIR" \
  "${trusted_origin_args[@]}" \
  "$CODE_SERVER_DIR"
