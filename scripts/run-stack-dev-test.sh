#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# ROOT is resolved at runtime, so ShellCheck cannot follow this source path.
# shellcheck disable=SC1091
source "$ROOT/scripts/run-stack-dev.sh" --help >/dev/null

assert_equal() {
  local expected="$1"
  local actual="$2"
  local label="$3"
  if [[ "$actual" != "$expected" ]]; then
    printf 'FAIL: %s\nexpected: %s\nactual:   %s\n' "$label" "$expected" "$actual" >&2
    return 1
  fi
}

assert_equal \
  "ghcr.io/sakurs2/cocola-sandbox-runtime" \
  "$(sandbox_image_repository "ghcr.io/sakurs2/cocola-sandbox-runtime:latest")" \
  "tagged repository"
assert_equal \
  "registry.local:5000/team/runtime" \
  "$(sandbox_image_repository "registry.local:5000/team/runtime:v1")" \
  "registry port"
assert_equal \
  "ghcr.io/sakurs2/cocola-sandbox-runtime" \
  "$(sandbox_image_repository "ghcr.io/sakurs2/cocola-sandbox-runtime@sha256:abc")" \
  "digest repository"

SANDBOX_IMAGE_REMOTE="ghcr.io/sakurs2/cocola-sandbox-runtime:latest"
MOCK_CURRENT_IMAGE_ID="sha256:current"
MOCK_STALE_IMAGE_IDS=$'sha256:old-one\nsha256:current\nsha256:old-two'
MOCK_USED_IMAGE_ID="sha256:old-two"
REMOVED_IMAGE_IDS=""

docker() {
  if [[ "${1:-}" == "exec" && "${3:-}" == "crictl" && "${4:-}" == "inspecti" ]]; then
    assert_equal "test-node" "${2:-}" "inspect node"
    assert_equal "-o" "${5:-}" "inspect output flag"
    assert_equal "go-template" "${6:-}" "inspect output format"
    assert_equal "--template" "${7:-}" "inspect template flag"
    assert_equal "{{.status.id}}" "${8:-}" "inspect template"
    assert_equal "$SANDBOX_IMAGE_REMOTE" "${9:-}" "inspect image"
    printf '%s\n' "$MOCK_CURRENT_IMAGE_ID"
    return 0
  fi
  if [[ "${1:-}" == "exec" && "${3:-}" == "crictl" && "${4:-}" == "images" ]]; then
    assert_equal "test-node" "${2:-}" "cleanup node"
    assert_equal "-q" "${5:-}" "quiet filter"
    assert_equal "-f" "${6:-}" "dangling filter flag"
    assert_equal "dangling=true" "${7:-}" "dangling filter"
    assert_equal "-f" "${8:-}" "reference filter flag"
    assert_equal \
      'reference=^ghcr\.io/sakurs2/cocola-sandbox-runtime$' \
      "${9:-}" \
      "exact repository filter"
    printf '%s\n' "$MOCK_STALE_IMAGE_IDS"
    return 0
  fi
  if [[ "${1:-}" == "exec" && "${3:-}" == "crictl" && "${4:-}" == "ps" ]]; then
    assert_equal "-a" "${5:-}" "all containers flag"
    assert_equal "-q" "${6:-}" "quiet containers flag"
    assert_equal "--image" "${7:-}" "container image flag"
    if [[ "${8:-}" == "$MOCK_USED_IMAGE_ID" ]]; then
      printf 'container-using-old-image\n'
    fi
    return 0
  fi
  if [[ "${1:-}" == "exec" && "${3:-}" == "crictl" && "${4:-}" == "rmi" ]]; then
    REMOVED_IMAGE_IDS+="${5:-}"$'\n'
    return 0
  fi
  printf 'unexpected docker call: %s\n' "$*" >&2
  return 1
}

cleanup_stale_sandbox_images "test-node"
assert_equal $'sha256:old-one\n' "$REMOVED_IMAGE_IDS" "safe stale image removal"

MOCK_STALE_IMAGE_IDS=""
REMOVED_IMAGE_IDS=""
cleanup_stale_sandbox_images "test-node"
assert_equal "" "$REMOVED_IMAGE_IDS" "no stale images"

TEST_FORWARD_PID_FILE="$(mktemp)"
trap 'rm -f "$TEST_FORWARD_PID_FILE"' EXIT
FORWARD_PID_FILE="$TEST_FORWARD_PID_FILE"
printf '987654321\n' >"$FORWARD_PID_FILE"
stop_forward 987654320
assert_equal "987654321" "$(cat "$FORWARD_PID_FILE")" "old supervisor preserves new forward"
stop_forward 987654321
if [[ -e "$FORWARD_PID_FILE" ]]; then
  printf 'FAIL: owned forward pid file was not removed\n' >&2
  exit 1
fi

printf 'run-stack-dev tests passed\n'
