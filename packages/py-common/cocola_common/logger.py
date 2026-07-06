"""Structured logging factory. Wraps structlog so services never import it directly."""

from __future__ import annotations

import logging
import sys

import structlog


def _add_trace_context(_logger, _method_name, event_dict):
    """structlog processor: stamp the active OTel trace_id/span_id onto each line.

    Cheap and safe with tracing OFF: when there is no recording span the helper
    returns ``{}`` and nothing is added, so logs stay clean in zero-config dev
    while becoming trace-correlatable the moment OTel is enabled (ADR-0011).
    """
    from cocola_common.tracing import trace_fields

    fields = trace_fields()
    if fields:
        event_dict.update(fields)
    return event_dict


def _service_fields(name: str) -> dict[str, str]:
    parts = [p for p in name.split(".") if p]
    if len(parts) >= 2 and parts[0] == "cocola":
        service = parts[1]
        component = ".".join(parts[2:]) or service
        return {"service": service, "component": component}
    return {"service": name, "component": name}


def get_logger(name: str, level: str = "INFO") -> structlog.stdlib.BoundLogger:
    """Return a JSON-emitting structured logger bound to `name`.

    Configure once per process (idempotent due to structlog cache).
    """
    logging.basicConfig(
        format="%(message)s",
        stream=sys.stdout,
        level=getattr(logging, level.upper(), logging.INFO),
    )
    structlog.configure(
        processors=[
            structlog.stdlib.add_log_level,
            _add_trace_context,
            structlog.processors.TimeStamper(fmt="iso"),
            structlog.processors.StackInfoRenderer(),
            structlog.processors.format_exc_info,
            structlog.processors.JSONRenderer(),
        ],
        wrapper_class=structlog.stdlib.BoundLogger,
        logger_factory=structlog.stdlib.LoggerFactory(),
        cache_logger_on_first_use=True,
    )
    return structlog.get_logger(name).bind(**_service_fields(name))
