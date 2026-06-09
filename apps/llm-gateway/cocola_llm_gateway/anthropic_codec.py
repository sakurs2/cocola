"""Anthropic Messages API front-end codec.

The gateway's public face is Anthropic-compatible (`POST /v1/messages`) because
that is the only wire format the Claude Agent SDK speaks. This module is the
ONLY place that knows the Anthropic schema on the inbound/outbound edge:

    Anthropic request JSON  --parse-->  ChatRequest        (normalized in)
    StreamEvent stream      --encode--> Anthropic SSE bytes (normalized out)
    StreamEvent stream      --collect-> Anthropic JSON      (non-stream out)

Keeping this isolated means the rest of the gateway (routing, billing, upstream
adapters) never imports the vendor schema, and a second front-end (e.g. an
OpenAI-style `/v1/chat/completions`) can be added later as a sibling codec.

SSE event sequence we emit (matching Anthropic's documented order):
    message_start -> content_block_start -> (content_block_delta)* ->
    content_block_stop -> message_delta -> message_stop
"""
from __future__ import annotations

import json
from collections.abc import AsyncIterator
from typing import Any

from pydantic import BaseModel, Field

from cocola_llm_gateway.types import (
    ChatMessage,
    ChatParams,
    ChatRequest,
    StreamEvent,
    StreamEventType,
    Usage,
)

# --------------------------------------------------------------------------- #
# Inbound: Anthropic request -> ChatRequest
# --------------------------------------------------------------------------- #


class AnthropicMessage(BaseModel):
    role: str
    # Anthropic allows a plain string OR a list of content blocks.
    content: Any


class AnthropicRequest(BaseModel):
    """Subset of the Anthropic Messages request we honor in M3.

    Unknown fields are ignored (forward-compatible). `system` is a top-level
    field in the Anthropic schema, not a message, so we lift it into our
    normalized message list as a leading system turn.
    """

    model: str
    messages: list[AnthropicMessage]
    max_tokens: int = Field(default=1024, ge=1)
    system: Any = None
    temperature: float | None = None
    top_p: float | None = None
    stop_sequences: list[str] = Field(default_factory=list)
    stream: bool = False

    model_config = {"extra": "ignore"}


def _flatten_content(content: Any) -> str:
    """Collapse Anthropic content (string | list[block]) to plain text.

    Only `text` blocks are preserved in M3; tool_use/tool_result/image blocks
    are dropped here (richer passthrough is an additive follow-up). This is
    lossy on purpose and documented as a known M3 limitation.
    """
    if content is None:
        return ""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts: list[str] = []
        for block in content:
            if isinstance(block, dict):
                if block.get("type") == "text":
                    parts.append(str(block.get("text", "")))
            elif isinstance(block, str):
                parts.append(block)
        return "".join(parts)
    return str(content)


def to_chat_request(
    body: dict[str, Any],
    *,
    resolved_model: str,
    user_id: str = "",
    session_id: str = "",
    metadata: dict[str, str] | None = None,
) -> ChatRequest:
    """Parse a raw Anthropic request body into a normalized ChatRequest.

    `resolved_model` is the real upstream model id the router picked; the
    caller-facing alias (body['model']) is preserved in metadata for billing.
    """
    req = AnthropicRequest.model_validate(body)
    messages: list[ChatMessage] = []

    system_text = _flatten_content(req.system)
    if system_text:
        messages.append(ChatMessage(role="system", content=system_text))

    for m in req.messages:
        role = m.role if m.role in ("user", "assistant", "system") else "user"
        messages.append(ChatMessage(role=role, content=_flatten_content(m.content)))

    params = ChatParams(
        max_tokens=req.max_tokens,
        temperature=req.temperature,
        top_p=req.top_p,
        stop=list(req.stop_sequences),
        stream=req.stream,
    )
    meta = dict(metadata or {})
    meta.setdefault("requested_model", req.model)
    return ChatRequest(
        model=resolved_model,
        messages=messages,
        params=params,
        user_id=user_id,
        session_id=session_id,
        metadata=meta,
    )


# --------------------------------------------------------------------------- #
# Outbound: SSE framing
# --------------------------------------------------------------------------- #


def sse_frame(event: str, data: dict[str, Any]) -> bytes:
    """Encode a single SSE frame (`event:` + `data:` + blank line)."""
    return f"event: {event}\ndata: {json.dumps(data, ensure_ascii=False)}\n\n".encode()


def _message_start_payload(message_id: str, model: str, usage: Usage) -> dict[str, Any]:
    return {
        "type": "message_start",
        "message": {
            "id": message_id,
            "type": "message",
            "role": "assistant",
            "model": model,
            "content": [],
            "stop_reason": None,
            "stop_sequence": None,
            "usage": {
                "input_tokens": usage.prompt_tokens,
                "output_tokens": usage.completion_tokens,
            },
        },
    }


