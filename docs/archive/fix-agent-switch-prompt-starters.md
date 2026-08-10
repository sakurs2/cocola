# fix: keep Prompt Starters visible when switching Agents

- Change time: 2026-08-10 11:54 (+08:00)

## Reason

Selecting any Agent in a new conversation immediately removed the four global
Prompt Starter controls below the composer. This was caused by an explicit
legacy condition that replaced the starter list with an empty array whenever
an Agent was selected. The starters are general input helpers and do not depend
on the selected Agent, so changing the executor should not remove them or shift
the welcome layout.

## Changes

- `apps/web/components/assistant-ui/thread.tsx`: render the global Prompt
  Starters for both standard chat and Agent chat; Agent selection continues to
  affect execution only.
- `apps/web/lib/agent-capabilities.test.mjs` and
  `apps/web/lib/chat-message-layout.test.mjs`: replace the legacy hidden-state
  assertion with regression coverage for stable Prompt Starter visibility.

No Agent-specific prompt configuration or additional loading state was added.
