# fix: restore reproducible image releases

- Change time: 2026-08-09 20:58 (+08:00)
- Related run: https://github.com/sakurs2/cocola/actions/runs/31308208080

## Reason

The `v0.1.16` release passed the regular Go and Web checks but failed while
building the Gateway and Web container images. The Gateway image builds its Go
module with `GOWORK=off`, while the upgraded OpenViking checksum existed only in
`go.work.sum`. The Web lockfile was generated with the root `.npmrc`, but the
Docker build did not copy that configuration or the new `ui-compat` workspace
package before running a frozen install.

The image matrix also assigned rolling aliases such as `latest` before every
image had succeeded. A partially failed release could therefore leave rolling
aliases pointing at different Cocola versions.

## Changes

- `apps/gateway/go.mod`, `apps/gateway/go.sum`: make the Gateway module tidy and
  independently buildable outside the Go workspace.
- `Makefile`, `.github/workflows/ci.yml`: tidy and build service modules with
  `GOWORK=off` so regular CI exercises the same module boundary as Docker.
- `apps/web/Dockerfile`: copy `.npmrc` and all Web workspace packages before the
  frozen install, and use the root `packageManager` field as the sole pnpm
  version source.
- `.github/workflows/release.yml`: publish the immutable release tag first and
  promote semver and `latest` aliases only after all versioned images pass.
- `scripts/tests/test_release_workflow.py`: cover standalone module checks,
  complete Web build inputs, and gated alias promotion.

The promotion step is idempotent. GHCR cannot atomically update aliases across
multiple packages, but build failures can no longer advance any rolling alias;
a transient promotion failure can be safely retried.
