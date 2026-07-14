"""Anthropic Messages API front-end codec.

The gateway's public face is Anthropic-compatible (`POST /v1/messages`) because
that is the only wire format the Claude Agent SDK speaks. This module is the
ONLY place that knows the Anthropic schema on the inbound/outbound edge:

    Anthropic request JSON  --parse-->  ChatRequest        (normalized in)
    StreamEvent stream      --encode--> Anthropic SSE bytes (normalized out)
    StreamEvent stream      --collect-> Anthropic JSON      (non-stream out)

Keeping this isolated means the rest of the gateway (routing, billing, upstream
adapters) never imports the vendor schema, and a different public protocol can
be added later as a sibling codec.

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
    # ADR-0010: opaque tool definitions / choice, preserved for passthrough.
    tools: list[dict[str, Any]] = Field(default_factory=list)
    tool_choice: dict[str, Any] | None = None

    model_config = {"extra": "ignore"}


def _has_non_text_block(content: Any) -> bool:
    """True if `content` is a block array carrying any non-text block
    (tool_use / tool_result / image). Such content must be preserved verbatim
    (ADR-0010) rather than flattened to text."""
    if not isinstance(content, list):
        return False
    for block in content:
        if isinstance(block, dict) and block.get("type") not in (None, "text"):
            return True
    return False


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
        # ADR-0010: keep the raw block array when it carries non-text blocks
        # (tool_use / tool_result / image) so nothing is lost on the way to the
        # upstream; `content` keeps the text-flattened form for billing.
        blocks = m.content if _has_non_text_block(m.content) else None
        messages.append(
            ChatMessage(
                role=role,
                content=_flatten_content(m.content),
                content_blocks=blocks,
            )
        )

    params = ChatParams(
        max_tokens=req.max_tokens,
        temperature=req.temperature,
        top_p=req.top_p,
        stop=list(req.stop_sequences),
        stream=req.stream,
        tools=list(req.tools),
        tool_choice=req.tool_choice,
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
    text_block_open = False
    out_tokens = 0
    model = fallback_model
    finish_reason = "end_turn"

    def _ensure_message_start(usage: Usage | None) -> list[bytes]:
        """Emit `message_start` exactly once. NOTE: unlike the M3 version this
        no longer synthesizes a text content_block_start — that is deferred to
        `_ensure_text_block` so PASSTHROUGH (ADR-0010) can drive its own block
        indices for tool_use without colliding with a phantom index-0 text
        block. The two modes are mutually exclusive within one response."""
        nonlocal started
        frames: list[bytes] = []
        if not started:
            started = True
            frames.append(
                sse_frame(
                    "message_start", _message_start_payload(message_id, model, usage or Usage())
                )
            )
        return frames

    def _ensure_text_block() -> list[bytes]:
        """Open the legacy single index-0 text block on demand. Used only by
        providers that emit CONTENT_DELTA (fake / openai-compat). Anthropic
        passthrough never calls this — it ships its own block frames."""
        nonlocal text_block_open
        frames: list[bytes] = []
        if not text_block_open:
            text_block_open = True
            frames.append(
                sse_frame(
                    "content_block_start",
                    {
                        "type": "content_block_start",
                        "index": 0,
                        "content_block": {"type": "text", "text": ""},
                    },
                )
            )
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
                for f in _ensure_message_start(ev.usage):
                    yield f
            elif ev.type is StreamEventType.PASSTHROUGH:
                # ADR-0010: relay the raw upstream Anthropic content-block frame
                # verbatim (covers text_delta AND tool_use input_json_delta).
                for f in _ensure_message_start(None):
                    yield f
                frame = ev.extra.get("frame")
                if isinstance(frame, dict):
                    yield sse_frame(str(frame.get("type", "")), frame)
            elif ev.type is StreamEventType.CONTENT_DELTA:
                for f in _ensure_message_start(None):
                    yield f
                for f in _ensure_text_block():
                    yield f
                yield sse_frame(
                    "content_block_delta",
                    {
                        "type": "content_block_delta",
                        "index": 0,
                        "delta": {"type": "text_delta", "text": ev.text},
                    },
                )
            elif ev.type is StreamEventType.MESSAGE_DELTA:
                if ev.usage is not None:
                    out_tokens += ev.usage.completion_tokens
                if ev.finish_reason:
                    finish_reason = ev.finish_reason
            elif ev.type is StreamEventType.ERROR:
                # Surface an error frame; SDK treats this as a stream error.
                for f in _ensure_message_start(None):
                    yield f
                yield sse_frame(
                    "error",
                    {
                        "type": "error",
                        "error": {"type": ev.code or "api_error", "message": ev.error},
                    },
                )
                if text_block_open:
                    yield sse_frame(
                        "content_block_stop", {"type": "content_block_stop", "index": 0}
                    )
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
    for f in _ensure_message_start(None):
        yield f
    if text_block_open:
        yield sse_frame("content_block_stop", {"type": "content_block_stop", "index": 0})
    yield sse_frame(
        "message_delta",
        {
            "type": "message_delta",
            "delta": {"stop_reason": finish_reason, "stop_sequence": None},
            "usage": {"output_tokens": out_tokens},
        },
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

    # ADR-0010: reconstruct rich content blocks (tool_use etc.) from PASSTHROUGH
    # frames, keyed by the upstream's block index. `partial_json` deltas for a
    # tool_use block arrive incrementally and are concatenated, then parsed once
    # at content_block_stop. Falls back to a single text block when no
    # passthrough frames are seen (fake / openai-compat providers).
    blocks: dict[int, dict[str, Any]] = {}
    json_buf: dict[int, list[str]] = {}
    block_order: list[int] = []
    saw_passthrough = False

    def _finalize_block(idx: int) -> None:
        blk = blocks.get(idx)
        if blk is None:
            return
        if blk.get("type") == "tool_use":
            raw = "".join(json_buf.get(idx, []))
            try:
                blk["input"] = json.loads(raw) if raw.strip() else {}
            except json.JSONDecodeError:
                blk["input"] = {}

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
            elif ev.type is StreamEventType.PASSTHROUGH:
                saw_passthrough = True
                frame = ev.extra.get("frame")
                if not isinstance(frame, dict):
                    continue
                ftype = frame.get("type")
                idx = int(frame.get("index", 0))
                if ftype == "content_block_start":
                    cb = dict(frame.get("content_block", {}) or {})
                    blocks[idx] = cb
                    json_buf[idx] = []
                    block_order.append(idx)
                elif ftype == "content_block_delta":
                    delta = frame.get("delta", {}) or {}
                    dtype = delta.get("type")
                    if dtype == "text_delta":
                        blocks.setdefault(idx, {"type": "text", "text": ""})
                        if idx not in block_order:
                            block_order.append(idx)
                        blocks[idx]["text"] = blocks[idx].get("text", "") + str(
                            delta.get("text", "")
                        )
                    elif dtype == "input_json_delta":
                        json_buf.setdefault(idx, []).append(str(delta.get("partial_json", "")))
                elif ftype == "content_block_stop":
                    _finalize_block(idx)
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

    if saw_passthrough:
        # Finalize any tool_use block whose stop frame was dropped, then emit in
        # upstream order.
        for idx in block_order:
            if blocks.get(idx, {}).get("type") == "tool_use" and "input" not in blocks[idx]:
                _finalize_block(idx)
        content = [blocks[i] for i in block_order if i in blocks]
        if not content:
            content = [{"type": "text", "text": ""}]
    else:
        content = [{"type": "text", "text": "".join(text_parts)}]

    return {
        "id": message_id,
        "type": "message",
        "role": "assistant",
        "model": model,
        "content": content,
        "stop_reason": finish_reason,
        "stop_sequence": None,
        "usage": {
            "input_tokens": usage.prompt_tokens,
            "output_tokens": usage.completion_tokens,
        },
    }
