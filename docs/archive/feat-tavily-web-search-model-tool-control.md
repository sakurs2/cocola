# feat: add Tavily web search and model-native web tool controls

- Change time: 2026-08-24 13:27 (+08:00)

## Reason

Anthropic-compatible model routes such as Qwen and DeepSeek do not necessarily
implement Claude Code's server-side `WebSearch` and `WebFetch` tools. Exposing
those tools caused conversations to attempt an unsupported capability, while
Cocola had no simple workspace-level way to provide a model-independent web
search fallback.

## Changes

- `db/migrations/00066_anthropic_server_web_tools.sql`: added a fail-closed
  model-route capability and enabled it during migration only for
  `anthropic-messages` routes whose provider host is exactly
  `api.anthropic.com`.
- `apps/admin-api` and `apps/web/app/admin/models/page.tsx`: added Admin API and
  model-form support for explicitly controlling Anthropic native web tools.
- `apps/web/app/admin/mcps/page.tsx`: added a persistent, disabled Tavily preset
  card and a compact, centered setup flow that publishes the official remote
  MCP through the existing encrypted MCP configuration API, with
  workspace-default enablement and no connection test during persistence. The
  preset is display-only until configured and never creates a fake database
  record. Configured Tavily cards now reopen the focused management flow: the
  encrypted API key is reported as saved, a blank field preserves it, and a
  newly entered value rotates it without exposing plaintext to the browser.
- `apps/web/components/ui/mcp-server-icon.tsx` and both MCP catalog pages:
  centralized deterministic server icons so Admin and user cards render the
  same identity, including a stable Tavily search mark in its brand blue.
- The generic MCP editor now shows the safe saved URL hint as the input value
  without writing it back over the encrypted URL, aligns the visibility action,
  and uses the same control height for Transport and adjacent inputs.
- `apps/gateway`, `packages/proto`, `apps/agent-runtime`, and
  `deploy/sandbox-runtime/shim/agent_shim.py`: propagated the saved capability
  once per run and excluded `WebSearch`/`WebFetch` from the SDK when disabled;
  Tavily MCP tools remain independent.
- `apps/agent-runtime/cocola_agent_runtime/server.py`: added the fixed Tavily
  MCP egress hostname and a versioned `search -> extract` routing policy only
  when Tavily is effective for the current user. Tool availability continues
  to come from MCP discovery rather than a hard-coded runtime implementation.
- Added regression coverage for defaulting, CRUD round trips, encrypted Tavily
  headers, user opt-out, fail-closed capability reads, protobuf propagation,
  shim tool filtering, prompt ordering, and runtime egress configuration.

The implementation deliberately reuses the existing MCP encryption,
preference, status, and sandbox injection paths. It does not add a Tavily
connector, key table, proxy service, CLI dependency, or save-time network call.
