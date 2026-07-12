# ADR-0019: Single-Gateway reconnectable chat runs

- Status: Accepted
- Date: 2026-07-12
- Deciders: cocola maintainers

This ADR also supersedes ADR-0016's decision to remove Warm Pool. The current
OpenSandbox implementation restores session state from MinIO checkpoints after
claim, so pre-warming does not require hot-mounting a session volume.

## Context

The MVP tied an Agent query to the browser's `POST /v1/chat` request. A browser
refresh, proxy idle timeout, or network interruption therefore cancelled work
that might already have been running for tens of minutes. A Gateway restart
could also leave the audit row permanently `running`. Sandbox Exec and model
timeouts were shorter than the intended Agent run, and an active sandbox did
not receive lease heartbeats.

cocola is deployed as one logical Gateway. High availability, cross-process
execution takeover, and replaying tool side effects are not requirements. The
design should prefer a small number of explicit states over distributed worker
coordination.

## Decision

Interactive chat execution is owned by one Gateway process and detached from
the HTTP subscription:

- PostgreSQL stores one `conversation_runs` row, the user message, and one
  deterministic assistant draft/final message per Run. No event log, worker
  lease, queue, or task-claim protocol is added.
- `POST /v1/chat` creates the Run atomically and then subscribes to the local
  background execution. `client_request_id` makes POST retries idempotent; a
  partial unique index permits only one `running` Run per conversation.
- A reconnect receives a complete assistant snapshot and then live incremental
  events. The browser stores only `run_id`, conversation id, and assistant id
  in `sessionStorage`. SSE comments keep idle proxy connections alive.
- Browser disconnect only removes a subscriber. Explicit Stop calls the Run
  cancel endpoint, which cancels gRPC and the underlying sandbox Exec stream.
- The assistant draft is upserted once per second. Final assistant content and
  the terminal Run state are committed in one PostgreSQL transaction. Terminal
  states are `success`, `error`, `cancelled`, and `interrupted` and never move
  backwards.
- On graceful shutdown the Gateway stops accepting Runs, cancels local work,
  and records `interrupted`. On startup, any stale `running` row is also changed
  to `interrupted`; execution is never replayed automatically.
- One Agent Run defaults to 3600 seconds, its sandbox token defaults to the Run
  timeout plus 15 minutes, one model call defaults to 300 seconds, and the
  active sandbox receives a heartbeat every 20 seconds. The shim Exec uses the
  Run timeout explicitly instead of the provider's five-minute default.
- Conversation, SessionMap, and sandbox reuse are owner-scoped. A caller-pinned
  sandbox id cannot bypass the owner-scoped binder.

Warm Pool remains compatible: a claimed sandbox follows the same `reused=false`
checkpoint-restore path as a cold sandbox, and active claimed sandboxes receive
the same heartbeat.

## Alternatives Considered

- **Keep request-bound execution** — simplest implementation, but normal browser
  and proxy failures destroy long-running work and conflate navigation with
  cancellation.
- **Persist every token/event** — enables exact event replay, but adds sequence
  allocation, retention, polling, and more intermediate states. A latest
  snapshot is sufficient for a single Gateway.
- **Distributed worker lease and execution takeover** — could improve
  availability, but Claude sessions and tool side effects cannot be safely
  replayed without a separate idempotency protocol. This is outside cocola's
  current requirements.

## Consequences

- A browser can refresh or reconnect without cancelling a long Agent task.
- Process failure has one honest result, `interrupted`, with the latest saved
  partial answer; it does not leave a permanent `running` row or duplicate tool
  calls.
- Gateway memory is bounded by one reducer and bounded subscriber buffers per
  active Run. Text and thinking events are merged in 100 ms windows.
- A Gateway restart does not continue execution. This is an intentional
  availability tradeoff in exchange for a smaller, auditable state machine.
- PostgreSQL is part of the reliability boundary. If draft persistence remains
  unavailable for 30 seconds, the Agent is stopped instead of producing output
  that cannot be recovered.
