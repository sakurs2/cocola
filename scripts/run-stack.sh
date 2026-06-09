#!/usr/bin/env bash
# run-stack.sh - one-click local dev stack for cocola.
#
# Boots the full request path on your machine and keeps it running until you
# press Ctrl-C, then tears everything down cleanly:
#
#   web (Next.js) --(SSE proxy)--> gateway (Go BFF) --(gRPC)--> agent-runtime (Py)
#                                                                   |
#                                              COCOLA_LLM_BASE_URL  v
#                                                          llm-gateway (Py, FastAPI)
#
# Always started:  agent-runtime + gateway   (zero-config, EchoProvider)
# Opt-in:          llm-gateway  (--with-llm)  drives the real Claude Agent SDK path
#                  web          (--with-web)  the browser test tool from Step 4
#                  --all        enables both
#
# Design notes
#   * Port 8080 collision: the gateway BFF and the llm-gateway BOTH default to
#     8080. We pin llm-gateway to COCOLA_LLM_PORT (default 8081) and point the
#     agent-runtime COCOLA_LLM_BASE_URL at it, so they never fight.
#   * A sandbox-manager is NOT started here (its build is containerized). If you
#     export COCOLA_SANDBOX_ADDR we pass it through so the agent binds + routes
#     bash/file tools into that sandbox.
#   * Every child logs to .run-logs/<name>.log; this script prints a token you
#     can paste into the web UI or a curl call.
#
# Usage:
#   bash scripts/run-stack.sh            # echo stack: agent-runtime + gateway
#   bash scripts/run-stack.sh --with-web # + browser test tool on :3000
#   bash scripts/run-stack.sh --all      # + llm-gateway (real SDK path) + web
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

WITH_LLM=0
WITH_WEB=0
for arg in "$@"; do
  case "$arg" in
    --with-llm) WITH_LLM=1 ;;
    --with-web) WITH_WEB=1 ;;
    --all)      WITH_LLM=1; WITH_WEB=1 ;;
    -h|--help)
      grep '^#' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "unknown flag: $arg (try --help)" >&2; exit 2 ;;
  esac
done

# ----------------------------------------------------------------- config
export COCOLA_AUTH_SECRET="${COCOLA_AUTH_SECRET:-local-dev-secret}"

AGENT_HOST="${COCOLA_AGENT_HOST:-127.0.0.1}"
AGENT_PORT="${COCOLA_AGENT_PORT:-50061}"
AGENT_ADDR="$AGENT_HOST:$AGENT_PORT"

GATEWAY_HOST="${COCOLA_GATEWAY_HOST:-127.0.0.1}"
GATEWAY_PORT="${COCOLA_GATEWAY_PORT:-8080}"
GATEWAY_ADDR="$GATEWAY_HOST:$GATEWAY_PORT"

LLM_HOST="${COCOLA_LLM_HOST:-127.0.0.1}"
LLM_PORT="${COCOLA_LLM_PORT:-8081}"   # NOT 8080: that is the gateway port.

WEB_PORT="${COCOLA_WEB_PORT:-3000}"

LOG_DIR="$ROOT/.run-logs"
mkdir -p "$LOG_DIR"

PIDS=()

log_redirect() { printf '%s/%s.log' "$LOG_DIR" "$1"; }

cleanup() {
  echo
  echo "==> shutting down dev stack"
  for pid in "${PIDS[@]:-}"; do
    [[ -n "$pid" ]] || continue
    # Kill the whole process group so go-run / uv child procs die too.
    kill -- "-$pid" >>"$LOG_DIR/cleanup.log" 2>&1 || kill "$pid" >>"$LOG_DIR/cleanup.log" 2>&1 || true
  done
  wait 2>>"$LOG_DIR/cleanup.log" || true
  echo "==> done."
}
trap cleanup EXIT INT TERM

# Wait until a TCP port accepts a connection. Uses nc (preinstalled on macOS).
wait_port() {
  local host="$1" port="$2" name="$3" tries="${4:-120}"
  for ((i=0; i<tries; i++)); do
    if nc -z "$host" "$port" >>"$(log_redirect wait)" 2>&1; then
      return 0
    fi
    sleep 0.5
  done
  echo "!! timed out waiting for $name on $host:$port" >&2
  echo "   see $(log_redirect "$name") for details" >&2
  return 1
}

