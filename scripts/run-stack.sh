#!/usr/bin/env bash
# run-stack.sh - deploy mode 1 for cocola: everything NATIVE except the sandbox.
#
# This is the DEFAULT local debug stack (Makefile: `make dev`). Only the
# sandbox's OWN container dependencies stay containerized -- the OpenSandbox
# server (:8090) plus redis/postgres/minio (docker-compose.dev.yml). EVERY
# cocola-authored service runs NATIVE in the foreground and is torn down on
# Ctrl-C:
#
#   web (Next.js) --(SSE proxy)--> gateway (Go BFF) --(gRPC)--> agent-runtime (Py)
#                                                                   |
#                              (Route A: brain runs inside sandbox) v
#                                                          llm-gateway (Py, FastAPI)
#
#   native  : sandbox-manager :50051  llm-gateway :8081  admin-api :8092
#             agent-runtime :50061    gateway :8080      web :3000
#   contain.: OpenSandbox server :8090 + redis/postgres/minio (dev.yml)
#
# Result: REAL Route A (brain in sandbox) + real model, and editing ANY cocola
# service means just Ctrl-C + re-run -- ZERO image rebuilds. The formal/full
# Docker mode is `make prod` (scripts/start.sh + docker-compose.full.yml).
#
# Design notes
#   * Port 8080 collision: the gateway BFF and the llm-gateway BOTH default to
#     8080. We pin llm-gateway to COCOLA_LLM_PORT (default 8081) so they never
#     fight; the sandbox brain reaches it via COCOLA_SANDBOX_LLM_BASE_URL.
#   * Route A (brain-in-sandbox, ADR-0009) is the only real path; the legacy
#     Route B was decommissioned (see ADR-0009).
#   * Every child logs to .run-logs/<name>.log; this script prints a token you
#     can paste into the web UI or a curl call.
#
# Usage:
#   bash scripts/run-stack.sh            # dev mode (= make dev)
#   bash scripts/run-stack.sh --help     # show this header
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

export PATH="/Applications/OrbStack.app/Contents/MacOS/xbin:/opt/homebrew/bin:/usr/local/bin:$PATH"

# Deploy mode 1 has exactly ONE shape now. These three switches are always on:
# every cocola service runs NATIVE (sandbox-manager/admin-api included), the
# real llm-gateway is up, and the web tool is served. They are kept as constants
# so the guarded blocks read clearly. There are no mode flags.
WITH_LLM=1
WITH_WEB=1
DEV_STACK=1
for arg in "$@"; do
  case "$arg" in
    -h|--help)
      grep '^#' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "unknown flag: $arg (this script has one mode; try --help)" >&2; exit 2 ;;
  esac
done

# ----------------------------------------------------------------- config
# Auto-load repo-root .env if present, so `make dev` can pick up your real model
# endpoint/key without manual exports. Existing env wins over the file
# (so a one-off `COCOLA_LLM_PROVIDER=fake make dev` still overrides .env).
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
export AUTH_SECRET="${AUTH_SECRET:-local-dev-auth-secret}"
export COCOLA_ADMIN_KEY="${COCOLA_ADMIN_KEY:-local-dev-admin-key}"
export COCOLA_MODEL_SECRET_KEY="${COCOLA_MODEL_SECRET_KEY:-cocola-local-model-secret}"
export COCOLA_CONFIG_SECRET_KEY="${COCOLA_CONFIG_SECRET_KEY:-$COCOLA_MODEL_SECRET_KEY}"
export COCOLA_BOOTSTRAP_ADMIN_USERNAME="${COCOLA_BOOTSTRAP_ADMIN_USERNAME:-admin}"
export COCOLA_BOOTSTRAP_ADMIN_EMAIL="${COCOLA_BOOTSTRAP_ADMIN_EMAIL:-admin@cocola.local}"
export COCOLA_BOOTSTRAP_ADMIN_PASSWORD="${COCOLA_BOOTSTRAP_ADMIN_PASSWORD:-cocola-admin}"
export COCOLA_BOOTSTRAP_ADMIN_RESET="${COCOLA_BOOTSTRAP_ADMIN_RESET:-true}"
export COCOLA_BOOTSTRAP_ADMIN_PRINT="${COCOLA_BOOTSTRAP_ADMIN_PRINT:-true}"

