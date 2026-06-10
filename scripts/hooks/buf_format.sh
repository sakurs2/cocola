#!/usr/bin/env bash
# Format staged .proto files with buf, if installed. Skips gracefully (exit 0)
# when buf is absent so commits are not blocked on machines without the proto
# toolchain. CI / `make proto-lint` enforces it authoritatively.
set -euo pipefail
if ! command -v buf >/dev/null 2>&1; then
  echo "buf not found; skipping proto format (install buf to enable)"
  exit 0
fi
changed=0
for f in "$@"; do
  before=$(cat "$f")
  buf format -w "$f"
  if [[ "$before" != "$(cat "$f")" ]]; then
    echo "buf-formatted: $f"
    changed=1
  fi
done
exit "$changed"
