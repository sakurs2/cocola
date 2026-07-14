#!/usr/bin/env bash
# One-command local dev profile for cocola + OpenSandbox Kubernetes runtime.
#
# This intentionally wraps the existing pieces instead of replacing them:
#   - k3d provides a lightweight local k3s cluster.
#   - Helm installs OpenSandbox Server/Controller.
#   - scripts/run-stack.sh still starts cocola's native dev stack.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

export PATH="/Applications/OrbStack.app/Contents/MacOS/xbin:/opt/homebrew/bin:/usr/local/bin:$PATH"

# Keep background helpers out of the terminal's foreground process group so
# Ctrl+C reaches this supervisor first and dependencies can stop in order.
if command -v setsid >/dev/null 2>&1; then
  SETSID="setsid"
else
  SETSID=""
  set -m
fi

ACTION="${1:-up}"

CLUSTER="${COCOLA_K8S_CLUSTER:-cocola-sandbox}"
PREPULL_SANDBOX_IMAGE="${COCOLA_K8S_PREPULL_SANDBOX_IMAGE:-1}"
SANDBOX_IMAGE_REMOTE_DEFAULT="ghcr.io/sakurs2/cocola-sandbox-runtime:latest"
SANDBOX_IMAGE_REMOTE="${COCOLA_K8S_SANDBOX_IMAGE_REMOTE:-$SANDBOX_IMAGE_REMOTE_DEFAULT}"
SERVER_PORT="${COCOLA_OPENSANDBOX_HOST_PORT:-8090}"
RELEASE="${COCOLA_OPENSANDBOX_K8S_RELEASE:-opensandbox}"
SYSTEM_NAMESPACE="${COCOLA_OPENSANDBOX_K8S_SYSTEM_NAMESPACE:-opensandbox-system}"
SANDBOX_NAMESPACE="${COCOLA_OPENSANDBOX_K8S_NAMESPACE:-opensandbox}"
OPENSANDBOX_REPO="${OPENSANDBOX_REPO:-$HOME/Desktop/github/opensandbox}"
CHART_DIR="${COCOLA_OPENSANDBOX_K8S_CHART:-$OPENSANDBOX_REPO/kubernetes/charts/opensandbox}"
VALUES_FILE="${COCOLA_OPENSANDBOX_K8S_VALUES:-$ROOT/deploy/opensandbox-k8s/values.local.yaml}"
PVC_FILE="${COCOLA_OPENSANDBOX_K8S_PVC:-$ROOT/deploy/opensandbox-k8s/cocola-plugins-pvc.yaml}"
BATCHSANDBOX_TEMPLATE_FILE="${COCOLA_OPENSANDBOX_K8S_BATCHSANDBOX_TEMPLATE:-$ROOT/deploy/opensandbox-k8s/batchsandbox-template.yaml}"
BATCHSANDBOX_TEMPLATE_CM="cocola-batchsandbox-template"
SERVER_SERVICE="${COCOLA_OPENSANDBOX_K8S_SERVER_SERVICE:-opensandbox-server}"
LOG_DIR="$ROOT/.run-logs"
FORWARD_PID_FILE="$LOG_DIR/opensandbox-dev-forward.pid"
STACK_PID_FILE="$LOG_DIR/dev-stack.pid"
SETUP_LOG="$LOG_DIR/dev-setup.log"

log() { printf '\033[1;36m[dev]\033[0m %s\n' "$*"; }
err() { printf '\033[1;31m[dev:err]\033[0m %s\n' "$*" >&2; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || { err "missing command: $1"; return 127; }
}

preflight() {
  require_cmd docker
  require_cmd k3d
  require_cmd kubectl
  require_cmd helm
  docker info >/dev/null 2>&1 || { err "Docker daemon is unavailable"; return 1; }
}

cluster_exists() {
  k3d cluster list "$CLUSTER" >/dev/null 2>&1
}

ensure_chart() {
  if [[ ! -f "$CHART_DIR/Chart.yaml" ]]; then
    err "OpenSandbox chart not found: $CHART_DIR"
    err "set OPENSANDBOX_REPO=/path/to/opensandbox or COCOLA_OPENSANDBOX_K8S_CHART=/path/to/chart"
    return 1
  fi
}

ensure_cluster() {
  preflight
  if cluster_exists; then
    log "using existing k3d cluster: $CLUSTER"
  else
    log "creating single-node k3d cluster: $CLUSTER"
    k3d cluster create "$CLUSTER" \
      --servers 1 \
      --agents 0
  fi
  k3d kubeconfig merge "$CLUSTER" --kubeconfig-switch-context >/dev/null
}

