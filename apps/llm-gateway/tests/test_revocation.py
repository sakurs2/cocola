"""Revocation denylist: store impls, TTL cache, and the gateway gate."""

import httpx

from cocola_llm_gateway.auth import MemoryRevocationStore, TTLCachedRevocation
from cocola_llm_gateway.server import create_app
from tests.conftest import auth_pair, build_service

_MSG = {
    "model": "default",
    "max_tokens": 32,
    "stream": False,
    "messages": [{"role": "user", "content": "hi"}],
}


def _client(app):
    return httpx.AsyncClient(
        transport=httpx.ASGITransport(app=app), base_url="http://t"
    )


# ---- store ----


async def test_memory_store_revoke_and_check():
    s = MemoryRevocationStore()
    assert await s.is_revoked("abc") is False
    await s.revoke("abc")
    assert await s.is_revoked("abc") is True
    # idempotent + blank ids are never revoked
    await s.revoke("abc")
    assert await s.is_revoked("") is False


async def test_memory_store_seeded():
    s = MemoryRevocationStore({"x", "y"})
    assert await s.is_revoked("x") is True
    assert await s.is_revoked("z") is False


# ---- ttl cache ----


async def test_ttl_cache_serves_from_cache_then_refreshes():
    inner = MemoryRevocationStore()
    cache = TTLCachedRevocation(inner, ttl_s=10.0)
    # Negative result cached at t=0.
    assert await cache.is_revoked("k", now=0.0) is False
    # Backend revokes, but the cached negative answer persists within the TTL.
    await inner.revoke("k")
    assert await cache.is_revoked("k", now=5.0) is False
    # After the TTL window the cache refreshes and sees the revocation.
    assert await cache.is_revoked("k", now=11.0) is True


async def test_ttl_cache_revoke_invalidates_entry():
    inner = MemoryRevocationStore()
    cache = TTLCachedRevocation(inner, ttl_s=10.0)
    assert await cache.is_revoked("k", now=0.0) is False
    # Revoking through the cache drops the stale negative entry immediately.
    await cache.revoke("k")
    assert await cache.is_revoked("k", now=1.0) is True


# ---- gateway gate ----


async def test_revoked_token_rejected_on_messages():
    svc, _ = build_service(reply="x")
    iss, vrf = auth_pair()
    tok = iss.issue("emp-1")
    jti = vrf.verify(tok).token_id
    deny = MemoryRevocationStore({jti})
    app = create_app(svc, verifier=vrf, revocation=deny)
    async with _client(app) as c:
        r = await c.post("/v1/messages", json=_MSG, headers={"x-api-key": tok})
        assert r.status_code == 401
        assert r.json()["error"]["type"] == "authentication_error"
        assert "revoked" in r.json()["error"]["message"]


async def test_unrevoked_token_passes_with_denylist_present():
    svc, _ = build_service(reply="x")
    iss, vrf = auth_pair()
    tok = iss.issue("emp-2")
    deny = MemoryRevocationStore({"some-other-id"})
    app = create_app(svc, verifier=vrf, revocation=deny)
    async with _client(app) as c:
        r = await c.post("/v1/messages", json=_MSG, headers={"x-api-key": tok})
        assert r.status_code == 200


async def test_revocation_gate_covers_usage_and_quota():
    svc, _ = build_service(reply="x")
    iss, vrf = auth_pair()
    tok = iss.issue("emp-3")
    jti = vrf.verify(tok).token_id
    deny = MemoryRevocationStore({jti})
    app = create_app(svc, verifier=vrf, revocation=deny)
    async with _client(app) as c:
        assert (await c.get("/v1/usage", headers={"x-api-key": tok})).status_code == 401
        assert (await c.get("/v1/quota", headers={"x-api-key": tok})).status_code == 401


async def test_no_denylist_means_no_gate():
    # Without a revocation store, behavior is unchanged (token passes).
    svc, _ = build_service(reply="x")
    iss, vrf = auth_pair()
    tok = iss.issue("emp-4")
    async with _client(create_app(svc, verifier=vrf)) as c:
        r = await c.post("/v1/messages", json=_MSG, headers={"x-api-key": tok})
        assert r.status_code == 200
