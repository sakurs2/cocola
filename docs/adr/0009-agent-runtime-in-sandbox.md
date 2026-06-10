# ADR-0009: Run the Claude Code agent runtime *inside* each user's sandbox

- Status: Proposed
- Date: 2026-06-10
- Deciders: @wangjiahui
- Revises: ADR-0007 (gateway BFF + agent-runtime gRPC), ADR-0004 (LLM gateway proxy)

## Context

cocola's goal is an **enterprise, self-hosted, Mira-like** agent platform: the
company deploys it once, employees open a web page, and **nothing is installed on
the employee's machine**. Claude Code therefore lives entirely on the server
side — this goal is already met today (the employee only ever talks to the web
app over HTTP/SSE).

Through M5, cocola placed the Claude Agent SDK (and the `claude` CLI subprocess
it spawns) in a **central, multi-tenant `agent-runtime` process**. The agent's
"brain" (the ReAct loop, context, `~/.claude` memory) runs on that shared host;
only its "hands" reach a sandbox, via an in-process MCP server
(`sandbox_tools.py`) that forwards `bash` / `read_file` / `write_file` calls into
the session's bound container. Call this **Route B: brain in the centre, hands in
the sandbox**.

Reviewing two reference write-ups — Tencent's CubeSandbox and an internal
Mira/ByteDance discussion of how Claude Code is operated server-side — surfaced
that the industry converges on a different shape for multi-tenant agents, which
we call **Route A: the whole runtime runs inside each user's sandbox**. Mira's
Cowork mode "is basically vanilla Claude Code" running per-user in a sandbox.

Route B has three structural problems for an enterprise multi-tenant target:

1. **Shared-host blast radius.** One `agent-runtime` process spawns N users'
   `claude` CLI children. A crash, a noisy neighbour, or a session-id collision
   in the in-process registry affects unrelated users.
2. **`~/.claude` isolation is a patch, not a property.** Memory/config/sessions
   live on the shared host; we isolate them only with a per-invocation temp
   `CLAUDE_CONFIG_DIR`. This is the root cause of the 503 settings.json leak we
   already hit (the host's global `~/.claude/settings.json` env block overriding
   our injected `ANTHROPIC_BASE_URL`).
3. **The MCP forwarding layer is a seam holding a split architecture together,
   and it is leaky.** We allow-list only the three MCP tools, but we do **not**
   disable Claude Code's *native* Bash/Read/Write/Edit/Glob. Those default to
   executing on the **agent-runtime host's** filesystem. Without an explicit
   `disallowed_tools` + pinned `permission_mode`, the model can bypass the
   sandbox entirely and run untrusted code on the shared host. "Looks isolated,
   brain actually exposed."

## Decision

**Adopt Route A: package the Claude Code runtime (Node + `claude` CLI +
claude-agent-sdk + a thin shim) into the per-user sandbox image. The brain and
the hands both run inside the user's own container.** `agent-runtime` degrades
from "the process that runs the agent" to a **control-plane router**: it
authenticates, resolves `user_id`/`session_id` → sandbox, ensures the sandbox is
up, forwards the prompt to the in-sandbox shim, and relays the event stream back
to the gateway. It no longer spawns `claude` itself.

Consequences of the brain moving into the sandbox:

### 1. Native tools are now safe — delete the MCP forwarding seam

With the CLI running inside the user's own container, Claude Code's **native**
Bash/Read/Write/Edit/Glob execute against *that user's own* filesystem — they
are naturally isolated, and one user's command cannot touch another's. Therefore:

- **`sandbox_tools.py` (the in-process MCP forwarding server) is removed.** It
  was a Route-B stitch; Route A makes it dead weight.
- The agent runs with the **full native Claude Code toolset** (Edit, Glob, Grep,
  WebFetch, Task subagents, …) — i.e. "vanilla Claude Code" fidelity, not three
  hand-rolled MCP tools.
- The Route-B emergency patch (adding `disallowed_tools` to fence off native
  execution) is **moot and will not be implemented**.

### 2. The security boundary moves from "tool allow-list" to "network egress"

