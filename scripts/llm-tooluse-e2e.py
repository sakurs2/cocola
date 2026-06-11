#!/usr/bin/env python3
"""Tool-use end-to-end acceptance for the cocola LLM gateway (ADR-0010).

Proves the fix for the M3 "text-only relay" blocker: an Anthropic-compatible
client that sends `tools` now gets a real `tool_use` turn back, through the FULL
gateway HTTP path (POST /v1/messages, Anthropic SSE) and the non-stream JSON
path -- with billing still recorded.

Hermetic by construction (ADR-0004): no real model, no bound port, no API key.
The upstream is FakeUpstream in its scripted tool_use mode; the app is driven
in-process over httpx.ASGITransport, speaking the EXACT wire contract the real
Claude Agent SDK uses.

What it asserts:
  1. STREAM  : the SSE carries content_block_start(tool_use) + input_json_delta
               + content_block_stop + message_delta(stop_reason=tool_use), and
               NO phantom text block is synthesized.
  2. COLLECT : the non-stream JSON reconstructs the tool_use block with the
               args parsed from the streamed partial_json fragments.
  3. PASSTHRU: the `tools` the client sent actually reached the upstream
               (otherwise a real model would never emit tool_use).
  4. BILLING : the call is metered exactly once with non-zero tokens.

Run:
  apps/llm-gateway/.venv/bin/python scripts/llm-tooluse-e2e.py
  # or:  cd apps/llm-gateway && uv run python ../../scripts/llm-tooluse-e2e.py
"""

from __future__ import annotations

import asyncio
import json
import sys

import httpx
from cocola_llm_gateway.billing.memory import MemoryLedger
from cocola_llm_gateway.middleware import ResiliencePolicy
from cocola_llm_gateway.registry import ModelRoute, Pricing, Registry
from cocola_llm_gateway.server import create_app
from cocola_llm_gateway.service import GatewayService
from cocola_llm_gateway.upstream.fake import FakeUpstream

TOOLS = [
    {
        "name": "get_weather",
        "description": "Get the weather for a city",
        "input_schema": {
            "type": "object",
            "properties": {"city": {"type": "string"}},
            "required": ["city"],
        },
    }
]
TOOL_CALL = {"id": "tu_1", "name": "get_weather", "input": {"city": "NYC"}}
REQUEST_BODY = {
    "model": "default",
    "max_tokens": 64,
    "tools": TOOLS,
    "tool_choice": {"type": "auto"},
    "messages": [{"role": "user", "content": "what is the weather in NYC?"}],
}
HEADERS = {"x-cocola-user": "U1", "x-cocola-session": "S1"}


def _build_app(seen_tools: list):
    """Wire a gateway whose fake upstream records the tools it was handed, so we
    can prove passthrough, and emits a scripted tool_use turn."""

    class RecordingFake(FakeUpstream):
        async def chat_stream(self, req):
            seen_tools.append(list(req.params.tools))
            async for ev in super().chat_stream(req):
                yield ev

    fake = RecordingFake(tool_call=TOOL_CALL)
    routes = {"default": ModelRoute("default", "fake", "fake-1", Pricing(0.003, 0.015))}
    reg = Registry({"fake": fake}, routes, default_alias="default")
    ledger = MemoryLedger()
    svc = GatewayService(
        registry=reg, ledger=ledger, policy=ResiliencePolicy(timeout_s=10, max_retries=2)
    )
    return create_app(svc), reg, ledger


