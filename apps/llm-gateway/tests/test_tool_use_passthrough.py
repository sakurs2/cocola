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
from cocola_llm_gateway.upstream.anthropic import _build_payload, _tool_turn_violations


async def _events(seq):
    for ev in seq:
        yield ev


def _assert_tool_results_immediately_after(payload):
    assert _tool_turn_violations(payload["messages"]) == []
    messages = payload["messages"]
    for idx, message in enumerate(messages):
        if message["role"] != "assistant" or not isinstance(message["content"], list):
            continue
        tool_use_ids = [
            block["id"]
            for block in message["content"]
            if isinstance(block, dict) and block.get("type") == "tool_use"
        ]
        if not tool_use_ids:
            continue
        next_message = messages[idx + 1]
        assert next_message["role"] == "user"
        assert isinstance(next_message["content"], list)
        assert [
            block.get("tool_use_id")
            for block in next_message["content"][: len(tool_use_ids)]
            if isinstance(block, dict) and block.get("type") == "tool_result"
        ] == tool_use_ids


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


def test_anthropic_payload_promotes_tool_result_blocks_before_text():
    body = {
        "model": "claude-sonnet",
        "max_tokens": 100,
        "messages": [
            {"role": "user", "content": "compile this"},
            {
                "role": "assistant",
                "content": [
                    {"type": "text", "text": "I'll run the compiler."},
                    {
                        "type": "tool_use",
                        "id": "call_1",
                        "name": "Bash",
                        "input": {"command": "cc main.c"},
                    },
                ],
            },
            {
                "role": "user",
                "content": [
                    {"type": "text", "text": "<system-reminder>continue</system-reminder>"},
                    {"type": "tool_result", "tool_use_id": "call_1", "content": "ok"},
                ],
            },
        ],
    }
    req = to_chat_request(body, resolved_model="real-model")

    payload = _build_payload(req, stream=True)

    _assert_tool_results_immediately_after(payload)
    assert len(payload["messages"]) == 4
    user_after_tool = payload["messages"][2]
    assert user_after_tool["content"][0]["type"] == "tool_result"
    assert user_after_tool["content"][0]["tool_use_id"] == "call_1"
    assert len(user_after_tool["content"]) == 1
    assert payload["messages"][3]["content"] == [
        {"type": "text", "text": "<system-reminder>continue</system-reminder>"}
    ]


def test_anthropic_payload_moves_late_tool_result_next_to_tool_use():
    body = {
        "model": "claude-sonnet",
        "max_tokens": 100,
        "messages": [
            {"role": "user", "content": "compile this"},
            {
                "role": "assistant",
                "content": [
                    {"type": "text", "text": "I'll run the compiler."},
                    {
                        "type": "tool_use",
                        "id": "call_late",
                        "name": "Bash",
                        "input": {"command": "cc main.c"},
                    },
                ],
            },
            {"role": "user", "content": "continue"},
            {
                "role": "user",
                "content": [
                    {"type": "tool_result", "tool_use_id": "call_late", "content": "compiled"}
                ],
            },
        ],
    }
    req = to_chat_request(body, resolved_model="real-model")

    payload = _build_payload(req, stream=True)

    _assert_tool_results_immediately_after(payload)
    assert len(payload["messages"]) == 4
    user_after_tool = payload["messages"][2]
    assert user_after_tool["content"][0]["type"] == "tool_result"
    assert user_after_tool["content"][0]["tool_use_id"] == "call_late"
    assert len(user_after_tool["content"]) == 1
    assert payload["messages"][3]["content"] == [{"type": "text", "text": "continue"}]


def test_anthropic_payload_moves_tool_use_blocks_after_trailing_thinking_and_text():
    body = {
        "model": "claude-sonnet",
        "max_tokens": 100,
        "messages": [
            {"role": "user", "content": "compile this"},
            {
                "role": "assistant",
                "content": [
                    {"type": "thinking", "thinking": "I need to compile first."},
                    {
                        "type": "tool_use",
                        "id": "call_compile",
                        "name": "Bash",
                        "input": {"command": "cc main.c"},
                    },
                    {"type": "thinking", "thinking": "The compiler result will decide."},
                    {"type": "text", "text": "Running the compiler now."},
                ],
            },
            {
                "role": "user",
                "content": [
                    {"type": "tool_result", "tool_use_id": "call_compile", "content": "ok"}
                ],
            },
        ],
    }
    req = to_chat_request(body, resolved_model="real-model")

    payload = _build_payload(req, stream=True)

    _assert_tool_results_immediately_after(payload)
    assistant_blocks = payload["messages"][1]["content"]
    assert [block["type"] for block in assistant_blocks] == [
        "thinking",
        "thinking",
        "text",
        "tool_use",
    ]
    assert assistant_blocks[-1]["id"] == "call_compile"


def test_anthropic_payload_inserts_missing_tool_result_error():
    body = {
        "model": "claude-sonnet",
        "max_tokens": 100,
        "messages": [
            {"role": "user", "content": "compile this"},
            {
                "role": "assistant",
                "content": [
                    {
                        "type": "tool_use",
                        "id": "call_missing",
                        "name": "Bash",
                        "input": {"command": "cc main.c"},
                    }
                ],
            },
            {"role": "user", "content": "what happened?"},
        ],
    }
    req = to_chat_request(body, resolved_model="real-model")

    payload = _build_payload(req, stream=True)

    _assert_tool_results_immediately_after(payload)
    user_after_tool = payload["messages"][2]
    assert user_after_tool["content"][0]["type"] == "tool_result"
    assert user_after_tool["content"][0]["tool_use_id"] == "call_missing"
    assert user_after_tool["content"][0]["is_error"] is True
    assert len(user_after_tool["content"]) == 1
    assert payload["messages"][3]["content"] == [{"type": "text", "text": "what happened?"}]


def test_anthropic_payload_completes_partial_multi_tool_results():
    body = {
        "model": "claude-sonnet",
        "max_tokens": 100,
        "messages": [
            {"role": "user", "content": "write and compile code"},
            {
                "role": "assistant",
                "content": [
                    {
                        "type": "tool_use",
                        "id": "call_write",
                        "name": "Write",
                        "input": {"file_path": "main.c"},
                    },
                    {
                        "type": "tool_use",
                        "id": "call_compile",
                        "name": "Bash",
                        "input": {"command": "cc main.c"},
                    },
                    {
                        "type": "tool_use",
                        "id": "call_run",
                        "name": "Bash",
                        "input": {"command": "./a.out"},
                    },
                ],
            },
            {
                "role": "user",
                "content": [
                    {"type": "tool_result", "tool_use_id": "call_write", "content": "wrote file"}
                ],
            },
        ],
    }
    req = to_chat_request(body, resolved_model="real-model")

    payload = _build_payload(req, stream=True)

    _assert_tool_results_immediately_after(payload)
    tool_results = payload["messages"][2]["content"]
    assert [block["tool_use_id"] for block in tool_results] == [
        "call_write",
        "call_compile",
        "call_run",
    ]
    assert tool_results[0]["content"] == "wrote file"
    assert tool_results[1]["is_error"] is True
    assert tool_results[2]["is_error"] is True


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