prepull_sandbox_image() {
  if [[ "$PREPULL_SANDBOX_IMAGE" != "1" ]]; then
    log "skipping sandbox image pre-pull: COCOLA_K8S_PREPULL_SANDBOX_IMAGE=$PREPULL_SANDBOX_IMAGE"
    return 0
  fi

  local node="k3d-${CLUSTER}-server-0"
  log "pre-pulling sandbox image on k3d node $node: $SANDBOX_IMAGE_REMOTE"
  if ! docker exec "$node" crictl pull "$SANDBOX_IMAGE_REMOTE"; then
    err "failed to pre-pull $SANDBOX_IMAGE_REMOTE inside k3d node $node"
    err "check GHCR package visibility/network access, or set COCOLA_K8S_PREPULL_SANDBOX_IMAGE=0 to skip"
    return 1
  fi
}

install_opensandbox() {
  ensure_chart

  log "building Helm dependencies for $CHART_DIR"
  helm dependency build "$CHART_DIR"

  log "creating sandbox namespace $SANDBOX_NAMESPACE"
  kubectl create namespace "$SANDBOX_NAMESPACE" \
    --dry-run=client \
    -o yaml \
    | kubectl apply -f -

  log "creating OpenSandbox system namespace $SYSTEM_NAMESPACE"
  kubectl create namespace "$SYSTEM_NAMESPACE" \
    --dry-run=client \
    -o yaml \
    | kubectl apply -f -

  log "creating cocola-plugins PVC"
  kubectl -n "$SANDBOX_NAMESPACE" apply -f "$PVC_FILE"

  log "creating BatchSandbox template ConfigMap $BATCHSANDBOX_TEMPLATE_CM"
  kubectl -n "$SYSTEM_NAMESPACE" create configmap "$BATCHSANDBOX_TEMPLATE_CM" \
    --from-file=batchsandbox-template.yaml="$BATCHSANDBOX_TEMPLATE_FILE" \
    --dry-run=client \
    -o yaml \
    | kubectl apply -f -

  log "installing OpenSandbox release=$RELEASE namespace=$SYSTEM_NAMESPACE"
  helm upgrade --install "$RELEASE" "$CHART_DIR" \
    --namespace "$SYSTEM_NAMESPACE" \
    --create-namespace \
    -f "$VALUES_FILE"

  log "waiting for OpenSandbox deployments"
  kubectl -n "$SYSTEM_NAMESPACE" rollout status deployment \
    -l app.kubernetes.io/instance="$RELEASE" \
    --timeout=240s
}

uninstall_opensandbox() {
  log "uninstalling OpenSandbox release=$RELEASE from namespace=$SYSTEM_NAMESPACE"
  helm uninstall "$RELEASE" --namespace "$SYSTEM_NAMESPACE" || true
  log "deleting local BatchSandbox template ConfigMap"
  kubectl -n "$SYSTEM_NAMESPACE" delete configmap "$BATCHSANDBOX_TEMPLATE_CM" --ignore-not-found=true
  log "deleting local cocola-plugins PVC"
  kubectl -n "$SANDBOX_NAMESPACE" delete -f "$PVC_FILE" --ignore-not-found=true
}

print_opensandbox_status() {
  log "system namespace pods"
  kubectl -n "$SYSTEM_NAMESPACE" get pods -o wide || true
  log "sandbox namespace pods"
  kubectl -n "$SANDBOX_NAMESPACE" get pods -o wide || true
  log "sandbox PVCs"
  kubectl -n "$SANDBOX_NAMESPACE" get pvc || true
}

delete_sandbox_workloads() {
  log "deleting sandbox workloads before OpenSandbox teardown"
  kubectl -n "$SANDBOX_NAMESPACE" delete batchsandbox --all \
    --ignore-not-found=true \
    --timeout=120s \
    || true
  kubectl -n "$SANDBOX_NAMESPACE" delete pod \
    -l opensandbox.io/id \
    --ignore-not-found=true \
    --timeout=120s \
    || true
}

stop_forward() {
  if [[ -f "$FORWARD_PID_FILE" ]]; then
    local pid
    pid="$(cat "$FORWARD_PID_FILE" 2>/dev/null || true)"
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
      local i
      for ((i = 0; i < 25; i++)); do
        if ! kill -0 "$pid" 2>/dev/null; then
          break
        fi
        sleep 0.2
      done
      if kill -0 "$pid" 2>/dev/null; then
        log "OpenSandbox port-forward did not stop in time; killing it"
        kill -KILL -- "-$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null || true
      fi
      wait "$pid" 2>/dev/null || true
    fi
    rm -f "$FORWARD_PID_FILE"
  fi
}

stop_stack() {
  if [[ -f "$STACK_PID_FILE" ]]; then
    local pid
    pid="$(cat "$STACK_PID_FILE" 2>/dev/null || true)"
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
    rm -f "$STACK_PID_FILE"
  fi
}

graceful_stop() {
  trap '' INT TERM
  stop_stack
  stop_forward
  rm -f "$STACK_PID_FILE"
  exit 0
}

