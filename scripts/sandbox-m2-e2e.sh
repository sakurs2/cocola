#!/usr/bin/env bash
# M2 end-to-end acceptance test: session<->sandbox binding under concurrency.
#
# Boots sandbox-manager (native binary) wired to Redis, then runs the bench:
# N distinct sessions each issue K CONCURRENT Acquire calls. Asserts the two M2
# invariants (verified inside `sandbox-cli bench`):
#   1. intra-session convergence  — all K acquires for a session share one sandbox
#   2. inter-session isolation     — distinct sandbox count == session count
# Tears the server down and releases all sandboxes on exit.
#
# Requires a reachable Redis (default localhost:6379); start one with
#   docker compose -f deploy/compose/docker-compose.dev.yml up -d redis
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ADDR="${COCOLA_SANDBOX_ADDR:-:50051}"
REDIS="${COCOLA_REDIS_ADDR:-localhost:6379}"
SESSIONS="${SESSIONS:-50}"
PER_SESSION="${PER_SESSION:-4}"

cd "$ROOT"
[ -x bin/sandbox-manager ] || scripts/sandbox-build.sh

echo "== starting sandbox-manager on $ADDR (redis=$REDIS) =="
COCOLA_SANDBOX_ADDR="$ADDR" COCOLA_REDIS_ADDR="$REDIS" COCOLA_SANDBOX_PROVIDER=docker ./bin/sandbox-manager &
SRV_PID=$!
trap 'kill $SRV_PID 2>/dev/null || true' EXIT
sleep 1

echo "== concurrency bench: $SESSIONS sessions x $PER_SESSION acquires =="
./bin/sandbox-cli -addr "$ADDR" bench -sessions "$SESSIONS" -per-session "$PER_SESSION" -cleanup=true

echo "M2 E2E OK"
