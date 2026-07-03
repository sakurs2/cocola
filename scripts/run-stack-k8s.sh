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

ACTION="${1:-up}"

CLUSTER="${COCOLA_K8S_CLUSTER:-cocola-sandbox}"
REGISTRY_HOST="${COCOLA_K8S_REGISTRY_HOST:-cocola-registry.localhost}"
REGISTRY_PORT="${COCOLA_K8S_REGISTRY_PORT:-5001}"
SANDBOX_IMAGE_LOCAL="${COCOLA_K8S_SANDBOX_IMAGE_LOCAL:-cocola/sandbox-runtime:dev}"
SANDBOX_IMAGE_PUSH="${COCOLA_K8S_SANDBOX_IMAGE_PUSH:-localhost:${REGISTRY_PORT}/cocola/sandbox-runtime:dev}"
SANDBOX_IMAGE_REMOTE="${COCOLA_K8S_SANDBOX_IMAGE_REMOTE:-${REGISTRY_HOST}:5000/cocola/sandbox-runtime:dev}"
SERVER_PORT="${COCOLA_OPENSANDBOX_HOST_PORT:-8090}"
RELEASE="${COCOLA_OPENSANDBOX_K8S_RELEASE:-opensandbox}"
SYSTEM_NAMESPACE="${COCOLA_OPENSANDBOX_K8S_SYSTEM_NAMESPACE:-opensandbox-system}"
SANDBOX_NAMESPACE="${COCOLA_OPENSANDBOX_K8S_NAMESPACE:-opensandbox}"
OPENSANDBOX_REPO="${OPENSANDBOX_REPO:-$HOME/Desktop/github/opensandbox}"
CHART_DIR="${COCOLA_OPENSANDBOX_K8S_CHART:-$OPENSANDBOX_REPO/kubernetes/charts/opensandbox}"
VALUES_FILE="${COCOLA_OPENSANDBOX_K8S_VALUES:-$ROOT/deploy/opensandbox-k8s/values.local.yaml}"
PVC_FILE="${COCOLA_OPENSANDBOX_K8S_PVC:-$ROOT/deploy/opensandbox-k8s/cocola-plugins-pvc.yaml}"
SERVER_SERVICE="${COCOLA_OPENSANDBOX_K8S_SERVER_SERVICE:-opensandbox-server}"
LOG_DIR="$ROOT/.run-logs"
FORWARD_PID_FILE="$LOG_DIR/opensandbox-k8s-forward.pid"
STACK_PID_FILE="$LOG_DIR/up-k8s-stack.pid"

log() { printf '\033[1;36m[up-k8s]\033[0m %s\n' "$*"; }
err() { printf '\033[1;31m[up-k8s:err]\033[0m %s\n' "$*" >&2; }

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
      --agents 0 \
      --registry-create "${REGISTRY_HOST}:0.0.0.0:${REGISTRY_PORT}"
  fi
  k3d kubeconfig merge "$CLUSTER" --kubeconfig-switch-context >/dev/null
}

push_sandbox_image() {
  if ! docker image inspect "$SANDBOX_IMAGE_LOCAL" >/dev/null 2>&1; then
    err "missing local image: $SANDBOX_IMAGE_LOCAL"
    err "build the sandbox runtime image first, then rerun: make up-k8s"
    return 1
  fi
  log "pushing sandbox image to local k3d registry: $SANDBOX_IMAGE_PUSH"
  docker tag "$SANDBOX_IMAGE_LOCAL" "$SANDBOX_IMAGE_PUSH"
  if ! docker push "$SANDBOX_IMAGE_PUSH"; then
    err "failed to push $SANDBOX_IMAGE_PUSH"
    err "if the cluster was created before the local registry existed, run: make reset-k8s"
    return 1
  fi
  log "Kubernetes sandbox pods will pull image as: $SANDBOX_IMAGE_REMOTE"
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

  log "creating cocola-plugins PVC"
  kubectl -n "$SANDBOX_NAMESPACE" apply -f "$PVC_FILE"

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
      log "stopping OpenSandbox port-forward (pid=$pid)"
      kill "$pid" 2>/dev/null || true
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
      log "stopping cocola dev stack (pid=$pid)"
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
    rm -f "$STACK_PID_FILE"
  fi
}

