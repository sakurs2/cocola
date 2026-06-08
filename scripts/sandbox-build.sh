#!/usr/bin/env bash
# Reproducible build for the sandbox-manager stack.
#
# Why containerized: on the corporate-managed macOS host, the system Go
# toolchain hits two walls -- (1) the TLS intercept returns OSStatus -26276 for
# the native cert verifier, and (2) macOS App Management forbids writing the
# ".gitmodules" files some module zips contain. Building inside a Linux golang
# image sidesteps both: it fetches modules over the plain-HTTP byted proxy and
# writes the cache to a Docker volume outside the TCC-protected paths.
#
# Output: native darwin/arm64 binaries in ./bin so they talk to the host's
# Docker daemon directly during local e2e.
#
# Usage:
#   scripts/sandbox-build.sh             # build sandbox-manager + sandbox-cli
#   GOOS=linux GOARCH=amd64 scripts/...  # cross-compile for deployment
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_IMAGE="${GO_IMAGE:-golang:1.25}"
GOPROXY="${COCOLA_GOPROXY:-http://goproxy.byted.org}"
GOOS="${GOOS:-darwin}"
GOARCH="${GOARCH:-arm64}"
MODCACHE_VOL="${COCOLA_GOMOD_VOL:-cocola-gomod25}"

mkdir -p "$ROOT/bin"

docker run --rm \
  -v "$ROOT":/src \
  -v "$MODCACHE_VOL":/go/pkg/mod \
  -w /src/apps/sandbox-manager \
  -e GOPROXY="$GOPROXY" \
  -e GOSUMDB=off \
  -e GOWORK=off \
  -e GOTOOLCHAIN=local \
  -e CGO_ENABLED=0 \
  -e GOOS="$GOOS" \
  -e GOARCH="$GOARCH" \
  "$GO_IMAGE" \
  sh -c 'set -e; go build -o /src/bin/sandbox-manager ./cmd/sandbox-manager; go build -o /src/bin/sandbox-cli ./cmd/sandbox-cli'

echo "built: bin/sandbox-manager bin/sandbox-cli ($GOOS/$GOARCH)"
