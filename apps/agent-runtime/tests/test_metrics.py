"""PrometheusServerInterceptor (gRPC RED metrics) tests.

Hermetic by design: per <network_security> we never bind a port, so instead of
standing up grpc.aio.server() we drive intercept_service() directly. We hand it
a fake `continuation` that returns a real unary_stream handler (the runtime's
Query is server-streaming) and a fake `handler_call_details` carrying the full
method name, then invoke the *wrapped* behavior with a fake context. We assert
the RED vectors record transport="grpc", method=<full method>, and the right
StatusCode name on both the happy path (OK) and an unexpected error (UNKNOWN).
"""

import grpc
import pytest
from cocola_common.metrics import Registry
from cocola_common.metrics_grpc import PrometheusServerInterceptor


class FakeContext:
    """grpc.aio.ServicerContext stand-in: only code() is read by the interceptor."""

    def __init__(self):
        self._code = None

    def code(self):
        return self._code


def _details(method: str):
    class D:
        pass

    d = D()
    d.method = method
    d.invocation_metadata = ()
    return d


async def _intercept_unary_stream(interceptor, method, behavior):
    """Run intercept_service for a unary_stream handler and return the wrapped one."""

    async def continuation(handler_call_details):
        return grpc.unary_stream_rpc_method_handler(behavior)

    return await interceptor.intercept_service(continuation, _details(method))


async def _drain(async_gen):
    return [item async for item in async_gen]


def _sample(reg, name, labels):
    for metric in reg.registry.collect():
        for s in metric.samples:
            if s.name == name and all(s.labels.get(k) == v for k, v in labels.items()):
                return s.value
    return None


METHOD = "/cocola.agent.v1.AgentRuntimeService/Query"


async def test_unary_stream_records_ok():
    reg = Registry("agent-runtime-test")
    interceptor = PrometheusServerInterceptor(reg)

    async def behavior(request, context):
        yield "a"
        yield "b"

    wrapped = await _intercept_unary_stream(interceptor, METHOD, behavior)
    out = await _drain(wrapped.unary_stream("req", FakeContext()))

    assert out == ["a", "b"]  # streaming preserved through the wrapper
    count = _sample(
        reg,
        "cocola_requests_total",
        {"service": "agent-runtime-test", "transport": "grpc", "method": METHOD, "code": "OK"},
    )
    assert count == 1.0
    # duration histogram observed exactly once for this method
    n = _sample(
        reg,
        "cocola_request_duration_seconds_count",
        {"service": "agent-runtime-test", "transport": "grpc", "method": METHOD},
    )
    assert n == 1.0
    # in-flight returned to zero
    inflight = _sample(
        reg,
        "cocola_requests_in_flight",
        {"service": "agent-runtime-test", "transport": "grpc"},
    )
    assert inflight == 0.0


async def test_unary_stream_records_unknown_on_unexpected_error():
    reg = Registry("agent-runtime-test")
    interceptor = PrometheusServerInterceptor(reg)

    async def behavior(request, context):
        yield "partial"
        raise RuntimeError("boom")

    wrapped = await _intercept_unary_stream(interceptor, METHOD, behavior)

    with pytest.raises(RuntimeError, match="boom"):
        await _drain(wrapped.unary_stream("req", FakeContext()))

    count = _sample(
        reg,
        "cocola_requests_total",
        {"service": "agent-runtime-test", "transport": "grpc", "method": METHOD, "code": "UNKNOWN"},
    )
    assert count == 1.0
    inflight = _sample(
        reg,
        "cocola_requests_in_flight",
        {"service": "agent-runtime-test", "transport": "grpc"},
    )
    assert inflight == 0.0


async def test_passthrough_handler_untouched():
    """A handler arity cocola does not use (unary_unary) is left unwrapped but
    still records via the same path."""
    reg = Registry("agent-runtime-test")
    interceptor = PrometheusServerInterceptor(reg)

    async def behavior(request, context):
        return "pong"

    async def continuation(hcd):
        return grpc.unary_unary_rpc_method_handler(behavior)

    wrapped = await interceptor.intercept_service(continuation, _details(METHOD))
    result = await wrapped.unary_unary("ping", FakeContext())

    assert result == "pong"
    count = _sample(
        reg,
        "cocola_requests_total",
        {"service": "agent-runtime-test", "transport": "grpc", "method": METHOD, "code": "OK"},
    )
    assert count == 1.0
