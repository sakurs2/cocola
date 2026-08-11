# fix: route Linux Compose sandbox traffic through the Docker host endpoint

- Change time: 2026-08-11 11:50 (+08:00)

## Reason

On a standard Linux Docker host, a newly created Project conversation failed before any model call with `WIKI_PROVISION_FAILED`. OpenSandbox successfully created the sandbox and started Execd, but the readiness probe timed out after 30 seconds because OpenSandbox Server ran on the Compose network while dynamically created sandboxes ran on Docker's built-in bridge. Linux isolated those networks, so the server proxy could not reach the sandbox's private IP. The same topology happened to work under the macOS development container runtime and therefore escaped the existing real-server verification.

## Changes

- `apps/cli/internal/assets/compose.yaml`: make managed Compose deployments request OpenSandbox's host-mapped endpoint and let Gateway resolve `host.docker.internal`, preserving Sandbox isolation from Cocola's internal service network while restoring Exec, Preview, and Terminal connectivity.
- `apps/cli/internal/assets/assets_test.go`: add a regression test for the managed Compose endpoint mode and required host-gateway mappings.
- `apps/sandbox-manager/internal/provider/opensandbox/opensandbox.go`: document the actual reachability boundary of server-proxied and Docker host-mapped endpoints.
- `apps/sandbox-manager/internal/provider/opensandbox/opensandbox_test.go`: verify direct endpoint resolution omits the server-proxy request and preserves the runtime-provided host-mapped URL.
- Tradeoff: the fix reuses ports OpenSandbox already publishes for runtime endpoints instead of joining untrusted sandbox containers to Cocola's internal Compose network.
