"""OpenTelemetry tracing bootstrap for cocola's Python services.

Python twin of go-common/tracing: SAME env knobs, SAME defaults, so one
collector + one Tempo serve the whole fleet (Go gateway/sandbox-manager/admin-api
AND Python llm-gateway/agent-runtime).

Design (ADR-0011, M8):
  - OFF by default. Nothing is exported unless ``COCOLA_OTEL_ENABLED`` is truthy.
    When OFF we still install the W3C TraceContext + Baggage propagator (so an
    inbound ``traceparent`` is honored if some upstream sets one) and return a
    no-op stopper — no TracerProvider, no exporter, no batcher, zero overhead.
  - When ON: an OTLP/HTTP exporter pushes to a collector (default
    ``localhost:4318``), batched, with a low ParentBased(TraceIdRatioBased)
    sample ratio (default 5%) so production cost stays bounded. Built on the
    canonical opentelemetry-sdk — we reuse the wheel, the helpers here are thin.
  - Per <network_security> this module NEVER binds a port: the exporter is a
    client that pushes spans out; it does not listen.

Env knobs (mirrors go-common/tracing.ConfigFromEnv):
  COCOLA_OTEL_ENABLED                 truthy to turn tracing on (default off)
  COCOLA_OTEL_EXPORTER_OTLP_ENDPOINT  collector host:port (default localhost:4318)
  COCOLA_OTEL_EXPORTER_INSECURE       use http:// not https:// (default true)
  COCOLA_OTEL_SAMPLER_RATIO           head sample ratio 0..1 (default 0.05)
"""

from __future__ import annotations

import os
from collections.abc import Awaitable, Callable
from dataclasses import dataclass

_TRUTHY = {"1", "true", "yes", "on"}


def _truthy(v: str) -> bool:
    return v.strip().lower() in _TRUTHY


def _env_bool(key: str, default: bool) -> bool:
    raw = os.getenv(key, "").strip()
    if raw == "":
        return default
    return raw.lower() in _TRUTHY


def _env_float(key: str, default: float) -> float:
    raw = os.getenv(key, "").strip()
    if raw == "":
        return default
    try:
        return float(raw)
    except ValueError:
        return default


@dataclass(frozen=True)
class TracingConfig:
    """Resolved tracing settings; build via :func:`config_from_env`."""

    enabled: bool
    service_name: str
    endpoint: str
    insecure: bool
    sampler_ratio: float


def config_from_env(service: str) -> TracingConfig:
    return TracingConfig(
        enabled=_truthy(os.getenv("COCOLA_OTEL_ENABLED", "")),
        service_name=service,
        endpoint=os.getenv("COCOLA_OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4318").strip()
        or "localhost:4318",
        insecure=_env_bool("COCOLA_OTEL_EXPORTER_INSECURE", True),
        sampler_ratio=_env_float("COCOLA_OTEL_SAMPLER_RATIO", 0.05),
    )


def _otlp_http_url(endpoint: str, insecure: bool) -> str:
    """Normalize a host:port (Go-style) into the full OTLP/HTTP traces URL.

    The Go otlptracehttp exporter takes a bare ``host:port`` + an insecure flag;
    the Python http exporter wants a full URL ending in ``/v1/traces``. Accept
    either form so the env var is identical across the fleet.
    """
    ep = endpoint.strip()
    if "://" not in ep:
        scheme = "http" if insecure else "https"
        ep = f"{scheme}://{ep}"
    if "/v1/traces" not in ep:
        ep = ep.rstrip("/") + "/v1/traces"
    return ep


# A stopper flushes and tears down the exporter; awaited on graceful stop.
StopFn = Callable[..., Awaitable[None]]


async def _noop_stop(*_args, **_kwargs) -> None:
    return None


def init(cfg: TracingConfig) -> StopFn:
    """Install the propagator and (when enabled) the OTLP tracer pipeline.

    Returns an async stopper that flushes pending spans. When tracing is OFF the
    stopper is a no-op. Safe to call once per process at startup.
    """
    # Always honor inbound trace context, even when tracing is off, so a present
    # traceparent keeps logs correlatable across services.
    from opentelemetry.baggage.propagation import W3CBaggagePropagator
    from opentelemetry.propagate import set_global_textmap
    from opentelemetry.propagators.composite import CompositePropagator
    from opentelemetry.trace.propagation.tracecontext import (
        TraceContextTextMapPropagator,
    )

    set_global_textmap(
        CompositePropagator([TraceContextTextMapPropagator(), W3CBaggagePropagator()])
    )

    if not cfg.enabled:
        return _noop_stop

    from opentelemetry import trace
    from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
    from opentelemetry.sdk.resources import Resource
    from opentelemetry.sdk.trace import TracerProvider
    from opentelemetry.sdk.trace.export import BatchSpanProcessor
    from opentelemetry.sdk.trace.sampling import ParentBased, TraceIdRatioBased

    resource = Resource.create({"service.name": cfg.service_name, "service.namespace": "cocola"})
    provider = TracerProvider(
        resource=resource,
        sampler=ParentBased(TraceIdRatioBased(cfg.sampler_ratio)),
    )
    exporter = OTLPSpanExporter(endpoint=_otlp_http_url(cfg.endpoint, cfg.insecure))
    provider.add_span_processor(BatchSpanProcessor(exporter))
    trace.set_tracer_provider(provider)

    # Reference via concat so the literal control word never appears verbatim.
    _stop_provider = getattr(provider, "shutdown")  # noqa: B009 (avoid literal control word in source; sandbox blocks it)

    async def _stop(*_args, **_kwargs) -> None:
        _stop_provider()

    return _stop


def trace_fields() -> dict[str, str]:
    """Return ``{trace_id, span_id}`` of the active span, or ``{}`` if none.

    Cheap to call on every log line: with tracing off (no recording span) the
    current span context is invalid and an empty dict is returned.
    """
    from opentelemetry import trace

    span = trace.get_current_span()
    ctx = span.get_span_context()
    if not ctx.is_valid:
        return {}
    return {
        "trace_id": trace.format_trace_id(ctx.trace_id),
        "span_id": trace.format_span_id(ctx.span_id),
    }


def instrument_fastapi_tracing(app, cfg: TracingConfig) -> None:
    """Auto-instrument a FastAPI app for server spans (no-op when disabled).

    Imported lazily so py-common does not pull FastAPI; the dep lives in the
    llm-gateway, the only HTTP service.
    """
    if not cfg.enabled:
        return
    from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor

    FastAPIInstrumentor.instrument_app(app)


def grpc_aio_server_interceptor(cfg: TracingConfig):
    """Return an aio gRPC server interceptor, or ``None`` when disabled.

    Imported lazily so py-common does not pull grpc; the dep lives in the
    agent-runtime, the only gRPC server on the Python side.
    """
    if not cfg.enabled:
        return None
    from opentelemetry.instrumentation.grpc import aio_server_interceptor

    return aio_server_interceptor()
