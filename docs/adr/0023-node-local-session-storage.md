# ADR-0023: Node-local Session storage with explicit reset

- Status: Accepted
- Date: 2026-07-16
- Deciders: cocola maintainers

## Context

Complex Agent tasks download dependencies and produce intermediate files that
must survive sandbox reclamation. The platform targets small teams, but must
still support multiple sandbox nodes. Longhorn, replicated storage, snapshot
controllers and periodic reconciliation would add operational cost beyond this
product boundary. MinIO checkpoints also duplicate live filesystem state and
make recovery depend on upload timing.

We accept that a node disk failure can permanently lose every Session stored on
that node. We do not provide replication, backup, cross-node migration or
rollback in this version.

## Decision

- Each Session owns one `ReadWriteOnce` PVC in the
  `cocola-local-session` StorageClass, backed by k3s local-path storage.
- The default PVC request is `2Gi`. It is placement/accounting metadata and a
  soft request, not an enforced filesystem quota.
- PostgreSQL `session_storage` is the persistent source of truth for Session,
  owner, PVC, node and generation. Redis only tracks a running sandbox.
- A restored Session is forced back to its recorded node. If that node cannot
  accept a sandbox, Acquire returns `WORKSPACE_NODE_UNAVAILABLE` and leaves the
  binding unchanged.
- Cross-node recovery is destructive and requires explicit user confirmation.
  It creates a new PVC, increments generation and surfaces the reset and
  previous node in the Environment UI.
- The sandbox mounts one claim at `/session`; workspace, Claude/Codex state,
  Cocola Skill state and user-local tools are symlinked from that volume.
  `/cache`, rootfs and secrets remain ephemeral.
- Sandbox reclamation destroys compute only. Conversation deletion commits the
  database deletion under the Run lock, releases that lock, then submits a
  bounded best-effort PVC cleanup; failures remain visible to Admin as orphans.
- MinIO remains responsible for attachments, Artifacts and Skill bundles. It no
  longer stores or restores Session checkpoints.
- Sandboxes are always created on demand. No warm pool, storage polling loop,
  timed reconciliation, migration controller or automatic orphan fallback is
  introduced.
- Storage visibility is request-driven. A read-only probe DaemonSet exposes
  backing-filesystem `statfs` data and on-demand allocated-byte measurement for
  one validated PVC path. It has no timer, file browser or content endpoint.

## Alternatives Considered

- **Longhorn with replica 1** — provides a future path to replication and
  movement, but still introduces controllers, CRDs, disks, health states and
  a larger operations surface without improving the selected single-copy
  guarantee.
- **MinIO filesystem checkpoints** — portable across nodes, but expensive for
  dependency-heavy workspaces and vulnerable to partial or stale snapshots.
- **Shared NFS/CephFS** — offers node-independent recovery, but requires a
  separate storage service and makes that service part of every task's I/O
  path.
- **Automatic empty-workspace fallback** — simple for scheduling, but silently
  discards user state and can mix an old workspace with a new generation.

## Consequences

- **Positive** — persistence uses k3s built-ins, recovery is a mount rather than
  an archive transfer, and multi-node behavior is explicit and deterministic.
- **Positive** — the implementation has no storage background worker or
  intermediate storage state machine.
- **Positive** — Admin can see per-node physical headroom without waking a
  Sandbox; expensive per-Session directory traversal requires an explicit
  operator action.
- **Negative** — capacity requests are not hard quotas; operators must monitor
  node disks and mount suitable storage at `/var/lib/cocola/storage`.
- **Negative** — temporary node unavailability blocks affected Sessions, and
  node/disk loss requires an explicit empty-workspace reset.
- **Followup** — browser and remote IDE features may use the reserved persistent
  directories without changing this storage contract.
