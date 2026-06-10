#!/usr/bin/env python3
"""M4 end-to-end acceptance: identity (signed token) + token quota.

Builds on the M3 slice (Claude Agent SDK -> cocola gateway -> billing) and adds
the M4 guardrails:
  - The token cocola mints IS the ANTHROPIC_API_KEY the SDK presents. The gateway
    verifies it offline and attributes usage to the token's subject (not a mock
    header).
  - A per-user daily token quota blocks the *next* request once the cap is hit,
    returning an Anthropic-compatible 429 the SDK surfaces as an error.

Hermetic (ADR-0004/0005): FakeUpstream, no real model, no bound port — a fake
`query_fn` drives the gateway in-process over httpx.ASGITransport using the exact
HTTP contract the real CLI uses, now carrying the bearer token as x-api-key.

Run:  PYTHONPATH=apps/agent-runtime apps/llm-gateway/.venv/bin/python scripts/llm-m4-e2e.py
"""

import asyncio
import json
import sys
from dataclasses import dataclass, field

import httpx
from cocola_agent_runtime.agent_provider import AgentOptions
from cocola_agent_runtime.claude_sdk_provider import ClaudeAgentSDKProvider, ClaudeSDKConfig
from cocola_llm_gateway.auth import AuthConfig, Issuer, Verifier
from cocola_llm_gateway.billing.memory import MemoryLedger
from cocola_llm_gateway.middleware import ResiliencePolicy
from cocola_llm_gateway.quota import Enforcer, MemoryQuotaStore, QuotaPolicy
from cocola_llm_gateway.registry import ModelRoute, Pricing, Registry
from cocola_llm_gateway.server import create_app
from cocola_llm_gateway.service import GatewayService
from cocola_llm_gateway.upstream.fake import FakeUpstream


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


class QuotaBlocked(Exception):
    pass


def make_fake_cli_query(app, token, session_id):
    """Fake CLI that presents the cocola token as x-api-key (like the real SDK)."""
    asgi = httpx.ASGITransport(app=app)

    async def fake_query(*, prompt, options=None, transport=None):  # noqa: ARG001
        async with httpx.AsyncClient(transport=asgi, base_url="http://gw") as client:
            text_parts, event = [], None
            async with client.stream(
                "POST",
                "/v1/messages",
                json={
                    "model": "default",
                    "max_tokens": 64,
                    "stream": True,
                    "messages": [{"role": "user", "content": prompt}],
                },
                headers={"x-api-key": token, "x-cocola-session": session_id},
            ) as resp:
                if resp.status_code == 429:
                    raise QuotaBlocked("gateway returned 429 (quota exceeded)")
                if resp.status_code != 200:
                    await resp.aread()
                    raise RuntimeError(f"gateway error {resp.status_code}")
                async for line in resp.aiter_lines():
                    if line.startswith("event:"):
                        event = line.split(":", 1)[1].strip()
                    elif line.startswith("data:") and event == "content_block_delta":
                        text_parts.append(
                            json.loads(line.split(":", 1)[1].strip())["delta"]["text"]
                        )
            yield AssistantMessage(content=[TextBlock("".join(text_parts))])
            yield ResultMessage(session_id=session_id)

    return fake_query


async def run_once(app, token, session_id):
    prov = ClaudeAgentSDKProvider(
        ClaudeSDKConfig(base_url="http://gw", model="default", api_key=token),
        query_fn=make_fake_cli_query(app, token, session_id),
    )
    return [
        (e.kind, e.data)
        async for e in prov.query("ping", AgentOptions(user_id="emp-42", session_id=session_id))
    ]


async def main() -> int:
    # Auth on (shared secret) + a tiny per-user daily quota so the 2nd call trips.
    secret = "m4-e2e-secret"
    auth_cfg = AuthConfig(secret=secret, issuer="cocola")
    verifier = Verifier(auth_cfg)
    token = Issuer(auth_cfg).issue("emp-42", tenant_id="team-a", ttl_s=3600)

    fake = FakeUpstream(reply="hello from cocola gateway")
    routes = {"default": ModelRoute("default", "fake", "fake-1", Pricing(0.003, 0.015))}
    reg = Registry({"fake": fake}, routes, default_alias="default")
    ledger = MemoryLedger()
    enforcer = Enforcer(QuotaPolicy(user_daily_tokens=5), MemoryQuotaStore())
    svc = GatewayService(reg, ledger, ResiliencePolicy(timeout_s=10, max_retries=2), enforcer)
    app = create_app(svc, verifier=verifier)

    # 1) Authorized call succeeds, attributed to the TOKEN subject.
    out = await run_once(app, token, "S1")
    texts = [d["text"] for k, d in out if k == "text"]
    assert texts == ["hello from cocola gateway"], texts
    agg = await ledger.aggregate_user("emp-42")
    assert agg.calls == 1, agg
    print(f"authorized : text={texts[0]!r} billed_to=emp-42 tokens={agg.total_tokens}")

    # 2) Quota now exceeded -> next call blocked with 429 at the gateway.
    try:
        await run_once(app, token, "S2")
        print("FAIL: second call was not blocked")
        return 1
    except QuotaBlocked:
        print("quota      : second call correctly blocked with HTTP 429")

    # 3) An invalid token is rejected (401).
    async with httpx.AsyncClient(
        transport=httpx.ASGITransport(app=app), base_url="http://gw"
    ) as client:
        r = await client.post(
            "/v1/messages",
            json={
                "model": "default",
                "max_tokens": 8,
                "stream": False,
                "messages": [{"role": "user", "content": "x"}],
            },
            headers={"x-api-key": "bogus.invalid.sig"},
        )
        assert r.status_code == 401, r.status_code
        print("auth       : invalid token correctly rejected with HTTP 401")

    await reg.aclose()
    print("M4 E2E OK")
    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
