# fix: give Linux Sandboxes a dedicated path to authenticated Cocola services

- Change time: 2026-08-11 14:11 (+08:00)

## Reason

On a native Linux Docker host, Agent turns reached a running Sandbox but failed before the first model call with `API Error: Unable to connect to API (ENOTFOUND)`. Managed deployments injected `ANTHROPIC_BASE_URL=http://host.docker.internal:<host-port>` into dynamically created OpenSandbox containers. Docker Desktop provides that hostname automatically on macOS, while native Linux containers require an explicit host mapping; OpenSandbox v0.1.14 does not expose an `extra_hosts` lifecycle or server configuration field. The same hidden dependency affected the Project and Skill brokers and Local Project Forgejo clones.

## Changes

- `apps/cli/internal/assets/compose.yaml`: create a dedicated `cocola-sandbox-services` user-defined bridge, attach only Gateway, LLM Gateway, and Forgejo with explicit aliases, and configure managed OpenSandbox containers to use that officially supported custom network.
- `apps/cli/internal/config/config.go`: make managed installations inject Docker-internal service URLs for model calls, brokers, and Local Project clones; externally managed OpenSandbox behavior remains unchanged.
- `apps/cli/internal/config/upgrade.go`: migrate only the legacy Cocola-generated `host.docker.internal` defaults and preserve operator-owned endpoints and inline comments.
- `apps/cli/internal/assets/assets_test.go` and `apps/cli/internal/config/config_test.go`: cover network membership, managed defaults, legacy migration, and custom endpoint preservation.
- Tradeoff: one additional Docker bridge is retained as part of the managed deployment topology. This avoids hard-coded bridge IPs, Docker daemon changes, runtime `/etc/hosts` mutation, host-port hairpinning, and exposing the broader Compose control-plane network to untrusted Sandboxes.
