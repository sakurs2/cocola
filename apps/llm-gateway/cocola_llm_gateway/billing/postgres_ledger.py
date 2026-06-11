"""Postgres-backed ledger — the durable accounting source of truth (M7).

When ``COCOLA_PG_DSN`` is set, usage records land in the ``usage_ledger`` table
(schema owned by ``db/migrations`` and applied by admin-api's goose migrator;
this module never defines schema, it only reads/writes). Records are the billing
truth, so unlike the Redis ledger there is no TTL: rows persist across restarts.

Aggregates are computed on read with ``SUM``/``COUNT`` rather than maintained as
separate counters: the ``/v1/usage`` aggregate surface is a cold debug path, so
a grouped scan keyed by the indexed ``user_id``/``session_id`` columns is simpler
and avoids a second write that could drift from the row truth.

Connection pooling uses ``psycopg_pool.AsyncConnectionPool``. The pool is opened
lazily on first use so the synchronous composition root (bootstrap) can construct
the backend without a running event loop.
"""

from __future__ import annotations

import asyncio

from psycopg.rows import dict_row
from psycopg_pool import AsyncConnectionPool

from cocola_llm_gateway.billing.ledger import Aggregate, UsageRecord

_INSERT = """
INSERT INTO usage_ledger
    (request_id, ts, user_id, session_id, alias, real_model, provider,
     prompt_tokens, completion_tokens, cost_usd, status, error)
VALUES (%s, to_timestamp(%s), %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
ON CONFLICT (request_id) DO NOTHING
"""

_SELECT_COLS = (
    "request_id, extract(epoch FROM ts) AS ts_unix, user_id, session_id, "
    "alias, real_model, provider, prompt_tokens, completion_tokens, "
    "cost_usd, status, error"
)

_AGG = (
    "SELECT count(*) AS calls, "
    "COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, "
    "COALESCE(SUM(completion_tokens), 0) AS completion_tokens, "
    "COALESCE(SUM(cost_usd), 0) AS cost_usd "
    "FROM usage_ledger WHERE {col} = %s"
)


def _rec_from_row(r: dict) -> UsageRecord:
    return UsageRecord(
        request_id=r["request_id"],
        user_id=r["user_id"],
        session_id=r["session_id"],
        alias=r["alias"],
        real_model=r["real_model"],
        provider=r["provider"],
        prompt_tokens=int(r["prompt_tokens"]),
        completion_tokens=int(r["completion_tokens"]),
        cost_usd=float(r["cost_usd"]),
        ts_unix=float(r["ts_unix"]),
        status=r["status"],
        error=r["error"],
    )


class PostgresLedger:
    """Durable ledger over a Postgres connection pool."""

    def __init__(self, dsn: str):
        self._pool = AsyncConnectionPool(
            dsn,
            open=False,
            kwargs={"autocommit": True, "row_factory": dict_row},
        )
        self._lock = asyncio.Lock()
        self._opened = False

    async def _ready(self) -> None:
        if self._opened:
            return
        async with self._lock:
            if not self._opened:
                await self._pool.open()
                self._opened = True

    async def record(self, rec: UsageRecord) -> None:
        await self._ready()
        async with self._pool.connection() as conn:
            await conn.execute(
                _INSERT,
                (
                    rec.request_id,
                    rec.ts_unix,
                    rec.user_id,
                    rec.session_id,
                    rec.alias,
                    rec.real_model,
                    rec.provider,
                    rec.prompt_tokens,
                    rec.completion_tokens,
                    rec.cost_usd,
                    rec.status,
                    rec.error,
                ),
            )

    async def recent(self, *, user_id: str = "", limit: int = 50) -> list[UsageRecord]:
        await self._ready()
        sql = f"SELECT {_SELECT_COLS} FROM usage_ledger"
        params: list = []
        if user_id:
            sql += " WHERE user_id = %s"
            params.append(user_id)
        sql += " ORDER BY ts DESC LIMIT %s"
        params.append(max(0, limit))
        async with self._pool.connection() as conn:
            cur = await conn.execute(sql, params)
            rows = await cur.fetchall()
        return [_rec_from_row(r) for r in rows]

    async def _aggregate(self, col: str, value: str) -> Aggregate:
        await self._ready()
        async with self._pool.connection() as conn:
            cur = await conn.execute(_AGG.format(col=col), (value,))
            r = await cur.fetchone()
        if r is None:
            return Aggregate()
        return Aggregate(
            calls=int(r["calls"]),
            prompt_tokens=int(r["prompt_tokens"]),
            completion_tokens=int(r["completion_tokens"]),
            cost_usd=float(r["cost_usd"]),
        )

    async def aggregate_user(self, user_id: str) -> Aggregate:
        return await self._aggregate("user_id", user_id)

    async def aggregate_session(self, session_id: str) -> Aggregate:
        return await self._aggregate("session_id", session_id)

    async def aclose(self) -> None:
        if self._opened:
            await self._pool.close()
            self._opened = False
