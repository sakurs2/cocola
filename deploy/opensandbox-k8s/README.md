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
- `docker`, `k3d`, `kubectl`, `helm` and `go`.
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

Each Conversation gets a `cocola-local-session` PVC. The k3s local-path
provisioner stores it under `/var/lib/cocola/storage` on the selected node;
mount a dedicated disk at that path before joining a production node. The PVC
request defaults to `2Gi`, but local-path treats that value as a soft capacity
request rather than a filesystem quota.

The path must also appear in the local-path provisioner's `nodePathMap` for
every node. Create k3s with
`--default-local-storage-path=/var/lib/cocola/storage`, or update the
`kube-system/local-path-config` ConfigMap before applying the StorageClass.
Every joined sandbox node owns the Session Volumes placed on that node; loss of
its disk loses those workspaces. Cocola does not install Longhorn, replicate
volumes or run a storage reconciliation controller.

`make dev` also builds and imports a small `cocola-storage-probe` image, then
deploys it as a read-only DaemonSet. The Admin **Storage** tab uses Kubernetes
Pod Proxy to read filesystem headroom and, only after an explicit click, walk
one Session Volume to measure allocated bytes. The probe exposes no file-list
or file-content API and does not run a timer, worker or background scan.

For a shared k3s deployment, publish `apps/admin-api/storage-probe.Dockerfile`
with the other release images, apply the DaemonSet in the Sandbox namespace,
and set its image to the published `cocola-storage-probe` tag:

```bash
kubectl -n opensandbox apply -f deploy/opensandbox-k8s/cocola-storage-probe.yaml
kubectl -n opensandbox set image daemonset/cocola-storage-probe \
  storage-probe=ghcr.io/<owner>/cocola-storage-probe:<version>
```

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

## Admin UI checks

Open `/admin/sandbox-nodes` in the web UI. The sandbox count is based on
OpenSandbox's `opensandbox.io/id` pod label and should reflect pods in the
`opensandbox` namespace.

Open `/admin/storage` to view each node's backing filesystem capacity and the
Session Storage list. Node capacity is cheap `statfs` data read at page refresh;
Session actual usage remains `Not measured` until an administrator clicks
**Measure** for that PVC.

The Kubernetes identity used by `admin-api` must be able to list Nodes and
Pods, list/get PVCs, get PVs, and access the `pods/proxy` subresource in the
Sandbox namespace. No direct node SSH or Sandbox wake-up is used.

## Stop

```bash
scripts/run-stack-dev.sh down
```

This keeps the local k3d cluster so the next `make dev` is faster. To delete the
cluster and reclaim disk:

```bash
scripts/run-stack-dev.sh reset
```
