# fix: Make rich conversation lifecycle state authoritative

- Change time: 2026-07-25 03:25 (+08:00)

## Reason

Question answer Runs could consume a pending Question even when Claude rejected session startup, readonly history bypassed the live rich-part normalization path, and the Web client exposed a cancellable running state before it had a durable Run ID. Question and Structured Result behavior was also being requested through injected system prompts instead of being enforced at the runtime protocol boundary.

## Changes

- `deploy/sandbox-runtime/shim/agent_shim.py`: removed prompt-dependent rich-node behavior, added Claude SDK tool lifecycle hooks for terminal Run Control isolation, disabled native `AskUserQuestion` for every Claude Run, and added a `run_accepted` event backed by the SDK initialization message.
- `apps/agent-runtime/cocola_agent_runtime`: removed Question and Structured Result system prompt injection and forwarded runtime acceptance without treating it as assistant content.
- `apps/gateway/internal/chatrun` and `apps/gateway/internal/httpapi`: added an explicit Question answer acceptance transition; Runs that fail before acceptance now restore the Question to `pending`, while accepted Runs remain non-repeatable.
- `apps/web/lib/rich-message-normalization.ts` and `apps/web/components/conversation-readonly.tsx`: made live and readonly rich nodes share the same bounded wire normalization.
- `apps/web/app/runtime-provider.tsx`: delayed the running state until a durable Run ID exists and reconciled interrupted answer startup against authoritative history and the active Run endpoint.
- Added focused Python, Go, and Node tests for tool isolation, runtime acceptance, Question rollback, optionless history, snake-case answers, and zero-valued Run summaries.
- No Sandbox image was built locally.
