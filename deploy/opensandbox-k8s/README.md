# OpenSandbox Kubernetes Runtime POC

This directory contains the local k3d/k3s wiring for running cocola against
OpenSandbox's Kubernetes runtime.

## Quick Start

For local development, prefer the one-command profile:

```bash
make up-k8s
```

It creates or reuses a single-node k3d cluster, creates a local registry,
installs OpenSandbox's Kubernetes runtime, starts a local port-forward, and
then starts cocola through the normal native dev stack. Sandbox pods pull
`ghcr.io/sakurs2/cocola-sandbox-runtime:latest` by default, and `make up-k8s`
pre-pulls that image into the k3d node before accepting chat traffic.

Useful follow-ups:

```bash
make status-k8s   # k3d / OpenSandbox / Docker resource status
make down-k8s     # stop OpenSandbox K8s runtime and port-forward, keep cluster
make reset-k8s    # delete the local k3d cluster and reclaim disk
```

`make up` remains the default fast local path. Use `make up-k8s` only when you
want to validate Kubernetes sandbox placement or the sandbox node admin UI.

## Prerequisites

- A working Kubernetes context, typically a local k3d cluster.
- `docker`, `k3d`, `kubectl` and `helm`.
- OpenSandbox cloned locally. The default path is:
  `/Users/bytedance/Desktop/github/opensandbox`

The simplified profile pulls the official cocola sandbox runtime image from
GHCR by default. This avoids rebuilding or pushing the large sandbox image on
every local Kubernetes run. The image is pre-pulled during startup so the first
chat does not pay the full cold-pull cost inside `POST /sandboxes`.

For image development, you can still opt into the old local-registry flow:

```bash
COCOLA_K8S_PUSH_SANDBOX_IMAGE=1 make up-k8s
```

That mode uses two addresses:

- Host push address: `localhost:5001/cocola/sandbox-runtime:dev`
- Kubernetes pull address: `cocola-registry.localhost:5000/cocola/sandbox-runtime:dev`

Do not pass the host `localhost:5001` address to OpenSandbox sandbox pods. From
inside a k3d node, `localhost` is the node container itself, so pod image pulls
will fail with `ImagePullBackOff`.

The local values intentionally run without an OpenSandbox API key and set
`OPENSANDBOX_INSECURE_SERVER=YES`; do not use this values file as-is for a
shared environment. `make up-k8s` injects the Kubernetes-specific OpenSandbox
URL, node selector, timeout, and sandbox image variables automatically.

## Sandbox pod template

`make up-k8s` creates a `cocola-batchsandbox-template` ConfigMap from
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
By default it uses the same GHCR image as `make up-k8s`:
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
make down-k8s
```

This keeps the local k3d cluster so the next `make up-k8s` is faster. To delete
the cluster and reclaim disk:

```bash
make reset-k8s
```
