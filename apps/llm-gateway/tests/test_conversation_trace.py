from cocola_llm_gateway.conversation_trace import TraceContext
from cocola_llm_gateway.types import ChatMessage, ChatParams, ChatRequest
from tests.conftest import build_service


class FakeTraceStore:
    def __init__(self):
        self.calls = []

    async def record_model_call(self, context, **fields):
        self.calls.append((context, fields))

    async def aclose(self):
        return None


def test_traceparent_parser_rejects_invalid_and_zero_ids():
    assert TraceContext.parse(None) is None
    assert TraceContext.parse("bad") is None
    assert TraceContext.parse("00-" + "0" * 32 + "-" + "1" * 16 + "-01") is None
    parsed = TraceContext.parse("00-" + "a" * 32 + "-" + "b" * 16 + "-01")
    assert parsed is not None
    assert parsed.trace_id == "a" * 32
    assert parsed.parent_span_id == "b" * 16


async def test_model_call_records_safe_timing_and_usage():
    service, _ = build_service(reply="trace me")
    trace_store = FakeTraceStore()
    service._trace_store = trace_store
    request = ChatRequest(
        model="fake-1",
        messages=[ChatMessage(role="user", content="private prompt")],
        params=ChatParams(),
        user_id="U1",
        session_id="S1",
        metadata={"requested_model": "default"},
    )
    context = TraceContext("a" * 32, "b" * 16)

    _ = [event async for event in service.chat_stream(request, trace_context=context)]

    assert len(trace_store.calls) == 1
    recorded_context, fields = trace_store.calls[0]
    assert recorded_context == context
    assert fields["duration_us"] >= 0
    assert fields["input_tokens"] > 0
    assert "private prompt" not in repr(fields)
