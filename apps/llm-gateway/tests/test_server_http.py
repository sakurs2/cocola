"""HTTP-level tests over httpx.ASGITransport (in-process, no bound port).

These are the regression guard for the streaming/non-streaming billing bug:
both paths MUST write exactly one usage record per call.
"""
import json

import httpx
import pytest

from cocola_llm_gateway.server import create_app
from tests.conftest import build_service


def _client(app):
    transport = httpx.ASGITransport(app=app)
    return httpx.AsyncClient(transport=transport, base_url="http://t")


async def test_healthz():
    svc, _ = build_service()
    async with _client(create_app(svc)) as c:
        r = await c.get("/healthz")
        assert r.status_code == 200
        body = r.json()
        assert body["status"] == "ok"
        assert body["default_alias"] == "default"


async def test_streaming_emits_anthropic_sse_and_bills_once():
    svc, ledger = build_service(reply="hello world")
    async with _client(create_app(svc)) as c:
        events = []
        async with c.stream(
            "POST", "/v1/messages",
            json={"model": "default", "max_tokens": 32, "stream": True,
                  "messages": [{"role": "user", "content": "hi"}]},
            headers={"x-cocola-user": "U1", "x-cocola-session": "S1"},
        ) as resp:
            assert resp.status_code == 200
            async for line in resp.aiter_lines():
                if line.startswith("event:"):
                    events.append(line.split(":", 1)[1].strip())
        assert events[0] == "message_start"
        assert events[-1] == "message_stop"
    recent = await ledger.recent(user_id="U1")
    assert len(recent) == 1


async def test_non_streaming_returns_json_and_bills_once():
    svc, ledger = build_service(reply="hello world")
    async with _client(create_app(svc)) as c:
        r = await c.post(
            "/v1/messages",
            json={"model": "default", "max_tokens": 32, "stream": False,
                  "messages": [{"role": "user", "content": "hi"}]},
            headers={"x-cocola-user": "U1", "x-cocola-session": "S1"},
        )
        assert r.status_code == 200
        body = r.json()
        assert body["content"][0]["text"] == "hello world"
        assert body["usage"]["input_tokens"] >= 1
    recent = await ledger.recent(user_id="U1")
    assert len(recent) == 1


async def test_both_paths_accumulate_two_records():
    """The exact scenario that exposed the suspended-generator billing bug."""
    svc, ledger = build_service(reply="hello world")
    async with _client(create_app(svc)) as c:
        async with c.stream(
            "POST", "/v1/messages",
            json={"model": "default", "max_tokens": 32, "stream": True,
                  "messages": [{"role": "user", "content": "a"}]},
            headers={"x-cocola-user": "U1", "x-cocola-session": "S1"},
        ) as resp:
            async for _ in resp.aiter_lines():
                pass
        await c.post(
            "/v1/messages",
            json={"model": "default", "max_tokens": 32, "stream": False,
                  "messages": [{"role": "user", "content": "b"}]},
            headers={"x-cocola-user": "U1", "x-cocola-session": "S1"},
        )
        r = await c.get("/v1/usage?user_id=U1&session_id=S1")
        u = r.json()
    assert len(u["recent"]) == 2
    assert u["user_aggregate"]["calls"] == 2
    assert u["session_aggregate"]["calls"] == 2


async def test_unknown_model_returns_404():
    svc, _ = build_service()
    async with _client(create_app(svc)) as c:
        r = await c.post(
            "/v1/messages",
            json={"model": "ghost", "max_tokens": 32, "stream": False,
                  "messages": [{"role": "user", "content": "hi"}]},
        )
        assert r.status_code == 404
        assert r.json()["error"]["type"] == "NOT_FOUND"
