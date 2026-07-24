# feat: Add Claude Code Plan Mode

- Change time: 2026-07-24 02:24 (+08:00)

## Reason

Cocola needs a first-class planning workflow for Claude Code conversations. Users should be able to request a read-only plan, review the resulting Markdown plan, and explicitly approve execution without losing the Claude session or allowing workspace changes during planning.

## Changes

- `packages/proto/cocola/agent/v1/agent.proto` and generated clients: added the public interaction mode and project workspace revision fields.
- `db/migrations/00045_conversation_plans.sql`: added durable versioned plans, run interaction modes, plan ownership, lifecycle constraints, and the single-current-plan invariant.
- `apps/gateway/internal/`: added Plan request validation, durable plan creation, authoritative history hydration, approval, cancellation, idempotent execution, status transitions, model checks, and project workspace revision guards.
- `apps/agent-runtime/` and `deploy/sandbox-runtime/shim/`: mapped Plan Mode to Claude's native plan permission mode, suppressed `ExitPlanMode` protocol noise, parsed bounded `<cocola_plan>` output, preserved clarification turns, reused the Claude session for execution, and disabled planning-time side effects.
- `apps/web/`: added the Execute/Plan selector, Plan Mode guidance, plan-aware streaming and history restoration, review cards, approval and cancellation actions, revision flows, and execute-only progress docking.
- `deploy/sandbox-runtime/Dockerfile`: pinned the currently verified Claude Agent SDK version without upgrading it.
- `scripts/sandbox-runtime-verify.sh`: added an optional credentialed end-to-end acceptance flow that verifies read-only planning, session reuse, approved execution, file changes, and test execution.
- Tests cover mode validation, plan parsing, versioning, state transitions, idempotency, history hydration, workspace drift, session reuse, side-effect isolation, UI state, and English product copy.

## Review hardening

- Plan runs now disable filesystem settings and enforce an empty MCP configuration in the in-sandbox Claude SDK options.
- Approved execution requires the exact Claude session that produced the plan and fails instead of silently starting a fresh session.
- Normal run starts and plan approval workspace validation share a conversation-scoped gate, closing the validation-to-start race.
- Web approval retries reuse one idempotency key with a finite retry budget, including a later retry after an uncertain response.
- Stopping approved execution keeps the run stream connected until the authoritative `stopped` plan status arrives.

## Notes

- Plan Mode is a built-in capability with no environment variable, product configuration, experiment flag, or rollout switch.
- Execute remains the default interaction mode.
- Scheduled Tasks and non-Claude Code runtimes reject Plan Mode.
- Plan content is excluded from traces and logs.
