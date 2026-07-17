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
from cocola_common import ErrorCode, get_logger

from cocola_llm_gateway.types import ChatRequest, StreamEvent, StreamEventType, Usage
from cocola_llm_gateway.upstream.errors import UpstreamError

log = get_logger("cocola.llm-gateway.upstream.anthropic")


@dataclass
class AnthropicConfig:
    base_url: str = "https://api.anthropic.com"
    api_key: str = ""
    anthropic_version: str = "2023-06-01"
    timeout_s: float = 600.0
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
    msgs = _normalize_tool_result_order(msgs)

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


def _normalize_tool_result_order(msgs: list[dict]) -> list[dict]:
    """Make Anthropic-compatible relays happy with Claude Code tool turns.

    Some relays enforce that the user message immediately after an assistant
    `tool_use` starts with the matching `tool_result` block(s). Claude Code may
    include text reminders in that same user message; official clients tolerate
    this poorly across providers. We repair the transcript by moving matching
    tool_result blocks to the required location. If a result is missing entirely,
    insert a short error result so the model can continue instead of the relay
    rejecting the whole request with a 400.

    DeepSeek's Anthropic-compatible endpoint is stricter than Anthropic's docs
    imply: if an assistant message contains a `tool_use`, no later block in the
    same assistant message may be text/thinking. Keep non-tool blocks, but move
    all tool_use blocks to the end so the next user message can immediately
    start with the matching tool_result blocks.
    """
    out = [dict(m) for m in msgs]
    idx = 0
    while idx < len(out):
        msg = out[idx]
        if msg.get("role") != "assistant":
            idx += 1
            continue
        msg["content"] = _tool_use_blocks_last(msg.get("content"))
        pending = _tool_use_ids(msg.get("content"))
        if not pending:
            idx += 1
            continue
        results = _complete_tool_results(_take_tool_results_after(out, idx + 1, pending), pending)

        if idx + 1 < len(out) and out[idx + 1].get("role") == "user":
            remaining = _content_as_blocks(out[idx + 1].get("content"))
            out[idx + 1]["content"] = results
            if remaining:
                out.insert(idx + 2, {"role": "user", "content": remaining})
        else:
            out.insert(idx + 1, {"role": "user", "content": results})
        idx += 2
    return out


def _tool_use_blocks_last(content: object) -> object:
    if not isinstance(content, list):
        return content
    tool_blocks: list[object] = []
    other_blocks: list[object] = []
    for block in content:
        if isinstance(block, dict) and block.get("type") == "tool_use":
            tool_blocks.append(block)
        else:
            other_blocks.append(block)
    if not tool_blocks or not other_blocks:
        return content
    return other_blocks + tool_blocks


def _tool_use_ids(content: object) -> list[str]:
    if not isinstance(content, list):
        return []
    ids: list[str] = []
    for block in content:
        if isinstance(block, dict) and block.get("type") == "tool_use":
            bid = str(block.get("id", "")).strip()
            if bid:
                ids.append(bid)
    return ids


def _take_tool_results_after(out: list[dict], start: int, pending_ids: list[str]) -> list[dict]:
    pending = set(pending_ids)
    found: dict[str, dict] = {}
    j = start
    while j < len(out):
        msg = out[j]
        content = msg.get("content")
        if msg.get("role") != "user" or not isinstance(content, list):
            j += 1
            continue

        changed = False
        rest: list[object] = []
        for block in content:
            tool_use_id = _tool_result_id(block)
            if tool_use_id in pending and tool_use_id not in found:
                found[tool_use_id] = block
                changed = True
            else:
                rest.append(block)

        if not changed:
            j += 1
            continue
        if rest or j == start:
            msg["content"] = rest
            j += 1
        else:
            del out[j]

    return [found[tool_use_id] for tool_use_id in pending_ids if tool_use_id in found]


def _complete_tool_results(results: list[dict], pending_ids: list[str]) -> list[dict]:
    found = {_tool_result_id(block): block for block in results}
    return [
        found.get(tool_use_id) or _missing_tool_result(tool_use_id) for tool_use_id in pending_ids
    ]


def _tool_result_id(block: object) -> str:
    if not isinstance(block, dict) or block.get("type") != "tool_result":
        return ""
    return str(block.get("tool_use_id", "")).strip()


def _content_as_blocks(content: object) -> list[object]:
    if isinstance(content, list):
        return content
    if isinstance(content, str) and content:
        return [{"type": "text", "text": content}]
    return []


def _missing_tool_result(tool_use_id: str) -> dict:
    return {
        "type": "tool_result",
        "tool_use_id": tool_use_id,
        "content": "Tool result was unavailable in the local transcript.",
        "is_error": True,
    }


