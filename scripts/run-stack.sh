#!/usr/bin/env bash
# run-stack.sh - one-click local dev stack for cocola.
#
# Boots the full request path on your machine and keeps it running until you
# press Ctrl-C, then tears everything down cleanly:
#
#   web (Next.js) --(SSE proxy)--> gateway (Go BFF) --(gRPC)--> agent-runtime (Py)
#                                                                   |
#                              (Route A: brain runs inside sandbox) v
#                                                          llm-gateway (Py, FastAPI)
#
# Always started:  agent-runtime + gateway   (zero-config, EchoProvider)
# Opt-in:          llm-gateway  (--with-llm)  the model upstream the sandbox brain hits
#                  web          (--with-web)  the browser test tool from Step 4
#                  --all        enables both
#
# Design notes
#   * Port 8080 collision: the gateway BFF and the llm-gateway BOTH default to
#     8080. We pin llm-gateway to COCOLA_LLM_PORT (default 8081) so they never
#     fight; the sandbox brain reaches it via COCOLA_SANDBOX_LLM_BASE_URL.
#   * Route A (brain-in-sandbox, ADR-0009) is the only real path. A
#     sandbox-manager is NOT started here (its build is containerized), so for a
#     REAL Route-A run you must export COCOLA_SANDBOX_ADDR pointing at one; we
#     pass it through and the agent runs the whole Claude brain inside that
#     sandbox. With no executor reachable, agent-runtime uses EchoProvider (no
#     real model calls). The legacy Route B was decommissioned (see ADR-0009).
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
# Auto-load repo-root .env if present, so `make up-all` can pick up your real
# model endpoint/key without manual exports. Existing env wins over the file
# (so a one-off `COCOLA_LLM_PROVIDER=fake make up` still overrides .env).
if [[ -f "$ROOT/.env" ]]; then
  echo "==> loading $ROOT/.env"
  set -a
  # shellcheck disable=SC1091
  while IFS= read -r line; do
    [[ "$line" =~ ^[[:space:]]*# ]] && continue
    [[ -z "${line// }" ]] && continue
    key="${line%%=*}"
    # Only set vars that are not already present in the environment.
    if [[ -z "${!key:-}" ]]; then
      eval "export $line"
    fi
  done < "$ROOT/.env"
  set +a
fi

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

# Ports this script owns. Teardown frees every one of them as a hard backstop:
# go run / uv run / pnpm dev fork the real listeners as grandchildren that get
# reparented to launchd on macOS, escaping our process group -- so killing the
# group is NOT enough to guarantee the port is released. Freeing by port is.
OWNED_PORTS=("$AGENT_PORT" "$GATEWAY_PORT")
[[ "$WITH_LLM" == "1" ]] && OWNED_PORTS+=("$LLM_PORT")
[[ "$WITH_WEB" == "1" ]] && OWNED_PORTS+=("$WEB_PORT")

log_redirect() { printf '%s/%s.log' "$LOG_DIR" "$1"; }

# Graceful, deterministic teardown. The contract: when this returns, NONE of our
# ports stay occupied. Three phases, escalating only as needed:
#   1. SIGTERM each service process group  -> lets go/uv/node flush & exit.
#   2. wait briefly for them to die on their own.
#   3. SIGKILL any survivor groups, then free every owned port by force.
# Phase 3 port sweep is the backstop that catches reparented grandchildren the
# process-group signal cannot reach (the real cause of "exited but port busy").
_SHUTTING_DOWN=0
cleanup() {
  # The trap fires for INT/TERM and again for the subsequent EXIT; run once.
  [[ "$_SHUTTING_DOWN" == "1" ]] && return
  _SHUTTING_DOWN=1
  trap '' INT TERM   # ignore repeat Ctrl-C while we tear down

  echo
  echo "==> shutting down dev stack"

  # Phase 1: polite SIGTERM to each process group (fall back to the bare pid).
  for pid in "${PIDS[@]:-}"; do
    [[ -n "$pid" ]] || continue
    kill -TERM -- "-$pid" 2>>"$LOG_DIR/cleanup.log" \
      || kill -TERM "$pid" 2>>"$LOG_DIR/cleanup.log" || true
  done

  # Phase 2: give them up to ~3s to exit cleanly.
  for ((i=0; i<15; i++)); do
    alive=0
    for pid in "${PIDS[@]:-}"; do
      [[ -n "$pid" ]] || continue
      kill -0 "$pid" 2>>"$LOG_DIR/cleanup.log" && alive=1
    done
    [[ "$alive" == "0" ]] && break
    sleep 0.2
  done

  # Phase 3a: SIGKILL any process groups still standing.
  for pid in "${PIDS[@]:-}"; do
    [[ -n "$pid" ]] || continue
    kill -KILL -- "-$pid" 2>>"$LOG_DIR/cleanup.log" \
      || kill -KILL "$pid" 2>>"$LOG_DIR/cleanup.log" || true
  done

  # Phase 3b: backstop -- guarantee every port we own is released, whatever still
  # holds it (reparented children are unreachable via the process group).
  for port in "${OWNED_PORTS[@]:-}"; do
    [[ -n "$port" ]] || continue
    free_port "$port" "teardown" >>"$LOG_DIR/cleanup.log" 2>&1 || true
  done

  wait 2>>"$LOG_DIR/cleanup.log" || true
  echo "==> done. all owned ports released."
}
trap cleanup EXIT INT TERM

# Free a TCP port before we bind it. A previous run that crashed (or a stray
# diagnostic process) can leave a server squatting one of our ports; the new
# child then fails to bind and the request silently flows to the WRONG process
# (this actually bit us: a stale llm-gateway on 8081 swallowed traffic so the
# real one logged nothing). Kill any listener on the port up front. macOS-safe:
# resolve PIDs via lsof, then SIGTERM, escalate to SIGKILL if still alive.
free_port() {
  local port="$1" name="$2" pids
  pids="$(lsof -ti "TCP:$port" -s "TCP:LISTEN" 2>/dev/null || true)"
  [[ -z "$pids" ]] && return 0
  echo "==> port $port ($name) busy; freeing stale listener(s): $pids"
  # shellcheck disable=SC2086
  kill $pids 2>/dev/null || true
  for ((i=0; i<10; i++)); do
    pids="$(lsof -ti "TCP:$port" -s "TCP:LISTEN" 2>/dev/null || true)"
    [[ -z "$pids" ]] && return 0
    sleep 0.3
  done
  # shellcheck disable=SC2086
  kill -9 $pids 2>/dev/null || true
}

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

# ----------------------------------------------------------- process groups
# We run each service in its own process group so Ctrl-C can kill the whole
# tree (go run / uv run fork child binaries). setsid does this on Linux but is
# absent on macOS, so fall back to shell job control (set -m), which also puts
# every `&` job in its own process group. cleanup() does `kill -- -$pid`.
if command -v setsid >/dev/null 2>&1; then
  SETSID="setsid"
else
  SETSID=""
  set -m
fi

# ----------------------------------------------------------------- llm-gateway
if [[ "$WITH_LLM" == "1" ]]; then
  free_port "$LLM_PORT" llm-gateway
  ( cd apps/llm-gateway && \
    COCOLA_LLM_HOST="$LLM_HOST" COCOLA_LLM_PORT="$LLM_PORT" \
    $SETSID uv run python -m cocola_llm_gateway ) >"$(log_redirect llm-gateway)" 2>&1 &
  PIDS+=("$!")
  echo "==> starting llm-gateway on $LLM_HOST:$LLM_PORT (provider: ${COCOLA_LLM_PROVIDER:-fake})"
  wait_port "$LLM_HOST" "$LLM_PORT" "llm-gateway"
  # Route A: the sandbox's in-sandbox claude CLI reaches the gateway via
  # COCOLA_SANDBOX_LLM_BASE_URL (injected at sandbox creation), not via the
  # agent-runtime process env. Surface the URL so a real Route-A run can point
  # the sandbox at it.
  export COCOLA_SANDBOX_LLM_BASE_URL="${COCOLA_SANDBOX_LLM_BASE_URL:-http://$LLM_HOST:$LLM_PORT}"
  echo "    llm-gateway up; sandbox brain should target $COCOLA_SANDBOX_LLM_BASE_URL"
fi

# ----------------------------------------------------------------- MinIO (attachments)
# P1a attachment storage (ADR-0017): the gateway uploads every file to MinIO and
# agent-runtime backend-pulls large ones. run-stack runs services NATIVELY, so
# (unlike docker-compose.full.yml) MinIO is not wired implicitly -- we bring it
# up via the dev compose file and export COCOLA_MINIO_* here. The gateway and
# agent-runtime subshells inherit this exported env and activate the object
# store; without it the gateway stays inline-only and large files fail. MinIO
# shares the dev-infra lifecycle (stop with `make dev-down`), not this script's.
# Skips cleanly when docker is unavailable or COCOLA_SKIP_MINIO=1.
if [[ "${COCOLA_SKIP_MINIO:-0}" != "1" ]] && command -v docker >/dev/null 2>&1; then
  echo "==> starting MinIO (attachments) via docker-compose.dev.yml"
  if docker compose -f deploy/docker-compose/docker-compose.dev.yml up -d minio minio-init \
       >"$(log_redirect minio)" 2>&1 && wait_port 127.0.0.1 9000 "minio" 60; then
    export COCOLA_MINIO_ENDPOINT="${COCOLA_MINIO_ENDPOINT:-127.0.0.1:9000}"
    export COCOLA_MINIO_ACCESS_KEY="${COCOLA_MINIO_ACCESS_KEY:-cocola}"
    export COCOLA_MINIO_SECRET_KEY="${COCOLA_MINIO_SECRET_KEY:-cocola_dev_pw}"
    export COCOLA_MINIO_BUCKET="${COCOLA_MINIO_BUCKET:-cocola}"
    export COCOLA_ATTACHMENT_INLINE_MAX_BYTES="${COCOLA_ATTACHMENT_INLINE_MAX_BYTES:-16777216}"
    MINIO_CONSOLE="http://127.0.0.1:9001"
    echo "    MinIO up: S3 ${COCOLA_MINIO_ENDPOINT}, console ${MINIO_CONSOLE} (cocola / cocola_dev_pw)"
  else
    echo "!! MinIO failed to start/ready; continuing inline-only (see .run-logs/minio.log)" >&2
    echo "   large attachments will be capped; run \`make dev-up\` manually or set COCOLA_SKIP_MINIO=1 to silence." >&2
  fi
else
  echo "==> MinIO skipped (COCOLA_SKIP_MINIO=1 or docker unavailable); attachments stay inline-only"
fi

# ----------------------------------------------------------------- dev token
# Minted up front: in the real-LLM path the agent-runtime must present a VALID
# cocola token to the gateway as ANTHROPIC_API_KEY (the gateway verifies it with
# the shared COCOLA_AUTH_SECRET). The default "cocola-local" is not a token and
# would be rejected, so we reuse this minted one. The same token is printed in
# the banner for curl / the web UI.
echo "==> minting a dev token (admin-mint)"
TOKEN="$(go run ./apps/admin-api/cmd/admin-mint -user emp-42 -tenant team-platform -ttl 3600)"

# With a real LLM upstream the sandbox brain must present a real cocola token as
# ANTHROPIC_AUTH_TOKEN; with EchoProvider (no llm-gateway) it is never used, so
# leave the harmless default.
AGENT_API_KEY="cocola-local"
[[ "$WITH_LLM" == "1" ]] && AGENT_API_KEY="$TOKEN"

# ----------------------------------------------------------------- agent-runtime
free_port "$AGENT_PORT" agent-runtime
(
  cd apps/agent-runtime
  COCOLA_AGENT_HOST="$AGENT_HOST" COCOLA_AGENT_PORT="$AGENT_PORT" \
  COCOLA_AGENT_API_KEY="$AGENT_API_KEY" \
  COCOLA_ANTHROPIC_MODEL="${COCOLA_LLM_DEFAULT_ALIAS:-cocola-default}" \
  COCOLA_SANDBOX_ADDR="${COCOLA_SANDBOX_ADDR:-}" \
    $SETSID uv run python -m cocola_agent_runtime
) >"$(log_redirect agent-runtime)" 2>&1 &
PIDS+=("$!")
echo "==> starting agent-runtime on $AGENT_ADDR (log: .run-logs/agent-runtime.log)"
wait_port "$AGENT_HOST" "$AGENT_PORT" "agent-runtime"

# ----------------------------------------------------------------- gateway
free_port "$GATEWAY_PORT" gateway
(
  COCOLA_GATEWAY_ADDR="$GATEWAY_ADDR" COCOLA_AGENT_ADDR="$AGENT_ADDR" \
    $SETSID go run ./apps/gateway/cmd/gateway
) >"$(log_redirect gateway)" 2>&1 &
PIDS+=("$!")
echo "==> starting gateway on $GATEWAY_ADDR -> $AGENT_ADDR (log: .run-logs/gateway.log)"
wait_port "$GATEWAY_HOST" "$GATEWAY_PORT" "gateway"

# ----------------------------------------------------------------- web (opt-in)
if [[ "$WITH_WEB" == "1" ]]; then
  free_port "$WEB_PORT" web
  (
    cd apps/web
    COCOLA_GATEWAY_URL="http://$GATEWAY_ADDR" \
      $SETSID pnpm dev --port "$WEB_PORT"
  ) >"$(log_redirect web)" 2>&1 &
  PIDS+=("$!")
  echo "==> starting web on http://127.0.0.1:$WEB_PORT (log: .run-logs/web.log)"
  wait_port "127.0.0.1" "$WEB_PORT" "web" 240
fi

# ----------------------------------------------------------------- ready banner
echo
echo "======================================================================"
echo " cocola dev stack is UP"
echo "----------------------------------------------------------------------"
echo " gateway   : http://$GATEWAY_ADDR   (POST /v1/chat, SSE)"
echo " agent-rt  : $AGENT_ADDR (gRPC)"
[[ "$WITH_LLM" == "1" ]] && echo " llm-gw    : http://$LLM_HOST:$LLM_PORT  (provider: ${COCOLA_LLM_PROVIDER:-fake})"
[[ "$WITH_LLM" == "1" && -n "${COCOLA_LLM_CONFIG:-}" ]] && echo " llm-cfg   : ${COCOLA_LLM_CONFIG}"
[[ "$WITH_LLM" == "1" && -z "${COCOLA_LLM_CONFIG:-}" ]] && echo " llm-up    : ${COCOLA_ANTHROPIC_BASE_URL:-${COCOLA_OPENAI_BASE_URL:-<provider default>}}"
[[ "$WITH_WEB" == "1" ]] && echo " web       : http://127.0.0.1:$WEB_PORT  (paste the token below)"
[[ -n "${MINIO_CONSOLE:-}" ]] && echo " minio     : ${MINIO_CONSOLE}  (console; cocola / cocola_dev_pw)"
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
