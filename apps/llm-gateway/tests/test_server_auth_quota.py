"""HTTP-level auth + quota tests over httpx.ASGITransport (in-process)."""

import httpx
from cocola_llm_gateway.quota import MemoryOverrideStore
from cocola_llm_gateway.server import create_app
from tests.conftest import auth_pair, build_enforcer, build_service

_MSG = {
    "model": "default",
    "max_tokens": 32,
    "stream": False,
    "messages": [{"role": "user", "content": "hi"}],
}


def _client(app):
    return httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://t")


async def test_missing_token_401_when_auth_enabled():
    svc, _ = build_service()
    _, vrf = auth_pair()
    async with _client(create_app(svc, verifier=vrf)) as c:
        r = await c.post("/v1/messages", json=_MSG)
        assert r.status_code == 401
        assert r.json()["error"]["type"] == "authentication_error"


async def test_invalid_token_401():
    svc, _ = build_service()
    _, vrf = auth_pair()
    async with _client(create_app(svc, verifier=vrf)) as c:
        r = await c.post("/v1/messages", json=_MSG, headers={"x-api-key": "bogus.token.sig"})
        assert r.status_code == 401


async def test_valid_token_authorizes_and_attributes_user():
    svc, ledger = build_service(reply="hello world")
    iss, vrf = auth_pair()
    tok = iss.issue("emp-42", tenant_id="team-a")
    async with _client(create_app(svc, verifier=vrf)) as c:
        r = await c.post("/v1/messages", json=_MSG, headers={"x-api-key": tok})
        assert r.status_code == 200
    # Billing was attributed to the token subject, not a mock header.
    recent = await ledger.recent(user_id="emp-42")
    assert len(recent) == 1


async def test_bearer_authorization_header_accepted():
    svc, _ = build_service(reply="x")
    iss, vrf = auth_pair()
    tok = iss.issue("emp-1")
    async with _client(create_app(svc, verifier=vrf)) as c:
        r = await c.post("/v1/messages", json=_MSG, headers={"authorization": f"Bearer {tok}"})
        assert r.status_code == 200


async def test_quota_exceeded_returns_429():
    enf, store = build_enforcer(user_daily=5)  # tiny cap
    svc, _ = build_service(reply="hello world over the cap", enforcer=enf)
    iss, vrf = auth_pair()
    tok = iss.issue("emp-9")
    async with _client(create_app(svc, verifier=vrf)) as c:
        # First call goes through (under cap at request time) but overshoots the cap.
        r1 = await c.post("/v1/messages", json=_MSG, headers={"x-api-key": tok})
        assert r1.status_code == 200
        # Second call is rejected before opening a stream.
        r2 = await c.post("/v1/messages", json=_MSG, headers={"x-api-key": tok})
        assert r2.status_code == 429
        body = r2.json()
        assert body["error"]["type"] == "rate_limit_error"
        assert body["error"]["scope"] == "user"


async def test_quota_endpoint_reports_usage():
    enf, _ = build_enforcer(user_daily=1000)
    svc, _ = build_service(reply="hello world", enforcer=enf)
    iss, vrf = auth_pair()
    tok = iss.issue("emp-q", tenant_id="team-z")
    async with _client(create_app(svc, verifier=vrf)) as c:
        await c.post("/v1/messages", json=_MSG, headers={"x-api-key": tok})
        r = await c.get("/v1/quota", headers={"x-api-key": tok})
        assert r.status_code == 200
        body = r.json()
        assert body["user_id"] == "emp-q"
        scopes = {s["scope"]: s for s in body["scopes"]}
        assert "user" in scopes
        assert scopes["user"]["used"] >= 1
        assert scopes["user"]["limit"] == 1000


async def test_no_quota_means_unlimited():
    svc, _ = build_service(reply="x")  # no enforcer
    iss, vrf = auth_pair()
    tok = iss.issue("emp-1")
    async with _client(create_app(svc, verifier=vrf)) as c:
        for _ in range(3):
            r = await c.post("/v1/messages", json=_MSG, headers={"x-api-key": tok})
            assert r.status_code == 200
        q = await c.get("/v1/quota", headers={"x-api-key": tok})
        assert q.json()["scopes"] == []


