from cocola_llm_gateway.anthropic_codec import (
    collect_to_anthropic_response,
    stream_to_anthropic_sse,
    to_chat_request,
)
from cocola_llm_gateway.types import StreamEvent, StreamEventType, Usage


async def _events(seq):
    for ev in seq:
        yield ev


def test_to_chat_request_lifts_system_and_keeps_alias():
    body = {
        "model": "claude-sonnet",
        "system": "be terse",
        "messages": [{"role": "user", "content": "hi"}],
        "max_tokens": 50,
    }
    req = to_chat_request(body, resolved_model="real-model", user_id="U1", session_id="S1")
    assert req.model == "real-model"
    assert req.metadata["requested_model"] == "claude-sonnet"
    assert req.messages[0].role == "system"
    assert req.messages[0].content == "be terse"
    assert req.user_id == "U1"


def test_to_chat_request_flattens_block_content():
    body = {
        "model": "m",
        "messages": [{"role": "user", "content": [
            {"type": "text", "text": "a"},
            {"type": "image", "source": {}},
            {"type": "text", "text": "b"},
        ]}],
    }
    req = to_chat_request(body, resolved_model="m")
    assert req.messages[-1].content == "ab"


async def test_stream_to_sse_event_order():
    seq = [
        StreamEvent(StreamEventType.MESSAGE_START, usage=Usage(prompt_tokens=2), model="m"),
        StreamEvent(StreamEventType.CONTENT_DELTA, text="he"),
        StreamEvent(StreamEventType.CONTENT_DELTA, text="llo"),
        StreamEvent(StreamEventType.MESSAGE_DELTA, usage=Usage(completion_tokens=2),
                    finish_reason="end_turn"),
        StreamEvent(StreamEventType.MESSAGE_STOP),
    ]
    frames = b"".join([f async for f in stream_to_anthropic_sse(_events(seq), fallback_model="m")])
    text = frames.decode()
    order = [ln.split(":", 1)[1].strip() for ln in text.splitlines() if ln.startswith("event:")]
    assert order == [
        "message_start", "content_block_start",
        "content_block_delta", "content_block_delta",
        "content_block_stop", "message_delta", "message_stop",
    ]
    assert '"text": "he"' in text and '"text": "llo"' in text


async def test_sse_closes_source_generator():
    """The codec must aclose() the source so an upstream's finally runs even
    though we break on MESSAGE_STOP (this is the billing-on-stream guarantee)."""
    closed = {"v": False}

    async def gen():
        try:
            yield StreamEvent(StreamEventType.MESSAGE_START, usage=Usage(prompt_tokens=1), model="m")
            yield StreamEvent(StreamEventType.CONTENT_DELTA, text="x")
            yield StreamEvent(StreamEventType.MESSAGE_STOP)
            yield StreamEvent(StreamEventType.CONTENT_DELTA, text="never")
        finally:
            closed["v"] = True

    _ = b"".join([f async for f in stream_to_anthropic_sse(gen(), fallback_model="m")])
    assert closed["v"] is True


async def test_collect_closes_source_generator():
    closed = {"v": False}

    async def gen():
        try:
            yield StreamEvent(StreamEventType.MESSAGE_START, usage=Usage(prompt_tokens=1), model="m")
            yield StreamEvent(StreamEventType.CONTENT_DELTA, text="hi")
            yield StreamEvent(StreamEventType.MESSAGE_STOP)
        finally:
            closed["v"] = True

    out = await collect_to_anthropic_response(gen(), fallback_model="m")
    assert out["content"][0]["text"] == "hi"
    assert closed["v"] is True
