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
from collections.abc import AsyncIterator

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

    def __init__(self, reply: str = "", *, chunk_size: int = 4, delay_s: float = 0.0):
        self._reply = reply
        self._chunk_size = max(1, chunk_size)
        self._delay_s = delay_s

    def _resolve_reply(self, req: ChatRequest) -> str:
        if self._reply:
            return self._reply
        last_user = ""
        for m in req.messages:
            if m.role == "user":
                last_user = m.content
        return f"echo: {last_user}" if last_user else "echo: (empty)"

    async def chat_stream(self, req: ChatRequest) -> AsyncIterator[StreamEvent]:
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

    async def aclose(self) -> None:  # pragma: no cover - nothing to release
        return None
