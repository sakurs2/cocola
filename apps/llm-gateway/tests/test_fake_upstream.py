from cocola_llm_gateway.types import ChatMessage, ChatParams, ChatRequest, StreamEventType
from cocola_llm_gateway.upstream.fake import FakeUpstream


def _req(text="hello world foo"):
    return ChatRequest(
        model="fake-1",
        messages=[ChatMessage(role="user", content=text)],
        params=ChatParams(),
    )


async def test_fake_emits_full_event_sequence():
    up = FakeUpstream(reply="echo: hello world foo", chunk_size=4)
    events = [ev async for ev in up.chat_stream(_req())]
    kinds = [e.type for e in events]
    assert kinds[0] is StreamEventType.MESSAGE_START
    assert kinds[-1] is StreamEventType.MESSAGE_STOP
    assert StreamEventType.CONTENT_DELTA in kinds
    assert StreamEventType.MESSAGE_DELTA in kinds


async def test_fake_default_reply_echoes_last_user():
    up = FakeUpstream()  # no reply -> derives "echo: <last user>"
    events = [ev async for ev in up.chat_stream(_req("ping"))]
    text = "".join(e.text for e in events if e.type is StreamEventType.CONTENT_DELTA)
    assert text == "echo: ping"


async def test_fake_usage_tokens_are_reported():
    up = FakeUpstream(reply="abcdef", chunk_size=2)
    start = None
    delta = None
    async for ev in up.chat_stream(_req("one two three")):
        if ev.type is StreamEventType.MESSAGE_START:
            start = ev
        elif ev.type is StreamEventType.MESSAGE_DELTA:
            delta = ev
    assert start.usage.prompt_tokens == 3  # word count
    assert delta.usage.completion_tokens == 3  # 6 chars / chunk 2


async def test_fake_tool_use_mode_emits_passthrough_frames():
    """ADR-0010: in tool_call mode the fake emits Anthropic-shaped PASSTHROUGH
    frames (content_block_start/_delta/_stop) and stop_reason=tool_use, instead
    of a text turn."""
    import json

    up = FakeUpstream(
        tool_call={"id": "tu_1", "name": "get_weather", "input": {"city": "NYC"}},
        chunk_size=4,
    )
    events = [ev async for ev in up.chat_stream(_req("weather?"))]
    kinds = [e.type for e in events]
    assert kinds[0] is StreamEventType.MESSAGE_START
    assert kinds[-1] is StreamEventType.MESSAGE_STOP
    # No legacy text path in tool mode.
    assert StreamEventType.CONTENT_DELTA not in kinds
    assert StreamEventType.PASSTHROUGH in kinds

    frames = [e.extra["frame"] for e in events if e.type is StreamEventType.PASSTHROUGH]
    assert frames[0]["type"] == "content_block_start"
    assert frames[0]["content_block"]["type"] == "tool_use"
    assert frames[0]["content_block"]["name"] == "get_weather"
    assert frames[-1]["type"] == "content_block_stop"

    # The streamed input_json_delta fragments concatenate to the full args.
    joined = "".join(
        f["delta"]["partial_json"] for f in frames if f["type"] == "content_block_delta"
    )
    assert json.loads(joined) == {"city": "NYC"}

    mdelta = next(e for e in events if e.type is StreamEventType.MESSAGE_DELTA)
    assert mdelta.finish_reason == "tool_use"


async def test_fake_text_mode_unchanged_by_tool_support():
    """Regression: omitting tool_call keeps the exact text behavior tests rely
    on (backward compatibility of the additive change)."""
    up = FakeUpstream()
    events = [ev async for ev in up.chat_stream(_req("ping"))]
    kinds = [e.type for e in events]
    assert StreamEventType.CONTENT_DELTA in kinds
    assert StreamEventType.PASSTHROUGH not in kinds
    text = "".join(e.text for e in events if e.type is StreamEventType.CONTENT_DELTA)
    assert text == "echo: ping"
