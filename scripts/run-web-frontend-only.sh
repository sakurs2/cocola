#!/usr/bin/env bash

# Run only the worktree Web UI while the primary checkout keeps the Cocola
# backend stack on :3000. The shared Auth.js secret lets both origins decrypt
# the same development session cookie; API and workspace traffic are proxied by
# the Web app, so no backend service is started or stopped here.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMMON_GIT_DIR="$(git -C "$ROOT" rev-parse --git-common-dir)"
if [[ "$COMMON_GIT_DIR" != /* ]]; then
  COMMON_GIT_DIR="$ROOT/$COMMON_GIT_DIR"
fi
PRIMARY_ROOT="$(cd "$(dirname "$COMMON_GIT_DIR")" && pwd)"

# Match run-stack.sh's env loading behavior without copying a secret-bearing
# file into the worktree. Explicit shell environment values keep precedence.
if [[ -f "$PRIMARY_ROOT/.env" ]]; then
  set -a
  while IFS= read -r line; do
    [[ "$line" =~ ^[[:space:]]*# ]] && continue
    [[ -z "${line// }" ]] && continue
    key="${line%%=*}"
    if [[ -z "${!key:-}" ]]; then
      eval "export $line"
    fi
  done < "$PRIMARY_ROOT/.env"
  set +a
fi

export AUTH_SECRET="${AUTH_SECRET:-local-dev-auth-secret}"
export COCOLA_WEB_BACKEND_ORIGIN="${COCOLA_WEB_BACKEND_ORIGIN:-http://127.0.0.1:3000}"
export COCOLA_WEB_HOST="${COCOLA_WEB_HOST:-127.0.0.1}"
export PORT="${PORT:-3006}"

cd "$ROOT/apps/web"
exec node server.mjs
