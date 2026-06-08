#!/usr/bin/env bash
# Generate Python gRPC stubs for the proto package.
#
# Containerized for the same reason as the Go build: the host pip hits the
# corporate TLS bug (OSStatus -26276). Inside the container we install
# grpcio-tools from the plain-HTTP byted PyPI mirror, then emit stubs +
# .pyi type stubs into packages/proto/gen/python, added to PYTHONPATH by
# consumers.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PY_IMAGE="${PY_IMAGE:-python:3.11-slim}"
PIP_INDEX="${COCOLA_PIP_INDEX:-http://mirrors.byted.org/pypi/simple/}"
PIP_HOST="${COCOLA_PIP_HOST:-mirrors.byted.org}"
OUT="packages/proto/gen/python"

mkdir -p "$ROOT/$OUT"

docker run --rm \
  -v "$ROOT":/src -w /src/packages/proto \
  "$PY_IMAGE" \
  sh -c "
    set -e
    pip install --no-cache-dir -i $PIP_INDEX --trusted-host $PIP_HOST 'grpcio-tools>=1.64'
    python -m grpc_tools.protoc -I . \
      --python_out=gen/python --grpc_python_out=gen/python --pyi_out=gen/python \
      cocola/sandbox/v1/sandbox.proto cocola/agent/v1/*.proto cocola/common/v1/*.proto
    find gen/python/cocola -type d -exec touch {}/__init__.py ';'
  "

echo "generated: $OUT"
