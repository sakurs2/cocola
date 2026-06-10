#!/usr/bin/env bash
# Format files with the project-local prettier, if installed. Skips gracefully
# (exit 0) when prettier is absent so commits are not blocked on machines that
# have not run `pnpm install`. CI / `make web-lint` enforces it authoritatively.
set -euo pipefail
PRETTIER="node_modules/.bin/prettier"
if [[ ! -x "$PRETTIER" ]]; then
  echo "prettier not found at $PRETTIER; skipping (run pnpm install to enable)"
  exit 0
fi
"$PRETTIER" --write --ignore-unknown "$@" >/dev/null 2>&1 || {
  "$PRETTIER" --write --ignore-unknown "$@"
  exit 1
}
# report what changed (prettier --write is silent on no-op)
exit 0
