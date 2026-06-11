#!/usr/bin/env bash
# Thin launcher for the cocola in-sandbox stdio shim (Route A, ADR-0009).
#
# Kept as a stable entrypoint path so the control plane never hard-codes the
# Python module location. Forwards stdin/stdout/stderr verbatim and preserves
# the shim's exit code. All arguments pass through (e.g. --selfcheck).
set -euo pipefail
exec /opt/cocola/venv/bin/python /opt/cocola/shim/agent_shim.py "$@"