start_forward() {
  mkdir -p "$LOG_DIR"
  stop_forward
  (
    $SETSID kubectl -n "$SYSTEM_NAMESPACE" port-forward "svc/$SERVER_SERVICE" "$SERVER_PORT:80"
  ) >"$LOG_DIR/opensandbox-dev-forward.log" 2>&1 &
  echo "$!" >"$FORWARD_PID_FILE"

  local i
  for ((i=1; i<=90; i++)); do
    if curl -fsS -m 2 "http://127.0.0.1:$SERVER_PORT/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  err "OpenSandbox port-forward did not become healthy; see .run-logs/opensandbox-dev-forward.log"
  return 1
}

up() {
  mkdir -p "$LOG_DIR"
  printf '\n=== make dev %s ===\n' "$(date '+%Y-%m-%d %H:%M:%S %z')" >>"$SETUP_LOG"
  log "preparing sandbox runtime"
  if ! {
    ensure_cluster
    prepull_sandbox_image
    install_opensandbox
    start_forward
  } >>"$SETUP_LOG" 2>&1; then
    err "sandbox runtime preparation failed; see .run-logs/dev-setup.log"
    tail -40 "$SETUP_LOG" >&2 || true
    return 1
  fi
  log "sandbox runtime ready"

  log "starting application services"
  trap graceful_stop INT TERM
  trap 'rm -f "$STACK_PID_FILE"; stop_forward' EXIT
  (
    COCOLA_OPENSANDBOX_MANAGED=0 \
      COCOLA_OPENSANDBOX_URL="http://127.0.0.1:${SERVER_PORT}/v1" \
      COCOLA_OPENSANDBOX_DIRECT_EXEC=0 \
      COCOLA_OPENSANDBOX_HTTP_TIMEOUT="${COCOLA_OPENSANDBOX_HTTP_TIMEOUT:-240s}" \
      COCOLA_CLUSTER_MANAGER_MODE="${COCOLA_CLUSTER_MANAGER_MODE:-k3s}" \
      COCOLA_SANDBOX_NODE_NAMESPACE="${COCOLA_SANDBOX_NODE_NAMESPACE:-opensandbox}" \
      COCOLA_SANDBOX_NODE_POD_SELECTOR="${COCOLA_SANDBOX_NODE_POD_SELECTOR:-opensandbox.io/id}" \
      COCOLA_SANDBOX_IMAGE="$SANDBOX_IMAGE_REMOTE" \
        bash scripts/run-stack.sh
  ) &
  local stack_pid="$!"
  echo "$stack_pid" >"$STACK_PID_FILE"
  # Background jobs already have their own process groups. Disable monitor
  # mode before waiting so Bash does not print one termination line per job.
  set +m
  set +e
  wait "$stack_pid"
  local status="$?"
  set -e
  case "$status" in
    0)
      return 0
      ;;
    130|143)
      return 0
      ;;
    *)
      return "$status"
      ;;
  esac
}

down() {
  preflight
  stop_stack
  stop_forward
  log "uninstalling OpenSandbox Kubernetes runtime; keeping k3d cluster $CLUSTER"
  delete_sandbox_workloads
  uninstall_opensandbox
  log "stopping cocola infra containers"
  make dev-down || true
}

reset() {
  preflight
  stop_forward
  if cluster_exists; then
    log "deleting k3d cluster: $CLUSTER"
    k3d cluster delete "$CLUSTER"
  else
    log "k3d cluster not found: $CLUSTER"
  fi
}

status() {
  preflight
  if cluster_exists; then
    k3d cluster list "$CLUSTER"
    k3d node list | grep "$CLUSTER" || true
    kubectl config use-context "k3d-$CLUSTER" >/dev/null 2>&1 || true
    print_opensandbox_status
  else
    log "k3d cluster not found: $CLUSTER"
  fi
  if [[ -f "$FORWARD_PID_FILE" ]]; then
    local pid
    pid="$(cat "$FORWARD_PID_FILE" 2>/dev/null || true)"
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      log "OpenSandbox port-forward running: pid=$pid"
    else
      log "OpenSandbox port-forward pid file exists but process is not running"
    fi
  else
    log "OpenSandbox port-forward is not running"
  fi
  docker system df
}

case "$ACTION" in
  up) up ;;
  down) down ;;
  reset) reset ;;
  status) status ;;
  -h|--help)
    cat <<TXT
Usage: scripts/run-stack-dev.sh <up|down|reset|status>

Common targets:
  make dev          # create/reuse k3d, install OpenSandbox K8s runtime, start cocola dev stack

Environment:
  COCOLA_K8S_CLUSTER              default: cocola-sandbox
  COCOLA_K8S_SANDBOX_IMAGE_REMOTE default: ghcr.io/sakurs2/cocola-sandbox-runtime:latest
  COCOLA_K8S_PREPULL_SANDBOX_IMAGE default: 1; pre-pull the remote image during make dev
  COCOLA_OPENSANDBOX_K8S_BATCHSANDBOX_TEMPLATE default: deploy/opensandbox-k8s/batchsandbox-template.yaml
  OPENSANDBOX_REPO                default: \$HOME/Desktop/github/opensandbox
TXT
    ;;
  *)
    err "unknown action: $ACTION"
    exit 2
    ;;
esac
