# fix: align Web source-contract tests with the single-runtime UI

- Change time: 2026-08-12 10:48 (+08:00)

## Reason

The Web CI failed after the Codex and OpenAI Responses removal because two
source-contract tests still matched implementation details that had already
been intentionally replaced. The confirmation-dialog test expected the old
loading-label class expression, while the Project task test still required the
removed runtime-picker flag and setter flow.

## Changes

- `apps/web/lib/destructive-confirmation-ui.test.mjs`: verify the current
  centered inline-grid loader contract without depending on the obsolete JSX
  attribute shape.
- `apps/web/lib/project-task-ui.test.mjs`: verify that Project tasks resolve
  the configured single runtime and that runtime-picker state is absent.
- Kept production components unchanged because their current behavior is the
  intended product behavior; only stale source-contract assertions changed.