AGENT_HOST="${COCOLA_AGENT_HOST:-127.0.0.1}"
AGENT_PORT="${COCOLA_AGENT_PORT:-50061}"
AGENT_ADDR="$AGENT_HOST:$AGENT_PORT"

GATEWAY_HOST="${COCOLA_GATEWAY_HOST:-127.0.0.1}"
GATEWAY_PORT="${COCOLA_GATEWAY_PORT:-8080}"
GATEWAY_ADDR="$GATEWAY_HOST:$GATEWAY_PORT"

# In dev mode the sandbox brain runs INSIDE a container and reaches this gateway
# over host.docker.internal (the Docker host bridge), NOT loopback. A gateway
# bound to 127.0.0.1 is unreachable from the container -> the in-sandbox claude
# CLI stalls on its first model call until the client gives up ("chat hangs with
# no response"). So default the dev bind to 0.0.0.0; native-only modes keep
# loopback. Either is still overridable via COCOLA_LLM_HOST.
_llm_host_default="127.0.0.1"
[[ "$DEV_STACK" == "1" ]] && _llm_host_default="0.0.0.0"
LLM_HOST="${COCOLA_LLM_HOST:-$_llm_host_default}"
LLM_PORT="${COCOLA_LLM_PORT:-8081}"   # NOT 8080: that is the gateway port.

WEB_PORT="${COCOLA_WEB_PORT:-3000}"

LOG_DIR="$ROOT/.run-logs"
mkdir -p "$LOG_DIR"
# cleanup.log is append-only during teardown. Reset it for each stack run so
# repeated local starts do not accumulate thousands of stale process checks.
: > "$LOG_DIR/cleanup.log"

PIDS=()

# Ports this script owns. Teardown frees every one of them as a hard backstop:
# go run / uv run / pnpm dev fork the real listeners as grandchildren that get
# reparented to launchd on macOS, escaping our process group -- so killing the
# group is NOT enough to guarantee the port is released. Freeing by port is.
OWNED_PORTS=("$AGENT_PORT" "$GATEWAY_PORT")
[[ "$WITH_LLM" == "1" ]] && OWNED_PORTS+=("$LLM_PORT")
[[ "$WITH_WEB" == "1" ]] && OWNED_PORTS+=("$WEB_PORT")
# dev runs sandbox-manager (50051) and admin-api (8092) NATIVELY too.
[[ "$DEV_STACK" == "1" ]] && OWNED_PORTS+=(50051 8092)

log_redirect() { printf '%s/%s.log' "$LOG_DIR" "$1"; }

docker_compose() {
  if docker compose version >/dev/null 2>&1; then
    docker compose "$@"
    return
  fi
  if command -v docker-compose >/dev/null 2>&1; then
    docker-compose "$@"
    return
  fi
  echo "!! docker compose is unavailable; install Docker Compose v2 plugin or docker-compose v1." >&2
  return 127
}

env_bool_false() {
  case "${1:-}" in
    0|false|FALSE|False|no|NO|No|off|OFF|Off) return 0 ;;
    *) return 1 ;;
  esac
}

opensandbox_health_url() {
  local url="${1:-}"
  url="${url%/}"
  url="${url%/v1}"
  printf '%s/health' "$url"
}

# Graceful, deterministic teardown. The contract: when this returns, NONE of our
# ports stay occupied. Three phases, escalating only as needed:
#   1. SIGTERM each service process group  -> lets go/uv/node flush & exit.
#   2. wait briefly for them to die on their own.
#   3. SIGKILL any survivor groups, then free every owned port by force.
# Phase 3 port sweep is the backstop that catches reparented grandchildren the
# process-group signal cannot reach (the real cause of "exited but port busy").
_SHUTTING_DOWN=0

