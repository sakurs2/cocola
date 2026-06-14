"""Unit tests for cocola_common.tracing (the shared OTel bootstrap).

These live in agent-runtime's suite because py-common has no test harness of its
own and agent-runtime already depends on cocola_common + opentelemetry. They are
hermetic: per <network_security> tracing is exercised with the exporter OFF
(propagator-only) so nothing ever stands up a listener or pushes over a socket.
"""

from __future__ import annotations

import cocola_common
from cocola_common.tracing import (
    TracingConfig,
    _otlp_http_url,
    config_from_env,
    grpc_aio_server_interceptor,
    init,
    trace_fields,
)
from opentelemetry import trace


def test_config_from_env_defaults(monkeypatch):
    for k in (
        "COCOLA_OTEL_ENABLED",
        "COCOLA_OTEL_EXPORTER_OTLP_ENDPOINT",
        "COCOLA_OTEL_EXPORTER_INSECURE",
        "COCOLA_OTEL_SAMPLER_RATIO",
    ):
        monkeypatch.delenv(k, raising=False)
    cfg = config_from_env("agent-runtime")
    assert cfg.enabled is False
    assert cfg.service_name == "agent-runtime"
    assert cfg.endpoint == "localhost:4318"
    assert cfg.insecure is True
    assert cfg.sampler_ratio == 0.05


def test_config_from_env_overrides(monkeypatch):
    monkeypatch.setenv("COCOLA_OTEL_ENABLED", "true")
    monkeypatch.setenv("COCOLA_OTEL_EXPORTER_OTLP_ENDPOINT", "collector:4318")
    monkeypatch.setenv("COCOLA_OTEL_EXPORTER_INSECURE", "false")
    monkeypatch.setenv("COCOLA_OTEL_SAMPLER_RATIO", "0.5")
    cfg = config_from_env("svc")
    assert cfg.enabled is True
    assert cfg.endpoint == "collector:4318"
    assert cfg.insecure is False
    assert cfg.sampler_ratio == 0.5


def test_otlp_http_url_normalization():
    assert _otlp_http_url("localhost:4318", True) == "http://localhost:4318/v1/traces"
    assert _otlp_http_url("collector:4318", False) == "https://collector:4318/v1/traces"
    # already a full URL: respected, /v1/traces appended once
    assert _otlp_http_url("http://c:4318/v1/traces", True) == "http://c:4318/v1/traces"


async def test_init_disabled_returns_noop_and_sets_propagator(monkeypatch):
    monkeypatch.delenv("COCOLA_OTEL_ENABLED", raising=False)
    cfg = config_from_env("agent-runtime")
    stop = init(cfg)
    # No exporter/provider stood up: the global provider is still the API no-op.
    provider = trace.get_tracer_provider()
    assert not hasattr(provider, "add_span_processor")
    # The stopper is awaitable and harmless.
    await stop()


async def test_init_disabled_propagator_roundtrips_traceparent(monkeypatch):
    monkeypatch.delenv("COCOLA_OTEL_ENABLED", raising=False)
    init(config_from_env("svc"))
    from opentelemetry.propagate import extract

    carrier = {"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}
    ctx = extract(carrier)
    span_ctx = trace.get_current_span(ctx).get_span_context()
    assert span_ctx.is_valid
    assert trace.format_trace_id(span_ctx.trace_id) == "4bf92f3577b34da6a3ce929d0e0e4736"


def test_trace_fields_empty_without_span():
    # No active recording span -> nothing to stamp on a log line.
    assert trace_fields() == {}


def test_grpc_interceptor_none_when_disabled():
    cfg = TracingConfig(
        enabled=False,
        service_name="svc",
        endpoint="localhost:4318",
        insecure=True,
        sampler_ratio=0.05,
    )
    assert grpc_aio_server_interceptor(cfg) is None


def test_public_api_reexported():
    # The service composition roots import these off the package root.
    assert cocola_common.config_from_env("x").service_name == "x"
    assert callable(cocola_common.init)
    assert callable(cocola_common.trace_fields)
