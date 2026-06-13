# cocola on Kubernetes (gVisor sandbox provider)

These manifests deploy `sandbox-manager` with the **K8s + gVisor** sandbox
provider (`COCOLA_SANDBOX_PROVIDER=k8s`). Each user sandbox runs as a Pod under
the `runsc` RuntimeClass for strong isolation, with two PVCs for persistence and
a per-sandbox NetworkPolicy for egress lockdown.

## Prerequisites

- A Kubernetes cluster (v1.29+).
- **gVisor installed on the nodes**: the `containerd-shim-runsc-v1` shim must be
  present and registered in containerd, matching the `runsc` handler in
  `01-runtimeclass.yaml`. On managed clusters this is often a node-pool toggle
  (e.g. GKE Sandbox). Without it, sandbox Pods stay `Pending`/`ContainerCreating`.
- A **CNI that enforces NetworkPolicy** (Calico, Cilium, etc.). Domain-based
  allowlist entries additionally require a DNS-aware CNI (e.g. Cilium); plain
  CIDR/IP entries work on any policy-enforcing CNI.
- The control-plane services (redis, llm-gateway, etc.) reachable at the
  in-cluster DNS names referenced in `04-sandbox-manager.yaml`. This directory
  ships only the sandbox plane; the rest of the stack is deployed separately.

## Apply (raw manifests)

```sh
kubectl apply -f deploy/k8s/00-namespaces.yaml
kubectl apply -f deploy/k8s/01-runtimeclass.yaml
kubectl apply -f deploy/k8s/02-rbac.yaml
kubectl apply -f deploy/k8s/03-sandbox-base.yaml   # populate the plugins PVC out of band
kubectl apply -f deploy/k8s/04-sandbox-manager.yaml
```

## Apply (Helm)

```sh
helm install cocola deploy/helm/cocola-sandbox \
  --set sandbox.storageClass=<your-sc> \
  --set sandbox.llmBaseURL=http://llm-gateway.cocola.svc.cluster.local:8080
```

See `deploy/helm/cocola-sandbox/values.yaml` for all tunables. If the cluster
already provides a gVisor RuntimeClass (e.g. GKE's `gvisor`), set
`runtimeClass.install=false` and `sandbox.runtimeClass=gvisor`.

## What runs where

| Object | Namespace | Purpose |
|---|---|---|
| `sandbox-manager` Deployment/Service | `cocola` | control plane; drives sandboxes via the API server |
| sandbox Pods (`cocola-sbx-*`) | `cocola-sandboxes` | user workloads under `runsc` |
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
