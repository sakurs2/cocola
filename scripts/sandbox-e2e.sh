#!/usr/bin/env bash
# M1 end-to-end smoke test.
#
# Boots sandbox-manager (native binary), then runs BOTH the Go CLI demo and the
# Python agent-runtime demo against it through the real Docker daemon. Tears the
# server down on exit. Returns non-zero if either demo fails.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ADDR="${COCOLA_SANDBOX_ADDR:-:50051}"
PY_IMAGE="${PY_IMAGE:-python:3.11-slim}"
PIP_INDEX="${COCOLA_PIP_INDEX:-http://mirrors.byted.org/pypi/simple/}"
PIP_HOST="${COCOLA_PIP_HOST:-mirrors.byted.org}"

cd "$ROOT"
[ -x bin/sandbox-manager ] || scripts/sandbox-build.sh

echo "== starting sandbox-manager on $ADDR =="
COCOLA_SANDBOX_ADDR="$ADDR" COCOLA_SANDBOX_PROVIDER=docker ./bin/sandbox-manager &
SRV_PID=$!
trap 'kill $SRV_PID 2>/dev/null || true' EXIT
sleep 1

echo "== Go CLI demo =="
./bin/sandbox-cli -addr "$ADDR" demo

echo "== Python agent-runtime demo =="
docker run --rm -v "$ROOT":/src -w /src \
  --add-host host.docker.internal:host-gateway \
  -e PYTHONPATH="/src/packages/proto/gen/python:/src/apps/agent-runtime" \
  "$PY_IMAGE" \
  sh -c "
    set -e
    pip install --no-cache-dir -i $PIP_INDEX --trusted-host $PIP_HOST 'grpcio>=1.64' protobuf >/dev/null
    python -m cocola_agent_runtime.sandbox_demo --addr host.docker.internal${ADDR}
  "

echo "M1 E2E OK"
