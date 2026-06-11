"""Postgres-backed quota counters + a Redis fast-path mirror (M7).

Two pieces:

``PostgresQuotaStore``
    Durable period-windowed counters in the ``quota_counters`` table
    (schema owned by ``db/migrations``; this module only reads/writes). ``add``
    is an atomic ``INSERT ... ON CONFLICT DO UPDATE`` that accumulates
    ``used_tokens`` for a ``(scope, subject, period_key)`` row — the same
    semantics as the Redis store but durable across restarts. There is no TTL:
    old period rows simply stop being read (a future janitor can prune them).

``MirroredQuotaStore``
    Composes a fast path (Redis) over a durable path (Postgres). ``add`` writes
    the durable store FIRST (so the authoritative total never lags), then mirrors
    the increment to the fast path best-effort — a Redis failure logs but never
    loses the count. ``get`` reads the DURABLE total: because durable is written
    first it is always >= the fast path, so reading durable can never
    under-report. This is the correctness anchor across restarts — a Redis flush
    or a cold replica must not silently reset a subject's used budget. Redis
    still absorbs the high-frequency cross-replica increments (the plan's
    "Redis as the counting fast-path that writes through to PG").
"""

from __future__ import annotations

import asyncio

from cocola_common import get_logger
from psycopg_pool import AsyncConnectionPool

from cocola_llm_gateway.quota.store import QuotaStore

log = get_logger("cocola.llm-gateway.quota")

_ADD = """
INSERT INTO quota_counters (scope, subject, period_key, used_tokens, updated_at)
VALUES (%s, %s, %s, %s, now())
ON CONFLICT (scope, subject, period_key)
DO UPDATE SET used_tokens = quota_counters.used_tokens + EXCLUDED.used_tokens,
             updated_at = now()
RETURNING used_tokens
"""

_GET = (
    "SELECT used_tokens FROM quota_counters WHERE scope = %s AND subject = %s AND period_key = %s"
)


class PostgresQuotaStore:
    """Durable token counters over a Postgres connection pool.

    ``ttl_s`` is accepted for interface parity with the Redis store but ignored:
    rows are durable, periods self-retire by key.
    """

    def __init__(self, dsn: str):
        self._pool = AsyncConnectionPool(dsn, open=False, kwargs={"autocommit": True})
        self._lock = asyncio.Lock()
        self._opened = False

    async def _ready(self) -> None:
        if self._opened:
            return
        async with self._lock:
            if not self._opened:
                await self._pool.open()
                self._opened = True

    async def get(self, scope: str, subject: str, period: str) -> int:
        await self._ready()
        async with self._pool.connection() as conn:
            cur = await conn.execute(_GET, (scope, subject, period))
            row = await cur.fetchone()
        return int(row[0]) if row else 0

    async def add(self, scope: str, subject: str, period: str, tokens: int, ttl_s: int) -> int:
        await self._ready()
        async with self._pool.connection() as conn:
            cur = await conn.execute(_ADD, (scope, subject, period, tokens))
            row = await cur.fetchone()
        return int(row[0]) if row else 0

    async def aclose(self) -> None:
        if self._opened:
            await self._pool.close()
            self._opened = False


class MirroredQuotaStore:
    """Redis fast-path mirror over a durable Postgres counter.

    Durable PG is the source of truth. ``add`` increments durable first, then
    mirrors to the fast path best-effort. ``get`` returns the durable total: the
    write order guarantees durable >= fast, so a durable read never
    under-reports a subject's used budget — even right after a Redis flush or on
    a cold replica. (Slight OVER-counting from concurrent in-flight requests is
    already an accepted property of the enforcer; UNDER-counting after a restart
    is not, hence durable-authoritative reads.)
    """

    def __init__(self, fast: QuotaStore, durable: QuotaStore):
        self._fast = fast
        self._durable = durable

    async def get(self, scope: str, subject: str, period: str) -> int:
        return await self._durable.get(scope, subject, period)

    async def add(self, scope: str, subject: str, period: str, tokens: int, ttl_s: int) -> int:
        total = await self._durable.add(scope, subject, period, tokens, ttl_s)
        try:
            await self._fast.add(scope, subject, period, tokens, ttl_s)
        except Exception as e:  # noqa: BLE001 - mirror is best-effort
            log.warning("quota fast-path mirror failed", error=repr(e))
        return total

    async def aclose(self) -> None:
        for st in (self._fast, self._durable):
            try:
                await st.aclose()
            except Exception as e:  # noqa: BLE001
                log.warning("quota store close failed", error=repr(e))
