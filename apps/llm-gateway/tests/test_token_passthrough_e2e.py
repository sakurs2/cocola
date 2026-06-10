"""M4 token-passthrough contract: the gateway-verified token IS the SDK key.

This is the seam where M3 (agent-runtime drives the Claude Agent SDK) meets M4
(the gateway mints/verifies tokens). The cocola-issued token the gateway verifies
is the SAME credential the agent-runtime injects into the SDK subprocess as
ANTHROPIC_API_KEY, while ANTHROPIC_BASE_URL points the SDK at the cocola gateway
instead of api.anthropic.com.

We prove it hermetically — no real SDK subprocess, no bound port (ADR-0004: the
only provider tests may use is FakeUpstream). A fake query_fn stands in for
claude_agent_sdk.query: instead of spawning the CLI it reads EXACTLY the env the
provider would inject (provider._build_env()) and drives the gateway's ASGI app
in-process with that token as x-api-key — the same request the real CLI makes.
The gateway verifies the token, routes through FakeUpstream, and returns an
Anthropic response, which the fake reshapes into SDK messages so the real
provider maps them to the runtime's generic AgentEvents.
"""

from __future__ import annotations

import sys
from dataclasses import dataclass
from pathlib import Path

import httpx

# The agent-runtime provider is pure Python (no SDK import at module load), so we
# can exercise the REAL provider here. Put its package root on sys.path; mirrors
# how agent-runtime's own conftest stitches in the proto stubs.
# Append (do NOT insert at 0): agent-runtime also ships a top-level ``tests``
# package, and shadowing this app's ``tests.conftest`` would break the import
# below. ``cocola_agent_runtime`` is uniquely named, so append suffices.
_AGENT_SRC = Path(__file__).resolve().parents[2] / "agent-runtime"
if _AGENT_SRC.is_dir() and str(_AGENT_SRC) not in sys.path:
    sys.path.append(str(_AGENT_SRC))

from cocola_agent_runtime.agent_provider import AgentOptions  # noqa: E402
from cocola_agent_runtime.claude_sdk_provider import (  # noqa: E402
    ClaudeAgentSDKProvider,
    ClaudeSDKConfig,
)
from cocola_llm_gateway.server import create_app  # noqa: E402
from tests.conftest import auth_pair, build_service  # noqa: E402


# SDK-shaped messages (duck-typed by class name, matching the real SDK taxonomy
# the provider maps over). Kept minimal — only what this contract needs.
@dataclass
class TextBlock:
    text: str


@dataclass
class AssistantMessage:
    content: list
    model: str = "default"
    error: object = None


@dataclass
class ResultMessage:
    subtype: str = "success"
    is_error: bool = False
    num_turns: int = 1
    session_id: str = "S1"
    total_cost_usd: float = 0.0
    result: str = ""


def _sdk_query_via_gateway(app, provider):
    """A fake claude_agent_sdk.query that drives the gateway over ASGI.

    It uses EXACTLY the env the provider would hand the real CLI, proving the
    passthrough contract: env["ANTHROPIC_API_KEY"] is presented to the gateway as
    x-api-key, env["ANTHROPIC_BASE_URL"] is the gateway root. A non-200 is
    surfaced as an assistant error, just as a failed SDK call would manifest.
    """

    async def fake_query(*, prompt, options=None, transport=None):
        env = provider._build_env()
        base_url = env["ANTHROPIC_BASE_URL"]
        api_key = env["ANTHROPIC_API_KEY"]
        body = {
            "model": provider._config.model,
            "max_tokens": 64,
            "stream": False,
            "messages": [{"role": "user", "content": prompt}],
        }
        asgi = httpx.ASGITransport(app=app)
        async with httpx.AsyncClient(transport=asgi, base_url=base_url) as c:
            r = await c.post("/v1/messages", json=body, headers={"x-api-key": api_key})
        if r.status_code != 200:
            yield AssistantMessage(content=[], error=f"gateway {r.status_code}: {r.text}")
            return
        payload = r.json()
        text = "".join(
            b.get("text", "") for b in payload.get("content", []) if b.get("type") == "text"
        )
        yield AssistantMessage(
            content=[TextBlock(text)],
            model=payload.get("model", provider._config.model),
        )
        yield ResultMessage(num_turns=1, session_id=payload.get("id", "S1"), result=text)

    return fake_query


def _provider_wired_to(app, *, base_url, token, model="default"):
    cfg = ClaudeSDKConfig(base_url=base_url, model=model, api_key=token)
    # Construct with a no-op query_fn (avoids importing the real SDK), then wire
    # the gateway-driving fake — it needs the provider to read _build_env().
    prov = ClaudeAgentSDKProvider(cfg, query_fn=lambda **kw: iter(()))
    prov._query = _sdk_query_via_gateway(app, prov)
    return prov


async def test_token_passthrough_real_response_flows_back():
    svc, ledger = build_service(reply="hello from cocola gateway")
    iss, vrf = auth_pair()
    token = iss.issue("emp-77", tenant_id="team-platform")
    app = create_app(svc, verifier=vrf)

    prov = _provider_wired_to(app, base_url="http://cocola-gateway:8080", token=token)
    opts = AgentOptions(user_id="emp-77", session_id="S1")
    events = [(e.kind, e.data) async for e in prov.query("hi there", opts)]
    kinds = [k for k, _ in events]

    # The model's text crossed the gateway and mapped to a runtime AgentEvent.
    assert "text" in kinds
    text = next(d["text"] for k, d in events if k == "text")
    assert text == "hello from cocola gateway"
    assert kinds[-1] == "done"
    # No error => the gateway ACCEPTED the SDK-presented token.
    assert "error" not in kinds

    # Billing was attributed to the TOKEN SUBJECT, proving the token the SDK sent
    # as ANTHROPIC_API_KEY is the very token the gateway verified (M4 收口).
    recent = await ledger.recent(user_id="emp-77")
    assert len(recent) == 1


async def test_gateway_rejects_unsigned_token_surfaces_error():
    svc, ledger = build_service(reply="should not be returned")
    _, vrf = auth_pair()  # auth ENABLED
    app = create_app(svc, verifier=vrf)

    prov = _provider_wired_to(app, base_url="http://cocola-gateway:8080", token="bogus.token.sig")
    opts = AgentOptions(user_id="x", session_id="S1")
    events = [(e.kind, e.data) async for e in prov.query("hi", opts)]
    kinds = [k for k, _ in events]

    # A bad key never reaches the model: error surfaces, no text, no billing.
    assert "error" in kinds
    err = next(d["error"] for k, d in events if k == "error")
    assert "401" in err
    assert "text" not in kinds
    assert await ledger.recent(user_id="x") == []


def test_env_injection_matches_config():
    # The env-layer half of the contract: base_url -> ANTHROPIC_BASE_URL and the
    # cocola token -> ANTHROPIC_API_KEY (never a real Anthropic key).
    cfg = ClaudeSDKConfig(base_url="http://gw:8080", model="default", api_key="cocola-tok")
    prov = ClaudeAgentSDKProvider(cfg, query_fn=lambda **kw: iter(()))
    env = prov._build_env()
    assert env["ANTHROPIC_BASE_URL"] == "http://gw:8080"
    assert env["ANTHROPIC_API_KEY"] == "cocola-tok"
