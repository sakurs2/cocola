#!/usr/bin/env bash
# Docker Compose compatibility wrapper.
#
# Some local Docker installs provide the v2 plugin as `docker compose`; others
# still expose the standalone `docker-compose` binary. Keep Makefile/script
# targets portable by routing through this tiny shim.
set -euo pipefail

if docker compose version >/dev/null 2>&1; then
  exec docker compose "$@"
fi

if command -v docker-compose >/dev/null 2>&1; then
  exec docker-compose "$@"
fi

echo "docker compose is unavailable; install Docker Compose v2 plugin or docker-compose v1." >&2
exit 127
