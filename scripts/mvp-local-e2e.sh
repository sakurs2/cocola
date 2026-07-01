#!/usr/bin/env bash
# mvp-local-e2e.sh - manual, real cross-language run of the backend MVP path:
#
#   curl --(SSE)--> gateway (Go BFF) --(gRPC)--> agent-runtime (Python)
#
# This is the human counterpart to the in-process Go bufconn test
# (apps/gateway/internal/integration/e2e_test.go). That test proves the seam
# wiring in CI without ports; THIS script proves the real two-process,
# two-language path on your machine. It uses the EchoProvider (no LLM needed):
# agent-runtime echoes the prompt back as streamed events.
#
# Usage:
#   bash scripts/mvp-local-e2e.sh
#
# Requirements: uv (Python), a Go toolchain, curl. No external network.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# Shared HS256 secret: the gateway verifies what admin-mint signs.
export COCOLA_AUTH_SECRET="${COCOLA_AUTH_SECRET:-local-dev-secret}"
AGENT_ADDR="${COCOLA_AGENT_ADDR:-127.0.0.1:50061}"
GATEWAY_ADDR="${COCOLA_GATEWAY_ADDR:-127.0.0.1:8080}"
NULL="/dev/null"

# Leaving COCOLA_SANDBOX_ADDR unset makes agent-runtime use EchoProvider, so
# this runbook needs no real model. For a real run set COCOLA_SANDBOX_ADDR to a
# sandbox-manager (Route A, ADR-0009); the sandbox brain reaches the llm-gateway
# via the injected COCOLA_SANDBOX_LLM_BASE_URL.

cleanup() {
  [[ -n "${AGENT_PID:-}" ]] && kill "$AGENT_PID" 2>"$NULL" || true
  [[ -n "${GW_PID:-}"   ]] && kill "$GW_PID"    2>"$NULL" || true
}
trap cleanup EXIT

echo "==> [1/4] starting agent-runtime gRPC on $AGENT_ADDR (EchoProvider)"
(
  cd apps/agent-runtime
  COCOLA_AGENT_HOST="${AGENT_ADDR%:*}" COCOLA_AGENT_PORT="${AGENT_ADDR##*:}" \
    uv run python -m cocola_agent_runtime
) &
AGENT_PID=$!

echo "==> [2/4] starting gateway BFF on $GATEWAY_ADDR (-> $AGENT_ADDR)"
(
  COCOLA_GATEWAY_ADDR="$GATEWAY_ADDR" COCOLA_AGENT_ADDR="$AGENT_ADDR" \
    go run ./apps/gateway/cmd/gateway
) &
GW_PID=$!

# Give both processes a moment to bind.
sleep 3

echo "==> [3/4] minting a cocola token (admin-mint)"
TOKEN="$(COCOLA_AUTH_SECRET="$COCOLA_AUTH_SECRET" \
  go run ./apps/admin-api/cmd/admin-mint -user emp-42 -tenant team-platform)"
echo "    token: ${TOKEN:0:24}...(truncated)"

echo "==> [4/4] POST /v1/chat (SSE stream below)"
echo "----------------------------------------------------------------------"
curl -sN -X POST "http://$GATEWAY_ADDR/v1/chat" \
  -H "authorization: Bearer $TOKEN" \
  -H "content-type: application/json" \
  -d '{"prompt":"hello cocola","session_id":"sess-local-1"}'
echo
echo "----------------------------------------------------------------------"
echo "==> done. Expect 'event: text' frames echoing the prompt, then 'event: done'."
