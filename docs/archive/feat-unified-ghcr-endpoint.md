# feat: unify container downloads behind a configurable GHCR endpoint

- Change time: 2026-08-11 01:44 (+08:00)

## Reason

Cocola cold starts depended directly on GHCR, Docker Hub, Codeberg, and an Alibaba OpenSandbox registry. Users in restricted network environments had to understand each registry, while earlier all-or-nothing mirror presets failed when a public proxy did not carry a Docker Hub namespace. The deployment needed one explicit GHCR endpoint without reintroducing automatic region detection or hidden fallback behavior.

## Changes

- `apps/cli/internal/config` and `apps/cli/internal/command`: add a validated, state-backed GHCR endpoint; resolve all non-Execd runtime images through it; and apply endpoint changes with the existing candidate deployment, health check, and rollback transaction.
- `apps/cli/internal/doctor`: report the selected endpoint and reject drift between state and resolved image references without performing remote registry requests.
- `deploy/third-party-images.lock.json` and generated Go constants: establish one digest-, platform-, source-, and license-pinned dependency inventory.
- `.github/workflows/sync-third-party-images.yml` and `scripts/`: add a manually triggered, immutable multi-platform image copy flow plus an independently versioned source and license archive. Ordinary Cocola releases do not run this workflow.
- `README.md`, `docs/cli.md`, and `THIRD_PARTY_NOTICES.md`: document direct defaults, optional China proxy use, custom Registry precedence, failure semantics, and redistribution notices.
- Key tradeoff: maintaining Cocola-controlled GHCR copies adds a low-frequency package publishing workflow, but removes Docker Hub and Codeberg from runtime startup without adding a service, daemon setting, geographic detection, or automatic trust-boundary fallback.
