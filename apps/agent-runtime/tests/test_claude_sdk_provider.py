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


@dataclass
class StreamEvent:
    event: dict
    uuid: str = "u1"
    session_id: str = "S1"
    parent_tool_use_id: object = None


def _text_delta(text):
    return StreamEvent(
        event={"type": "content_block_delta", "delta": {"type": "text_delta", "text": text}}
    )


def _thinking_delta(text):
    return StreamEvent(
        event={"type": "content_block_delta", "delta": {"type": "thinking_delta", "thinking": text}}
    )


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


# ---- token streaming (include_partial) -------------------------------------
#
# These drive the mapper directly with include_partial=True, mirroring the real
# SDK path the provider takes when it owns query_fn. We can't flip the provider's
# own _include_partial via an injected fake (that fake forces it False), so we
# exercise the mapping seam that the flag gates.

from cocola_agent_runtime.claude_sdk_provider import (  # noqa: E402
    _message_to_events,
    _stream_event_to_events,
)


def test_stream_event_text_delta_maps_to_text():
    out = _stream_event_to_events(
        {"type": "content_block_delta", "delta": {"type": "text_delta", "text": "Hel"}}
    )
    assert [(e.kind, e.data) for e in out] == [("text", {"text": "Hel"})]


def test_stream_event_thinking_delta_maps_to_thinking():
    out = _stream_event_to_events(
        {"type": "content_block_delta", "delta": {"type": "thinking_delta", "thinking": "hm"}}
    )
    assert [(e.kind, e.data) for e in out] == [("thinking", {"thinking": "hm"})]


def test_stream_event_control_frames_are_dropped():
    assert _stream_event_to_events({"type": "message_start"}) == []
    assert _stream_event_to_events({"type": "content_block_stop"}) == []
    assert _stream_event_to_events(None) == []
    # empty deltas produce nothing (no blank events)
    assert (
        _stream_event_to_events(
            {"type": "content_block_delta", "delta": {"type": "text_delta", "text": ""}}
        )
        == []
    )


def test_partial_streaming_maps_deltas_and_dedups_final_block():
    # A realistic streamed turn: three text deltas, then the SDK's end-of-turn
    # AssistantMessage carrying the SAME full text plus a tool use.
    messages = [
        StreamEvent(
            event={"type": "content_block_delta", "delta": {"type": "text_delta", "text": "Hel"}}
        ),
        StreamEvent(
            event={"type": "content_block_delta", "delta": {"type": "text_delta", "text": "lo"}}
        ),
        AssistantMessage(
            content=[TextBlock("Hello"), ToolUseBlock(id="t1", name="bash", input={})]
        ),
    ]
    out = []
    for m in messages:
        out.extend(_message_to_events(m, include_partial=True))
    kinds = [(e.kind, e.data.get("text")) for e in out if e.kind == "text"]
    # Two token events, and the duplicate full TextBlock was suppressed.
    assert kinds == [("text", "Hel"), ("text", "lo")]
    # Non-text blocks in the final message still flow through.
    assert any(e.kind == "tool_use" for e in out)


def test_non_partial_keeps_full_block():
    # Without partial streaming the full TextBlock is the only source of text.
    out = _message_to_events(AssistantMessage(content=[TextBlock("Hello")]), include_partial=False)
    assert [(e.kind, e.data) for e in out] == [("text", {"text": "Hello"})]
