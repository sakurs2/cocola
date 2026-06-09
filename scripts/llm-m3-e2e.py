#!/usr/bin/env python3
"""M3 end-to-end acceptance: Claude Agent SDK -> cocola gateway -> billing.

Proves the core M3 design — the project does NOT build its own agent; it points
the Claude Agent SDK at the cocola LLM gateway via ANTHROPIC_BASE_URL, and every
model call flows through the gateway's routing + metering + billing.

Hermetic by construction (ADR-0004):
  - No real model: the gateway's upstream is FakeUpstream.
  - No real SDK subprocess / no bound port: a fake `query_fn` stands in for the
    Claude Code CLI and drives the gateway in-process over httpx.ASGITransport,
    speaking the exact HTTP contract the real CLI uses (POST /v1/messages,
    Anthropic SSE).

Flow:  prompt -> ClaudeAgentSDKProvider -> fake CLI -> gateway (ASGI) ->
       FakeUpstream -> Anthropic SSE -> mapped to AgentEvents; assert the
       gateway recorded exactly one billed call with non-zero tokens.

Run:  PYTHONPATH=apps/agent-runtime apps/llm-gateway/.venv/bin/python scripts/llm-m3-e2e.py
"""
import asyncio
import json
import sys
from dataclasses import dataclass, field

import httpx

from cocola_llm_gateway.billing.memory import MemoryLedger
from cocola_llm_gateway.middleware import ResiliencePolicy
from cocola_llm_gateway.registry import ModelRoute, Pricing, Registry
from cocola_llm_gateway.server import create_app
from cocola_llm_gateway.service import GatewayService
from cocola_llm_gateway.upstream.fake import FakeUpstream

from cocola_agent_runtime.agent_provider import AgentOptions
from cocola_agent_runtime.claude_sdk_provider import (
    ClaudeAgentSDKProvider,
    ClaudeSDKConfig,
)


@dataclass
class TextBlock:
    text: str


@dataclass
class AssistantMessage:
    content: list
    model: str = "fake-1"
    parent_tool_use_id: object = None
    error: object = None


@dataclass
class ResultMessage:
    subtype: str = "success"
    is_error: bool = False
    num_turns: int = 1
    session_id: str = "S1"
    total_cost_usd: float = 0.0
    usage: dict = field(default_factory=dict)
    result: str = "ok"


def make_fake_cli_query(app, user_id, session_id):
    """Stand-in for claude_agent_sdk.query that behaves like the real CLI."""
    asgi = httpx.ASGITransport(app=app)

    async def fake_query(*, prompt, options=None, transport=None):  # noqa: ARG001
        async with httpx.AsyncClient(transport=asgi, base_url="http://gw") as client:
            text_parts, event = [], None
            async with client.stream(
                "POST", "/v1/messages",
                json={"model": "default", "max_tokens": 64, "stream": True,
                      "messages": [{"role": "user", "content": prompt}]},
                headers={"x-cocola-user": user_id, "x-cocola-session": session_id},
            ) as resp:
                async for line in resp.aiter_lines():
                    if line.startswith("event:"):
                        event = line.split(":", 1)[1].strip()
                    elif line.startswith("data:") and event == "content_block_delta":
                        text_parts.append(json.loads(line.split(":", 1)[1].strip())["delta"]["text"])
            yield AssistantMessage(content=[TextBlock("".join(text_parts))])
            yield ResultMessage(session_id=session_id)

    return fake_query


async def main() -> int:
    fake = FakeUpstream(reply="hello from cocola gateway")
    routes = {"default": ModelRoute("default", "fake", "fake-1", Pricing(0.003, 0.015))}
    reg = Registry({"fake": fake}, routes, default_alias="default")
    ledger = MemoryLedger()
    svc = GatewayService(registry=reg, ledger=ledger,
                         policy=ResiliencePolicy(timeout_s=10, max_retries=2))
    app = create_app(svc)

    cfg = ClaudeSDKConfig(base_url="http://gw", model="default", api_key="cocola-e2e")
    prov = ClaudeAgentSDKProvider(cfg, query_fn=make_fake_cli_query(app, "U1", "S1"))

    out = [(e.kind, e.data) async for e in
           prov.query("ping the gateway", AgentOptions(user_id="U1", session_id="S1"))]

    texts = [d["text"] for k, d in out if k == "text"]
    assert texts == ["hello from cocola gateway"], texts
    assert out[-1][0] == "done"

    recent = await ledger.recent(user_id="U1", limit=10)
    agg = await ledger.aggregate_user("U1")
    assert len(recent) == 1, recent
    assert agg.calls == 1
    assert agg.prompt_tokens > 0 and agg.completion_tokens > 0

    await reg.aclose()
    print(f"agent text : {texts[0]!r}")
    print(f"billed     : calls={agg.calls} in={agg.prompt_tokens} "
          f"out={agg.completion_tokens} cost=${agg.cost_usd:.6f}")
    print("M3 E2E OK")
    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
