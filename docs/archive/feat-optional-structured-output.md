# feat: Add optional built-in structured output

- Change time: 2026-07-25 15:42 (+08:00)

## Reason

Structured Result Cards previously required the user to select a Skill with an
explicit result contract. That made the presentation capability unavailable to
ordinary Claude Code conversations even when a summary, table, list, or metrics
card would be materially clearer than prose.

## Changes

- `apps/agent-runtime`: adds an explicit `none | required | optional` result
  policy. Claude Code Execute runs use optional built-in presentation tools,
  while selected Skills with result contracts retain required submission
  semantics. Plan runs and Codex remain disabled.
- `deploy/sandbox-runtime/shim/agent_shim.py`: registers four typed terminal MCP
  tools for optional Summary, Table, List, and Metrics results, validates their
  payloads, and emits the existing immutable `structured_result_ready` event.
- `deploy/sandbox-runtime/skills/cocola-structured-output`: adds a versioned
  platform Skill that explains when structured presentation is clearer than
  normal Markdown and when it should not be used.
- Tests cover policy selection, typed tool registration, valid and invalid
  result submission, Skill precedence, Codex isolation, and platform Skill
  packaging.

No system prompt, Markdown tag protocol, output parser, database migration,
feature flag, or local Sandbox image build was added.
