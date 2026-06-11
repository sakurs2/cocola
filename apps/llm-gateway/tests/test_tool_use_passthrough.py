"""ADR-0010: end-to-end tool-use passthrough through the gateway codec.

Proves the three things M3 dropped now survive:
  1. inbound `tools` / `tool_choice` and tool_result content blocks are parsed
     into the normalized ChatRequest (not flattened away),
  2. a streaming upstream tool_use (content_block_start + input_json_delta +
     stop) is re-emitted verbatim as Anthropic SSE,
  3. the non-stream collector reconstructs the tool_use block (with parsed
     input) from passthrough frames.
"""

from cocola_llm_gateway.anthropic_codec import (
    collect_to_anthropic_response,
    stream_to_anthropic_sse,
    to_chat_request,
)
from cocola_llm_gateway.types import StreamEvent, StreamEventType, Usage


async def _events(seq):
    for ev in seq:
        yield ev


def test_inbound_preserves_tools_and_tool_result_blocks():
    body = {
        "model": "claude-sonnet",
        "max_tokens": 100,
        "tools": [
            {
                "name": "get_weather",
                "description": "Get weather",
                "input_schema": {"type": "object", "properties": {"city": {"type": "string"}}},
            }
        ],
        "tool_choice": {"type": "auto"},
        "messages": [
            {"role": "user", "content": "weather in NYC?"},
            {
                "role": "assistant",
                "content": [
                    {
                        "type": "tool_use",
                        "id": "tu_1",
                        "name": "get_weather",
                        "input": {"city": "NYC"},
                    }
                ],
            },
            {
                "role": "user",
                "content": [
                    {"type": "tool_result", "tool_use_id": "tu_1", "content": "sunny, 20C"}
                ],
            },
        ],
    }
    req = to_chat_request(body, resolved_model="real-model")
    # tools / tool_choice carried opaquely
    assert req.params.tools and req.params.tools[0]["name"] == "get_weather"
    assert req.params.tool_choice == {"type": "auto"}
    # assistant tool_use block preserved verbatim
    assistant = req.messages[1]
    assert assistant.content_blocks is not None
    assert assistant.content_blocks[0]["type"] == "tool_use"
    # user tool_result block preserved verbatim
    tool_result = req.messages[2]
    assert tool_result.content_blocks is not None
    assert tool_result.content_blocks[0]["type"] == "tool_result"
    # plain text message still flattened, no spurious blocks
    assert req.messages[0].content == "weather in NYC?"
    assert req.messages[0].content_blocks is None


def _tool_use_stream():
    return [
        StreamEvent(StreamEventType.MESSAGE_START, usage=Usage(prompt_tokens=5), model="m"),
        StreamEvent(
            StreamEventType.PASSTHROUGH,
            extra={
                "frame": {
                    "type": "content_block_start",
                    "index": 0,
                    "content_block": {
                        "type": "tool_use",
                        "id": "tu_1",
                        "name": "get_weather",
                        "input": {},
                    },
                }
            },
        ),
        StreamEvent(
            StreamEventType.PASSTHROUGH,
            extra={
                "frame": {
                    "type": "content_block_delta",
                    "index": 0,
                    "delta": {"type": "input_json_delta", "partial_json": '{"city":'},
                }
            },
        ),
        StreamEvent(
            StreamEventType.PASSTHROUGH,
            extra={
                "frame": {
                    "type": "content_block_delta",
                    "index": 0,
                    "delta": {"type": "input_json_delta", "partial_json": ' "NYC"}'},
                }
            },
        ),
        StreamEvent(
            StreamEventType.PASSTHROUGH,
            extra={"frame": {"type": "content_block_stop", "index": 0}},
        ),
        StreamEvent(
            StreamEventType.MESSAGE_DELTA,
            usage=Usage(completion_tokens=7),
            finish_reason="tool_use",
        ),
        StreamEvent(StreamEventType.MESSAGE_STOP),
    ]


async def test_stream_relays_tool_use_verbatim():
    frames = b"".join(
        [f async for f in stream_to_anthropic_sse(_events(_tool_use_stream()), fallback_model="m")]
    )
    text = frames.decode()
    order = [ln.split(":", 1)[1].strip() for ln in text.splitlines() if ln.startswith("event:")]
    assert order == [
        "message_start",
        "content_block_start",
        "content_block_delta",
        "content_block_delta",
        "content_block_stop",
        "message_delta",
        "message_stop",
    ]
    # tool_use identity + incremental args survive
    assert '"type": "tool_use"' in text
    assert '"name": "get_weather"' in text
    assert '"input_json_delta"' in text
    assert '"tool_use"' in text  # stop_reason in message_delta
    # NO phantom text block was synthesized
    assert '"text_delta"' not in text


async def test_collect_reconstructs_tool_use_block():
    out = await collect_to_anthropic_response(_events(_tool_use_stream()), fallback_model="m")
    assert out["stop_reason"] == "tool_use"
    assert len(out["content"]) == 1
    blk = out["content"][0]
    assert blk["type"] == "tool_use"
    assert blk["id"] == "tu_1"
    assert blk["name"] == "get_weather"
    assert blk["input"] == {"city": "NYC"}  # partial_json fragments parsed


async def test_text_only_path_still_synthesizes_single_block():
    """Regression: fake / openai-compat providers emit CONTENT_DELTA, which must
    still produce one index-0 text block (no passthrough)."""
    seq = [
        StreamEvent(StreamEventType.MESSAGE_START, usage=Usage(prompt_tokens=1), model="m"),
        StreamEvent(StreamEventType.CONTENT_DELTA, text="hi"),
        StreamEvent(
            StreamEventType.MESSAGE_DELTA,
            usage=Usage(completion_tokens=1),
            finish_reason="end_turn",
        ),
        StreamEvent(StreamEventType.MESSAGE_STOP),
    ]
    out = await collect_to_anthropic_response(_events(seq), fallback_model="m")
    assert out["content"] == [{"type": "text", "text": "hi"}]