def _tool_turn_violations(messages: object) -> list[dict]:
    if not isinstance(messages, list):
        return [{"message_index": -1, "missing_tool_result_ids": ["messages_not_list"]}]
    violations: list[dict] = []
    for idx, msg in enumerate(messages):
        if not isinstance(msg, dict) or msg.get("role") != "assistant":
            continue
        tool_use_ids = _tool_use_ids(msg.get("content"))
        if not tool_use_ids:
            continue
        content = msg.get("content")
        if isinstance(content, list):
            last_non_tool_use = max(
                (
                    block_idx
                    for block_idx, block in enumerate(content)
                    if not (isinstance(block, dict) and block.get("type") == "tool_use")
                ),
                default=-1,
            )
            first_tool_use = next(
                (
                    block_idx
                    for block_idx, block in enumerate(content)
                    if isinstance(block, dict) and block.get("type") == "tool_use"
                ),
                None,
            )
            if first_tool_use is not None and last_non_tool_use > first_tool_use:
                violations.append({"message_index": idx, "tool_use_not_terminal_ids": tool_use_ids})
                continue
        if idx + 1 >= len(messages):
            violations.append({"message_index": idx, "missing_tool_result_ids": tool_use_ids})
            continue
        nxt = messages[idx + 1]
        content = nxt.get("content") if isinstance(nxt, dict) else None
        if not isinstance(nxt, dict) or nxt.get("role") != "user" or not isinstance(content, list):
            violations.append({"message_index": idx, "missing_tool_result_ids": tool_use_ids})
            continue
        got = [_tool_result_id(block) for block in content[: len(tool_use_ids)]]
        missing = [
            tool_use_id
            for tool_use_id, result_id in zip(tool_use_ids, got, strict=False)
            if result_id != tool_use_id
        ]
        if len(got) < len(tool_use_ids):
            missing.extend(tool_use_ids[len(got) :])
        if missing:
            violations.append({"message_index": idx, "missing_tool_result_ids": missing})
    return violations


def _tool_payload_summary(messages: object) -> list[dict]:
    if not isinstance(messages, list):
        return []
    summary: list[dict] = []
    for idx, msg in enumerate(messages):
        if not isinstance(msg, dict):
            summary.append({"index": idx, "role": "unknown", "content": "not_object"})
            continue
        content = msg.get("content")
        blocks = content if isinstance(content, list) else []
        summary.append(
            {
                "index": idx,
                "role": msg.get("role", ""),
                "content_shape": "blocks" if isinstance(content, list) else type(content).__name__,
                "block_types": [
                    block.get("type", "") for block in blocks if isinstance(block, dict)
                ],
                "tool_use_ids": _tool_use_ids(content),
                "tool_result_ids": [
                    result_id
                    for result_id in (_tool_result_id(block) for block in blocks)
                    if result_id
                ],
            }
        )
    return summary


def _log_tool_payload_if_invalid(payload: dict) -> None:
    violations = _tool_turn_violations(payload.get("messages"))
    if not violations:
        return
    log.warning(
        "anthropic payload has tool transcript violations before upstream request",
        violations=violations,
        tool_summary=_tool_payload_summary(payload.get("messages")),
    )


def _log_tool_payload_rejected(status_code: int, body: str, payload: dict) -> None:
    if "tool_use" not in body and "tool_result" not in body:
        return
    log.warning(
        "anthropic upstream rejected tool transcript",
        status_code=status_code,
        violations=_tool_turn_violations(payload.get("messages")),
        tool_summary=_tool_payload_summary(payload.get("messages")),
    )


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
        _log_tool_payload_if_invalid(payload)
        try:
            async with self._client.stream("POST", "/v1/messages", json=payload) as resp:
                if resp.status_code >= 400:
                    body = (await resp.aread()).decode(errors="replace")
                    _log_tool_payload_rejected(resp.status_code, body, payload)
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
        _log_tool_payload_if_invalid(payload)
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
            _log_tool_payload_rejected(resp.status_code, body, payload)
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
            elif btype == "thinking":
                start_block = {"type": "thinking", "thinking": ""}
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
            elif btype == "thinking":
                yield StreamEvent(
                    StreamEventType.PASSTHROUGH,
                    extra={
                        "frame": {
                            "type": "content_block_delta",
                            "index": idx,
                            "delta": {
                                "type": "thinking_delta",
                                "thinking": str(block.get("thinking", "")),
                            },
                        }
                    },
                )
                signature = str(block.get("signature", ""))
                if signature:
                    yield StreamEvent(
                        StreamEventType.PASSTHROUGH,
                        extra={
                            "frame": {
                                "type": "content_block_delta",
                                "index": idx,
                                "delta": {
                                    "type": "signature_delta",
                                    "signature": signature,
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
