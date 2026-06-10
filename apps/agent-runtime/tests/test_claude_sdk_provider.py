"""ClaudeAgentSDKProvider mapping tests.

We never spawn the real SDK/CLI here: a fake query_fn yields SDK-shaped
messages (duck-typed by class name) and we assert they map to the runtime's
generic AgentEvent vocabulary. This keeps the test hermetic — no subprocess,
no network, no model.
"""

from dataclasses import dataclass, field

from cocola_agent_runtime.agent_provider import AgentOptions
from cocola_agent_runtime.claude_sdk_provider import (
    ClaudeAgentSDKProvider,
    ClaudeSDKConfig,
)


@dataclass
class TextBlock:
    text: str


@dataclass
class ThinkingBlock:
    thinking: str
    signature: str = ""


@dataclass
class ToolUseBlock:
    id: str
    name: str
    input: dict


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


def _provider(messages):
    async def fake_query(*, prompt, options=None, transport=None):
        for m in messages:
            yield m

    cfg = ClaudeSDKConfig(base_url="http://gw", model="default")
    return ClaudeAgentSDKProvider(cfg, query_fn=fake_query)


async def _run(prov, prompt="hi"):
    opts = AgentOptions(user_id="U1", session_id="S1")
    return [(e.kind, e.data) async for e in prov.query(prompt, opts)]


async def test_maps_text_and_appends_done():
    prov = _provider([AssistantMessage(content=[TextBlock("hello")])])
    out = await _run(prov)
    assert out[0] == ("text", {"text": "hello"})
    assert out[-1][0] == "done"


async def test_maps_tool_use_and_thinking():
    msg = AssistantMessage(
        content=[
            ThinkingBlock(thinking="hmm"),
            ToolUseBlock(id="t1", name="bash", input={"cmd": "ls"}),
            TextBlock("after tool"),
        ]
    )
    out = await _run(_provider([msg]))
    kinds = [k for k, _ in out]
    assert kinds == ["thinking", "tool_use", "text", "done"]
    tu = next(d for k, d in out if k == "tool_use")
    assert tu["name"] == "bash" and tu["input"] == {"cmd": "ls"}


async def test_maps_result_message():
    out = await _run(_provider([ResultMessage(num_turns=3, total_cost_usd=0.01)]))
    res = next(d for k, d in out if k == "result")
    assert res["num_turns"] == 3
    assert res["total_cost_usd"] == 0.01
    assert res["is_error"] is False


async def test_assistant_error_surfaces():
    out = await _run(_provider([AssistantMessage(content=[], error="overloaded")]))
    kinds = [k for k, _ in out]
    assert "error" in kinds


def test_build_env_injects_base_url_and_key():
    cfg = ClaudeSDKConfig(base_url="http://gw:9000", model="default", api_key="K")
    prov = ClaudeAgentSDKProvider(cfg, query_fn=lambda **kw: iter(()))
    env = prov._build_env()
    assert env["ANTHROPIC_BASE_URL"] == "http://gw:9000"
    # cocola routes the SDK at its own gateway with a cocola-issued JWT, so the
    # credential goes in ANTHROPIC_AUTH_TOKEN (Authorization: Bearer), NOT
    # ANTHROPIC_API_KEY (x-api-key, reserved for direct Anthropic keys).
    assert env["ANTHROPIC_AUTH_TOKEN"] == "K"
    assert "ANTHROPIC_API_KEY" not in env