async def test_usage_requires_token_when_auth_enabled():
    svc, _ = build_service()
    _, vrf = auth_pair()
    async with _client(create_app(svc, verifier=vrf)) as c:
        r = await c.get("/v1/usage")  # no token
        assert r.status_code == 401
        assert r.json()["error"]["type"] == "authentication_error"


async def test_usage_only_returns_own_records_when_auth_enabled():
    # emp-A spends; emp-B must not be able to read emp-A's usage by passing
    # ?user_id=emp-A — the verified token is authoritative.
    svc, _ = build_service(reply="hello world")
    iss, vrf = auth_pair()
    tok_a = iss.issue("emp-A")
    tok_b = iss.issue("emp-B")
    async with _client(create_app(svc, verifier=vrf)) as c:
        await c.post("/v1/messages", json=_MSG, headers={"x-api-key": tok_a})
        # emp-B tries to peek at emp-A's usage via the query param.
        r = await c.get("/v1/usage?user_id=emp-A", headers={"x-api-key": tok_b})
        assert r.status_code == 200
        body = r.json()
        # Scoped to emp-B (the token subject): no emp-A records leak through.
        assert body["recent"] == []
        assert body["user_aggregate"]["calls"] == 0
        # emp-A reads their own usage and sees the call.
        r2 = await c.get("/v1/usage?user_id=ignored", headers={"x-api-key": tok_a})
        assert r2.json()["user_aggregate"]["calls"] == 1


async def test_usage_honors_query_user_when_auth_disabled():
    # Back-compat: with auth OFF, the query param still selects the subject.
    svc, _ = build_service(reply="x")
    async with _client(create_app(svc)) as c:  # no verifier => disabled
        await c.post("/v1/messages", json=_MSG, headers={"x-cocola-user": "legacy-u"})
        r = await c.get("/v1/usage?user_id=legacy-u")
        assert r.status_code == 200
        assert r.json()["user_aggregate"]["calls"] == 1


async def test_healthz_reports_auth_state():
    svc, _ = build_service()
    _, vrf = auth_pair()
    async with _client(create_app(svc, verifier=vrf)) as c:
        assert (await c.get("/healthz")).json()["auth_enabled"] is True
    # default (no verifier) => disabled
    async with _client(create_app(svc)) as c:
        assert (await c.get("/healthz")).json()["auth_enabled"] is False


async def test_disabled_auth_allows_anonymous():
    svc, _ = build_service(reply="x")
    async with _client(create_app(svc)) as c:  # no verifier => disabled
        r = await c.post("/v1/messages", json=_MSG)
        assert r.status_code == 200


async def test_per_subject_override_enforced_over_http():
    # No static cap, but an override caps emp-vip to a tiny budget. The first
    # call overshoots; the second is rejected with 429.
    ov = MemoryOverrideStore({("user", "emp-vip"): 5})
    enf, _ = build_enforcer(overrides=ov)  # no static caps
    svc, _ = build_service(reply="well over five tokens of reply text here", enforcer=enf)
    iss, vrf = auth_pair()
    tok = iss.issue("emp-vip")
    async with _client(create_app(svc, verifier=vrf)) as c:
        r1 = await c.post("/v1/messages", json=_MSG, headers={"x-api-key": tok})
        assert r1.status_code == 200
        r2 = await c.post("/v1/messages", json=_MSG, headers={"x-api-key": tok})
        assert r2.status_code == 429
        assert r2.json()["error"]["scope"] == "user"


async def test_override_supersedes_static_cap_over_http():
    # Static default would cap at 5; an override lifts emp-rich to a high cap so
    # repeated calls all pass and /v1/quota reports the override limit.
    ov = MemoryOverrideStore({("user", "emp-rich"): 100000})
    enf, _ = build_enforcer(user_daily=5, overrides=ov)
    svc, _ = build_service(reply="hello world", enforcer=enf)
    iss, vrf = auth_pair()
    tok = iss.issue("emp-rich")
    async with _client(create_app(svc, verifier=vrf)) as c:
        for _ in range(3):
            r = await c.post("/v1/messages", json=_MSG, headers={"x-api-key": tok})
            assert r.status_code == 200
        q = await c.get("/v1/quota", headers={"x-api-key": tok})
        scopes = {sc["scope"]: sc for sc in q.json()["scopes"]}
        assert scopes["user"]["limit"] == 100000
