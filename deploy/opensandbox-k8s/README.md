# OpenSandbox Kubernetes Runtime

This directory contains the local k3d/k3s wiring for running cocola against
OpenSandbox's Kubernetes runtime.

## Quick Start

For local development, prefer the one-command profile:

```bash
make dev
```

It creates or reuses a single-node k3d cluster, creates a local registry,
installs OpenSandbox's Kubernetes runtime, starts a local port-forward, and
then starts cocola through the normal native dev stack. Sandbox pods pull
`ghcr.io/sakurs2/cocola-sandbox-runtime:latest` by default, and `make dev`
pre-pulls that image into the k3d node before accepting chat traffic.

Advanced maintenance actions are still available directly through
`scripts/run-stack-dev.sh <down|reset|status>`, but they are intentionally not
top-level Make targets.

## Prerequisites

- A working Kubernetes context, typically a local k3d cluster.
- `docker`, `k3d`, `kubectl` and `helm`.
- OpenSandbox cloned locally. The default path is:
  `/Users/bytedance/Desktop/github/opensandbox`

The simplified profile pulls the official cocola sandbox runtime image from
GHCR by default. This avoids rebuilding or pushing the large sandbox image on
every local Kubernetes run. The image is pre-pulled during startup so the first
chat does not pay the full cold-pull cost inside `POST /sandboxes`.

The local values intentionally run without an OpenSandbox API key and set
`OPENSANDBOX_INSECURE_SERVER=YES`; do not use this values file as-is for a
shared environment. `make dev` injects the Kubernetes-specific OpenSandbox URL,
node selector, lifecycle HTTP timeout, and sandbox image variables automatically.
Single-command Exec is capped by `COCOLA_OPENSANDBOX_EXEC_TIMEOUT` (default
`5m`); increase it for deliberately long browser automation, but prefer bounded
Playwright navigation timeouts for screenshots.

## Sandbox pod template

`make dev` creates a `cocola-batchsandbox-template` ConfigMap from
`deploy/opensandbox-k8s/batchsandbox-template.yaml` and mounts it into the
OpenSandbox server. The template is merged into every new sandbox pod. Cocola
uses it to mount `/dev/shm` as a 256Mi memory-backed `emptyDir`, which gives
Chromium and Playwright enough shared memory for headless screenshots. The
memory is not preallocated, but actual writes count toward node memory pressure.

## Verify provider lifecycle

```bash
make verify-opensandbox-k8s
```

This verifies create, health, streaming exec, file upload/download and destroy.
By default it uses the same GHCR image as `make dev`:
`ghcr.io/sakurs2/cocola-sandbox-runtime:latest`. It intentionally skips
pause/resume because OpenSandbox Kubernetes snapshots require registry
configuration.

To verify PVC persistence:

```bash
make verify-opensandbox-k8s ARGS="-persist"
```

## Node UI checks

Open `/admin/sandbox-nodes` in the web UI. The sandbox count is based on
OpenSandbox's `opensandbox.io/id` pod label and should reflect pods in the
`opensandbox` namespace.

## Stop

```bash
scripts/run-stack-dev.sh down
```

This keeps the local k3d cluster so the next `make dev` is faster. To delete the
cluster and reclaim disk:

```bash
scripts/run-stack-dev.sh reset
```
