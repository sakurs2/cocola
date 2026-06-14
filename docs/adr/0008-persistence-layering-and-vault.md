# ADR-0008: Persistence layering, lifecycle, and sandbox backend (K8s + gVisor)

- Status: Accepted (持久化分层与 K8s/gVisor 后端已随 M6/M7 落地；Vault 密钥托管留待后续)
- Date: 2026-06-10 (accepted 2026-06-14)
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
| T1a | **Process-ephemeral** | dies with the container | `/tmp`, scratch, process RAM | container overlay (no persistence) |
| T1b | **Session-scoped** | survives container hibernate, cleaned at session end | session workspace (`workspace/{session}`), in-progress files | **session volume** (PVC), per session |
| T2 | **User-persistent** | survives container destruction; per-user isolated | `~/.claude` (config/memory/**sessions**), long-lived files, git clones | **user volume** (PVC), per user + object store for versions + Postgres for metadata |
| T3 | **Platform secrets** | platform-owned; rotatable; least-privilege | `COCOLA_AUTH_SECRET`, upstream `ANTHROPIC_API_KEY`, signing keys | Vault |

Ownership is a strict hierarchy: T1 ⊂ session, T2 ⊂ user, T3 ⊂ platform.

> **Why split T1 into a/b** (learned from Mira's model). With Route A's
> scale-to-zero hibernate, a sandbox Pod can be destroyed *while its session is
> still alive*. If the session workspace were pure container overlay (T1a), those
> in-progress files would vanish on every hibernate. Mira keeps the session
> workspace on the mounted disk too (only cleaning it at session end), so it
> survives Pod churn. We adopt the same: T1b is disk-backed but
> session-scoped.

### 2. Two mounted volumes + read-only system paths (the Mira model)

This is the keystone that makes Route A's hibernate cheap and safe, and it
follows Mira's proven layout: Mira mounts a NAS under `/opt/tiger/mira_nas/` with
two directories of different lifecycle — `userdata/{user_id}/` (cross-session,
per user) and `workspace/{session_id}/` (per session) — while system paths stay
read-only. cocola adopts the same shape with two PVCs instead of one `$HOME`
mount:

- **User volume — `cocola_user/{userID}/`** (T2, cross-session). Mounted into the
  sandbox and **`~/.claude` is bound here** (e.g. `~/.claude` → the user
  volume's `.claude/` subdir), so config, memory, and Claude Code's **session
  files** persist across sessions and across Pod churn. Long-lived files, git
  clones, and user-installed deps live here too. A dedicated **`secrets/`
  subdir** (mirroring Mira's `userdata/.../secrets/`) holds user-scoped tokens /
  PAT / SSH keys; sensitive items here are the T2 surface that integrates with
  Vault (§5).
- **Session volume — `cocola_session/{sessionID}/`** (T1b). The session
  workspace. Disk-backed so it **survives container hibernate**, but cleaned at
  session end. This is the key fix over a naive `$HOME`-only mount: under
  scale-to-zero, a Pod can be destroyed while the session is still open, and a
  pure-overlay workspace would lose in-progress files on every hibernate.
- **System paths stay read-only.** The image's system layers are immutable; all
  mutable state is funnelled into the two mounted volumes. This keeps the image
  reproducible and makes "what persists" explicit rather than accidental — the
  same discipline Mira enforces ("系统路径不可改写").
- **Nothing else persists by default.** Like Mira (where `~/files` is *not*
  persistent), any path outside the two mounts is ephemeral. Persistence is an
  explicit mount, never a surprise.

Splitting "container down" from "data lost":

- **On disk (survives):** everything on the user volume (`~/.claude`, long-lived
  files) and the session volume (open workspace). The container can be destroyed
  and the data remains.
- **In memory (lost):** the running `claude` process and its RAM context die with
  the container.

Volume identity: `cocola:vol:user:{userID}` and `cocola:vol:session:{sessionID}`;
the `user → volume` and `session → volume` bindings are recorded in Postgres so
any control-plane replica can resolve and re-mount them.

This *retires* a worry from earlier drafts: we do **not** need MicroVM-style
memory snapshot/restore. Because Claude Code persists sessions to `~/.claude` on
the user volume, "resume" is reconstructed from disk, not from frozen RAM.

### 3. Lifecycle: lazy-start + session binding + hibernate via scale-to-zero

Reusing and extending ADR-0003's lease/GC:

- **Lazy-start.** No sandbox on pure chat; create on the first execution turn.
- **Session binding.** One sandbox per (user, session); multiple turns reuse it
  and see prior files. Sessions are mutually isolated.
- **Hibernate = destroy Pod + keep both PVCs (scale-to-zero), NOT memory
  freeze.** When a sandbox is idle past its cooldown, the Pod is deleted to free
  CPU/RAM; the user volume (T2) and session volume (T1b) are retained, so an open
  session's workspace survives the hibernate.
- **Resume = new Pod + re-mount the same two volumes + `claude --resume <session_id>`.**
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
- **gVisor cutover gate (not a pre-build blocker):** the Route-A link is built
  and proven on plain Docker (runc) first; the gVisor (runsc) compatibility spike
  is the acceptance gate *before the production gVisor cutover*, not before
  build-out. Because runsc only swaps the isolation layer (`--runtime=runsc`),
  the runc link reused unchanged. The spike verifies `claude --version`, one
  real query (egress + native bash/file IO), and mounted-volume resume under
  runsc, since gVisor can impose overhead or gaps on IO-heavy / uncommon
  syscalls.

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
- **Live volumes → K8s PVCs** (per §2): one user volume per user (T2) + one
  session volume per open session (T1b).

### 7. Sequencing — model first, production volume rides on M6

1. Stand up Postgres (metadata) + MinIO (versions).
2. Implement the three tiers and `user → volume` resolution.
3. Prove T1b/T2 on the Docker provider with bind-mounted per-user and
   per-session dirs (`~/.claude` bound into the user dir), locally verifiable
   today, no K8s needed.
4. Integrate Vault for T3 (Agent Sidecar / CSI).
5. Realise the production PVC-backed volume + scale-to-zero hibernate + warm pool
   on K8s+gVisor (M6). The mount interface (`SandboxSpec.UserID` → user volume,
   `SandboxSpec.SessionID` → session volume) is identical to step 3; only the
   backing changes, so M7 is not blocked by M6.

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
  + per-session dirs with `~/.claude` bound into the user dir, system paths
  read-only (M7); Vault Sidecar/CSI wiring + converge secrets, including the user
  volume's `secrets/` subdir (M7); K8s PVCs (user + session) + scale-to-zero
  hibernate + warm pool (M6); gVisor compatibility spike (pre-prod gVisor
  cutover); T2 version/GC policy in object store (M7+).
