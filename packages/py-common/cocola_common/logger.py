"""Structured logging factory. Wraps structlog so services never import it directly."""

from __future__ import annotations

import logging
import sys

import structlog


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
            structlog.processors.TimeStamper(fmt="iso"),
            structlog.processors.StackInfoRenderer(),
            structlog.processors.format_exc_info,
            structlog.processors.JSONRenderer(),
        ],
        wrapper_class=structlog.stdlib.BoundLogger,
        logger_factory=structlog.stdlib.LoggerFactory(),
        cache_logger_on_first_use=True,
    )
    return structlog.get_logger(name)
