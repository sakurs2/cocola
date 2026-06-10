# ADR-0008: Persistence layering, lifecycle, and sandbox backend (K8s + gVisor)

- Status: Proposed
- Date: 2026-06-10
- Deciders: @wangjiahui
- Depends on: ADR-0009 (runtime runs inside each user's sandbox)

## Context

ADR-0009 moves the Claude Code runtime *inside* each user's sandbox (Route A).
That decision makes three previously-deferred questions urgent and concrete:

1. **What persists, where, for how long** — the brain (and its `~/.claude`
   memory/sessions) now lives in a container that we will routinely destroy to
   save cost. User data must outlive the container.
2. **How a stateful, container-resident agent hibernates** — per-command release
   is impossible (ADR-0009); the container is at least session-lived, so idle
   cost must be reclaimed without losing user state.
3. **Which sandbox backend** runs all this for an enterprise self-host.

Through M5, state was either ephemeral (workspace, dies with the container) or
flat env vars (`COCOLA_AUTH_SECRET`, upstream API keys). That does not survive a
multi-user, hibernating, Route-A deployment.

## Decision

### 1. Three persistence tiers

| Tier | Name | Lifecycle | Examples | Backend |
| ---- | ---- | --------- | -------- | ------- |
| T1 | **Session-ephemeral** | created/destroyed with the container's *root* fs | `/tmp`, scratch, process RAM | container overlay (no persistence) |
| T2 | **User-persistent** | survives container destruction; per-user isolated | `~/.claude` (config/memory/**sessions**), user workspace files | **external mounted volume** (K8s PVC) + object store for versions + Postgres for metadata |
| T3 | **Platform secrets** | platform-owned; rotatable; least-privilege | `COCOLA_AUTH_SECRET`, upstream `ANTHROPIC_API_KEY`, signing keys | Vault |

Ownership is a strict hierarchy: T1 ⊂ session, T2 ⊂ user, T3 ⊂ platform.

### 2. External mounted volume decouples "container down" from "data lost"

This is the keystone that makes Route A's hibernate cheap and safe. **Each user
gets an external persistent volume (a K8s PVC) mounted into the sandbox at the
container `$HOME`.** Therefore:

- **On disk (survives):** `~/.claude` (config, memory, and crucially Claude
  Code's **session files**) and the user's workspace files live on the PVC. The
  container can be destroyed and the data remains.
- **In memory (lost):** the running `claude` process and its RAM context die with
  the container.

Volume identity is `cocola:vol:{userID}`; the `user → PVC` binding is recorded in
Postgres so any control-plane replica can resolve and re-mount it.

This also *retires* a worry from earlier drafts: we do **not** need MicroVM-style
memory snapshot/restore. Because Claude Code persists sessions to `~/.claude` on
the mounted volume, "resume" is reconstructed from disk, not from frozen RAM.

### 3. Lifecycle: lazy-start + session binding + hibernate via scale-to-zero

Reusing and extending ADR-0003's lease/GC:

- **Lazy-start.** No sandbox on pure chat; create on the first execution turn.
- **Session binding.** One sandbox per (user, session); multiple turns reuse it
  and see prior files. Sessions are mutually isolated.
- **Hibernate = destroy Pod + keep PVC (scale-to-zero), NOT memory freeze.**
  When a sandbox is idle past its cooldown, the Pod is deleted to free CPU/RAM;
  the PVC (T2 data) is retained.
- **Resume = new Pod + re-mount the same PVC + `claude --resume <session_id>`.**
  The brain rebuilds its context from the on-disk session under `~/.claude`. The
  user sees their files, memory, and history intact; they pay a "remount + replay
  session" cost, cheaper than a clean cold start, slightly heavier than a
  MicroVM in-place unfreeze. This RAM-lost / disk-kept gap is the explicit,
  accepted cost of choosing K8s over MicroVM.
- **Warm pool.** A background pool of pre-pulled, pre-warmed Pods removes
  image-pull + boot latency from the request path under load. Tiers: idle pool →
  bind on demand → return/destroy → async refill. Suggested starting params
  (tunable, mirroring reference practice): idle cooldown ~30 min with renew-on-
  activity, hard max lifetime ~2 days.

### 4. Sandbox backend: K8s + gVisor (runsc) first; provider stays pluggable

- **K8s + gVisor** is the starting backend. Rationale: most enterprises already
  run K8s, so isolation is "add a `RuntimeClass=runsc`" rather than standing up a
  new hypervisor stack; **PVCs natively provide the per-user volume** of §2;
  gVisor's userspace-kernel syscall interception is strong enough to contain
  untrusted code; standard tooling is easy for a team to operate. This fits the
  project's reuse-don't-reinvent principle.
- **CubeSandbox is the deferred alternative**, not the start. Its ~60ms cold
  start and CoW snapshots are attractive, but it needs a heavier KVM/MicroVM ops
  stack. Because the `SandboxProvider` abstraction (ADR-0002) keeps backends
  pluggable, picking K8s now is **not a one-way door**: a CubeSandbox provider
  can be added later with zero business changes if concurrency makes cold start
  hurt.
- **Pre-flight gate:** run a gVisor (runsc) compatibility spike for the heavy
  image (Node + Claude Code) before full build-out — verify `claude --version`
  and one real query run under runsc, since gVisor can impose overhead or gaps on
  IO-heavy / uncommon syscalls.

### 5. Secrets via Vault, using mature integrations (no custom crypto)

T3 (and sensitive parts of T2, e.g. a user's own upstream key) move into
**HashiCorp Vault** via a mature integration — **Vault Agent Sidecar injection**
or the **Secrets Operator / CSI Provider** — never hand-rolled secret storage.
Scattered values (`COCOLA_AUTH_SECRET`, upstream `ANTHROPIC_API_KEY`) converge to
Vault paths (`secret/cocola/platform/*`, `secret/cocola/users/{id}/*`). Local dev
keeps a `.env` fallback so the laptop flow is unchanged.

Note the interaction with ADR-0009: Route A injects credentials into the sandbox,
so secret delivery and the egress lockdown (sandbox → llm-gateway only) are two
halves of the same boundary.

### 6. Storage backends

- **Metadata → PostgreSQL.** `user` / `session` / `volume` and their bindings,
  plus a file/version index. The first thing M7 builds; the only hard new dep.
- **File versions → S3-compatible object store (MinIO self-host).** Version
  history / snapshots of T2 files; self-hostable per project principles.
- **Live volumes → K8s PVC** (per §2), one per user.

### 7. Sequencing — model first, production volume rides on M6

1. Stand up Postgres (metadata) + MinIO (versions).
2. Implement the three tiers and `user → volume` resolution.
3. Prove T2 on the Docker provider with a bind-mounted per-user `$HOME` (locally
   verifiable today, no K8s needed).
4. Integrate Vault for T3 (Agent Sidecar / CSI).
5. Realise the production PVC-backed volume + scale-to-zero hibernate + warm pool
   on K8s+gVisor (M6). The mount interface (`SandboxSpec.UserID` → `$HOME`) is
   identical to step 3; only the backing changes, so M7 is not blocked by M6.

## Alternatives Considered

- **Keep everything in env vars + ephemeral sandboxes.** No per-user durability,
  no rotation, settings.json leak persists. Rejected.
- **MicroVM memory snapshot/restore for hibernate.** True in-place freeze/thaw,
  but needs CubeSandbox/Firecracker-class infra. Rejected for the starting
  backend: external volume + Claude Code's on-disk sessions give "good enough"
  resume on plain K8s.
- **App-level envelope encryption in Postgres instead of Vault.** Avoids the
  Vault dep but means owning key management / rotation / audit — the wheel we are
  told to reuse. Rejected.
- **DB BLOB for user files instead of object storage.** Couples file growth to DB
  size, poor for large/streamed files. Rejected.
- **Two tiers (drop the platform-secret split).** Conflates a user's data with
  platform signing keys, breaking least-privilege. Rejected.

## Consequences

- **Positive:** per-user durable `$HOME` that survives container destruction;
  cheap hibernate/resume on stock K8s (no MicroVM needed); Vault gives
  rotation/audit/least-privilege for free; the model is provable on Docker today
  and lifts to K8s+gVisor unchanged; backend stays swappable to CubeSandbox.
- **Negative / accepted risk:** new infra deps (Postgres always; MinIO/Vault for
  full deploys) — mitigated by a `.env` fallback for local dev. Resume loses RAM
  context and replays the session from disk (the K8s-vs-MicroVM gap). Heavy
  Route-A image makes cold start the thing the warm pool must hide.
- **Follow-ups:** Postgres schema + migrations (M7); Docker bind-mount per-user
  `$HOME` (M7); Vault Sidecar/CSI wiring + converge secrets (M7); K8s PVC +
  scale-to-zero hibernate + warm pool (M6); gVisor compatibility spike (pre-M6);
  T2 version/GC policy in object store (M7+).
