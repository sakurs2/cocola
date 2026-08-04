#!/usr/bin/env bash
# Canonical Cocola sandbox runtime entrypoint. Both the image CMD and the
# OpenSandbox create request enter here after provider-specific volume setup.
set -euo pipefail

log() { echo "[runtime] $*"; }

normalize_bool() {
  case "${1,,}" in
    1 | true | yes | on) printf '1' ;;
    0 | false | no | off) printf '0' ;;
    *) return 1 ;;
  esac
}

profile="${COCOLA_SANDBOX_PROFILE:-coding}"
case "$profile" in
  coding | minimal) ;;
  *)
    log "ERROR: unsupported COCOLA_SANDBOX_PROFILE=$profile (want coding or minimal)" >&2
    exit 64
    ;;
esac
export COCOLA_SANDBOX_PROFILE="$profile"

if [ -n "${COCOLA_CODE_SERVER_ENABLED:-}" ]; then
  if ! code_server_enabled="$(normalize_bool "$COCOLA_CODE_SERVER_ENABLED")"; then
    log "ERROR: invalid COCOLA_CODE_SERVER_ENABLED=$COCOLA_CODE_SERVER_ENABLED" >&2
    exit 64
  fi
elif [ "$profile" = "coding" ]; then
  code_server_enabled=1
else
  code_server_enabled=0
fi
export COCOLA_CODE_SERVER_ENABLED="$code_server_enabled"

if [ -n "${COCOLA_BROWSER_ENABLED:-}" ]; then
  if ! browser_enabled="$(normalize_bool "$COCOLA_BROWSER_ENABLED")"; then
    log "ERROR: invalid COCOLA_BROWSER_ENABLED=$COCOLA_BROWSER_ENABLED" >&2
    exit 64
  fi
elif [ "$profile" = "coding" ]; then
  browser_enabled=1
else
  browser_enabled=0
fi
export COCOLA_BROWSER_ENABLED="$browser_enabled"

# Keep the workspace contract identical for direct Docker and OpenSandbox.
# Provider-owned session links are prepared before this script; these stable
# subdirectories are runtime-owned and safe to create on every start.
mkdir -p \
  /workspace/outputs \
  /workspace/outputs/browser \
  /workspace/uploads \
  /workspace/downloads \
  /workspace/skills \
  /session/runtime/cocola \
  /session/runtime/browser \
  /session/runtime/browser/profile \
  /cache/xdg \
  /cache/uv \
  /cache/pip \
  /cache/npm \
  /cache/go-build \
  /cache/go-mod \
  /run/cocola
chown cocola:cocola \
  /workspace/outputs \
  /workspace/outputs/browser \
  /workspace/uploads \
  /workspace/downloads \
  /workspace/skills \
  /session/runtime/cocola \
  /session/runtime/browser \
  /session/runtime/browser/profile \
  /cache/xdg \
  /cache/uv \
  /cache/pip \
  /cache/npm \
  /cache/go-build \
  /cache/go-mod
chown root:cocola /run/cocola
chmod 0750 /run/cocola

# Claude Code keeps native Plan Mode files in its hidden session config. Prepare
# the directory before any Agent turn so models never need to create it through
# Bash. Fail closed if persisted session state replaced the directory with a
# symlink; the plan adapter applies the same containment rule when writing.
claude_plan_dir=/home/cocola/.claude/plans
if [ -L "$claude_plan_dir" ]; then
  log "ERROR: Claude plan directory must not be a symbolic link" >&2
  exit 65
fi
mkdir -p "$claude_plan_dir"
if [ -L "$claude_plan_dir" ]; then
  log "ERROR: Claude plan directory became a symbolic link during initialization" >&2
  exit 65
fi
chown cocola:cocola "$claude_plan_dir"
chmod 0700 "$claude_plan_dir"

if [ -n "${COCOLA_EGRESS_ALLOWLIST+x}" ]; then
  if /opt/cocola/init-firewall.sh; then
    log "egress firewall installed"
  else
    # Preserve the current operational behavior: keep core exec available and
    # surface the firewall failure loudly for the operator.
    log "WARNING: egress firewall FAILED to install" >&2
  fi
fi

log "starting profile=$profile code-server=$code_server_enabled browser=$browser_enabled"
exec /usr/bin/supervisord -c /opt/cocola/supervisord.conf
