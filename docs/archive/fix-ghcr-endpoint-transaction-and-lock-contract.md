# fix: make GHCR endpoint recovery and image locks authoritative

- Change time: 2026-08-11 02:18 (+08:00)

## Reason

Review of the unified GHCR endpoint implementation found three release-blocking edge cases. An interrupted endpoint switch could be resumed by a later plain `cocola start` even though the endpoint had never passed health checks; the third-party lock did not generate the target repository paths used by the CLI; and the synchronization workflow skipped remote and proxy validation for OpenViking because it is not copied into Cocola's namespace.

## Changes

- `apps/cli/internal/config` and `apps/cli/internal/command`: keep the last successfully started GHCR endpoint authoritative, store candidates only in `pending_upgrade`, commit them in `MarkStarted`, and restore the successful endpoint on a plain start without discarding a pending version upgrade.
- `scripts/generate_third_party_images.py` and generated Go constants: generate every target repository from `deploy/third-party-images.lock.json` and use those constants in image resolution.
- `.github/workflows/sync-third-party-images.yml`: validate digest, platforms, anonymous access, and proxy access for every locked runtime dependency while copying only entries marked for mirroring.
- Regression tests cover interrupted endpoint recovery, version-upgrade preservation, target repository drift, and non-mirrored dependency validation.
