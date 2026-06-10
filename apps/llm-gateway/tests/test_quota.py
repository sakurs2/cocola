"""Quota: policy windows, store counters, and the enforcer gate/commit."""

import pytest
from cocola_llm_gateway.auth import Identity
from cocola_llm_gateway.quota import (
    Enforcer,
    MemoryQuotaStore,
    QuotaExceeded,
    QuotaPolicy,
    day_window,
    month_window,
)
from cocola_llm_gateway.quota.policy import QuotaStatus


def test_policy_enabled_flags():
    assert not QuotaPolicy().any_enabled
    assert QuotaPolicy(user_daily_tokens=10).user_enabled
    assert QuotaPolicy(tenant_monthly_tokens=10).tenant_enabled
    assert not QuotaPolicy(user_daily_tokens=0).user_enabled


def test_day_window_shape():
    # 2026-06-09T04:00:00Z
    period, ttl = day_window(now=1780977600.0)
    assert period == "20260609"
    assert ttl > 0


def test_month_window_shape():
    period, ttl = month_window(now=1780977600.0)
    assert period == "202606"
    assert ttl > 0


def test_quota_status_math():
    st = QuotaStatus("user", "u1", "20260609", used=80, limit=100)
    assert st.remaining == 20
    assert not st.exceeded
    full = QuotaStatus("user", "u1", "20260609", used=100, limit=100)
    assert full.exceeded and full.remaining == 0
    unlimited = QuotaStatus("user", "u1", "20260609", used=999, limit=0)
    assert unlimited.unlimited and not unlimited.exceeded and unlimited.remaining == -1


async def test_memory_store_add_and_get():
    s = MemoryQuotaStore()
    assert await s.get("user", "u1", "p") == 0
    assert await s.add("user", "u1", "p", 10, 60) == 10
    assert await s.add("user", "u1", "p", 5, 60) == 15
    assert await s.get("user", "u1", "p") == 15
    # different period is isolated
    assert await s.get("user", "u1", "p2") == 0


async def test_enforcer_disabled_is_noop():
    enf = Enforcer(QuotaPolicy(), MemoryQuotaStore())
    ident = Identity(user_id="u1", tenant_id="t1")
    await enf.check(ident)  # no raise
    await enf.commit(ident, 10**9)  # no raise
    assert await enf.status(ident) == []


async def test_enforcer_user_daily_blocks_after_cap():
    enf = Enforcer(QuotaPolicy(user_daily_tokens=100), MemoryQuotaStore())
    ident = Identity(user_id="u1")
    await enf.check(ident)
    await enf.commit(ident, 60)
    await enf.check(ident)  # 60 < 100 still ok
    await enf.commit(ident, 50)  # now 110
    with pytest.raises(QuotaExceeded) as ei:
        await enf.check(ident)
    assert ei.value.status.scope == "user"
    assert ei.value.status.used == 110


async def test_enforcer_tenant_monthly_blocks():
    enf = Enforcer(QuotaPolicy(tenant_monthly_tokens=50), MemoryQuotaStore())
    ident = Identity(user_id="u1", tenant_id="t1")
    await enf.commit(ident, 60)
    with pytest.raises(QuotaExceeded) as ei:
        await enf.check(ident)
    assert ei.value.status.scope == "tenant"


async def test_enforcer_commit_zero_is_noop():
    store = MemoryQuotaStore()
    enf = Enforcer(QuotaPolicy(user_daily_tokens=100), store)
    ident = Identity(user_id="u1")
    await enf.commit(ident, 0)
    assert await store.get("user", "u1", day_window()[0]) == 0


async def test_enforcer_user_under_cap_passes_repeatedly():
    enf = Enforcer(QuotaPolicy(user_daily_tokens=1000), MemoryQuotaStore())
    ident = Identity(user_id="u1")
    for _ in range(5):
        await enf.check(ident)
        await enf.commit(ident, 100)
    statuses = await enf.status(ident)
    assert statuses[0].used == 500
    assert statuses[0].remaining == 500


async def test_enforcer_commit_failure_swallowed():
    class BoomStore(MemoryQuotaStore):
        async def add(self, *a, **k):
            raise RuntimeError("redis down")

    enf = Enforcer(QuotaPolicy(user_daily_tokens=100), BoomStore())
    # commit must not raise even though the store add explodes
    await enf.commit(Identity(user_id="u1"), 10)
