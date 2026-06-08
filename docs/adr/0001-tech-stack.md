# ADR-0001: Tech stack — Go + Python hybrid, Next.js frontend

- Status: Accepted
- Date: 2026-06-08
- Deciders: @wangjiahui

## Context

cocola needs to ship three classes of workload:

1. **High-throughput, low-latency edge** — public-facing gateway, sandbox
   orchestration, admin API. These are mostly I/O multiplexing with tight tail
   latency requirements and benefit from a single static binary deploy.
2. **LLM / Agent orchestration** — Agent runtime, LLM gateway. The reference
   SDK (Claude Code Agent SDK) is Python/TS only; the rich ML/data ecosystem is
   Python; iteration speed on prompt/agent logic matters more than raw QPS.
3. **Frontend** — interactive chat UI with streaming responses, optimistic UI,
   admin console.

We also need a contributor-friendly stack: a small team (and OSS contributors)
must be able to ship features without learning four ecosystems.

## Decision

- **Go** for `gateway`, `sandbox-manager`, `admin-api`.
- **Python 3.11+** for `agent-runtime`, `llm-gateway`.
- **TypeScript + Next.js (App Router) + Tailwind CSS 3** for `web`.
- **gRPC** for service-to-service; **SSE / WebSocket** for browser streams.
- **Buf** as the proto toolchain; shared `packages/proto` generates Go/Python/TS stubs.
- **Monorepo** with per-language workspaces (`go.work`, `pyproject.toml` + uv,
  `pnpm-workspace.yaml`).

## Alternatives Considered

- **All-Python backend.** Faster iteration, single language. Rejected because:
  Python sandbox orchestration on K8s gets painful at scale (GIL, async story
  for gRPC streaming is uneven), and we lose the static-binary deploy story.
- **All-Go backend.** Best ops story, but Claude Code Agent SDK has no Go port
  and rewriting it would dominate the project.
- **Node.js for agent runtime.** Viable (SDK exists). Rejected because the team's
  data/ML ecosystem (eval harnesses, dataset tooling) is Python-native.
- **Vite + React Router instead of Next.js.** Lighter, but we lose Next's SSR,
  middleware, and per-route streaming primitives that matter for chat UX.

## Consequences

- **Positive**
  - Each workload uses the right tool; ops gets static Go binaries for the hot path.
  - gRPC + Buf gives one contract source for three languages.
  - Frontend gets first-class streaming via Next App Router.
- **Negative**
  - Contributors must be comfortable in (at least) one of three languages.
  - CI matrix is larger (Go + Python + Node lanes).
  - Cross-language refactors (e.g. renaming a proto field) require touching three generated outputs.
- **Followups**
  - ADR-0002 (TBD): SandboxProvider abstraction and pluggable backends.
  - ADR-0003 (TBD): Storage layering (PG / Redis / S3 / NFS / Vault).
  - CI workflow gates each lane independently (M0 ships skeleton; full suite by M2).