process_group_alive() {
  local pid="$1"
  kill -0 -- "-$pid" 2>/dev/null || kill -0 "$pid" 2>/dev/null
}

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

  # Phase 2: give process groups up to 30s to exit cleanly. Checking the group
  # matters for wrappers such as `go run`: the wrapper may exit immediately on
  # TERM while its child is still flushing a checkpoint in the same group.
  for ((i=0; i<150; i++)); do
    alive=0
    for pid in "${PIDS[@]:-}"; do
      [[ -n "$pid" ]] || continue
      process_group_alive "$pid" && alive=1
    done
    [[ "$alive" == "0" ]] && break
    sleep 0.2
  done

  # Phase 3a: SIGKILL any process groups still standing.
  for pid in "${PIDS[@]:-}"; do
    [[ -n "$pid" ]] || continue
    if process_group_alive "$pid"; then
      kill -KILL -- "-$pid" 2>>"$LOG_DIR/cleanup.log" \
        || kill -KILL "$pid" 2>>"$LOG_DIR/cleanup.log" || true
    fi
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
# CRITICAL: a container that PUBLISHES a host port is fronted by the container
# engine's OWN proxy process (on OrbStack/Docker Desktop that is a single
# "OrbStack Helper" / "com.docker.backend" / "vpnkit" process). lsof on such a
# port returns THAT engine pid -- so a naive kill would SIGTERM the whole engine,
# tearing down the VM and every container with it. This actually bit us: running
# `make dev` while the full containerized stack still held 50051/8080/8092
# made free_port kill "OrbStack", the VM bounced, and every container died
# ("Received signal, requesting stop" in the OrbStack vmgr log). So free_port
# NEVER kills a container-engine/proxy process; ports it owns belong to
# containers and must be freed via docker (make dev-down / opensandbox-down).
# We only reap our OWN native children (go run / uv run / pnpm dev, etc.).
#
# We resolve the command name via `lsof -F c` (NOT ps): on locked-down corporate
# macOS `ps` can be denied, and a blank name there would fall through to a kill.
# Anything we cannot positively identify as ours is treated as an engine/unknown
# and SPARED -- fail-safe: when unsure, never kill.
_engine_name_re='OrbStack|orbstack|com\.docker|[Dd]ocker|vpnkit|qemu|colima|lima|containerd|dockerd'

# Print, one per line, ONLY the pids on a port that are safe for us to reap
# (i.e. positively NOT a container engine / port forwarder). Unknown => skipped.
_reapable_pids_on_port() {
  local port="$1" pid cmd
  # -F pc emits: p<pid> then c<command> lines, grouped per process.
  while IFS= read -r line; do
    case "$line" in
      p*) pid="${line#p}" ;;
      c*)
        cmd="${line#c}"
        if [[ -n "$pid" && ! "$cmd" =~ $_engine_name_re ]]; then
          printf '%s\n' "$pid"
        fi
        pid=""
        ;;
    esac
  done < <(lsof -nP -iTCP:"$port" -sTCP:LISTEN -Fpc 2>/dev/null || true)
}

