#!/usr/bin/env bash
# start.sh -- cocola 全栈一键启动（容器化控制面 + 真实模型 Route A）。
#
# 封装 deploy/docker-compose/docker-compose.full.yml 的标准启动流程，规避两个
# 已知坑：
#   1) 必带 --env-file .env，否则 llm-gateway 回落 fake provider（echo 回显）。
#   2) 网关发布端口用 18091（绕开宿主上遗留的 IPv4 :8081 监听导致的 401）。
#
# 当 .env 里 COCOLA_SANDBOX_PROVIDER=opensandbox 时，还会自动拉起/拆除独立的
# OpenSandbox server（docker-compose.opensandbox.yml，宿主 :8090），full.yml 里的
# sandbox-manager 经 host.docker.internal:8090/v1 连它。provider=docker 时跳过。
#
# 用法：
#   bash scripts/start.sh            # 启动整栈（镜像缺失时自动构建）
#   bash scripts/start.sh --build    # 强制重新构建镜像后启动
#   bash scripts/start.sh --down     # 停止并删除容器
#   bash scripts/start.sh --stop     # 仅停止（保留容器与数据）
#   bash scripts/start.sh --logs     # 跟随查看全部服务日志
#   bash scripts/start.sh --status   # 查看服务状态
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

COMPOSE_FILE="deploy/docker-compose/docker-compose.full.yml"
ENV_FILE=".env"
COMPOSE_BIN="$ROOT/scripts/docker-compose.sh"
DC=("$COMPOSE_BIN" -f "$COMPOSE_FILE" --env-file "$ENV_FILE")
# 必须用 BuildKit=0 以绕开公司网络对 docker.io 的 TLS 拦截
export DOCKER_BUILDKIT=0

# OpenSandbox server 是独立 compose(宿主发布 :8090),不在 full.yml 网络内;
# full.yml 里的 sandbox-manager 经 host.docker.internal:8090/v1 跨 host 桥接连它。
# 仅当沙箱后端为 opensandbox 时才需要它;provider=docker(DooD)不需要 server。
OSB_COMPOSE="deploy/docker-compose/docker-compose.opensandbox.yml"
OSB_DC=("$COMPOSE_BIN" -f "$OSB_COMPOSE")
OSB_PORT="${COCOLA_OPENSANDBOX_HOST_PORT:-8090}"

# 从 .env / 环境读出沙箱后端。.env 里已存在 COCOLA_SANDBOX_PROVIDER 时以其为准;
# 环境变量优先。默认 docker(与 full.yml 默认一致)。
sandbox_provider() {
  if [[ -n "${COCOLA_SANDBOX_PROVIDER:-}" ]]; then
    printf '%s' "$COCOLA_SANDBOX_PROVIDER"; return
  fi
  if [[ -f "$ENV_FILE" ]]; then
    local v
    v="$(grep -E '^COCOLA_SANDBOX_PROVIDER=' "$ENV_FILE" | tail -1 | cut -d= -f2-)"
    [[ -n "$v" ]] && { printf '%s' "$v"; return; }
  fi
  printf 'docker'
}

env_file_value() {
  local key="$1"
  if [[ -f "$ENV_FILE" ]]; then
    grep -E "^${key}=" "$ENV_FILE" | tail -1 | cut -d= -f2-
  fi
}

needs_opensandbox() { [[ "$(sandbox_provider)" == "opensandbox" ]]; }

opensandbox_managed() {
  local managed="${COCOLA_OPENSANDBOX_MANAGED:-}"
  [[ -z "$managed" ]] && managed="$(env_file_value COCOLA_OPENSANDBOX_MANAGED)"
  managed="${managed:-1}"
  case "$managed" in
    0|false|FALSE|False|no|NO|No|off|OFF|Off) return 1 ;;
    *) return 0 ;;
  esac
}

opensandbox_url() {
  if [[ -n "${COCOLA_OPENSANDBOX_URL:-}" ]]; then
    printf '%s' "$COCOLA_OPENSANDBOX_URL"; return
  fi
  env_file_value COCOLA_OPENSANDBOX_URL
}

opensandbox_up() {
  needs_opensandbox || { log "沙箱后端非 opensandbox,跳过 OpenSandbox server。"; return 0; }
  if ! opensandbox_managed; then
    local url
    url="$(opensandbox_url)"
    if [[ -z "$url" ]]; then
      err "COCOLA_OPENSANDBOX_MANAGED=0 时必须设置 COCOLA_OPENSANDBOX_URL，例如 http://127.0.0.1:8090/v1"
      return 1
    fi
    log "使用外部 OpenSandbox server ($url)，跳过 docker-compose OpenSandbox server。"
    return 0
  fi
  log "拉起 OpenSandbox server(host :$OSB_PORT)..."
  COCOLA_OPENSANDBOX_HOST_PORT="$OSB_PORT" "${OSB_DC[@]}" up -d
  log "等待 OpenSandbox server /health ..."
  local i
  for ((i=1; i<=60; i++)); do
    if curl -fsS -m 3 "http://localhost:$OSB_PORT/health" >/dev/null 2>&1; then
      log "OpenSandbox server 已就绪。"; return 0
    fi
    sleep 2
  done
  err "OpenSandbox server 未在超时内就绪;查看:${OSB_DC[*]} logs opensandbox-server"
  "${OSB_DC[@]}" logs --tail=50 opensandbox-server || true
  return 1
}

