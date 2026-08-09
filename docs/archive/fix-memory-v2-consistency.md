# fix: harden Memory V2 consistency and passive defaults

- Change time: 2026-08-09 17:10 (+08:00)

## Reason

The initial Memory V2 implementation had several correctness and maintenance risks: a clear operation could race with an in-flight OpenViking commit, the vector-index lock did not cover the model's semantic identity, Reset had no persistent barrier or resumable progress, passive Admin reads executed embedding readiness checks, and completed capture jobs accumulated indefinitely. The OpenViking configuration file also shared a persistent mount with runtime data, so recreated deployments could keep stale configuration.

## Changes

- `apps/gateway/internal/memory/`: added a recoverable cancellation intent around capture submission, bounded clear and provider-task polling, retryable transient database failures, category-scoped listing, and bounded terminal-job retention.
- `apps/admin-api/internal/service/` and `apps/admin-api/internal/store/`: locked embedding route, provider, model, endpoint, and dimension; added a persistent Reset barrier with per-account checkpoints and bounded parallel deletion; made configuration reads passive; and separated model selection from enablement.
- `db/migrations/00063_memory_v2_consistency.sql`: added cancellation and Reset state, the full embedding identity lock, progress records, and partial indexes. Memory remains an unreleased incompatible capability, so the migration intentionally resets preview Memory data and configuration.
- `deploy/docker-compose/docker-compose.dev.yml`, `apps/cli/internal/assets/compose.yaml`, and `apps/llm-gateway/`: moved OpenViking runtime data to a dedicated data-only volume, rotated the preview object prefix, made the development LLM port configurable, and deferred OpenViking initialization until a passive authenticated status endpoint reports an embedding route. The bounded bootstrap polling uses exponential backoff and never executes a model call, so Cocola startup remains healthy while Memory is disabled.
- `apps/web/app/admin/toolbox/memory-tool.tsx` and `apps/web/components/profile/memory-panel.tsx`: disabled mutable controls during operations, implemented explicit save-then-enable behavior, made Reset resumable, cancelled stale category requests, and added cursor pagination.
- Regression tests cover passive disabled startup, semantic embedding locks, bounded running tasks, transient capture failures, UI request cancellation, and paging.

The implementation deliberately adds no new worker service, message queue, or cache. PostgreSQL stores only durable intent and recovery checkpoints; OpenViking remains authoritative for memory content and provider tasks.
