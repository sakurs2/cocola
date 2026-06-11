# Multi-stage build for cocola admin-api (operator control plane).
#
# Build context MUST be the repo root: go.mod uses a relative replace
#   replace .../packages/go-common => ../../packages/go-common
# so packages/ must sit beside apps/ in the build context.
#
#   docker build -f apps/admin-api/Dockerfile -t cocola/admin-api:dev .

# ---- build stage ---------------------------------------------------------
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY apps/admin-api      ./apps/admin-api
COPY packages/go-common  ./packages/go-common
COPY db                  ./db
WORKDIR /src/apps/admin-api
# GOWORK=off: build this module standalone via its own go.mod + replace.
RUN GOWORK=off CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w" \
    -o /out/admin-api ./cmd/admin-api
# admin-mint is the offline token-minting helper; ship it too so operators can
# mint identity tokens with `docker compose exec admin-api admin-mint ...`.
RUN GOWORK=off CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w" \
    -o /out/admin-mint ./cmd/admin-mint

# ---- runtime stage -------------------------------------------------------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/admin-api  /usr/local/bin/admin-api
COPY --from=build /out/admin-mint /usr/local/bin/admin-mint
EXPOSE 8090
ENTRYPOINT ["/usr/local/bin/admin-api"]
