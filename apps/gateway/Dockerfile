# Multi-stage build for cocola gateway (public-facing BFF).
#
# Build context MUST be the repo root: go.mod replaces both go-common and the
# generated proto module with relative paths under packages/.
#
#   docker build -f apps/gateway/Dockerfile -t cocola/gateway:dev .

# ---- build stage ---------------------------------------------------------
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY apps/gateway        ./apps/gateway
COPY packages/go-common  ./packages/go-common
COPY packages/proto      ./packages/proto
WORKDIR /src/apps/gateway
RUN GOWORK=off CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w" \
    -o /out/gateway ./cmd/gateway

# ---- runtime stage -------------------------------------------------------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/gateway /usr/local/bin/gateway
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/gateway"]
