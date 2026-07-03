# OpenSandbox Kubernetes Runtime POC

This directory contains the local k3d/k3s wiring for running cocola against
OpenSandbox's Kubernetes runtime.

## Quick Start

For local development, prefer the one-command profile:

```bash
make up-k8s
```

It creates or reuses a single-node k3d cluster, creates a local registry,
pushes `cocola/sandbox-runtime:dev` into that registry, installs OpenSandbox's
Kubernetes runtime, starts a local port-forward, and then starts cocola through
the normal native dev stack.

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
- `cocola/sandbox-runtime:dev` built locally.

The simplified profile deliberately uses a k3d local registry instead of
`k3d image import`. Importing the full sandbox runtime image into every node can
consume a large amount of disk and is brittle on small local Docker VMs.

The image has two addresses in local k3d mode:

- Host push address: `localhost:5001/cocola/sandbox-runtime:dev`
- Kubernetes pull address: `cocola-registry.localhost:5000/cocola/sandbox-runtime:dev`

Do not pass the host `localhost:5001` address to OpenSandbox sandbox pods. From
inside a k3d node, `localhost` is the node container itself, so pod image pulls
will fail with `ImagePullBackOff`.

The local values intentionally run without an OpenSandbox API key and set
`OPENSANDBOX_INSECURE_SERVER=YES`; do not use this values file as-is for a
shared environment. `make up-k8s` injects the Kubernetes-specific OpenSandbox
URL, node selector, timeout, and sandbox image variables automatically.

## Verify provider lifecycle

```bash
make verify-opensandbox-k8s
```

This verifies create, health, streaming exec, file upload/download and destroy.
By default it uses the same local registry image as `make up-k8s`:
`cocola-registry.localhost:5000/cocola/sandbox-runtime:dev`. It intentionally skips
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
