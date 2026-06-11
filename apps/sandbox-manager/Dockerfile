# Multi-stage build for cocola sandbox-manager (the control plane).
#
# Why this exists: deployers must NOT need Go on the host. The go>=1.25
# requirement (transitive: go.opentelemetry.io/otel/trace@v1.44.0) is locked
# inside the builder image here, so `docker compose up` is the only dependency.
#
# Build context MUST be the repo root: go.mod uses relative replace directives
#   replace .../packages/go-common      => ../../packages/go-common
#   replace .../packages/proto/gen/go   => ../../packages/proto/gen/go
# so the container needs apps/sandbox-manager AND packages/ in the same layout.
#
#   docker build -f apps/sandbox-manager/Dockerfile -t cocola/sandbox-manager:dev .

# ---- build stage ---------------------------------------------------------
FROM golang:1.25-alpine AS build

WORKDIR /src

# Copy only what the module graph needs, preserving the relative layout the
# replace directives expect.
COPY apps/sandbox-manager ./apps/sandbox-manager
COPY packages/go-common   ./packages/go-common
COPY packages/proto       ./packages/proto

WORKDIR /src/apps/sandbox-manager

# GOWORK=off: the repo go.work does not list sandbox-manager; build it as a
# standalone module driven purely by its own go.mod + replace directives.
# CGO disabled -> a static binary that runs on a bare alpine runtime image.
RUN GOWORK=off CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w" \
    -o /out/sandbox-manager ./cmd/sandbox-manager

# Build the debug CLI into the same image so operators can drive the full
# create/exec/destroy loop with `docker compose exec sandbox-manager sandbox-cli`
# WITHOUT installing Go on the host (the whole point of containerising).
RUN GOWORK=off CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w" \
    -o /out/sandbox-cli ./cmd/sandbox-cli

# ---- runtime stage -------------------------------------------------------
FROM alpine:3.20

# ca-certificates for any TLS egress; the binary itself is static.
RUN apk add --no-cache ca-certificates

COPY --from=build /out/sandbox-manager /usr/local/bin/sandbox-manager
COPY --from=build /out/sandbox-cli     /usr/local/bin/sandbox-cli

# Default gRPC port (overridable via COCOLA_SANDBOX_ADDR).
EXPOSE 50051

ENTRYPOINT ["/usr/local/bin/sandbox-manager"]
