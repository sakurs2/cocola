# feat: add workspace Feishu connector and Cocola identity

- Change time: 2026-08-21 16:01 (+08:00)

## Reason

Ordinary conversations could not use lark-cli because Feishu application credentials only belonged
to custom Agent bots. At the same time, an empty administrator system prompt allowed the underlying
Claude Code runtime identity to surface when users asked who the assistant was. Cocola needs a
workspace-scoped Feishu capability for everyday chats and a built-in product identity that does not
depend on administrator configuration.

## Changes

- `db/migrations/00065_workspace_feishu_connector.sql`: allow user-owned workspace connectors and
  registration flows with nullable Agent ownership, and add partial uniqueness constraints for
  workspace and Agent scopes.
- `apps/gateway/internal/channel/feishu/`: extend the existing encrypted credential, registration,
  and tenant-token services to workspace connectors while keeping Connector Manager ownership and
  inbound Feishu messaging limited to Agent connectors.
- `apps/gateway/internal/httpapi/`, `internal/chatrun/`, and `internal/convo/`: add fixed authenticated
  workspace Connector APIs, bind new ordinary conversations, lazily bind existing conversations,
  clear bindings on disconnect, and preserve custom Agent isolation.
- `apps/agent-runtime/`: add the versioned Cocola product identity, preserve administrator and Agent
  instruction precedence, and prevent Plan mode from receiving executable Feishu credentials.
- `apps/web/`: add independent GitHub and Feishu summary cards, centered setup/manage dialogs,
  fixed Feishu proxy routes, bilingual copy, compact responsive styling, and administrator guidance
  explaining that the Cocola identity is built in.
- Tests cover workspace/Agent ownership boundaries, credential validation and redaction, conversation
  binding, prompt order, Plan-mode isolation, fixed Web proxy routes, and Connector UI states.

The implementation deliberately reuses `channel_connectors`, encrypted secrets, and short-lived TAT
metadata. It does not add a credential broker, long-lived token store, message WebSocket for workspace
connections, or a second administrator prompt state.
