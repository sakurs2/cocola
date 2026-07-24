# feat: Strengthen the Plan approval experience

- Change time: 2026-07-24 23:55 (+08:00)

## Reason

The existing Plan Card looked like a generic content container. Its status was
rendered as quiet secondary text, all actions had similar visual weight, and
the surrounding conversation did not clearly communicate that Plan Mode keeps
the workspace read-only until the user approves execution.

## Changes

- `apps/web/components/assistant-ui/plan-card.tsx`: turns the Plan Card into a
  state-aware approval gate with an accent seam, semantic status badges,
  explicit consequence copy, stronger primary actions, responsive controls,
  terminal-state collapsing, and accessible status announcements.
- `apps/web/components/assistant-ui/plan-card.tsx`: moves Copy and Cancel into a
  bounded overflow menu, adds pending feedback, and prevents stale approval or
  cancellation while a revision is in progress.
- `apps/web/components/assistant-ui/thread.tsx`: strengthens Plan Mode
  continuity across the composer, model toolbar, assistant header, and plan
  revision context without adding decorative motion.
- `apps/web/lib/plan-mode.mjs` and `apps/web/lib/plan-mode.test.mjs`: centralize
  the new English product copy, expose typed composer context, and make unknown
  Plan statuses fail closed as Failed rather than exposing approval.

## Design notes

- Indigo is reserved for the Plan workflow and approval action; lifecycle
  colors communicate Completed, Stopped, and Failed states.
- Ready and active plans remain expanded for review. Historical Completed,
  Superseded, and Cancelled plans start collapsed and can be reopened.
- Plan Markdown uses the conversation's natural scroll instead of a nested
  scroll region.