async def stream_to_anthropic_sse(
    events: AsyncIterator[StreamEvent],
    *,
    message_id: str = "msg_cocola",
    fallback_model: str = "",
) -> AsyncIterator[bytes]:
    """Transcode a normalized StreamEvent stream into Anthropic SSE bytes.

    We synthesize the content_block_start/stop frames the SDK expects around the
    text deltas, and fold our terminal MESSAGE_STOP into Anthropic's
    message_delta(usage,stop_reason) + message_stop pair.
    """
    started = False
    block_open = False
    out_tokens = 0
    model = fallback_model
    finish_reason = "end_turn"

    def _ensure_start(usage: Usage | None) -> list[bytes]:
        nonlocal started, block_open
        frames: list[bytes] = []
        if not started:
            started = True
            frames.append(
                sse_frame("message_start", _message_start_payload(message_id, model, usage or Usage()))
            )
            frames.append(
                sse_frame(
                    "content_block_start",
                    {"type": "content_block_start", "index": 0,
                     "content_block": {"type": "text", "text": ""}},
                )
            )
            block_open = True
        return frames

    # Drive the source via an explicit iterator so we can guarantee it is
    # closed even when we stop early (break/return). Closing the source throws
    # GeneratorExit into service.chat_stream, which runs its metering `finally`
    # — without this, billing on the streaming path would only fire on GC.
    agen = events.__aiter__()
    errored = False
    try:
        async for ev in agen:
            if ev.type is StreamEventType.MESSAGE_START:
                if ev.model:
                    model = ev.model
                for f in _ensure_start(ev.usage):
                    yield f
            elif ev.type is StreamEventType.CONTENT_DELTA:
                for f in _ensure_start(None):
                    yield f
                yield sse_frame(
                    "content_block_delta",
                    {"type": "content_block_delta", "index": 0,
                     "delta": {"type": "text_delta", "text": ev.text}},
                )
            elif ev.type is StreamEventType.MESSAGE_DELTA:
                if ev.usage is not None:
                    out_tokens += ev.usage.completion_tokens
                if ev.finish_reason:
                    finish_reason = ev.finish_reason
            elif ev.type is StreamEventType.ERROR:
                # Surface an error frame; SDK treats this as a stream error.
                for f in _ensure_start(None):
                    yield f
                yield sse_frame(
                    "error",
                    {"type": "error",
                     "error": {"type": ev.code or "api_error", "message": ev.error}},
                )
                if block_open:
                    yield sse_frame("content_block_stop", {"type": "content_block_stop", "index": 0})
                yield sse_frame("message_stop", {"type": "message_stop"})
                errored = True
                break
            elif ev.type is StreamEventType.MESSAGE_STOP:
                break
    finally:
        aclose = getattr(agen, "aclose", None)
        if aclose is not None:
            await aclose()

    if errored:
        return

    # Normal termination.
    for f in _ensure_start(None):
        yield f
    if block_open:
        yield sse_frame("content_block_stop", {"type": "content_block_stop", "index": 0})
    yield sse_frame(
        "message_delta",
        {"type": "message_delta",
         "delta": {"stop_reason": finish_reason, "stop_sequence": None},
         "usage": {"output_tokens": out_tokens}},
    )
    yield sse_frame("message_stop", {"type": "message_stop"})


# --------------------------------------------------------------------------- #
# Outbound: non-streaming JSON
# --------------------------------------------------------------------------- #


async def collect_to_anthropic_response(
    events: AsyncIterator[StreamEvent],
    *,
    message_id: str = "msg_cocola",
    fallback_model: str = "",
) -> dict[str, Any]:
    """Drain a StreamEvent stream into a single Anthropic Messages response.

    Used when the client sends `stream:false`. Token usage is aggregated the
    same way the metering hook does, so billing is consistent regardless of
    stream mode.
    """
    text_parts: list[str] = []
    usage = Usage()
    model = fallback_model
    finish_reason = "end_turn"

    # Drive via an explicit iterator and close it in `finally` so that breaking
    # early (on MESSAGE_STOP) still throws GeneratorExit into service.chat_stream
    # and runs its metering `finally`. Without this, non-stream billing would
    # only fire on nondeterministic GC.
    agen = events.__aiter__()
    try:
        async for ev in agen:
            if ev.type is StreamEventType.MESSAGE_START:
                if ev.model:
                    model = ev.model
                if ev.usage is not None:
                    usage.merge(ev.usage)
            elif ev.type is StreamEventType.CONTENT_DELTA:
                text_parts.append(ev.text)
            elif ev.type is StreamEventType.MESSAGE_DELTA:
                if ev.usage is not None:
                    usage.merge(ev.usage)
                if ev.finish_reason:
                    finish_reason = ev.finish_reason
            elif ev.type is StreamEventType.ERROR:
                raise RuntimeError(ev.error or "upstream error")
            elif ev.type is StreamEventType.MESSAGE_STOP:
                break
    finally:
        aclose = getattr(agen, "aclose", None)
        if aclose is not None:
            await aclose()

    return {
        "id": message_id,
        "type": "message",
        "role": "assistant",
        "model": model,
        "content": [{"type": "text", "text": "".join(text_parts)}],
        "stop_reason": finish_reason,
        "stop_sequence": None,
        "usage": {
            "input_tokens": usage.prompt_tokens,
            "output_tokens": usage.completion_tokens,
        },
    }
