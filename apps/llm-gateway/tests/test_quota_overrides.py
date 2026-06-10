"""Per-subject quota overrides: store, TTL cache, and enforcer integration."""

import pytest
from cocola_llm_gateway.auth import Identity
from cocola_llm_gateway.quota import (
    Enforcer,
    MemoryOverrideStore,
    MemoryQuotaStore,
    QuotaExceeded,
    QuotaPolicy,
    TTLCachedOverrides,
)

# ---- store ----


async def test_override_store_get_set_delete():
    s = MemoryOverrideStore()
    # None means "no override" (distinct from a 0 override).
    assert await s.get("user", "u1") is None
    await s.set("user", "u1", 500)
    assert await s.get("user", "u1") == 500
    # upsert
    await s.set("user", "u1", 800)
    assert await s.get("user", "u1") == 800
    # 0 is a real value: "explicitly unlimited", not "no override".
    await s.set("user", "u1", 0)
    assert await s.get("user", "u1") == 0
    await s.delete("user", "u1")
    assert await s.get("user", "u1") is None


async def test_override_store_scope_isolation_and_blanks():
    s = MemoryOverrideStore({("user", "u1"): 10, ("tenant", "u1"): 20})
    assert await s.get("user", "u1") == 10
    assert await s.get("tenant", "u1") == 20
    # blank subject is never an override
    assert await s.get("user", "") is None


# ---- ttl cache ----


async def test_ttl_cache_caches_both_hit_and_miss():
    inner = MemoryOverrideStore()
    cache = TTLCachedOverrides(inner, ttl_s=10.0)
    # miss cached
    assert await cache.get("user", "u1", now=0.0) is None
    await inner.set("user", "u1", 100)
    assert await cache.get("user", "u1", now=5.0) is None  # still cached miss
    assert await cache.get("user", "u1", now=11.0) == 100  # refreshed


async def test_ttl_cache_write_invalidates():
    inner = MemoryOverrideStore()
    cache = TTLCachedOverrides(inner, ttl_s=10.0)
    assert await cache.get("user", "u1", now=0.0) is None
    await cache.set("user", "u1", 50)
    assert await cache.get("user", "u1", now=1.0) == 50
    await cache.delete("user", "u1")
    assert await cache.get("user", "u1", now=2.0) is None


# ---- enforcer integration ----


async def test_override_enables_cap_where_default_unlimited():
    # No static cap, but an override caps just this user.
    ov = MemoryOverrideStore({("user", "vip"): 100})
    enf = Enforcer(QuotaPolicy(), MemoryQuotaStore(), overrides=ov)
    capped = Identity(user_id="vip")
    free = Identity(user_id="someone-else")
    await enf.commit(capped, 120, now=0)
    with pytest.raises(QuotaExceeded) as ei:
        await enf.check(capped, now=0)
    assert ei.value.status.limit == 100
    # a user with no override stays unlimited
    await enf.commit(free, 10**9, now=0)
    await enf.check(free, now=0)


async def test_override_supersedes_static_default():
    # Static default 100, override raises this user to 1000.
    ov = MemoryOverrideStore({("user", "u1"): 1000})
    enf = Enforcer(QuotaPolicy(user_daily_tokens=100), MemoryQuotaStore(), overrides=ov)
    ident = Identity(user_id="u1")
    await enf.commit(ident, 500, now=0)  # over the static 100 but under the override
    await enf.check(ident, now=0)  # no raise
    st = (await enf.status(ident, now=0))[0]
    assert st.limit == 1000 and st.used == 500


async def test_override_zero_means_unlimited_for_subject():
    # Static default 100, override 0 lifts the cap for this user entirely.
    ov = MemoryOverrideStore({("user", "u1"): 0})
    enf = Enforcer(QuotaPolicy(user_daily_tokens=100), MemoryQuotaStore(), overrides=ov)
    ident = Identity(user_id="u1")
    await enf.commit(ident, 10**9, now=0)
    await enf.check(ident, now=0)  # uncapped -> no raise
    assert await enf.status(ident, now=0) == []  # unlimited => no status row


async def test_no_override_falls_back_to_static_default():
    ov = MemoryOverrideStore()  # empty
    enf = Enforcer(QuotaPolicy(user_daily_tokens=100), MemoryQuotaStore(), overrides=ov)
    ident = Identity(user_id="u1")
    await enf.commit(ident, 150, now=0)
    with pytest.raises(QuotaExceeded) as ei:
        await enf.check(ident, now=0)
    assert ei.value.status.limit == 100


async def test_tenant_override():
    ov = MemoryOverrideStore({("tenant", "team-a"): 50})
    enf = Enforcer(QuotaPolicy(), MemoryQuotaStore(), overrides=ov)
    ident = Identity(user_id="u1", tenant_id="team-a")
    await enf.commit(ident, 60, now=0)
    with pytest.raises(QuotaExceeded) as ei:
        await enf.check(ident, now=0)
    assert ei.value.status.scope == "tenant" and ei.value.status.limit == 50
