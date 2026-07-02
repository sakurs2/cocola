"""Anthropic passthrough upstream.

Talks the real Anthropic Messages API over HTTP. Because our front-end and our
normalized types already follow the Anthropic taxonomy, this adapter is mostly a
faithful re-encode (ChatRequest -> Anthropic JSON) + SSE parse (Anthropic SSE ->
StreamEvent).

HARD CONSTRAINT (ADR-0004): base_url and api_key are injected via config ONLY.
Nothing about any endpoint is hardcoded here; the defaults below are the public
Anthropic values and exist purely so a developer with their own key can run it,
never to point at an internal inference endpoint.
"""

from __future__ import annotations

import json
from collections.abc import AsyncIterator
from dataclasses import dataclass

import httpx
from cocola_common import ErrorCode

from cocola_llm_gateway.types import ChatRequest, StreamEvent, StreamEventType, Usage
from cocola_llm_gateway.upstream.errors import UpstreamError


@dataclass
class AnthropicConfig:
    base_url: str = "https://api.anthropic.com"
    api_key: str = ""
    anthropic_version: str = "2023-06-01"
    timeout_s: float = 60.0
    connect_timeout_s: float = 10.0
    # When False, talk to the upstream in NON-streaming mode (POST once, read the
    # whole JSON body) and re-synthesize the downstream StreamEvent sequence
    # locally, so the gateway still presents a stream to its client. This exists
    # because some Anthropic-compatible relays have a working non-stream endpoint
    # but a broken SSE endpoint (it accepts the request, returns HTTP 200, then
    # never emits a byte -> the gateway hangs until httpx times out). Config knob;
    # default True preserves the historical streaming behavior. See
    # docs/plan/anthropic-nonstream-fallback.md.
    stream: bool = True


def _build_payload(req: ChatRequest, *, stream: bool) -> dict:
    """Re-encode a normalized ChatRequest into an Anthropic request body.

    The leading system turn (if any) is lifted back into the top-level `system`
    field, matching the Anthropic schema.
    """
    system_text = ""
    msgs = []
    for m in req.messages:
        if m.role == "system":
            # Concatenate multiple system turns defensively.
            system_text = f"{system_text}\n{m.content}".strip() if system_text else m.content
        else:
            # ADR-0010: when the normalized message preserved a raw Anthropic
            # content-block array (tool_use / tool_result / image), send it
            # verbatim; otherwise fall back to the plain text content.
            content: object = m.content_blocks if m.content_blocks is not None else m.content
            msgs.append({"role": m.role, "content": content})

    payload: dict = {
        "model": req.model,
        "messages": msgs,
        "max_tokens": req.params.max_tokens,
        "stream": stream,
    }
    if system_text:
        payload["system"] = system_text
    if req.params.temperature is not None:
        payload["temperature"] = req.params.temperature
    if req.params.top_p is not None:
        payload["top_p"] = req.params.top_p
    if req.params.stop:
        payload["stop_sequences"] = req.params.stop
    # ADR-0010: forward tool definitions / choice opaquely so the upstream can
    # actually emit tool_use. Without this the model never sees the tools.
    if req.params.tools:
        payload["tools"] = req.params.tools
    if req.params.tool_choice is not None:
        payload["tool_choice"] = req.params.tool_choice
    return payload


def _iter_sse_events(lines: list[str]):
    """Yield (event_type, data_dict) from accumulated SSE lines for one frame."""
    event_type = ""
    data_buf: list[str] = []
    for line in lines:
        if line.startswith("event:"):
            event_type = line[len("event:") :].strip()
        elif line.startswith("data:"):
            data_buf.append(line[len("data:") :].strip())
    if not data_buf:
        return None
    try:
        data = json.loads("".join(data_buf))
    except json.JSONDecodeError:
        return None
    return event_type or data.get("type", ""), data


