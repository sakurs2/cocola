"""Non-streaming fallback for the Anthropic upstream (config knob `stream=False`).

Some Anthropic-compatible relays have a working non-stream endpoint but a broken
SSE endpoint (accepts the request, returns HTTP 200, never emits a byte -> the
gateway hangs until httpx times out). `AnthropicConfig.stream=False` makes the
upstream POST once, read the whole JSON, and re-synthesize the downstream
StreamEvent sequence locally so the gateway still presents a stream to clients.

These tests stub the network with an httpx MockTransport so no real endpoint or
key is touched (ADR-0004).
"""

import json

import httpx
from cocola_llm_gateway.anthropic_codec import collect_to_anthropic_response
from cocola_llm_gateway.types import ChatMessage, ChatParams, ChatRequest, StreamEventType
from cocola_llm_gateway.upstream.anthropic import AnthropicConfig, AnthropicUpstream


def _req(text="hi"):
    return ChatRequest(
        model="claude-x",
        messages=[ChatMessage(role="user", content=text)],
        params=ChatParams(max_tokens=64),
    )


def _upstream_with_handler(handler, *, stream=False):
    """Build an AnthropicUpstream whose httpx client is backed by MockTransport."""
    up = AnthropicUpstream(AnthropicConfig(api_key="k", stream=stream))
    up._client = httpx.AsyncClient(
        base_url="https://relay.test",
        transport=httpx.MockTransport(handler),
        headers={"x-api-key": "k"},
    )
    return up


async def _drain(up, req):
    return [ev async for ev in up.chat_stream(req)]


async def test_nonstream_sends_stream_false_and_yields_text():
    captured = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["body"] = json.loads(request.content)
        return httpx.Response(
            200,
            json={
                "id": "msg_1",
                "type": "message",
                "role": "assistant",
                "model": "claude-x",
                "content": [{"type": "text", "text": "Hi there!"}],
                "stop_reason": "end_turn",
                "usage": {"input_tokens": 5, "output_tokens": 3},
            },
        )

    up = _upstream_with_handler(handler, stream=False)
    events = await _drain(up, _req())

    # The payload must ask the upstream for a NON-streaming response.
    assert captured["body"]["stream"] is False

    # Event shape: message_start -> passthrough(start/delta/stop) -> message_delta -> message_stop
    assert events[0].type is StreamEventType.MESSAGE_START
    assert events[0].usage.prompt_tokens == 5
    assert events[-1].type is StreamEventType.MESSAGE_STOP
    assert events[-2].type is StreamEventType.MESSAGE_DELTA
    assert events[-2].usage.completion_tokens == 3
    assert events[-2].finish_reason == "end_turn"

    # The codec must reconstruct the exact text from the synthesized frames.
    resp = await collect_to_anthropic_response(_iter(events), fallback_model="claude-x")
    assert resp["content"] == [{"type": "text", "text": "Hi there!"}]
    assert resp["usage"] == {"input_tokens": 5, "output_tokens": 3}
    assert resp["stop_reason"] == "end_turn"
    await up.aclose()


async def test_nonstream_reconstructs_tool_use_block():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "id": "msg_2",
                "type": "message",
                "role": "assistant",
                "model": "claude-x",
                "content": [
                    {
                        "type": "tool_use",
                        "id": "tu_1",
                        "name": "get_weather",
                        "input": {"city": "NYC"},
                    }
                ],
                "stop_reason": "tool_use",
                "usage": {"input_tokens": 7, "output_tokens": 2},
            },
        )

    up = _upstream_with_handler(handler, stream=False)
    events = await _drain(up, _req("weather?"))
    resp = await collect_to_anthropic_response(_iter(events), fallback_model="claude-x")
    assert resp["content"] == [
        {"type": "tool_use", "id": "tu_1", "name": "get_weather", "input": {"city": "NYC"}}
    ]
    assert resp["stop_reason"] == "tool_use"
    await up.aclose()


async def test_nonstream_timeout_maps_to_error_event():
    def handler(request: httpx.Request) -> httpx.Response:
        raise httpx.ReadTimeout("boom", request=request)

    up = _upstream_with_handler(handler, stream=False)
    events = await _drain(up, _req())
    assert len(events) == 1
    assert events[0].type is StreamEventType.ERROR
    assert events[0].code == "timeout"
    await up.aclose()


async def test_nonstream_http_error_maps_to_error_event():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(429, text="rate limited")

    up = _upstream_with_handler(handler, stream=False)
    events = await _drain(up, _req())
    assert len(events) == 1
    assert events[0].type is StreamEventType.ERROR
    assert events[0].code == "upstream_http_error"
    assert "429" in events[0].error
    await up.aclose()


async def test_stream_true_still_uses_sse_endpoint():
    """stream=True must keep the historical SSE path (payload stream=True)."""
    captured = {}
    start = json.dumps(
        {"type": "message_start", "message": {"model": "claude-x", "usage": {"input_tokens": 1}}}
    )
    sse = (
        f"event: message_start\ndata: {start}\n\n"
        'event: message_stop\ndata: {"type":"message_stop"}\n\n'
    )

    def handler(request: httpx.Request) -> httpx.Response:
        captured["body"] = json.loads(request.content)
        return httpx.Response(200, text=sse, headers={"content-type": "text/event-stream"})

    up = _upstream_with_handler(handler, stream=True)
    events = await _drain(up, _req())
    assert captured["body"]["stream"] is True
    assert events[0].type is StreamEventType.MESSAGE_START
    await up.aclose()


async def _iter(seq):
    for ev in seq:
        yield ev

