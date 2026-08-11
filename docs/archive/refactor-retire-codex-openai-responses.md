# refactor: retire Codex and OpenAI Responses support

- Change time: 2026-08-12 03:27 (+08:00)

## Reason

Cocola no longer uses Codex as an Agent runtime, and the OpenAI Responses
provider exposed configuration and runtime paths that were no longer part of
the supported product. Keeping both implementations increased image size,
configuration surface, protocol branching, and long-term maintenance cost.
The installation has no historical Codex conversations that require a
compatibility path.

## Changes

- `apps/web`, `apps/admin-api`, and `apps/gateway`: removed the Codex runtime
  picker and OpenAI Responses model configuration, while retaining OpenAI
  Embeddings for Memory.
- `apps/agent-runtime`, `deploy/sandbox-runtime`, and `apps/sandbox-manager`:
  made Claude Code the single supported Agent runtime and removed the Codex
  CLI, SDK, adapter, mounts, and verification paths.
- `apps/llm-gateway`: removed the Responses API endpoint, upstream adapter,
  streaming implementation, and Responses-specific tests.
- `db/migrations/00064_retire_codex_responses.sql`: removes legacy Responses
  routes and providers, narrows supported provider/protocol constraints, and
  refuses to proceed if a real Codex conversation is present.
- `docs/adr/0029-single-claude-agent-runtime.md`, configuration, deployment,
  and regression tests: documented and verified the single-runtime boundary.
- Tradeoff: durable runtime identity fields remain in shared contracts to
  avoid an unrelated schema rewrite, but every production entry point now
  resolves to `claude-code`.
