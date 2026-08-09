# feat: upgrade user memory to OpenViking Memory V2

- Change time: 2026-08-09 16:00 (+08:00)

## Reason

The previous Memory integration duplicated OpenViking's internal recovery and
retrieval responsibilities. It inferred task state from archive files, rebuilt
sessions after ambiguous failures, queried profile and recalled memories through
separate paths, and exposed an incomplete user experience. Those overlapping
state machines made failures difficult to diagnose and increased long-term
maintenance cost.

The product only needs durable personal memory for profile, preferences,
entities, and events. OpenViking v0.4.12 already provides the session, task,
transaction, and Memory V2 primitives needed for that bounded feature.

## Changes

- `apps/gateway/internal/memory/`: use the pinned OpenViking SDK for one bounded
  memory search, deterministic two-message sessions, durable task adoption,
  finite retries, epoch-safe clearing, and fail-open chat recall.
- `db/migrations/00062_memory_v2.sql`: reset incompatible Memory data and reduce
  capture jobs to provider session/task identifiers and the new terminal states.
- `apps/admin-api/`: validate extraction and embedding routes before enablement,
  lock the initialized embedding route and dimension, expose compact health and
  queue status, and provide an idempotent full reset flow.
- `apps/web/`: restore compact HeroUI Admin and user Memory surfaces with central
  confirmations, explicit degraded states, the two user switches, and the four
  supported memory categories.
- `apps/cli/`, `deploy/docker-compose/`, and `scripts/run-stack.sh`: pin
  OpenViking v0.4.12 by digest, isolate its V2 state, keep production networking
  internal, bind only the development port, expose internal Prometheus metrics,
  and separate process liveness from model-dependent readiness.
- `apps/llm-gateway/`: keep extraction and embedding credentials in Cocola and
  expose only fixed internal model aliases to OpenViking.

## Tradeoffs

- OpenViking's stable v0.4.12 Search API returns structured memory results but
  does not expose the proposed rendered-context request fields. Cocola therefore
  keeps one small deterministic renderer with strict item, score, and byte
  limits instead of depending on undocumented request parameters.
- Memory data is intentionally reset rather than migrated. Conversation,
  Project, Agent, and other product data are unchanged.
- The Gateway retains a small PostgreSQL intent queue so final answers never
  wait for memory extraction; OpenViking remains authoritative for its accepted
  tasks and internal crash recovery.
- OpenViking `/ready` exercises the configured embedding route, so the container
  defers starting until Cocola reports that an embedding route is configured.
  The bootstrap check is passive and does not execute or bill a model call;
  Admin still requires `/ready` before enabling Memory.
