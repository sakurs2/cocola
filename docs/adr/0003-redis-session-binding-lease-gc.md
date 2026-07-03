# ADR-0003: Redis-backed session↔sandbox binding with lease + two-stage GC

- Status: Accepted
- Date: 2026-06-09
- Deciders: @wangjiahui

## Context

A logical chat _session_ must map to exactly one live _sandbox_ across its
lifetime: every turn of a conversation has to land in the same container so the
workspace (`/workspace`), installed packages, and process state
persist between turns. M1 gave us a stateless `Create/Exec/Destroy` surface with
no notion of "which sandbox belongs to this session". M2 adds that binding plus
its lifecycle.

Three forces shape the design:

1. **Horizontal scale.** `sandbox-manager` must run as N stateless replicas
   behind a load balancer. Any replica may receive any request for any session,
   so the binding map cannot live in process memory — it must be shared state.
2. **Concurrency safety.** When a burst of requests for a _brand-new_ session
   arrives simultaneously (e.g. a client opens several tabs, or retries), they
   must converge on **one** sandbox, not race to create duplicates. 50 concurrent
   sessions must produce exactly 50 sandboxes.
3. **Lifecycle autonomy / cost.** Sandboxes are expensive (a container + gVisor
   sandbox each). Idle ones must be reclaimed automatically, but a user who
   steps away for 90 seconds and comes back should not pay a cold start.

## Decision

### Shared state in Redis, behind a `KV` interface

All binding state lives in Redis, accessed only through the
`packages/go-common/redis.KV` interface. This keeps the orchestrator decoupled
from go-redis specifically: a second-developer can back the binding with another
store by implementing `KV`, with zero orchestrator changes. The binder itself
holds **no** per-session state in memory, so replicas are interchangeable.

Key model (prefix `cocola:sb:`):

| Key                  | Value       | TTL            | Purpose                                    |
| -------------------- | ----------- | -------------- | ------------------------------------------ |
| `conv:{session}`     | sandbox id  | none           | forward lookup (session → sandbox)         |
| `rev:{sandbox}`      | session id  | none           | reverse lookup for cleanup                 |
| `meta:{sandbox}`     | JSON record | none           | durable registry; reaper's source of truth |
| `lock:{session}`     | owner token | `LockTTL` 30s  | guards create-and-bind critical section    |
| `lease:{sandbox}`    | `"1"`       | `LeaseTTL` 60s | heartbeat-renewed liveness signal          |
| `reaplock:{sandbox}` | owner token | 5s             | one-replica-per-sandbox reap guard         |

`conv/rev/meta` are durable; only `lease` (and the locks) expire. Idleness is
signalled by the **absence** of a lease, never by deleting meta — so the reaper
always has a complete inventory to scan.

### Concurrency: distributed lock + double-check

`Acquire` has a lock-free fast path (mapping exists → renew lease → return).
On a miss it takes a per-session lock (`SET NX EX`) and **re-checks** the mapping
under the lock before creating — so a racer that bound while we waited wins, and
we reuse its sandbox. Lock release is a Lua compare-and-delete (delete only if we
still own the token), preventing a slow holder from releasing a newer holder's
lock after TTL lapse. The bidirectional mapping + lease are written in a single
Lua script so a crash can never leave a half-written binding.

### Lifecycle: lease heartbeat + two-stage Pause-then-Destroy GC

- **Lease + heartbeat.** A sandbox stays alive while its lease is renewed.
  `Acquire` renews on every hit; long-running tasks that hold a sandbox between
  acquires call `Heartbeat`. `HeartbeatEvery` (20s) is one third of `LeaseTTL`
  (60s), so a single missed pulse never expires a healthy lease.
- **Stage 1 — Pause (idle).** When an _active_ sandbox's lease lapses, the reaper
  `Pause`s it (container freeze) and marks it `paused`. The workspace is
  preserved.
- **Stage 2 — Destroy (expired).** A _paused_ sandbox left untouched for
  `DestroyGrace` (120s) is `Destroy`ed and fully unbound.
- **Resurrection.** Any `Acquire`/`Heartbeat` on a paused sandbox `Resume`s it
  and flips it back to active — a returning user pays a cheap resume, not a cold
  create.

The reaper runs every `ReaperEvery` (10s), scanning `meta:*`. Each sandbox is
acted on under a short `reaplock:{sandbox}`, so concurrent reapers across
replicas each touch a given sandbox at most once per tick. Every state
transition is re-checked under that lock to close races against a concurrent
`Acquire` (e.g. abort a pause if the lease just came back).

### Chosen defaults

`LeaseTTL=60s`, `HeartbeatEvery=20s`, `DestroyGrace=120s`, `LockTTL=30s`,
`ReaperEvery=10s`. All overridable via `orchestrator.Config`.

## Alternatives considered

- **In-memory map in sandbox-manager.** Simplest, but breaks the moment we run >1
  replica, and loses all bindings on restart. Rejected: horizontal scale is a
  day-one requirement.
- **Single-stage Destroy on idle.** Less code, but every short pause-and-return
  pays a cold create. Rejected: Mira-style sessions are bursty; the resume hit
  rate justifies the extra state.
- **Redlock across multiple Redis nodes.** Overkill for a single-Redis dev/early
  prod deployment; the single-instance `SET NX` + Lua CAS is sufficient and
  swappable later via the `KV` seam.
- **Redis keyspace notifications for expiry instead of a polling reaper.**
  Tempting, but notifications are best-effort (lost on disconnect) and would make
  the Pause-then-Destroy two-stage transition awkward. A polling reaper over
  durable meta is simpler and self-healing.

## Consequences

- **Positive:** stateless, horizontally scalable sandbox-manager; verified
  concurrency safety (see `sandbox-cli bench`); cheap resume for returning
  sessions; backend-swappable binding store via `KV`.
- **Negative:** a Redis dependency on the hot path (mitigated: when Redis is
  unreachable, sandbox-manager degrades to the raw provider RPCs and only the
  binding RPCs return `Unimplemented`). Reaper polling adds a small constant load
  proportional to live-sandbox count.
- **Follow-ups:** export `Metrics.Snapshot()` via Prometheus/OTel (M3+); consider
  per-user sandbox quotas; revisit Redlock if a multi-node Redis is adopted.