opensandbox_down() {
  needs_opensandbox || return 0
  opensandbox_managed || { log "外部 OpenSandbox server 由调用方管理，跳过 down。"; return 0; }
  log "停止并删除 OpenSandbox server ..."
  "${OSB_DC[@]}" down || true
}

# sandbox-manager 运行时会经 OpenSandbox server 动态创建 per-session 沙箱容器
# (名字 sandbox-<uuid>、镜像 cocola/sandbox-runtime:dev、带 opensandbox.io/id label)。
# 它们不属于任一 compose 文件,所以 `compose down` 清不掉,会继续占用宿主端口
# (如 :50051),导致随后 `make up`(--hybrid 原生模式)因端口被占而失败。
# 这里按镜像 ancestor 精确匹配并强删,确保 --down/--stop 后端口彻底释放。
cleanup_sandboxes() {
  local ids
  ids="$(docker ps -aq --filter 'ancestor=cocola/sandbox-runtime:dev' 2>/dev/null || true)"
  if [[ -n "$ids" ]]; then
    log "清理动态沙箱容器 (cocola/sandbox-runtime:dev) ..."
    docker rm -f $ids >/dev/null 2>&1 || true
  fi
}

log()  { printf '\033[1;36m[start]\033[0m %s\n' "$*"; }
err()  { printf '\033[1;31m[start:err]\033[0m %s\n' "$*" >&2; }

require_env() {
  if [[ ! -f "$ENV_FILE" ]]; then
    err "缺少 $ENV_FILE -- 真实模型链路需要它（COCOLA_LLM_PROVIDER / COCOLA_ANTHROPIC_*）。"
    err "没有 .env 时 llm-gateway 会回落到 fake provider（echo 回显）。"
    exit 1
  fi
}

require_docker() {
  if ! docker info >/dev/null 2>&1; then
    err "Docker 守护进程不可用，请先打开 Docker Desktop。"
    exit 1
  fi
}

needs_build() {
  for img in cocola/gateway:dev cocola/llm-gateway:dev cocola/agent-runtime:dev \
             cocola/admin-api:dev cocola/sandbox-manager:dev cocola/web:dev; do
    if [[ -z "$(docker images -q "$img" 2>/dev/null)" ]]; then
      return 0
    fi
  done
  return 1
}

wait_healthy() {
  log "等待服务就绪 ..."
  local tries=60
  for ((i=1; i<=tries; i++)); do
    if curl -fsS -m 3 http://localhost:8080/healthz >/dev/null 2>&1; then
      log "gateway 已就绪。"
      return 0
    fi
    sleep 2
  done
  err "等待超时；请用 'bash scripts/start.sh --logs' 查看日志。"
  return 1
}

print_endpoints() {
  cat <<'TXT'

----------------------------------------------
  cocola 已启动
----------------------------------------------
  Web 界面 :  http://localhost:3000   <- 浏览器打开即用
  对话 API :  http://localhost:8080/v1/chat
  模型网关 :  http://localhost:18091  (接 .env 上游)

  Web 端 Bearer token：当前 dev 模式无需 token，留空即可
  （gateway 已设 COCOLA_AUTH_ALLOW_ANON=1，空 token 视为 dev-user）。

  常用：
    bash scripts/start.sh --status   # 看状态
    bash scripts/start.sh --logs     # 看日志
    bash scripts/start.sh --stop     # 停止（保留数据）
    bash scripts/start.sh --down     # 销毁容器
----------------------------------------------
TXT
}

main() {
  local action="${1:-up}"
  case "$action" in
    --down)
      require_docker
      log "停止并删除容器 ..."
      "${DC[@]}" down
      opensandbox_down
      cleanup_sandboxes
      ;;
    --stop)
      require_docker
      log "停止容器（保留数据）..."
      "${DC[@]}" stop
      if needs_opensandbox && opensandbox_managed; then
        "${OSB_DC[@]}" stop || true
      fi
      cleanup_sandboxes
      ;;
    --logs)
      "${DC[@]}" logs -f
      ;;
    --status)
      "${DC[@]}" ps
      ;;
    --build)
      require_docker; require_env
      log "强制构建镜像 ..."
      "${DC[@]}" build
      opensandbox_up
      log "启动整栈 ..."
      "${DC[@]}" up -d --remove-orphans
      wait_healthy && print_endpoints
      ;;
    up|"")
      require_docker; require_env
      if needs_build; then
        log "检测到镜像缺失，先构建（仅首次较慢）..."
        "${DC[@]}" build
      fi
      opensandbox_up
      log "启动整栈 ..."
      "${DC[@]}" up -d --remove-orphans
      wait_healthy && print_endpoints
      ;;
    -h|--help)
      sed -n '2,18p' "${BASH_SOURCE[0]}"
      ;;
    *)
      err "未知参数: $action （用 --help 查看用法）"
      exit 1
      ;;
  esac
}

main "$@"
