# ADR-0028: Hidden Forgejo and unified Project change requests

- Status: Accepted
- Date: 2026-08-08
- Deciders: Cocola maintainers

## Context

Local Project originally stored its only authoritative repository in one Session Volume and therefore allowed only
one Task. That design made concurrent work, cross-node scheduling, independent Task cleanup, review, and reliable
merge semantics difficult. GitHub Project already used per-Task branches, but its delivery lifecycle was different.

The authority must survive a damaged or reclaimed Task sandbox, support multiple concurrent Tasks, and provide a
single review and merge model without requiring every Local user to configure an external Git provider.

## Decision

Cocola deploys a pinned, internal-only Forgejo backed by a separate PostgreSQL database/user and persistent data
volume. Forgejo is hidden from normal users and administrators; Admin exposes health metadata only.

Local Project provisions one private repository named from the stable Project UUID and initializes `main` with an
allow-empty commit. GitHub repositories remain on GitHub. Every Task independently clones the authority into its
Session Volume, locks a base SHA, and works on `cocola/task-<id>`.

Both providers use the Cocola Change Request lifecycle. The platform performs exact clean-branch publishing, PR
creation, status refresh, expected-head squash merge, branch deletion, and merged Task read-only enforcement.
Provider operations are idempotent and queried on demand; no webhook or permanent synchronization worker is added.

Forgejo Project tokens are repository-restricted and encrypted at rest. AskPass injects them only into dedicated Git
subprocesses. The Provisioner credential remains in Gateway. Upgrade backups include the Cocola database, Forgejo
database, and Forgejo data volume as one recovery point.

## Alternatives Considered

- **Session Volume as authority** — lowest initial cost, but RWO/node affinity and one-Task coupling make concurrent
  and multi-node operation fragile.
- **Git Worktree isolation** — efficient on one host, but shares repository locks and storage and does not fit
  independent Session Volumes.
- **Custom bare Git service** — avoids another product dependency but transfers Smart HTTP, authorization, locking,
  GC, backup, merge, and audit maintenance to Cocola.
- **Require external Git for Local work** — simplest operations, but removes the zero-configuration Local Project
  experience.

## Consequences

- **Positive** — Local and GitHub Projects share multi-Task isolation, review, conflict, squash merge, and read-only
  completion semantics. A Task Volume is disposable and cannot destroy the Project authority.
- **Negative** — Cocola now operates a stateful Forgejo service, coordinates database plus repository-volume backups,
  and must explicitly validate each Forgejo upgrade.
- **Followups** — periodically exercise recovery, rotate repository tokens, and consider webhook synchronization only
  if on-demand refresh becomes an observed scalability problem.
