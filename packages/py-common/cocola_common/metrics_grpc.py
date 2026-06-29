"""grpc.aio server interceptor emitting cocola's RED metrics (transport="grpc").

Kept in its own module so importing cocola_common.metrics does not require grpc
to be installed (the llm-gateway has no grpc dependency). agent-runtime, which
already depends on grpcio, imports this.

The "method" label is the gRPC full method (e.g. "/cocola.agent.v1.AgentRuntime
Service/Query"), a bounded set; "code" is the StatusCode name (e.g. "OK",
"NOT_FOUND"), matching the Go interceptor's status.Code(err).String() contract.
"""

from __future__ import annotations

import inspect
import time

import grpc
from grpc.aio import ServerInterceptor

from cocola_common.metrics import Registry


class PrometheusServerInterceptor(ServerInterceptor):
    """Records every (unary or streaming) RPC into the RED vectors.

    Duration spans the whole handler — for server-streaming RPCs (the runtime's
    Query) that is the useful signal: how long the agent turn took end to end.
    """

    def __init__(self, registry: Registry) -> None:
        self.reg = registry

    async def intercept_service(self, continuation, handler_call_details):
        handler = await continuation(handler_call_details)
        if handler is None:
            return handler
        method = handler_call_details.method or "unknown"

        reg = self.reg

        def _wrap_unary_unary(behavior):
            async def wrapper(request, context):
                reg.inflight_inc("grpc")
                start = time.perf_counter()
                code = "OK"
                try:
                    return await behavior(request, context)
                except grpc.RpcError as exc:  # explicit status set by handler
                    code = _code_name(getattr(exc, "code", lambda: None)())
                    raise
                except Exception:
                    code = "UNKNOWN"
                    raise
                finally:
                    if code == "OK":
                        code = _code_name(context.code())
                    reg.observe_request("grpc", method, code, time.perf_counter() - start)
                    reg.inflight_dec("grpc")

            return wrapper

        def _wrap_unary_stream(behavior):
            # grpc.aio supports two server-streaming handler styles: an async
            # generator that yields responses, or a coroutine that streams via
            # context.write() and returns None. Preserve the original arity so
            # both are handled (cocola's Query uses the context.write() style).
            is_asyncgen = inspect.isasyncgenfunction(behavior)

            async def gen_wrapper(request, context):
                reg.inflight_inc("grpc")
                start = time.perf_counter()
                code = "OK"
                try:
                    async for resp in behavior(request, context):
                        yield resp
                except grpc.RpcError as exc:
                    code = _code_name(getattr(exc, "code", lambda: None)())
                    raise
                except Exception:
                    code = "UNKNOWN"
                    raise
                finally:
                    if code == "OK":
                        code = _code_name(context.code())
                    reg.observe_request("grpc", method, code, time.perf_counter() - start)
                    reg.inflight_dec("grpc")

            async def coro_wrapper(request, context):
                reg.inflight_inc("grpc")
                start = time.perf_counter()
                code = "OK"
                try:
                    return await behavior(request, context)
                except grpc.RpcError as exc:
                    code = _code_name(getattr(exc, "code", lambda: None)())
                    raise
                except Exception:
                    code = "UNKNOWN"
                    raise
                finally:
                    if code == "OK":
                        code = _code_name(context.code())
                    reg.observe_request("grpc", method, code, time.perf_counter() - start)
                    reg.inflight_dec("grpc")

            return gen_wrapper if is_asyncgen else coro_wrapper

        # Rebuild the handler preserving its (de)serializers and arity. Only the
        # two server-side arities cocola uses are wrapped; others pass through.
        if handler.unary_unary is not None:
            return grpc.unary_unary_rpc_method_handler(
                _wrap_unary_unary(handler.unary_unary),
                request_deserializer=handler.request_deserializer,
                response_serializer=handler.response_serializer,
            )
        if handler.unary_stream is not None:
            return grpc.unary_stream_rpc_method_handler(
                _wrap_unary_stream(handler.unary_stream),
                request_deserializer=handler.request_deserializer,
                response_serializer=handler.response_serializer,
            )
        return handler


def _code_name(code) -> str:
    """Best-effort StatusCode -> name. None (unset) means a clean return == OK."""
    if code is None:
        return "OK"
    name = getattr(code, "name", None)
    return name if isinstance(name, str) else str(code)
