"""Observability wiring tests for the llm-gateway (M8 S3).

Drive requests through the ASGI app with a Registry attached, then scrape the
mounted /metrics and assert the shared RED contract is honored AND that the
SSE-safe middleware does not break streaming.
"""

import httpx
from cocola_common import Registry
from cocola_llm_gateway.server import create_app
from tests.conftest import build_service


def _client(app):
    transport = httpx.ASGITransport(app=app)
    return httpx.AsyncClient(transport=transport, base_url="http://t")


async def test_metrics_endpoint_records_red():
    svc, _ = build_service(reply="hi")
    reg = Registry("llm-gateway-test")
    app = create_app(svc, metrics=reg)
    async with _client(app) as c:
        r = await c.post(
            "/v1/messages",
            json={
                "model": "default",
                "max_tokens": 16,
                "stream": False,
                "messages": [{"role": "user", "content": "hi"}],
            },
            headers={"x-cocola-user": "U1"},
        )
        assert r.status_code == 200

        scrape = await c.get("/metrics")
        assert scrape.status_code == 200
        body = scrape.text

    assert 'service="llm-gateway-test"' in body
    assert 'transport="http"' in body
    assert 'method="POST /v1/messages"' in body
    assert 'code="200"' in body
    # baseline collectors are present (python_info is cross-platform; the
    # ProcessCollector is a no-op off Linux, so we assert on python_info).
    assert "python_info" in body


async def test_metrics_does_not_break_sse_streaming():
    svc, ledger = build_service(reply="hello world")
    reg = Registry("llm-gateway-test")
    app = create_app(svc, metrics=reg)
    async with _client(app) as c:
        events = []
        async with c.stream(
            "POST",
            "/v1/messages",
            json={
                "model": "default",
                "max_tokens": 32,
                "stream": True,
                "messages": [{"role": "user", "content": "hi"}],
            },
            headers={"x-cocola-user": "U1", "x-cocola-session": "S1"},
        ) as resp:
            assert resp.status_code == 200
            async for line in resp.aiter_lines():
                if line.startswith("event:"):
                    events.append(line.split(":", 1)[1].strip())
        assert events[0] == "message_start"
        assert events[-1] == "message_stop"
    # Billing still records exactly once (instrumentation is transparent).
    assert len(await ledger.recent(user_id="U1")) == 1
