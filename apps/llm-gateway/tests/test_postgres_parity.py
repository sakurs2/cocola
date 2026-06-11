"""Postgres parity tests for the M7 durable ledger + quota counters.

These run only when ``COCOLA_TEST_PG_DSN`` points at a reachable Postgres that
already has the cocola schema applied (goose). They assert the Postgres backends
behave identically to the in-memory ones the rest of the suite uses, so the
``Ledger`` / ``QuotaStore`` Protocol contracts hold across both.

Bring up a throwaway PG and apply the schema, then run:

    docker run --rm -d --name cocola_pgtest -p 55432:5432 \
        -e POSTGRES_USER=cocola -e POSTGRES_PASSWORD=cocola_dev_pw \
        -e POSTGRES_DB=cocola postgres:16-alpine
    # apply db/migrations via admin-api goose (or psql -f), then:
    COCOLA_TEST_PG_DSN='postgres://cocola:cocola_dev_pw@localhost:55432/cocola' \
        uv run pytest tests/test_postgres_parity.py -v
"""

from __future__ import annotations

import os

import pytest
from cocola_llm_gateway.billing.ledger import UsageRecord
from cocola_llm_gateway.billing.memory import MemoryLedger
from cocola_llm_gateway.billing.postgres_ledger import PostgresLedger
from cocola_llm_gateway.quota.postgres_store import MirroredQuotaStore, PostgresQuotaStore
from cocola_llm_gateway.quota.store import MemoryQuotaStore

_DSN = os.getenv("COCOLA_TEST_PG_DSN", "").strip()
pg_only = pytest.mark.skipif(not _DSN, reason="COCOLA_TEST_PG_DSN not set")


def _rec(rid: str, user: str, session: str, p: int, c: int, cost: float) -> UsageRecord:
    return UsageRecord(
        request_id=rid,
        user_id=user,
        session_id=session,
        alias="default",
        real_model="fake-1",
        provider="fake",
        prompt_tokens=p,
        completion_tokens=c,
        cost_usd=cost,
    )


async def _truncate() -> None:
    import psycopg

    async with await psycopg.AsyncConnection.connect(_DSN, autocommit=True) as conn:
        await conn.execute("TRUNCATE usage_ledger, quota_counters")


async def _ledger_contract(led) -> None:
    await led.record(_rec("r1", "U1", "S1", 3, 6, 0.0045))
    await led.record(_rec("r2", "U1", "S1", 1, 2, 0.001))
    await led.record(_rec("r3", "U2", "S2", 10, 20, 0.03))
    # idempotent on duplicate request_id (PG ON CONFLICT; memory appends, so skip
    # the dup assertion for memory by only re-recording on stores that dedupe).

    recent_u1 = await led.recent(user_id="U1", limit=10)
    assert len(recent_u1) == 2
    assert [r.request_id for r in recent_u1] == ["r2", "r1"]  # newest first

    recent_all = await led.recent(limit=10)
    assert len(recent_all) == 3

    au = await led.aggregate_user("U1")
    assert au.calls == 2
    assert au.prompt_tokens == 4
    assert au.completion_tokens == 8
    assert au.cost_usd == pytest.approx(0.0055)

    asn = await led.aggregate_session("S2")
    assert asn.calls == 1
    assert asn.total_tokens == 30


async def _quota_contract(store) -> None:
    assert await store.get("user", "U1", "20260611") == 0
    assert await store.add("user", "U1", "20260611", 100, 86400) == 100
    assert await store.add("user", "U1", "20260611", 50, 86400) == 150
    assert await store.get("user", "U1", "20260611") == 150
    # separate period is independent
    assert await store.get("user", "U1", "20260612") == 0
    # separate subject is independent
    assert await store.add("tenant", "T1", "202606", 7, 86400) == 7
    assert await store.get("tenant", "T1", "202606") == 7


async def test_memory_ledger_contract():
    await _ledger_contract(MemoryLedger())


async def test_memory_quota_contract():
    await _quota_contract(MemoryQuotaStore())


@pg_only
async def test_postgres_ledger_parity():
    await _truncate()
    led = PostgresLedger(_DSN)
    try:
        await _ledger_contract(led)
        # PG-specific: duplicate request_id is a no-op (idempotent ingest).
        await led.record(_rec("r1", "U1", "S1", 999, 999, 9.9))
        au = await led.aggregate_user("U1")
        assert au.calls == 2  # unchanged by the duplicate
    finally:
        await led.aclose()


@pg_only
async def test_postgres_quota_parity():
    await _truncate()
    store = PostgresQuotaStore(_DSN)
    try:
        await _quota_contract(store)
    finally:
        await store.aclose()


@pg_only
async def test_mirrored_quota_reads_durable_truth():
    """Durable PG is authoritative: a cold/flushed fast path must not under-report."""
    await _truncate()
    durable = PostgresQuotaStore(_DSN)
    try:
        # seed durable directly (a count written before this replica started)
        assert await durable.add("user", "U9", "20260611", 42, 86400) == 42
        fast = MemoryQuotaStore()  # cold: knows nothing about U9
        mirror = MirroredQuotaStore(fast, durable)
        # get reads the durable total (not the cold fast path)
        assert await mirror.get("user", "U9", "20260611") == 42
        # add increments durable first, mirrors to fast best-effort
        assert await mirror.add("user", "U9", "20260611", 8, 86400) == 50
        assert await fast.get("user", "U9", "20260611") == 8  # fast got the delta
        # get still returns the durable truth, never the partial fast count
        assert await mirror.get("user", "U9", "20260611") == 50
        assert await durable.get("user", "U9", "20260611") == 50
    finally:
        await durable.aclose()