class AnthropicUpstream:
    """Streaming Anthropic Messages client implementing UpstreamProvider."""

    name = "anthropic"

    def __init__(self, cfg: AnthropicConfig):
        if not cfg.api_key:
            # Fail fast at construction so misconfiguration is obvious, not a
            # mid-stream 401.
            raise UpstreamError(
                ErrorCode.INVALID_ARGUMENT,
                "AnthropicUpstream requires an api_key (set via config/env)",
            )
        self._cfg = cfg
        self._stream = cfg.stream
        self._client = httpx.AsyncClient(
            base_url=cfg.base_url,
            timeout=httpx.Timeout(cfg.timeout_s, connect=cfg.connect_timeout_s),
            headers={
                "x-api-key": cfg.api_key,
                "anthropic-version": cfg.anthropic_version,
                "content-type": "application/json",
            },
        )

    async def chat_stream(self, req: ChatRequest) -> AsyncIterator[StreamEvent]:
        if not self._stream:
            async for ev in self._chat_nonstream(req):
                yield ev
            return
        payload = _build_payload(req, stream=True)
        try:
            async with self._client.stream("POST", "/v1/messages", json=payload) as resp:
                if resp.status_code >= 400:
                    body = (await resp.aread()).decode(errors="replace")
                    yield StreamEvent(
                        StreamEventType.ERROR,
                        error=f"upstream {resp.status_code}: {body[:500]}",
                        code="upstream_http_error",
                    )
                    return
                async for ev in self._parse_stream(resp):
                    yield ev
        except httpx.TimeoutException as e:
            yield StreamEvent(StreamEventType.ERROR, error=f"upstream timeout: {e}", code="timeout")
        except httpx.HTTPError as e:
            yield StreamEvent(
                StreamEventType.ERROR, error=f"upstream transport error: {e}", code="transport"
            )

    async def _chat_nonstream(self, req: ChatRequest) -> AsyncIterator[StreamEvent]:
        """Non-streaming path: POST once, read the whole JSON, then re-synthesize
        the same StreamEvent sequence the streaming path would have produced.

        The upstream returns a complete Anthropic Messages object with a
        `content: [block, ...]` array. We replay each block as the exact
        PASSTHROUGH content_block_start/(delta)/stop frames the downstream codec
        already understands (ADR-0010), so both text and tool_use blocks flow
        through the identical reconstruction logic -- no codec change needed.
        """
        payload = _build_payload(req, stream=False)
        try:
            resp = await self._client.post("/v1/messages", json=payload)
        except httpx.TimeoutException as e:
            yield StreamEvent(StreamEventType.ERROR, error=f"upstream timeout: {e}", code="timeout")
            return
        except httpx.HTTPError as e:
            yield StreamEvent(
                StreamEventType.ERROR, error=f"upstream transport error: {e}", code="transport"
            )
            return

        if resp.status_code >= 400:
            body = resp.text
            yield StreamEvent(
                StreamEventType.ERROR,
                error=f"upstream {resp.status_code}: {body[:500]}",
                code="upstream_http_error",
            )
            return
        try:
            data = resp.json()
        except json.JSONDecodeError as e:
            yield StreamEvent(
                StreamEventType.ERROR, error=f"upstream bad json: {e}", code="transport"
            )
            return

        usage = data.get("usage", {}) or {}
        yield StreamEvent(
            StreamEventType.MESSAGE_START,
            usage=Usage(prompt_tokens=int(usage.get("input_tokens", 0))),
            model=str(data.get("model", "")),
        )
        for idx, block in enumerate(data.get("content", []) or []):
            if not isinstance(block, dict):
                continue
            btype = block.get("type", "")
            # content_block_start announces the block (text starts empty; tool_use
            # carries id/name with empty input, matching the SSE shape).
            if btype == "tool_use":
                start_block = {
                    "type": "tool_use",
                    "id": block.get("id", ""),
                    "name": block.get("name", ""),
                    "input": {},
                }
            else:
                start_block = {"type": btype or "text", "text": ""}
            yield StreamEvent(
                StreamEventType.PASSTHROUGH,
                extra={
                    "frame": {
                        "type": "content_block_start",
                        "index": idx,
                        "content_block": start_block,
                    }
                },
            )
            if btype == "tool_use":
                yield StreamEvent(
                    StreamEventType.PASSTHROUGH,
                    extra={
                        "frame": {
                            "type": "content_block_delta",
                            "index": idx,
                            "delta": {
                                "type": "input_json_delta",
                                "partial_json": json.dumps(
                                    block.get("input", {}) or {}, separators=(",", ":")
                                ),
                            },
                        }
                    },
                )
            elif btype in ("", "text"):
                yield StreamEvent(
                    StreamEventType.PASSTHROUGH,
                    extra={
                        "frame": {
                            "type": "content_block_delta",
                            "index": idx,
                            "delta": {
                                "type": "text_delta",
                                "text": str(block.get("text", "")),
                            },
                        }
                    },
                )
            yield StreamEvent(
                StreamEventType.PASSTHROUGH,
                extra={"frame": {"type": "content_block_stop", "index": idx}},
            )
        yield StreamEvent(
            StreamEventType.MESSAGE_DELTA,
            usage=Usage(completion_tokens=int(usage.get("output_tokens", 0))),
            finish_reason=str(data.get("stop_reason", "") or ""),
        )
        yield StreamEvent(StreamEventType.MESSAGE_STOP)

    async def _parse_stream(self, resp: httpx.Response) -> AsyncIterator[StreamEvent]:
        """Parse Anthropic SSE frames into normalized StreamEvents."""
        frame: list[str] = []
        started = False
        async for raw in resp.aiter_lines():
            if raw == "":  # frame boundary
                parsed = _iter_sse_events(frame)
                frame = []
                if parsed is None:
                    continue
                etype, data = parsed
                out = self._map_event(etype, data)
                if out is not None:
                    if out.type is StreamEventType.MESSAGE_START:
                        started = True
                    yield out
            else:
                frame.append(raw)
        # Ensure a terminal event even if the upstream omitted message_stop.
        if started:
            yield StreamEvent(StreamEventType.MESSAGE_STOP)

    @staticmethod
    def _map_event(etype: str, data: dict) -> StreamEvent | None:
        if etype == "message_start":
            msg = data.get("message", {})
            usage = msg.get("usage", {})
            return StreamEvent(
                StreamEventType.MESSAGE_START,
                usage=Usage(prompt_tokens=int(usage.get("input_tokens", 0))),
                model=str(msg.get("model", "")),
            )
        if etype == "content_block_start":
            # ADR-0010: relay verbatim so tool_use blocks (and their id/name)
            # survive to the client.
            return StreamEvent(StreamEventType.PASSTHROUGH, extra={"frame": data})
        if etype == "content_block_delta":
            # ADR-0010: relay every content_block_delta verbatim (text_delta AND
            # input_json_delta for tool_use args). The downstream codec frames
            # it back into the Anthropic SSE stream unchanged.
            return StreamEvent(StreamEventType.PASSTHROUGH, extra={"frame": data})
        if etype == "content_block_stop":
            return StreamEvent(StreamEventType.PASSTHROUGH, extra={"frame": data})
        if etype == "message_delta":
            usage = data.get("usage", {})
            delta = data.get("delta", {})
            return StreamEvent(
                StreamEventType.MESSAGE_DELTA,
                usage=Usage(completion_tokens=int(usage.get("output_tokens", 0))),
                finish_reason=str(delta.get("stop_reason", "") or ""),
            )
        if etype == "message_stop":
            return StreamEvent(StreamEventType.MESSAGE_STOP)
        if etype == "error":
            err = data.get("error", {})
            return StreamEvent(
                StreamEventType.ERROR,
                error=str(err.get("message", "upstream error")),
                code=str(err.get("type", "api_error")),
            )
        # ping and other housekeeping frames are not needed downstream.
        return None

    async def aclose(self) -> None:
        await self._client.aclose()
