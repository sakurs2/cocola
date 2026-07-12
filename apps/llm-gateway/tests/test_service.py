import pytest
from cocola_common import CocolaError, ErrorCode
from cocola_llm_gateway.service import GatewayService
from cocola_llm_gateway.types import ChatMessage, ChatParams, ChatRequest, StreamEventType
from tests.conftest import build_service


def _req(text="one two three"):
    return ChatRequest(
        model="fake-1",
        messages=[ChatMessage(role="user", content=text)],
        params=ChatParams(),
        user_id="U1",
        session_id="S1",
        metadata={"requested_model": "default"},
    )


async def test_chat_stream_passes_events_and_meters():
    svc, ledger = build_service(reply="abcdef")
    events = [ev async for ev in svc.chat_stream(_req(), requested_alias="default")]
    assert events[0].type is StreamEventType.MESSAGE_START
    assert events[-1].type is StreamEventType.MESSAGE_STOP
    recent = await ledger.recent(user_id="U1")
    assert len(recent) == 1
    rec = recent[0]
    assert rec.prompt_tokens == 3  # word count
    assert rec.completion_tokens > 0
    assert rec.cost_usd > 0
    assert rec.status == "ok"


async def test_resolve_model_returns_real_id():
    svc, _ = build_service()
    assert await svc.resolve_model("default") == "fake-1"


async def test_resolve_unknown_alias_raises():
    svc, _ = build_service()
    with pytest.raises(CocolaError) as ei:
        await svc.resolve_model("ghost")
    assert ei.value.code is ErrorCode.NOT_FOUND


async def test_metering_fires_even_when_consumer_stops_early():
    """If a consumer breaks out of the stream, the metering finally must still
    run when the generator is closed. We emulate that with aclose()."""
    svc, ledger = build_service(reply="abcdef")
    agen = svc.chat_stream(_req(), requested_alias="default").__aiter__()
    await agen.__anext__()  # consume just the first event
    await agen.aclose()  # consumer abandons the stream
    recent = await ledger.recent(user_id="U1")
    assert len(recent) == 1
    # Aborted before MESSAGE_DELTA -> completion_tokens may be 0 but a record exists.
    assert recent[0].prompt_tokens == 3


async def test_registry_lease_is_released_after_stream():
    base, ledger = build_service(reply="abcdef")

    class LeaseSource:
        def __init__(self):
            self.acquired = 0
            self.released = 0

        async def acquire_registry(self):
            self.acquired += 1
            return base.registry

        async def release_registry(self, registry):
            assert registry is base.registry
            self.released += 1

        async def aclose(self):
            return None

    source = LeaseSource()
    svc = GatewayService(base.registry, ledger, registry_source=source)
    _ = [event async for event in svc.chat_stream(_req(), requested_alias="default")]
    assert source.acquired == 1
    assert source.released == 1