# ----------------------------------------------------------------- llm-gateway
if [[ "$WITH_LLM" == "1" ]]; then
  ( cd apps/llm-gateway && \
    COCOLA_LLM_HOST="$LLM_HOST" COCOLA_LLM_PORT="$LLM_PORT" \
    setsid uv run python -m cocola_llm_gateway ) >"$(log_redirect llm-gateway)" 2>&1 &
  PIDS+=("$!")
  echo "==> starting llm-gateway on $LLM_HOST:$LLM_PORT (provider: ${COCOLA_LLM_PROVIDER:-fake})"
  wait_port "$LLM_HOST" "$LLM_PORT" "llm-gateway"
  export COCOLA_LLM_BASE_URL="${COCOLA_LLM_BASE_URL:-http://$LLM_HOST:$LLM_PORT}"
  echo "    agent-runtime will route the SDK at $COCOLA_LLM_BASE_URL"
fi

# ----------------------------------------------------------------- agent-runtime
(
  cd apps/agent-runtime
  COCOLA_AGENT_HOST="$AGENT_HOST" COCOLA_AGENT_PORT="$AGENT_PORT" \
  COCOLA_LLM_BASE_URL="${COCOLA_LLM_BASE_URL:-}" \
  COCOLA_SANDBOX_ADDR="${COCOLA_SANDBOX_ADDR:-}" \
    setsid uv run python -m cocola_agent_runtime
) >"$(log_redirect agent-runtime)" 2>&1 &
PIDS+=("$!")
echo "==> starting agent-runtime on $AGENT_ADDR (log: .run-logs/agent-runtime.log)"
wait_port "$AGENT_HOST" "$AGENT_PORT" "agent-runtime"

# ----------------------------------------------------------------- gateway
(
  COCOLA_GATEWAY_ADDR="$GATEWAY_ADDR" COCOLA_AGENT_ADDR="$AGENT_ADDR" \
    setsid go run ./apps/gateway/cmd/gateway
) >"$(log_redirect gateway)" 2>&1 &
PIDS+=("$!")
echo "==> starting gateway on $GATEWAY_ADDR -> $AGENT_ADDR (log: .run-logs/gateway.log)"
wait_port "$GATEWAY_HOST" "$GATEWAY_PORT" "gateway"

# ----------------------------------------------------------------- web (opt-in)
if [[ "$WITH_WEB" == "1" ]]; then
  (
    cd apps/web
    COCOLA_GATEWAY_URL="http://$GATEWAY_ADDR" \
      setsid pnpm dev --port "$WEB_PORT"
  ) >"$(log_redirect web)" 2>&1 &
  PIDS+=("$!")
  echo "==> starting web on http://127.0.0.1:$WEB_PORT (log: .run-logs/web.log)"
  wait_port "127.0.0.1" "$WEB_PORT" "web" 240
fi

# ----------------------------------------------------------------- dev token
echo "==> minting a dev token (admin-mint)"
TOKEN="$(go run ./apps/admin-api/cmd/admin-mint -user emp-42 -tenant team-platform -ttl 3600)"

# ----------------------------------------------------------------- ready banner
echo
echo "======================================================================"
echo " cocola dev stack is UP"
echo "----------------------------------------------------------------------"
echo " gateway   : http://$GATEWAY_ADDR   (POST /v1/chat, SSE)"
echo " agent-rt  : $AGENT_ADDR (gRPC)"
[[ "$WITH_LLM" == "1" ]] && echo " llm-gw    : http://$LLM_HOST:$LLM_PORT"
[[ "$WITH_WEB" == "1" ]] && echo " web       : http://127.0.0.1:$WEB_PORT  (paste the token below)"
echo "----------------------------------------------------------------------"
echo " dev token : $TOKEN"
echo "----------------------------------------------------------------------"
echo " try it    :"
echo "   curl -sN -X POST http://$GATEWAY_ADDR/v1/chat \\"
echo "     -H \"authorization: Bearer \$TOKEN\" \\"
echo "     -H \"content-type: application/json\" \\"
echo "     -d '{\"prompt\":\"hello cocola\",\"session_id\":\"sess-1\"}'"
echo "----------------------------------------------------------------------"
echo " logs      : tail -f .run-logs/  (one file per service)"
echo " stop      : Ctrl-C (tears down every child process)"
echo "======================================================================"
echo

# Block until interrupted; cleanup() runs on the way out.
wait
