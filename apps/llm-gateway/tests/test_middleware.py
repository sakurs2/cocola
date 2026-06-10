from collections.abc import AsyncIterator

from cocola_llm_gateway.middleware import ResiliencePolicy, ResilientStreamer
from cocola_llm_gateway.types import (
    ChatMessage,
    ChatParams,
    ChatRequest,
    StreamEvent,
    StreamEventType,
)


def _req(user="U1"):
    return ChatRequest(
        model="m",
        messages=[ChatMessage(role="user", content="hi")],
        params=ChatParams(),
        user_id=user,
    )


class _Provider:
    """Programmable fake upstream for resilience tests."""

    name = "prog"

    def __init__(self, scripts):
        # scripts: list of "attempt plans"; each is a list of StreamEvent.
        self._scripts = scripts
        self.calls = 0

    async def chat_stream(self, req) -> AsyncIterator[StreamEvent]:
        plan = self._scripts[min(self.calls, len(self._scripts) - 1)]
        self.calls += 1
        for ev in plan:
            yield ev

    async def aclose(self):
        return None


async def _drain(streamer, req):
    return [ev async for ev in streamer.chat_stream(req)]


async def test_passthrough_success():
    p = _Provider(
        [
            [
                StreamEvent(StreamEventType.MESSAGE_START),
                StreamEvent(StreamEventType.CONTENT_DELTA, text="ok"),
                StreamEvent(StreamEventType.MESSAGE_STOP),
            ]
        ]
    )
    s = ResilientStreamer(p, ResiliencePolicy(max_retries=2))
    out = await _drain(s, _req())
    assert p.calls == 1
    assert out[-1].type is StreamEventType.MESSAGE_STOP


async def test_retry_before_first_byte_then_recovers():
    p = _Provider(
        [
            [StreamEvent(StreamEventType.ERROR, error="boom", code="x")],
            [
                StreamEvent(StreamEventType.MESSAGE_START),
                StreamEvent(StreamEventType.CONTENT_DELTA, text="ok"),
                StreamEvent(StreamEventType.MESSAGE_STOP),
            ],
        ]
    )
    s = ResilientStreamer(p, ResiliencePolicy(max_retries=2, backoff_base_s=0))
    out = await _drain(s, _req())
    assert p.calls == 2
    assert out[-1].type is StreamEventType.MESSAGE_STOP
    assert all(e.type is not StreamEventType.ERROR for e in out)


async def test_retry_exhaustion_surfaces_error():
    p = _Provider([[StreamEvent(StreamEventType.ERROR, error="boom", code="x")]])
    s = ResilientStreamer(p, ResiliencePolicy(max_retries=2, backoff_base_s=0))
    out = await _drain(s, _req())
    assert p.calls == 3  # 1 + 2 retries
    assert out[-1].type is StreamEventType.ERROR


async def test_no_retry_after_content_emitted():
    # MESSAGE_START counts as produced content -> a later ERROR is NOT retried.
    p = _Provider(
        [
            [
                StreamEvent(StreamEventType.MESSAGE_START),
                StreamEvent(StreamEventType.CONTENT_DELTA, text="partial"),
                StreamEvent(StreamEventType.ERROR, error="mid", code="x"),
            ]
        ]
    )
    s = ResilientStreamer(p, ResiliencePolicy(max_retries=5, backoff_base_s=0))
    out = await _drain(s, _req())
    assert p.calls == 1  # never retried mid-stream
    assert out[-1].type is StreamEventType.ERROR


async def test_rate_limit_blocks_second_call():
    p = _Provider(
        [
            [
                StreamEvent(StreamEventType.MESSAGE_START),
                StreamEvent(StreamEventType.MESSAGE_STOP),
            ]
        ]
    )
    # rate 1 rps, burst 1: first allowed, immediate second denied.
    s = ResilientStreamer(p, ResiliencePolicy(rate_limit_rps=1, rate_burst=1))
    out1 = await _drain(s, _req("U1"))
    out2 = await _drain(s, _req("U1"))
    assert out1[-1].type is StreamEventType.MESSAGE_STOP
    assert out2[0].type is StreamEventType.ERROR
    assert out2[0].code == "rate_limited"
