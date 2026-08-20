#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="$ROOT/apps/cli/internal/compose/testdata/compatibility.yaml"
PROJECT_NAME="${COCOLA_COMPAT_PROJECT:-cocola-docker-compatibility}"

cleanup() {
  docker compose --project-name "$PROJECT_NAME" --file "$COMPOSE_FILE" \
    down --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

engine_version="$(docker version --format '{{.Server.Version}}')"
compose_version="$(docker compose version --short)"
engine_version="${engine_version#v}"
compose_version="${compose_version#v}"

if [[ -n "${COCOLA_EXPECT_DOCKER_VERSION:-}" && "$engine_version" != "$COCOLA_EXPECT_DOCKER_VERSION" ]]; then
  printf 'Docker Engine version mismatch: expected %s, got %s\n' \
    "$COCOLA_EXPECT_DOCKER_VERSION" "$engine_version" >&2
  exit 1
fi
if [[ -n "${COCOLA_EXPECT_COMPOSE_VERSION:-}" && "$compose_version" != "$COCOLA_EXPECT_COMPOSE_VERSION" ]]; then
  printf 'Docker Compose version mismatch: expected %s, got %s\n' \
    "$COCOLA_EXPECT_COMPOSE_VERSION" "$compose_version" >&2
  exit 1
fi

docker compose --project-name "$PROJECT_NAME" --file "$COMPOSE_FILE" config --quiet
docker compose --project-name "$PROJECT_NAME" --file "$COMPOSE_FILE" \
  up --detach --wait --wait-timeout 60

container_id="$(
  docker compose --project-name "$PROJECT_NAME" --file "$COMPOSE_FILE" \
    ps --quiet probe
)"
if [[ -z "$container_id" ]]; then
  printf 'Compatibility probe container was not created\n' >&2
  exit 1
fi
health="$(docker inspect --format '{{.State.Health.Status}}' "$container_id")"
if [[ "$health" != "healthy" ]]; then
  printf 'Compatibility probe is %s, expected healthy\n' "$health" >&2
  exit 1
fi

printf 'Docker compatibility passed: Engine %s, Compose %s\n' \
  "$engine_version" "$compose_version"
