"""Shared building blocks for cocola Python services.

This package intentionally has a minimal surface in M0:
- logger: structured logging factory
- config: Pydantic base settings model
- errors: canonical Error envelope

Concrete implementations (DB session, gRPC clients, OpenTelemetry) land in later
milestones to avoid pulling heavy deps into the M0 import graph.
"""

from cocola_common.errors import CocolaError, ErrorCode
from cocola_common.logger import get_logger

__all__ = ["get_logger", "CocolaError", "ErrorCode"]
