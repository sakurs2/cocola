# cocola on Kubernetes (sandbox provider)

These manifests deploy `sandbox-manager` with the **K8s** sandbox provider
(`COCOLA_SANDBOX_PROVIDER=k8s`). By default each user sandbox runs as a plain
**runc** Pod with **Kubernetes user namespaces** (`hostUsers: false`, container
root mapped to a non-privileged host uid) — strong isolation with **zero
node-level installation**. gVisor (`runsc` RuntimeClass) is an optional
enhancement (see below). Each sandbox also gets two PVCs for persistence and a
per-sandbox NetworkPolicy for egress lockdown.

## Prerequisites

- **Any** CNCF-conformant Kubernetes cluster **v1.33+** (user namespaces are
  default-on since 1.33) with a reasonably modern kernel (>= 5.19). The Provider
  talks to the API server via standard client-go, so the distribution does not
  matter: local **k3d / kind / minikube / OrbStack / Docker Desktop** for dev,
  **k3s** (1.35.5 verified) or any managed cluster (EKS/GKE/AKS) for prod — same
  manifests, no code change. No node-level sandbox installation is required for
  the default runc + userns path.
  > On macOS the K8s node always runs inside a Linux VM (provided by Docker
  > Desktop / OrbStack / Lima), so the >= 5.19 kernel requirement is satisfied by
  > that VM, not by macOS itself.
- A **CNI that enforces NetworkPolicy**. k3s already ships a kube-router policy
  controller that enforces NetworkPolicy by default. Domain-based allowlist
  entries additionally require a DNS-aware CNI (e.g. Cilium); plain CIDR/IP
  entries work on any policy-enforcing CNI.
- **Optional (gVisor only):** if you opt into the gVisor enhancement, the
  `containerd-shim-runsc-v1` shim must be installed on every node and registered
  in containerd matching the `runsc` handler in `01-runtimeclass.yaml`.
- The control-plane services (redis, llm-gateway, etc.) reachable at the
  in-cluster DNS names referenced in `04-sandbox-manager.yaml`. This directory
  ships only the sandbox plane; the rest of the stack is deployed separately.

## Local development on macOS/Linux (k3d, recommended)

[k3d](https://k3d.io) runs the **same k3s distribution** you use in production,
but wrapped in Docker — so a manifest verified locally behaves identically on a
bare-metal k3s server (same built-in containerd, flannel, kube-router
NetworkPolicy). Three steps:

```sh
# 0) prerequisites: Docker (Docker Desktop / OrbStack) + k3d + kubectl
#    brew install k3d kubectl

# 1) create a single-node cluster (kubeconfig context is set automatically)
k3d cluster create cocola --servers 1
kubectl config current-context        # -> k3d-cocola
kubectl get nodes                     # -> Ready

# 2) build images locally, then import them INTO the k3d cluster
#    (k3d uses its own containerd; `docker build` images are not visible until imported)
docker build -t cocola/sandbox-runtime:dev deploy/sandbox-runtime
docker build -f apps/sandbox-manager/Dockerfile -t cocola/sandbox-manager:dev .
docker build -f apps/llm-gateway/Dockerfile  -t cocola/llm-gateway:dev .
k3d image import cocola/sandbox-runtime:dev cocola/sandbox-manager:dev cocola/llm-gateway:dev -c cocola

# 3) apply the manifests (see below). Tear down with:  k3d cluster delete cocola
```

> Why not k3s directly on macOS? k3s is Linux-only — on a Mac it would still
> need a Linux VM. k3d gives you that VM-backed k3s with a one-line create, and
> `k3d image import` replaces the `k3s ctr images import` dance.

## Apply (raw manifests)

```sh
kubectl apply -f deploy/k8s/00-namespaces.yaml
# 01-runtimeclass.yaml is OPTIONAL (gVisor only) — skip it for the default runc path
kubectl apply -f deploy/k8s/02-rbac.yaml
kubectl apply -f deploy/k8s/03-sandbox-base.yaml   # populate the plugins PVC out of band
kubectl apply -f deploy/k8s/04-sandbox-manager.yaml
kubectl apply -f deploy/k8s/05-deps-redis-llm-gateway.yaml  # test-time deps (redis+llm-gateway); see runbook
```

## Apply (Helm)

```sh
helm install cocola deploy/helm/cocola-sandbox \
  --set sandbox.storageClass=<your-sc> \
  --set sandbox.llmBaseURL=http://llm-gateway.cocola.svc.cluster.local:8080
```

See `deploy/helm/cocola-sandbox/values.yaml` for all tunables. The chart
defaults to the runc + userns path (`runtimeClass.install=false`,
`sandbox.runtimeClass=""`, `sandbox.hostUsers="false"`).

### Optional: gVisor enhancement

For stronger userspace-kernel isolation, install the gVisor shim on every node,
then either apply `01-runtimeclass.yaml` and set
`COCOLA_K8S_RUNTIME_CLASS=runsc` (raw manifests), or Helm
`--set runtimeClass.install=true --set sandbox.runtimeClass=runsc`. On a cluster
that already provides a gVisor RuntimeClass (e.g. GKE's `gvisor`), set
`runtimeClass.install=false` and `sandbox.runtimeClass=gvisor`. Full steps are
in `docs/runbook/m6-k8s-sandbox-acceptance.md` (Appendix A).

## What runs where

| Object | Namespace | Purpose |
|---|---|---|
| `sandbox-manager` Deployment/Service | `cocola` | control plane; drives sandboxes via the API server |
| sandbox Pods (`cocola-sbx-*`) | `cocola-sandboxes` | user workloads under runc + userns (or `runsc` if gVisor enabled) |
| user/session PVCs | `cocola-sandboxes` | ADR-0008 T1b/T2 persistence |
| binding ConfigMaps (`cocola-bind-*`) | `cocola-sandboxes` | cross-replica/hibernate resolve source of truth |
| per-sandbox NetworkPolicy (`cocola-egress-*`) | `cocola-sandboxes` | egress lockdown (ADR-0009) |

## Notes

- `sandbox-manager` runs **2 replicas**: any replica can Pause/Resume/Destroy any
  sandbox because the binding ConfigMap (not in-memory state) is the source of
  truth.
- Pause deletes the Pod but keeps PVCs + binding (scale-to-zero hibernate);
  Resume rebuilds the Pod from the binding and remounts the PVCs so
  `claude --resume` continues the conversation.
- The user PVC survives Destroy (cross-session userdata); the session PVC is
  reclaimed by the orchestrator's Release path.
