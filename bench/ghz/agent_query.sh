#!/usr/bin/env bash
# ghz load test: agent-runtime AgentRuntimeService/Query (server-streaming gRPC).
#
# ghz is the gRPC analogue of hey/wrk. Query is server-streaming, so ghz keeps
# the stream open until the server closes it (terminal done/error event),
# measuring full server-stream latency per call. We point ghz at the .proto
# directly (reflection is not required) so it works against any build.
#
# Prereqs:
#   - ghz installed (https://ghz.sh): brew install ghz, or download a release.
#   - agent-runtime reachable. Via docker-compose.full.yml it listens on :50061
#     INSIDE the compose network; publish it or run ghz from a sidecar. For a
#     local uv-run instance use COCOLA_AGENT_HOST/COCOLA_AGENT_PORT (default :50061).
#
# Usage:
#   bench/ghz/agent_query.sh [HOST:PORT]
#
# Knobs (env):
#   TARGET     gRPC host:port           (default localhost:50061, or $1)
#   CONC       concurrent streams       (default 20)
#   TOTAL      total requests           (default 2000)
#   DURATION   run for a duration       (overrides TOTAL when set, e.g. 30s)
#   PROMPT     prompt text              (default ping)
#   INSECURE   plaintext (no TLS)       (default 1)
#
# CI smoke:
#   CONC=2 TOTAL=20 bench/ghz/agent_query.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PROTO_DIR="${ROOT}/packages/proto"
PROTO="cocola/agent/v1/agent.proto"

TARGET="${1:-${TARGET:-localhost:50061}}"
CONC="${CONC:-20}"
TOTAL="${TOTAL:-2000}"
PROMPT="${PROMPT:-ping}"
INSECURE="${INSECURE:-1}"
DURATION="${DURATION:-}"

if ! command -v ghz >/dev/null 2>&1; then
  echo "ghz not found. Install from https://ghz.sh (brew install ghz)." >&2
  exit 127
fi

ARGS=(
  --proto "${PROTO}"
  --import-paths "${PROTO_DIR}"
  --call "cocola.agent.v1.AgentRuntimeService.Query"
  -d "{\"prompt\":\"${PROMPT}\",\"session_id\":\"ghz-{{.RequestNumber}}\",\"max_turns\":1}"
  -c "${CONC}"
)

# Duration mode takes precedence over a fixed request count.
if [[ -n "${DURATION}" ]]; then
  ARGS+=( -z "${DURATION}" )
else
  ARGS+=( -n "${TOTAL}" )
fi

[[ "${INSECURE}" == "1" ]] && ARGS+=( --insecure )

echo "ghz -> ${TARGET}  (c=${CONC}, $([[ -n "${DURATION}" ]] && echo "z=${DURATION}" || echo "n=${TOTAL}"))"
exec ghz "${ARGS[@]}" "${TARGET}"
