# cocola on Kubernetes (sandbox provider)

These manifests deploy `sandbox-manager` with the **K8s** sandbox provider
(`COCOLA_SANDBOX_PROVIDER=k8s`). By default each user sandbox runs as a plain
**runc** Pod with **Kubernetes user namespaces** (`hostUsers: false`, container
root mapped to a non-privileged host uid) — strong isolation with **zero
node-level installation**. gVisor (`runsc` RuntimeClass) is an optional
enhancement (see below). Each sandbox also gets two PVCs for persistence and a
per-sandbox NetworkPolicy for egress lockdown.

## Prerequisites

- A Kubernetes cluster **v1.33+** (user namespaces are default-on since 1.33;
  k3s 1.35.5 works out of the box) with a reasonably modern kernel (>= 5.19).
  No node-level sandbox installation is required for the default runc + userns
  path.
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
