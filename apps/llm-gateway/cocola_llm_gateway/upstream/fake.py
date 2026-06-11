"""Deterministic in-process upstream for hermetic tests and local demos.

FakeUpstream never touches the network. It echoes a canned (or derived) reply
as a token stream and reports plausible Usage, so the entire gateway — routing,
SSE codec, billing ledger, the Claude Agent SDK wiring — can be exercised end to
end without a real model endpoint or API key. This is the only provider our unit
tests are allowed to use (see ADR-0004 hard constraint).

Token accounting is intentionally simple and stable: prompt_tokens ~ whitespace
word count of the input, completion_tokens == number of streamed chunks. Tests
assert on these exact numbers, so the heuristic must stay deterministic.
"""

from __future__ import annotations

import asyncio
import json
from collections.abc import AsyncIterator
from typing import Any

from cocola_llm_gateway.types import ChatRequest, StreamEvent, StreamEventType, Usage


def _count_words(req: ChatRequest) -> int:
    return sum(len(m.content.split()) for m in req.messages)


class FakeUpstream:
    """A canned streaming provider.

    Args:
        reply: fixed reply text; if empty, a reply is derived from the last user
            message ("echo: <text>").
        chunk_size: characters per streamed CONTENT_DELTA (controls chunk count,
            which equals completion_tokens).
        delay_s: optional per-chunk sleep to simulate latency (default 0 for
            fast tests).
    """

    name = "fake"

    def __init__(
        self,
        reply: str = "",
        *,
        chunk_size: int = 4,
        delay_s: float = 0.0,
        tool_call: dict[str, Any] | None = None,
    ):
        """
        Args (tool_call):
            tool_call: when set, the fake emits a *tool_use* turn instead of a
                text turn — exercising the ADR-0010 passthrough path end to end
                without a real model. Shape:
                    {"id": "tu_1", "name": "get_weather",
                     "input": {"city": "NYC"}}
                The fake streams it exactly as Anthropic would: a
                content_block_start(tool_use) + chunked input_json_delta +
                content_block_stop, all via StreamEventType.PASSTHROUGH, then a
                message_delta with stop_reason="tool_use".
        """
        self._reply = reply
        self._chunk_size = max(1, chunk_size)
        self._delay_s = delay_s
        self._tool_call = tool_call

    def _resolve_reply(self, req: ChatRequest) -> str:
        if self._reply:
            return self._reply
        last_user = ""
        for m in req.messages:
            if m.role == "user":
                last_user = m.content
        return f"echo: {last_user}" if last_user else "echo: (empty)"

    async def chat_stream(self, req: ChatRequest) -> AsyncIterator[StreamEvent]:
        if self._tool_call is not None:
            async for ev in self._tool_use_stream(req):
                yield ev
            return

        reply = self._resolve_reply(req)
        prompt_tokens = _count_words(req)

        yield StreamEvent(
            StreamEventType.MESSAGE_START,
            usage=Usage(prompt_tokens=prompt_tokens),
            model=req.model,
        )

        chunks = [reply[i : i + self._chunk_size] for i in range(0, len(reply), self._chunk_size)]
        if not chunks:
            chunks = [""]
        for c in chunks:
            if self._delay_s:
                await asyncio.sleep(self._delay_s)
            yield StreamEvent(StreamEventType.CONTENT_DELTA, text=c)

        yield StreamEvent(
            StreamEventType.MESSAGE_DELTA,
            usage=Usage(completion_tokens=len(chunks)),
            finish_reason="end_turn",
        )
        yield StreamEvent(StreamEventType.MESSAGE_STOP)

    async def _tool_use_stream(self, req: ChatRequest) -> AsyncIterator[StreamEvent]:
        """Emit a scripted tool_use turn as Anthropic-shaped PASSTHROUGH frames.

        This is the hermetic mirror of a real model deciding to call a tool: it
        proves the gateway forwards `tools` (the caller must have sent them) and
        relays the resulting tool_use block + incremental JSON args intact.
        """
        tc = self._tool_call or {}
        block_id = str(tc.get("id", "tu_fake"))
        name = str(tc.get("name", "noop"))
        tool_input = tc.get("input", {})
        prompt_tokens = _count_words(req)

        yield StreamEvent(
            StreamEventType.MESSAGE_START,
            usage=Usage(prompt_tokens=prompt_tokens),
            model=req.model,
        )
        # content_block_start: announce the tool_use block (empty input).
        yield StreamEvent(
            StreamEventType.PASSTHROUGH,
            extra={
                "frame": {
                    "type": "content_block_start",
                    "index": 0,
                    "content_block": {
                        "type": "tool_use",
                        "id": block_id,
                        "name": name,
                        "input": {},
                    },
                }
            },
        )
        # input_json_delta: stream the args as the real API does — chunked so the
        # downstream reconstruction (concat + parse) is genuinely exercised.
        raw = json.dumps(tool_input, separators=(",", ":"))
        pieces = [raw[i : i + self._chunk_size] for i in range(0, len(raw), self._chunk_size)]
        if not pieces:
            pieces = [""]
        for piece in pieces:
            if self._delay_s:
                await asyncio.sleep(self._delay_s)
            yield StreamEvent(
                StreamEventType.PASSTHROUGH,
                extra={
                    "frame": {
                        "type": "content_block_delta",
                        "index": 0,
                        "delta": {"type": "input_json_delta", "partial_json": piece},
                    }
                },
            )
        yield StreamEvent(
            StreamEventType.PASSTHROUGH,
            extra={"frame": {"type": "content_block_stop", "index": 0}},
        )
        # completion_tokens == number of arg chunks, mirroring the text path's
        # "chunks == tokens" billing heuristic so accounting stays deterministic.
        yield StreamEvent(
            StreamEventType.MESSAGE_DELTA,
            usage=Usage(completion_tokens=len(pieces)),
            finish_reason="tool_use",
        )
        yield StreamEvent(StreamEventType.MESSAGE_STOP)

    async def aclose(self) -> None:  # pragma: no cover - nothing to release
        return None
