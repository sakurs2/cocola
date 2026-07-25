# feat: Add durable rich conversation nodes

- Change time: 2026-07-25 02:11 (+08:00)

## Reason

Claude Code conversations needed durable, first-class UI nodes for user clarification, terminal Run metadata, and Skill-defined structured output. The previous text and tool stream could not represent a resumable question or reconstruct these results after a Gateway restart or Sandbox recycle.

## Changes

- `db/migrations/00046_conversation_rich_parts.sql`: added durable Question state and the `waiting_input` Run terminal status.
- `apps/gateway/internal/chatrun`, `convo`, and `httpapi`: added Question answer/cancel transactions, session-resume continuation Runs, authoritative Question and Run Summary history reconstruction, and bounded Structured Result persistence.
- `apps/agent-runtime` and `deploy/sandbox-runtime/shim`: generalized the Cocola Run Control MCP server, disabled native `AskUserQuestion`, added terminal Question and Skill result tools, and validated result data against the declared JSON Schema.
- `apps/admin-api`: parsed and normalized versioned Skill result contracts from frontmatter without adding a database column.
- `apps/web`: added reusable Question, Run Summary, and Structured Result renderers for live and readonly conversations, including Composer locking and answer continuation behavior.
- Added focused Go, Python, and Web tests for persistence, idempotency, session resume, renderer limits, and history recovery.
- No feature flag or product configuration was added. The Sandbox image was not built locally; image-level acceptance remains assigned to the existing remote/CI Sandbox workflow.
