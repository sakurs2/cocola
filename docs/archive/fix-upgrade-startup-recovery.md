# fix: make upgrade startup and rollback deterministic

- Change time: 2026-08-10 11:17 (+08:00)

## Reason

Upgrading to v0.1.17 could fail after a development Forgejo container retained
`127.0.0.1:3001`. The CLI checked `0.0.0.0` instead of Forgejo's real loopback
binding, which did not detect the collision on OrbStack. A failed candidate
deployment then restored the old configuration without removing candidate
containers, and a later retry mistook the candidate Forgejo volume for data
owned by the previous release. OpenViking also inherited its image healthcheck
while intentionally waiting for an Embedding route, so the default disabled
Memory configuration blocked the whole Compose startup.

## Changes

- `apps/cli/internal/command/preflight.go`: check every published port at its
  actual bind address, including the Internal SCM loopback binding.
- `apps/cli/internal/compose/runner.go`: detect services from the installed
  Compose topology and add a failed-start cleanup path that drains services and
  removes candidate containers and networks without deleting named volumes.
- `apps/cli/internal/command/lifecycle.go`: ignore stray Forgejo volumes when
  the previous topology has no Forgejo service, and clean up a failed candidate
  topology before rolling back its configuration.
- `apps/cli/internal/assets/compose.yaml`: explicitly disable OpenViking's
  inherited healthcheck while Memory is waiting for its administrator-selected
  Embedding route, so disabled Memory does not block Cocola startup.
- CLI and Compose tests cover exact loopback binding, topology-aware backups,
  volume-preserving failed-start cleanup, and the disabled Memory healthcheck.

The cleanup deliberately preserves named volumes and images. Pull failures do
not invoke it because the previous running deployment may still be healthy;
cleanup is limited to failures after candidate startup begins.