Route A injects credentials (the cocola-issued token, `ANTHROPIC_BASE_URL`) into
the sandbox env, and the sandbox must reach the llm-gateway over the network.
Since untrusted code now runs *next to* those credentials, the containment story
becomes **the sandbox is the blast radius, bounded by the network**:

- The sandbox's **egress is locked down to an allowlist** — only the llm-gateway
  (and required internal services); **no arbitrary public internet**. This
  reuses the `Networking.EgressAllowlist` already on `SandboxSpec`.
- This is a *cleaner* model than Route B: we stop relying on "the model won't
  call native bash" and instead enforce a hard network perimeter.

### 3. The CLI is pre-baked into the image, never installed at runtime

Per the reference practice, the base image bakes in Node.js + Claude Code (via an
offline `npm pack` tgz to survive networks without registry access) + Python (uv)
+ the shim. Sandboxes are ready on start; Claude Code upgrades = swap the tgz,
rebuild the image, upper layers untouched. The shim wraps `query()` /
`ClaudeSDKClient` so the control plane talks to it over a defined RPC rather than
driving the non-streaming CLI directly.

### 4. Lifecycle = lazy-start + session binding + idle hibernate (see ADR-0008)

Because the brain is stateful and lives in the container, the container **must**
be at least session-lived — per-command "run then release" is incompatible with
Route A and is explicitly rejected (it only fits a stateless executor where the
brain is outside). Idle cost is handled by hibernate/resume, detailed in the
updated ADR-0008.

## Alternatives Considered

- **Stay on Route B, just patch the holes.** Add `disallowed_tools`, pin
  `permission_mode`, keep per-invocation `CLAUDE_CONFIG_DIR` + an M7 per-user
  volume. Pros: smallest diff, keeps the lightweight sandbox image, and keeps LLM
  credentials *out* of the untrusted-code environment (a genuine security plus).
  Cons: isolation stays a patchwork; shared-host blast radius and noisy-neighbour
  remain; we run a "thin custom agent", not vanilla Claude Code, so fidelity and
  the native toolset are permanently reduced; every Claude Code upgrade risks
  re-opening the native-tool bypass. Rejected for the enterprise multi-tenant
  target, where isolation must be a property, not a patch.
- **Per-command ephemeral sandbox (brain outside, stateless executor).** The E2B
  / code-interpreter shape. Rejected: incompatible with a stateful in-sandbox
  brain; and even CubeSandbox's own guidance is to bind a sandbox to the
  *session*, not a single tool call, "otherwise you lose state and thrash on
  start/stop".
- **Dual-mode (thin agent for chat + vanilla Claude Code for cowork), like
  Mira.** Attractive long-term and not precluded — the control plane stays the
  same. Deferred: we standardise on Route A first and may add a thin-agent lane
  later, rather than carrying two runtimes from day one.

## Consequences

- **Positive** — isolation, memory, and native-tool fidelity become *structural*
  (one container per user/session); the settings.json leak disappears by
  construction; the MCP forwarding seam is deleted (less code); the agent gains
  the full native Claude Code toolset; the security model is a clean network
  perimeter.
- **Negative / accepted risk** — the sandbox image gets heavy (Node + Claude Code
  + Python), so cold start grows from container-spawn to seconds; mitigated by
  the lazy-start + hibernate + warm-pool strategy in ADR-0008. Credentials now
  live inside the untrusted-code container, so **egress lockdown is mandatory,
  not optional**. `agent-runtime` must be rebuilt as a router (revises ADR-0007),
  and llm-gateway must accept traffic originating from sandboxes (revises
  ADR-0004's trust assumptions).
- **Follow-ups:**
  - Build the per-user sandbox base image (Node + Claude Code offline tgz + uv +
    shim); pin the shim's RPC contract.
  - Rework `agent-runtime` into a control-plane router; delete `sandbox_tools.py`
    and the Route-B MCP path in `claude_sdk_provider.py`.
  - Enforce `Networking.EgressAllowlist` end-to-end (sandbox → llm-gateway only).
  - Run a gVisor (runsc) compatibility spike for Node/Claude Code before full
    build-out (see ADR-0008 backend choice).