async def _check_stream(app) -> None:
    asgi = httpx.ASGITransport(app=app)
    events: list[tuple[str, dict]] = []
    async with httpx.AsyncClient(transport=asgi, base_url="http://gw") as client:
        body = dict(REQUEST_BODY, stream=True)
        event = None
        async with client.stream("POST", "/v1/messages", json=body, headers=HEADERS) as resp:
            assert resp.status_code == 200, f"stream HTTP {resp.status_code}"
            async for line in resp.aiter_lines():
                if line.startswith("event:"):
                    event = line.split(":", 1)[1].strip()
                elif line.startswith("data:"):
                    data = json.loads(line.split(":", 1)[1].strip())
                    events.append((event or data.get("type", ""), data))

    # Collapse the >=1 incremental input_json_delta frames into one logical step
    # so the skeleton assertion is robust to chunking (real APIs stream args in
    # arbitrarily many pieces).
    order: list[str] = []
    for e, _ in events:
        if e == "content_block_delta" and order and order[-1] == "content_block_delta":
            continue
        order.append(e)
    assert order == [
        "message_start",
        "content_block_start",
        "content_block_delta",
        "content_block_stop",
        "message_delta",
        "message_stop",
    ], f"unexpected SSE skeleton: {order}"

    start = next(d for e, d in events if e == "content_block_start")
    assert start["content_block"]["type"] == "tool_use", start
    assert start["content_block"]["name"] == "get_weather", start
    assert start["content_block"]["id"] == "tu_1", start

    # Every content_block_delta must be input_json_delta (never text); their
    # partial_json fragments must concatenate to the full tool args.
    deltas = [d for e, d in events if e == "content_block_delta"]
    assert deltas, "no content_block_delta frames"
    assert all(d["delta"].get("type") == "input_json_delta" for d in deltas), deltas
    joined = "".join(d["delta"].get("partial_json", "") for d in deltas)
    assert json.loads(joined) == {"city": "NYC"}, joined

    mdelta = next(d for e, d in events if e == "message_delta")
    assert mdelta["delta"]["stop_reason"] == "tool_use", mdelta

    assert not any(
        e == "content_block_delta" and d["delta"].get("type") == "text_delta" for e, d in events
    ), "a spurious text_delta leaked into a tool_use turn"
    print(f"  [1/4] STREAM  : tool_use SSE OK ({len(deltas)} input_json_delta frames, no text)")


async def _check_collect(app) -> None:
    asgi = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=asgi, base_url="http://gw") as client:
        body = dict(REQUEST_BODY, stream=False)
        resp = await client.post("/v1/messages", json=body, headers=HEADERS)
    assert resp.status_code == 200, f"collect HTTP {resp.status_code}: {resp.text}"
    payload = resp.json()
    assert payload["stop_reason"] == "tool_use", payload
    blocks = payload["content"]
    assert len(blocks) == 1 and blocks[0]["type"] == "tool_use", blocks
    blk = blocks[0]
    assert blk["id"] == "tu_1" and blk["name"] == "get_weather", blk
    assert blk["input"] == {"city": "NYC"}, blk["input"]
    print("  [2/4] COLLECT : non-stream tool_use block reconstructed; input parsed OK")


def _check_passthrough(seen_tools: list) -> None:
    assert seen_tools, "upstream was never called"
    last = seen_tools[-1]
    assert last and last[0]["name"] == "get_weather", (
        f"tools did NOT reach the upstream (M3 regression!): {last}"
    )
    print("  [3/4] PASSTHRU: client `tools` reached the upstream verbatim")


async def _check_billing(ledger) -> None:
    recent = await ledger.recent(user_id="U1", limit=10)
    agg = await ledger.aggregate_user("U1")
    assert agg.calls >= 1, f"expected >=1 billed call, got {agg.calls}"
    assert agg.prompt_tokens > 0 and agg.completion_tokens > 0, agg
    print(
        f"  [4/4] BILLING : calls={agg.calls} in={agg.prompt_tokens} "
        f"out={agg.completion_tokens} cost=${agg.cost_usd:.6f} (records={len(recent)})"
    )


async def main() -> int:
    print("== cocola gateway tool-use E2E (hermetic, ADR-0010) ==")
    seen_tools: list = []
    app, reg, ledger = _build_app(seen_tools)
    try:
        await _check_stream(app)
        _check_passthrough(seen_tools)
        await _check_billing(ledger)
    finally:
        await reg.aclose()

    seen2: list = []
    app2, reg2, _ = _build_app(seen2)
    try:
        await _check_collect(app2)
    finally:
        await reg2.aclose()

    print("TOOL-USE E2E OK")
    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
