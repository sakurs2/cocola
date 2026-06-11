#!/usr/bin/env bash

# M-minimal vertical slice: end-to-end demo + acceptance for the fully
# containerised control plane (sandbox-manager) + sandbox + persistent volume.
# Proves docs/plan/m-minimal-vertical-slice.md section 6:
#   1. compose up -> control plane healthy
#   2. create -> exec -> destroy loop works (DEMO OK)
#   3. persistence: data survives sandbox destruction
#   4. restart: data survives compose down && up
# Only host dependency is Docker + Docker Compose (no Go on host).
# Usage:
#   scripts/demo-minimal.sh
#   COCOLA_KEEP_UP=1 scripts/demo-minimal.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="$ROOT/deploy/docker-compose/docker-compose.yml"
ADDR="${COCOLA_SANDBOX_ADDR_HOST:-:50051}"
OUT="$ROOT/.run-logs/demo-minimal.out"
mkdir -p "$ROOT/.run-logs"

export COCOLA_DATA_ROOT="${COCOLA_DATA_ROOT:-$ROOT/.cocola-data}"
mkdir -p "$COCOLA_DATA_ROOT"

# Drive the CLI from INSIDE the sandbox-manager container so the host needs
# no Go toolchain. -T disables TTY so output is capturable in $().
CLI=(docker compose -f "$COMPOSE_FILE" exec -T sandbox-manager sandbox-cli -addr ":50051")

say() { printf '\n== %s\n' "$*"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
compose() { docker compose -f "$COMPOSE_FILE" "$@"; }

wait_port() {
  say "waiting for control plane (in-container probe)"
  for _ in $(seq 1 60); do
    if "${CLI[@]}" create -user _probe -session _probe >"$ROOT/.run-logs/probe.log" 2>&1; then
      return 0
    fi
    sleep 2
  done
  cat "$ROOT/.run-logs/probe.log" >&2 || true
  fail "control plane did not become ready"
}

cleanup() {
  if [ "${COCOLA_KEEP_UP:-0}" != "1" ]; then
    say "tearing down stack"
    compose down -v || true
  fi
}
trap cleanup EXIT

say "compose up -d (build sandbox-manager + redis)"
compose up -d --build
wait_port

say "demo loop: create -> exec -> destroy"
"${CLI[@]}" demo | tee "$OUT" || fail "demo loop errored"
grep -q "DEMO OK" "$OUT" || fail "demo did not print DEMO OK"

say "persistence: write in sbx-A, read from sbx-B after destroying A"
SBX_A="$("${CLI[@]}" create -user u1 -session s1)"
[ -n "$SBX_A" ] || fail "create A returned empty id"
"${CLI[@]}" exec -id "$SBX_A" -- sh -c 'mkdir -p /data/userdata/u1 && echo persisted > /data/userdata/u1/proof.txt'
"${CLI[@]}" destroy -id "$SBX_A"
SBX_B="$("${CLI[@]}" create -user u1 -session s2)"
[ -n "$SBX_B" ] || fail "create B returned empty id"
GOT="$("${CLI[@]}" exec -id "$SBX_B" -- cat /data/userdata/u1/proof.txt)"
[ "$GOT" = "persisted" ] || fail "persistence broken: got '$GOT'"
"${CLI[@]}" destroy -id "$SBX_B"
say "persistence OK (read back: $GOT)"

say "restart: down && up, data must survive"
compose down
compose up -d --build
wait_port
SBX_C="$("${CLI[@]}" create -user u1 -session s3)"
GOT2="$("${CLI[@]}" exec -id "$SBX_C" -- cat /data/userdata/u1/proof.txt)"
[ "$GOT2" = "persisted" ] || fail "restart persistence broken: got '$GOT2'"
"${CLI[@]}" destroy -id "$SBX_C"
say "restart persistence OK (read back: $GOT2)"

printf '\nDEMO-MINIMAL OK\n'
