#!/usr/bin/env bash
# Build, selfcheck, and publish the cocola sandbox runtime image to an OCI
# registry. Defaults target GHCR and publish latest/dev plus immutable sha-*
# tags; set VERSION_TAG=vX.Y.Z for a release tag.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CTX="$ROOT/deploy/sandbox-runtime"
PLATFORM="${PLATFORM:-linux/amd64}"
PLATFORMS="${PLATFORMS:-$PLATFORM}"
VERIFY_PLATFORM="${VERIFY_PLATFORM:-$PLATFORM}"
PUBLISH_DEV_TAG="${PUBLISH_DEV_TAG:-1}"
PUBLISH_LATEST_TAG="${PUBLISH_LATEST_TAG:-1}"
VERIFY="${VERIFY:-1}"

detect_owner() {
  if [ -n "${GITHUB_REPOSITORY_OWNER:-}" ]; then
    printf '%s' "$GITHUB_REPOSITORY_OWNER"
    return
  fi
  if command -v gh >/dev/null 2>&1; then
    gh repo view --json owner --jq '.owner.login' 2>/dev/null && return
  fi
  git -C "$ROOT" remote get-url origin 2>/dev/null \
    | sed -E 's#^git@github.com:([^/]+)/.*#\1#; s#^https://github.com/([^/]+)/.*#\1#'
}

REGISTRY="${REGISTRY:-ghcr.io}"
if [ -z "${IMAGE_REPO:-}" ]; then
  OWNER="${OWNER:-$(detect_owner)}"
  if [ -z "$OWNER" ]; then
    echo "cannot infer GitHub owner; set OWNER or IMAGE_REPO explicitly" >&2
    exit 1
  fi
  IMAGE_REPO="$REGISTRY/$OWNER/cocola-sandbox-runtime"
fi
GIT_SHA="${GIT_SHA:-$(git -C "$ROOT" rev-parse --short=12 HEAD)}"

tags=("$IMAGE_REPO:sha-$GIT_SHA")
if [ "$PUBLISH_LATEST_TAG" = "1" ]; then
  tags+=("$IMAGE_REPO:latest")
fi
if [ "$PUBLISH_DEV_TAG" = "1" ]; then
  tags+=("$IMAGE_REPO:dev")
fi
if [ -n "${VERSION_TAG:-}" ]; then
  tags+=("$IMAGE_REPO:$VERSION_TAG")
  if [[ "$VERSION_TAG" =~ ^v(.+) ]]; then
    tags+=("$IMAGE_REPO:${BASH_REMATCH[1]}")
  fi
  if [[ "$VERSION_TAG" =~ ^v?([0-9]+)\.([0-9]+)\.[0-9]+ ]]; then
    tags+=("$IMAGE_REPO:${BASH_REMATCH[1]}.${BASH_REMATCH[2]}")
  fi
fi

if [[ "$PLATFORMS" == *,* ]]; then
  if [ "$VERIFY" = "1" ]; then
    echo "building ${tags[0]} for selfcheck on $VERIFY_PLATFORM"
    docker build --platform "$VERIFY_PLATFORM" -t "${tags[0]}" "$CTX"
    echo "running sandbox runtime selfcheck with ${tags[0]}"
    IMAGE="${tags[0]}" SKIP_BUILD=1 SKIP_QUERY=1 "$ROOT/scripts/sandbox-runtime-verify.sh"
  fi

  tag_args=()
  for tag in "${tags[@]}"; do
    tag_args+=(--tag "$tag")
  done
  echo "building and pushing ${tags[0]} for $PLATFORMS"
  docker buildx build --platform "$PLATFORMS" --push "${tag_args[@]}" "$CTX"
else
  echo "building ${tags[0]} for $PLATFORMS"
  docker build --platform "$PLATFORMS" -t "${tags[0]}" "$CTX"

  for tag in "${tags[@]:1}"; do
    docker tag "${tags[0]}" "$tag"
  done

  if [ "$VERIFY" = "1" ]; then
    echo "running sandbox runtime selfcheck with ${tags[0]}"
    IMAGE="${tags[0]}" SKIP_BUILD=1 SKIP_QUERY=1 "$ROOT/scripts/sandbox-runtime-verify.sh"
  fi

  for tag in "${tags[@]}"; do
    echo "pushing $tag"
    docker push "$tag"
  done
fi

echo
echo "published tags:"
printf '  %s\n' "${tags[@]}"
