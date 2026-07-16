FROM golang:1.25-alpine AS build

WORKDIR /src
COPY apps/admin-api ./apps/admin-api
COPY packages/go-common ./packages/go-common
COPY db ./db
WORKDIR /src/apps/admin-api
RUN GOWORK=off CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w" \
    -o /out/storage-probe ./cmd/storage-probe

FROM scratch
COPY --from=build /out/storage-probe /storage-probe
EXPOSE 8095
ENTRYPOINT ["/storage-probe"]