free_port() {
  local port="$1" name="$2" pids all
  all="$(lsof -ti "TCP:$port" -s "TCP:LISTEN" 2>/dev/null || true)"
  [[ -z "$all" ]] && return 0
  pids="$(_reapable_pids_on_port "$port" | tr '\n' ' ')"
  pids="${pids// /  }"; pids="$(echo $pids)"   # normalize whitespace
  if [[ -z "$pids" ]]; then
    echo "==> port $port ($name) is held by the container engine (published container port); NOT killing it" >&2
    echo "    stop the owning container via docker / make dev-down / make opensandbox-down" >&2
    return 0
  fi
  echo "==> port $port ($name) busy; freeing stale NATIVE listener(s): $pids"
  # shellcheck disable=SC2086
  kill $pids 2>/dev/null || true
  for ((i=0; i<10; i++)); do
    pids="$(_reapable_pids_on_port "$port" | tr '\n' ' ')"; pids="$(echo $pids)"
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

# ----------------------------------------------------------------- dev mode
# THE single debug mode. Only the sandbox's OWN container dependencies stay
# containerized; every cocola-authored service runs NATIVELY in the foreground:
#
#   containers (sandbox deps only):
#     OpenSandbox server  host :8090   (drives execd/egress sibling containers)
#     redis :6379 / postgres :5432 / minio :9000,:9001   (docker-compose.dev.yml)
#   native (this script, Ctrl-C tears all down):
#     sandbox-manager :50051  llm-gateway :8081  admin-api :8092
#     agent-runtime :50061    gateway :8080      web :3000
#
# Result: REAL Route A (brain in sandbox) + real model, and editing ANY cocola
# service means just Ctrl-C + re-run -- ZERO image rebuilds. The idempotent
# `up -d` leaves the warm sandbox/infra containers untouched between runs.
#
# Wiring pitfalls handled here:
#   * sandbox-manager is native, so it CANNOT use host.docker.internal:8090 from
#     .env; we force COCOLA_OPENSANDBOX_URL -> http://127.0.0.1:8090/v1.
#   * We keep server-proxy exec (do NOT set COCOLA_OPENSANDBOX_DIRECT_EXEC): the
#     server would otherwise hand back a host.docker.internal exec URL a native
#     manager cannot resolve.
#   * native admin-api would collide with the OpenSandbox server on :8090, so it
#     listens on :8092 (COCOLA_ADMIN_ADDR).
#   * the sandbox BRAIN still runs inside a container, so it reaches the native
#     llm-gateway via host.docker.internal:$LLM_PORT (injected at creation).
# Sandbox/infra containers survive Ctrl-C; stop them with
# `make dev-down` (infra) and `make opensandbox-down` (sandbox server).
dev_up() {
  command -v docker >/dev/null 2>&1 || { echo "!! dev mode needs docker; is Docker Desktop running?" >&2; exit 1; }
  docker info >/dev/null 2>&1 || { echo "!! docker daemon unreachable; start Docker Desktop first." >&2; exit 1; }

  # Preflight: dev mode runs sandbox-manager/gateway/admin-api/agent-runtime/web
  # NATIVELY, so their host ports must be FREE. If the full containerized stack
  # is still up, its containers PUBLISH those same ports -- the native binds
  # would fail, and because a published port is fronted by the engine proxy we
  # must NOT (and now will not) try to reap it. Rather than crash mid-boot,
  # detect the conflicting stack here and tell the user to stop it first.
  # First self-heal: sandbox-manager may have left per-session sandbox
  # containers (image cocola/sandbox-runtime:dev, name sandbox-<uuid>) behind
  # from a prior run. Those are EPHEMERAL session sandboxes -- never part of the
  # app stack -- so they are always safe to reap, and one of them publishing
  # :50051 is the usual reason this preflight tripped. Remove them up front so a
  # stale sandbox alone never blocks a fresh `make dev`.
  local _stale_sbx
  _stale_sbx="$(docker ps -aq --filter 'ancestor=cocola/sandbox-runtime:dev' 2>/dev/null || true)"
  if [[ -n "$_stale_sbx" ]]; then
    echo "==> [dev] removing stale session sandbox container(s) holding host ports" >&2
    # shellcheck disable=SC2086
    docker rm -f $_stale_sbx >/dev/null 2>&1 || true
  fi

  local _conflict="" _p _cid
  for _p in 50051 8080 8092 3000 8081; do
    _cid="$(docker ps --filter "publish=$_p" --format '{{.Names}}' 2>/dev/null | head -1 || true)"
    [[ -n "$_cid" ]] && _conflict+=$'\n'"    :$_p is published by container '$_cid'"
  done
  if [[ -n "$_conflict" ]]; then
    echo "!! dev mode runs cocola services NATIVELY, but a containerized stack still holds their ports:$_conflict" >&2
    echo "   those app containers are the FULL stack (full Docker stack / start.sh). Stop it first, then re-run dev mode:" >&2
    echo "     bash scripts/start.sh --down     # tear down the full containerized stack" >&2
    echo "   (this mode then brings up ONLY the sandbox deps -- OpenSandbox server + redis/pg/minio -- itself)" >&2
    exit 1
  fi

  # (1) OpenSandbox server (host :8090) when this script is run directly. The
  # outer dev wrapper sets COCOLA_OPENSANDBOX_MANAGED=0 and points us at the
  # Kubernetes OpenSandbox port-forward it already prepared.
  local provider="${COCOLA_SANDBOX_PROVIDER:-}"
  if [[ -z "$provider" && -f "$ROOT/.env" ]]; then
    provider="$(grep -E '^COCOLA_SANDBOX_PROVIDER=' "$ROOT/.env" | tail -1 | cut -d= -f2-)"
  fi
  provider="${provider:-opensandbox}"
  if [[ "$provider" == "opensandbox" ]] && env_bool_false "${COCOLA_OPENSANDBOX_MANAGED:-1}"; then
    echo "==> [dev] external OpenSandbox server selected (COCOLA_OPENSANDBOX_MANAGED=0); skipping docker-compose OpenSandbox"
    if [[ -z "${COCOLA_OPENSANDBOX_URL:-}" ]]; then
      echo "!! COCOLA_OPENSANDBOX_MANAGED=0 requires COCOLA_OPENSANDBOX_URL (for example http://127.0.0.1:8090/v1)" >&2
      exit 1
    fi
    local external_health
    external_health="$(opensandbox_health_url "$COCOLA_OPENSANDBOX_URL")"
    if ! curl -fsS -m 3 "$external_health" >/dev/null 2>&1; then
      echo "!! external OpenSandbox server is not reachable: $external_health" >&2
      echo "   unset COCOLA_OPENSANDBOX_MANAGED/COCOLA_OPENSANDBOX_URL for the default make dev path," >&2
      echo "   or start the external OpenSandbox server before running make dev." >&2
      exit 1
    fi
  elif [[ "$provider" == "opensandbox" ]]; then
    local osb_port="${COCOLA_OPENSANDBOX_HOST_PORT:-8090}"
    echo "==> [dev] bringing up OpenSandbox server (host :$osb_port)"
    if ! COCOLA_OPENSANDBOX_HOST_PORT="$osb_port" \
      docker_compose -f deploy/docker-compose/docker-compose.opensandbox.yml up -d \
        >"$(log_redirect dev-opensandbox)" 2>&1; then
      echo "!! [dev] OpenSandbox server bring-up failed; see .run-logs/dev-opensandbox.log" >&2
      tail -80 "$(log_redirect dev-opensandbox)" >&2 || true
      exit 1
    fi
    local osb_ready=0
    for ((i=0; i<60; i++)); do
      if curl -fsS -m 3 "http://127.0.0.1:$osb_port/health" >/dev/null 2>&1; then
        osb_ready=1
        break
      fi
      sleep 2
    done
    if [[ "$osb_ready" != "1" ]]; then
      echo "!! [dev] OpenSandbox server did not become healthy on http://127.0.0.1:$osb_port/health" >&2
      docker_compose -f deploy/docker-compose/docker-compose.opensandbox.yml logs --tail=80 opensandbox-server >&2 || true
      exit 1
    fi
  fi

  # (2) Infra only: redis / postgres / minio (the third-party stateful deps).
  echo "==> [dev] starting containerized infra (redis/postgres/minio) via docker-compose.dev.yml"
  docker_compose -f deploy/docker-compose/docker-compose.dev.yml up -d \
      redis postgres minio minio-init \
      >"$(log_redirect dev-infra)" 2>&1 \
    || { echo "!! [dev] infra bring-up failed; see .run-logs/dev-infra.log" >&2; exit 1; }
  wait_port 127.0.0.1 6379 "redis"    120
  wait_port 127.0.0.1 5432 "postgres" 120
  wait_port 127.0.0.1 9000 "minio"    120

  # Shared infra wiring for every native process launched below.
  export COCOLA_REDIS_ADDR="${COCOLA_REDIS_ADDR:-127.0.0.1:6379}"
  export COCOLA_PG_DSN="${COCOLA_PG_DSN:-postgres://cocola:cocola_dev_pw@127.0.0.1:5432/cocola?sslmode=disable}"
  export COCOLA_MINIO_ENDPOINT="${COCOLA_MINIO_ENDPOINT:-127.0.0.1:9000}"
  export COCOLA_MINIO_ACCESS_KEY="${COCOLA_MINIO_ACCESS_KEY:-cocola}"
  export COCOLA_MINIO_SECRET_KEY="${COCOLA_MINIO_SECRET_KEY:-cocola_dev_pw}"
  export COCOLA_MINIO_BUCKET="${COCOLA_MINIO_BUCKET:-cocola}"
  export COCOLA_ATTACHMENT_INLINE_MAX_BYTES="${COCOLA_ATTACHMENT_INLINE_MAX_BYTES:-16777216}"
  # MinIO is already up here; skip run-stack's own dev.yml MinIO block later.
  export COCOLA_SKIP_MINIO=1
  # Web-UI dev ergonomics: blank token -> dev-user (matches the full stack).
  export COCOLA_AUTH_ALLOW_ANON="${COCOLA_AUTH_ALLOW_ANON:-1}"

  # (3) NATIVE sandbox-manager. It is a standalone Go module kept OUT of go.work,
  # so it MUST build/run with GOWORK=off from its own module dir. Talk to the
  # OpenSandbox server over the HOST loopback (host.docker.internal is
  # container-only and would not resolve here).
  local osb_url="${COCOLA_OPENSANDBOX_URL:-http://127.0.0.1:${COCOLA_OPENSANDBOX_HOST_PORT:-8090}/v1}"
  free_port 50051 sandbox-manager
  (
    cd apps/sandbox-manager
    GOWORK=off \
    COCOLA_SANDBOX_ADDR=":50051" \
    COCOLA_SANDBOX_PROVIDER="$provider" \
    COCOLA_OPENSANDBOX_URL="$osb_url" \
    COCOLA_REDIS_ADDR="$COCOLA_REDIS_ADDR" \
    COCOLA_SANDBOX_IMAGE="${COCOLA_SANDBOX_IMAGE:-cocola/sandbox-runtime:dev}" \
    COCOLA_SANDBOX_LLM_BASE_URL="${COCOLA_SANDBOX_LLM_BASE_URL:-http://host.docker.internal:$LLM_PORT}" \
    COCOLA_SANDBOX_LLM_TOKEN="${COCOLA_SANDBOX_LLM_TOKEN:-cocola-local}" \
    COCOLA_SANDBOX_MODEL_ALIAS="${COCOLA_SANDBOX_MODEL_ALIAS:-cocola-default}" \
      $SETSID go run ./cmd/sandbox-manager
  ) >"$(log_redirect sandbox-manager)" 2>&1 &
  PIDS+=("$!")
  echo "==> [dev] starting NATIVE sandbox-manager on :50051 (provider=$provider -> $osb_url; log: .run-logs/sandbox-manager.log)"
  wait_port 127.0.0.1 50051 "sandbox-manager" 180

  # (4) Point the app processes (launched by the main flow) at the native stack.
  export COCOLA_SANDBOX_ADDR="${COCOLA_SANDBOX_ADDR:-127.0.0.1:50051}"
  export COCOLA_SANDBOX_IMAGE="${COCOLA_SANDBOX_IMAGE:-cocola/sandbox-runtime:dev}"
  # The sandbox brain runs INSIDE a container -> reach the native llm-gateway
  # (:$LLM_PORT below) over the host bridge.
  export COCOLA_SANDBOX_LLM_BASE_URL="${COCOLA_SANDBOX_LLM_BASE_URL:-http://host.docker.internal:$LLM_PORT}"
  export COCOLA_SANDBOX_LLM_TOKEN="${COCOLA_SANDBOX_LLM_TOKEN:-cocola-local}"
  export COCOLA_SANDBOX_MODEL_ALIAS="${COCOLA_SANDBOX_MODEL_ALIAS:-cocola-default}"
  echo "==> [dev] sandbox + infra ready; launching native cocola services (real Route A)"
}

if [[ "$DEV_STACK" == "1" ]]; then
  dev_up
fi

# ----------------------------------------------------------------- llm-gateway
if [[ "$WITH_LLM" == "1" ]]; then
  free_port "$LLM_PORT" llm-gateway
  # In dev mode the sandbox brain presents the dev token COCOLA_SANDBOX_LLM_TOKEN
  # (default cocola-local). The containerized full stack runs llm-gateway with
  # auth OFF so that token is accepted; match that here by blanking the secret
  # for THIS process only (the global export stays put for gateway/agent-rt).
  LLM_AUTH_SECRET="${COCOLA_AUTH_SECRET:-}"
  [[ "$DEV_STACK" == "1" ]] && LLM_AUTH_SECRET=""
  ( cd apps/llm-gateway && \
    COCOLA_LLM_HOST="$LLM_HOST" COCOLA_LLM_PORT="$LLM_PORT" \
    COCOLA_AUTH_SECRET="$LLM_AUTH_SECRET" \
    COCOLA_MODEL_SECRET_KEY="$COCOLA_MODEL_SECRET_KEY" \
    COCOLA_CONFIG_SECRET_KEY="$COCOLA_CONFIG_SECRET_KEY" \
    COCOLA_LLM_REDIS_URL="${COCOLA_LLM_REDIS_URL:-redis://127.0.0.1:6379/0}" \
    $SETSID uv run python -m cocola_llm_gateway ) >"$(log_redirect llm-gateway)" 2>&1 &
  PIDS+=("$!")
  echo "==> starting llm-gateway on $LLM_HOST:$LLM_PORT (provider: ${COCOLA_LLM_PROVIDER:-fake})"
  # nc -z 0.0.0.0 is unreliable; a 0.0.0.0 bind also answers on loopback, so probe there.
  llm_probe_host="$LLM_HOST"; [[ "$LLM_HOST" == "0.0.0.0" ]] && llm_probe_host="127.0.0.1"
  wait_port "$llm_probe_host" "$LLM_PORT" "llm-gateway"
  # Route A: the sandbox's in-sandbox claude CLI reaches the gateway via
  # COCOLA_SANDBOX_LLM_BASE_URL (injected at sandbox creation), not via the
  # agent-runtime process env. Surface the URL so a real Route-A run can point
  # the sandbox at it.
  export COCOLA_SANDBOX_LLM_BASE_URL="${COCOLA_SANDBOX_LLM_BASE_URL:-http://$LLM_HOST:$LLM_PORT}"
  echo "    llm-gateway up; sandbox brain should target $COCOLA_SANDBOX_LLM_BASE_URL"
fi

# ----------------------------------------------------------------- admin-api (dev)
# dev mode runs admin-api NATIVELY too (agent-runtime's market-skills source).
# It listens on :8092 -- NOT :8090 -- because the OpenSandbox server owns host
# :8090. admin-api is a SOFT dependency: if it is down agent-runtime just warns
# "no market skills" and still serves chat. Persist to the same Postgres/Redis.
if [[ "$DEV_STACK" == "1" ]]; then
  free_port 8092 admin-api
  (
    COCOLA_ADMIN_ADDR=":8092" \
    COCOLA_REDIS_ADDR="${COCOLA_REDIS_ADDR:-127.0.0.1:6379}" \
    COCOLA_PG_DSN="${COCOLA_PG_DSN:-}" \
    COCOLA_SANDBOX_ADDR="${COCOLA_SANDBOX_ADDR:-127.0.0.1:50051}" \
    COCOLA_OPENSANDBOX_URL="${COCOLA_OPENSANDBOX_URL:-http://127.0.0.1:${COCOLA_OPENSANDBOX_HOST_PORT:-8090}/v1}" \
    COCOLA_LLM_GATEWAY_URL="${COCOLA_LLM_GATEWAY_URL:-http://$LLM_HOST:$LLM_PORT}" \
    COCOLA_MODEL_SECRET_KEY="$COCOLA_MODEL_SECRET_KEY" \
    COCOLA_CONFIG_SECRET_KEY="$COCOLA_CONFIG_SECRET_KEY" \
    COCOLA_BOOTSTRAP_ADMIN_USERNAME="$COCOLA_BOOTSTRAP_ADMIN_USERNAME" \
    COCOLA_BOOTSTRAP_ADMIN_EMAIL="$COCOLA_BOOTSTRAP_ADMIN_EMAIL" \
    COCOLA_BOOTSTRAP_ADMIN_PASSWORD="$COCOLA_BOOTSTRAP_ADMIN_PASSWORD" \
    COCOLA_BOOTSTRAP_ADMIN_PASSWORD_HASH="${COCOLA_BOOTSTRAP_ADMIN_PASSWORD_HASH:-}" \
    COCOLA_BOOTSTRAP_ADMIN_RESET="${COCOLA_BOOTSTRAP_ADMIN_RESET:-}" \
    COCOLA_BOOTSTRAP_ADMIN_PRINT="$COCOLA_BOOTSTRAP_ADMIN_PRINT" \
      $SETSID go run ./apps/admin-api/cmd/admin-api
  ) >"$(log_redirect admin-api)" 2>&1 &
  PIDS+=("$!")
  echo "==> [dev] starting NATIVE admin-api on :8092 (log: .run-logs/admin-api.log)"
  wait_port 127.0.0.1 8092 "admin-api" 120
  export COCOLA_ADMIN_BASE_URL="${COCOLA_ADMIN_BASE_URL:-http://127.0.0.1:8092}"
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
  if docker_compose -f deploy/docker-compose/docker-compose.dev.yml up -d minio minio-init \
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
    COCOLA_ADMIN_URL="${COCOLA_ADMIN_BASE_URL:-http://127.0.0.1:8092}" \
    COCOLA_ADMIN_KEY="$COCOLA_ADMIN_KEY" \
    AUTH_SECRET="$AUTH_SECRET" \
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
[[ "$WITH_WEB" == "1" ]] && echo " web       : http://127.0.0.1:$WEB_PORT"
[[ "$WITH_WEB" == "1" ]] && echo " web login : ${COCOLA_BOOTSTRAP_ADMIN_USERNAME} or ${COCOLA_BOOTSTRAP_ADMIN_EMAIL} / ${COCOLA_BOOTSTRAP_ADMIN_PASSWORD}"
[[ -n "${MINIO_CONSOLE:-}" ]] && echo " minio     : ${MINIO_CONSOLE}  (console; cocola / cocola_dev_pw)"
[[ "$DEV_STACK" == "1" ]] && echo " sandbox   : NATIVE sandbox-manager :50051 (provider=${COCOLA_SANDBOX_PROVIDER:-opensandbox}) + admin-api :8092; REAL Route A"
if [[ "$DEV_STACK" == "1" ]] && [[ "${COCOLA_SANDBOX_PROVIDER:-opensandbox}" == "opensandbox" ]] && env_bool_false "${COCOLA_OPENSANDBOX_MANAGED:-1}"; then
  echo " containers: only infra -- external OpenSandbox server ${COCOLA_OPENSANDBOX_URL:-<unset>} + redis/pg/minio (dev.yml)"
elif [[ "$DEV_STACK" == "1" ]]; then
  echo " containers: only sandbox deps -- OpenSandbox server :${COCOLA_OPENSANDBOX_HOST_PORT:-8090} + redis/pg/minio (dev.yml)"
fi
[[ "$DEV_STACK" == "1" ]] && echo " stop cont : make dev-down  (infra) + make opensandbox-down  (sandbox); they survive Ctrl-C, app relaunches with no rebuild"
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
