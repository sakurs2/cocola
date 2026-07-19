# ADR-0025: On-demand headless Browser capability

- Status: Accepted
- Date: 2026-07-19
- Deciders: cocola maintainers

## Context

Agents frequently need to inspect rendered pages, capture screenshots and
produce PDFs. The Runtime image already includes Playwright and Chromium, but
using them requires ad-hoc scripts and provides no stable policy, state or
output contract. A visual desktop, permanently running browser daemon, exposed
CDP endpoint or per-Sandbox observe API would add attack surface and lifecycle
cost that Cocola does not currently need.

A guest CLI alone is deterministic but not discoverable enough for an Agent:
the model still needs concise guidance about when to inspect, when to render an
artifact, where outputs belong and which security boundaries must not be
bypassed. Admin and Personal Skills are currently reconciled as a complete
Session snapshot, so simply copying a built-in Skill into a normal home
directory would be overwritten on the next catalog sync.

This phase does not implement HTML publication/preview, Jupyter, a visual
desktop, single-Sandbox observe, or a Sandbox MCP adapter.

## Decision

- The Runtime Manifest declares an on-demand `browser` capability. The
  `coding` Profile enables it by default and `minimal` disables it.
- `COCOLA_BROWSER_ENABLED` is an operator-owned override. Sandbox Manager
  validates it at startup, removes caller-supplied values and injects only the
  effective operator policy into newly created Sandboxes.
- `cocola-sandbox browser` exposes `status`, `inspect`, `screenshot` and `pdf`
  with human and JSON output. Navigation accepts only explicit HTTP(S) URLs;
  output paths are constrained beneath `/workspace`.
- Each operation launches a Playwright persistent context in headless mode and
  closes it before returning. No Browser daemon or control port is created.
- Browser profile state is stored under `/session/runtime/browser/profile` and
  output defaults to `/workspace/outputs/browser`. Both survive Sandbox compute
  reclamation through the Session Volume.
- The Browser runner and guest CLI remain root-owned image assets. Commands run
  as the fixed non-root `cocola` identity. Existing Sandbox egress policy is the
  network authority; Browser does not add a second allowlist.
- The Runtime image includes a versioned, root-owned
  `cocola-sandbox-browser` Agent Skill and a platform Skill manifest. The Skill
  documents the guest CLI workflow and boundaries; it contains no second
  execution implementation.
- Before each Run, Agent Runtime inspects the actual image inventory and
  atomically merges platform Skills with configured Admin and Personal Skills.
  Claude and Codex share the resulting Session snapshot through their standard
  Skill directories. Platform IDs are reserved; a configured Skill collision
  fails reconciliation instead of silently shadowing either source.
- The installed and currently available platform digests are compared during
  reconciliation. This allows old images with no platform Skill to coexist
  during a rolling rollout and makes a new image Skill version invalidate an
  otherwise unchanged configured-Skill snapshot.

## Alternatives Considered

- **Let every Agent write raw Playwright scripts** — already possible, but it
  lacks discoverability, policy gating, output conventions and a stable JSON
  interface for a later Skill or MCP adapter.
- **Run a resident Browser/CDP daemon** — supports long interactive sessions,
  but adds a privileged control surface, readiness/restart handling and an
  exposed port. Persistent Playwright state gives the needed continuity without
  that lifecycle cost.
- **Use stateless incognito contexts** — simpler cleanup, but loses authenticated
  website state whenever the command or Sandbox compute ends.
- **Allow file/data URLs** — convenient for local HTML, but mixes browser
  automation with the later isolated HTML-preview security boundary.
- **Publish the built-in Skill through the Admin catalog only** — it could be
  updated without an image, but the instructions could drift ahead of the guest
  CLI and disappear when the catalog is unavailable. Keeping the compatible
  default in the image makes the contract self-contained; later catalog Skills
  can still provide higher-level workflows under distinct IDs.

## Consequences

- **Positive** — Agents gain deterministic page inspection and rendering without
  a new network service; state and output paths are stable across providers.
- **Positive** — the built-in Skill makes the CLI discoverable in both Claude
  and Codex while keeping one execution and policy boundary. The same JSON
  contract can later be wrapped by an optional MCP server.
- **Negative** — one Chromium startup is paid per command, and concurrent
  commands sharing the same persistent profile are not supported.
- **Negative** — HTML files cannot be navigated directly in this phase.
- **Negative** — changing built-in Skill content requires publishing a Runtime
  image so the instructions and guest CLI stay version-aligned.
- **Followup** — add isolated Artifact/HTML preview; keep MCP as an optional thin
  adapter over the guest CLI.
