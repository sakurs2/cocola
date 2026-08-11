# feat: expose the Sandbox idle timeout and consistently show the China GHCR hint

- Change time: 2026-08-11 12:01 (+08:00)

## Reason

Sandbox reclaim used a hard-coded 10-minute lease and was not actually configurable from Admin Settings. The short default could reclaim a workspace sooner than users expected, while adding only a UI field would have produced a setting that did not affect Sandbox Manager. Separately, first-time installation already showed the optional Mainland China GHCR proxy command, but repeated `cocola install` runs used for upgrades and up-to-date checks did not, so `curl | sh` and direct CLI installation produced inconsistent guidance.

## Changes

- `apps/admin-api/internal/service/settings.go`: add an editable Sandbox idle timeout in minutes with a 30-minute default, bounded validation, and clear guidance that active leases adopt changes within about one minute.
- `apps/sandbox-manager/internal/orchestrator`: change the fallback lease to 30 minutes, poll the Admin-owned PostgreSQL setting once every 30 seconds through a single lazy database connection, and serve heartbeats from an atomic cached duration rather than querying PostgreSQL per Sandbox.
- `apps/sandbox-manager/cmd/sandbox-manager/main.go`: wire the runtime setting reader with bounded refreshes, last-valid-value degradation, logging, and shutdown cleanup.
- `apps/cli/internal/assets/compose.yaml`: make the 30-minute, minutes-based environment fallback explicit and consistent between Admin API and Sandbox Manager for managed deployments.
- `apps/cli/internal/command/install.go`: reuse one Mainland China GHCR hint at the end of first install, prepared upgrade, and already-current output; JSON output remains unchanged.
- Tests cover the Admin default, timeout validation, Binder runtime source, Compose fallback, and initial/upgrade/up-to-date installation summaries.
- Tradeoff: runtime configurability adds one indexed PostgreSQL lookup every 30 seconds per Sandbox Manager replica, but avoids a new service, queue, per-heartbeat query, or restart-only setting.
