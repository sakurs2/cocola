# Core chat reliability

The core chat path is a single-process background Run with a durable latest
snapshot. It supports long answers and browser reconnects without introducing a
distributed job system.

## HTTP contract

### Start or retry a Run

`POST /v1/chat` keeps the existing SSE response. The request additionally
accepts `client_request_id`; retrying the same id for the same user and
conversation returns the existing Run. A different request while that
conversation is running returns HTTP 409 with `run_id`.

The response header `x-cocola-run-id` identifies the Run. Every subscription
begins with:

```text
event: snapshot
data: {"kind":"snapshot","data":{"parts":"[...]","status":"running"}}
```

The snapshot replaces the local in-flight assistant parts. Subsequent normal
Agent events are incremental. `done` is the only terminal SSE event; its
`status` is one of `success`, `error`, `cancelled`, or `interrupted`. An SSE
comment ping is sent every 15 seconds while idle.

### Reconnect, discover, and cancel

- `GET /v1/chat/runs/{run_id}`: owner-checked snapshot followed by live events;
  a terminal Run returns snapshot plus `done` immediately.
- `GET /v1/chat/runs/active?conversation_id={id}`: returns the owner-checked
  active Run, or 404.
- `DELETE /v1/chat/runs/{run_id}`: explicit user cancellation. Closing an SSE
  connection does not call this endpoint and does not cancel execution.
- `DELETE /v1/conversations/{id}`: returns 409 while the conversation has a
  running Run. Stop is explicit; deletion is allowed only after the Run reaches
  a terminal state, so it cannot race the final assistant-message transaction.

## Failure semantics

- Browser/network/proxy disconnect: background Run continues; reconnect uses a
  complete snapshot, so repeated reconnects do not duplicate content.
- Agent business error: Run ends as `error`; a later provider `done` cannot
  overwrite it.
- User Stop: Run ends as `cancelled` and keeps any partial assistant message.
- Gateway or Agent Runtime shutdown/restart: Run ends as `interrupted`; latest
  persisted partial output remains visible. The Run is not replayed.
- PostgreSQL write outage: drafts retry for up to 30 seconds. Past that budget,
  the Agent is stopped to avoid an unrecoverable successful-looking answer.
- Final Run persistence makes at most four three-second normal attempts,
  followed by one three-second, message-free `interrupted` fallback. A total
  database outage leaves readiness failed and startup recovery closes any
  remaining stale `running` row.
- The browser makes at most eight attempts to start an idempotent Run and at
  most twenty reconnect attempts for one subscription. Exhaustion is visible to
  the user; refreshing can rediscover an active server-side Run.

## Defaults

| Setting                    |             Default |
| -------------------------- | ------------------: |
| Agent Run timeout          |              3600 s |
| Sandbox token TTL          | Run timeout + 900 s |
| Model call timeout         |               300 s |
| Sandbox heartbeat          |                20 s |
| SSE ping                   |                15 s |
| Text/thinking merge window |              100 ms |
| Assistant draft interval   |                 1 s |
| Graceful shutdown budget   |                30 s |

`make dev` and the full compose stack enable this behavior by default. There is
no rollout feature flag.
