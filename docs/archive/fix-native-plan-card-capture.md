# fix: Capture native Claude plans for Plan Cards

- Change time: 2026-07-24 18:02 (+08:00)

## Reason

Claude Code Agent SDK returns a completed native plan in the `plan` input of
the `ExitPlanMode` tool. The Sandbox shim suppressed that tool for Cocola's
approval workflow but discarded its input, so the run completed as ordinary
assistant text. Gateway therefore never received `plan_ready`, no
`conversation_plans` row was created, and the Web UI could not render a
`Plan vN` Card.

## Changes

- `deploy/sandbox-runtime/shim/agent_shim.py`: capture the latest non-empty
  native `ExitPlanMode.input.plan`, validate it with the existing 128 KiB
  limit, and emit `plan_ready` while preserving tagged-plan and clarification
  behavior.
- `apps/agent-runtime/tests/test_agent_shim_mcp.py`: cover native plan
  extraction, repeated native plan completion, oversized payload rejection,
  tagged plans, and clarification text.
- The fix does not build a local Sandbox image. A published Sandbox Runtime
  image containing this shim change is required before live Sandboxes use it.