start_forward() {
  mkdir -p "$LOG_DIR"
  stop_forward
  log "starting OpenSandbox port-forward on 127.0.0.1:$SERVER_PORT"
  (
    kubectl -n "$SYSTEM_NAMESPACE" port-forward "svc/$SERVER_SERVICE" "$SERVER_PORT:80"
  ) >"$LOG_DIR/opensandbox-k8s-forward.log" 2>&1 &
  echo "$!" >"$FORWARD_PID_FILE"

  local i
  for ((i=1; i<=90; i++)); do
    if curl -fsS -m 2 "http://127.0.0.1:$SERVER_PORT/health" >/dev/null 2>&1; then
      log "OpenSandbox server is reachable at http://127.0.0.1:$SERVER_PORT/v1"
      return 0
    fi
    sleep 1
  done
  err "OpenSandbox port-forward did not become healthy; see .run-logs/opensandbox-k8s-forward.log"
  return 1
}

up() {
  ensure_cluster
  push_sandbox_image
  install_opensandbox
  start_forward

  log "starting cocola dev stack with Kubernetes OpenSandbox runtime"
  trap 'stop_stack; stop_forward; exit 130' INT TERM
  trap 'rm -f "$STACK_PID_FILE"; stop_forward' EXIT
  (
    COCOLA_SANDBOX_PROVIDER=opensandbox \
      COCOLA_OPENSANDBOX_MANAGED=0 \
      COCOLA_OPENSANDBOX_URL="http://127.0.0.1:${SERVER_PORT}/v1" \
      COCOLA_OPENSANDBOX_DIRECT_EXEC=0 \
      COCOLA_OPENSANDBOX_HTTP_TIMEOUT="${COCOLA_OPENSANDBOX_HTTP_TIMEOUT:-90s}" \
      COCOLA_SANDBOX_NODE_NAMESPACE="${COCOLA_SANDBOX_NODE_NAMESPACE:-opensandbox}" \
      COCOLA_SANDBOX_NODE_POD_SELECTOR="${COCOLA_SANDBOX_NODE_POD_SELECTOR:-opensandbox.io/id}" \
      COCOLA_SANDBOX_IMAGE="$SANDBOX_IMAGE_REMOTE" \
        bash scripts/run-stack.sh
  ) &
  local stack_pid="$!"
  echo "$stack_pid" >"$STACK_PID_FILE"
  wait "$stack_pid"
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
Usage: scripts/run-stack-k8s.sh <up|down|reset|status>

Common targets:
  make up-k8s       # create/reuse k3d, install OpenSandbox K8s runtime, start cocola
  make down-k8s     # stop port-forward + OpenSandbox K8s runtime, keep k3d cluster
  make reset-k8s    # delete the local k3d cluster and reclaim test disk
  make status-k8s   # show k3d/OpenSandbox/Docker status

Environment:
  COCOLA_K8S_CLUSTER              default: cocola-sandbox
  COCOLA_K8S_REGISTRY_HOST        default: cocola-registry.localhost
  COCOLA_K8S_REGISTRY_PORT        default: 5001
  COCOLA_K8S_SANDBOX_IMAGE_LOCAL  default: cocola/sandbox-runtime:dev
  COCOLA_K8S_SANDBOX_IMAGE_PUSH   default: localhost:5001/cocola/sandbox-runtime:dev
  COCOLA_K8S_SANDBOX_IMAGE_REMOTE default: cocola-registry.localhost:5000/cocola/sandbox-runtime:dev
  OPENSANDBOX_REPO                default: \$HOME/Desktop/github/opensandbox
TXT
    ;;
  *)
    err "unknown action: $ACTION"
    exit 2
    ;;
esac
