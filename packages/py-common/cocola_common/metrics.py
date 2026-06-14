"""Shared Prometheus instrumentation for cocola's Python services.

This is the Python twin of go-common/metrics: it emits the SAME metric names and
labels so a single Prometheus + a single Grafana dashboard cover the whole fleet
(Go gateway/sandbox-manager/admin-api AND Python llm-gateway/agent-runtime):

  - cocola_requests_total{service,transport,method,code}
  - cocola_request_duration_seconds{service,transport,method}
  - cocola_requests_in_flight{service,transport}

Design (ADR-0011, M8):
  - Built on prometheus_client, the canonical library — the adapters here are
    thin (a route-aware ASGI middleware + a grpc.aio interceptor live in
    metrics_grpc), so we reuse the wheel rather than reinvent it. We deliberately
    do NOT use prometheus-fastapi-instrumentator: it ships its own metric names,
    which would fork the fleet's dashboard from the Go services. Matching the Go
    RED contract is worth the ~40 lines of middleware.
  - One CollectorRegistry per service (not the process-global default) so tests
    are hermetic and two registries never collide.
  - Per <network_security>, this module never binds a port. The /metrics ASGI app
    is mounted onto the service's existing FastAPI app, or (agent-runtime, which
    has no HTTP server) exposed via start_http_server only in real deployments.
"""

from __future__ import annotations

import time
from collections.abc import Sequence

from prometheus_client import (
    CONTENT_TYPE_LATEST,
    CollectorRegistry,
    Counter,
    Gauge,
    GCCollector,
    Histogram,
    PlatformCollector,
    ProcessCollector,
    generate_latest,
)

# Mirrors go-common/metrics defaultBuckets: sub-ms RPCs up to multi-second cold
# starts (sandbox create), so one histogram serves both.
DEFAULT_BUCKETS: tuple[float, ...] = (
    0.001,
    0.005,
    0.01,
    0.025,
    0.05,
    0.1,
    0.25,
    0.5,
    1,
    2.5,
    5,
    10,
    30,
)


class Registry:
    """Holds a per-service CollectorRegistry plus the cocola-standard RED vectors.

    The service name is carried as the ``service`` label on every series so a
    shared Prometheus can tell fleets apart, matching the Go side's const label.
    """

    def __init__(
        self,
        service: str,
        *,
        buckets: Sequence[float] = DEFAULT_BUCKETS,
    ) -> None:
        self.service = service
        self.registry = CollectorRegistry()

        # Free baseline: process + platform + GC stats, same spirit as the Go
        # runtime/process collectors.
        ProcessCollector(registry=self.registry)
        PlatformCollector(registry=self.registry)
        GCCollector(registry=self.registry)

        self._req_total = Counter(
            "cocola_requests_total",
            "Total requests handled, by transport, method and status code.",
            ["service", "transport", "method", "code"],
            registry=self.registry,
        )
        self._req_duration = Histogram(
            "cocola_request_duration_seconds",
            "Request handling latency in seconds, by transport and method.",
            ["service", "transport", "method"],
            buckets=tuple(buckets),
            registry=self.registry,
        )
        self._inflight = Gauge(
            "cocola_requests_in_flight",
            "In-flight requests, by transport.",
            ["service", "transport"],
            registry=self.registry,
        )

    # -- internal recording seams used by the middleware / interceptor --

    def observe_request(self, transport: str, method: str, code: str, seconds: float) -> None:
        self._req_total.labels(self.service, transport, method, code).inc()
        self._req_duration.labels(self.service, transport, method).observe(seconds)

    def inflight_inc(self, transport: str) -> None:
        self._inflight.labels(self.service, transport).inc()

    def inflight_dec(self, transport: str) -> None:
        self._inflight.labels(self.service, transport).dec()

    # -- exposition --

    def render(self) -> tuple[bytes, str]:
        """Render the Prometheus exposition for THIS registry as (body, content_type)."""
        return generate_latest(self.registry), CONTENT_TYPE_LATEST


class PrometheusASGIMiddleware:
    """Pure-ASGI RED middleware (transport="http").

    Pure ASGI rather than Starlette's BaseHTTPMiddleware on purpose: the latter
    buffers the response body, which would break the llm-gateway's SSE streaming.
    This wrapper only observes the response-start status and the wall-clock
    duration, never touching the body, so streams flush unchanged.

    The "method" label is ``"<HTTP method> <route template>"`` (e.g.
    "POST /v1/messages"). The template is read from ``scope["route"]`` AFTER the
    app routes the request, so path params never inflate label cardinality;
    unmatched paths fall back to "unmatched".
    """

    def __init__(self, app, registry: Registry) -> None:
        self.app = app
        self.reg = registry

    async def __call__(self, scope, receive, send):
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return

        method = scope.get("method", "GET")
        status_holder = {"code": 500}
        self.reg.inflight_inc("http")
        start = time.perf_counter()

        async def send_wrapper(message):
            if message["type"] == "http.response.start":
                status_holder["code"] = message["status"]
            await send(message)

        try:
            await self.app(scope, receive, send_wrapper)
        finally:
            duration = time.perf_counter() - start
            route = scope.get("route")
            template = getattr(route, "path", None) or "unmatched"
            label = f"{method} {template}"
            self.reg.observe_request("http", label, str(status_holder["code"]), duration)
            self.reg.inflight_dec("http")


def instrument_fastapi(app, registry: Registry, *, metrics_path: str = "/metrics"):
    """Wire RED instrumentation onto a FastAPI/Starlette app and add GET /metrics.

    Uses a plain route (not a sub-mount) so the path matches exactly with no
    trailing-slash 307 redirect — Prometheus does not follow redirects on the
    scrape path. Returns the app for chaining.
    """
    from starlette.responses import Response

    app.add_middleware(PrometheusASGIMiddleware, registry=registry)

    async def _metrics(_request):
        body, content_type = registry.render()
        return Response(content=body, media_type=content_type)

    app.add_route(metrics_path, _metrics, methods=["GET"])
    return app
