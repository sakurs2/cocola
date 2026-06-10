"""Normalized upstream failures.

Vendor SDKs raise a zoo of exceptions (httpx.TimeoutException, status errors,
JSON decode errors). We collapse them into one type with a stable code so
middleware and the HTTP layer can map consistently to client-facing errors
without importing vendor packages.
"""

from __future__ import annotations

from cocola_common import ErrorCode


class UpstreamError(Exception):
    """A failure talking to an upstream model provider."""

    def __init__(
        self,
        code: ErrorCode,
        message: str,
        *,
        status: int | None = None,
        retryable: bool = False,
        cause: Exception | None = None,
    ):
        super().__init__(message)
        self.code = code
        self.message = message
        self.status = status
        self.retryable = retryable
        self.cause = cause

    def __str__(self) -> str:  # pragma: no cover - trivial
        base = f"{self.code.value}: {self.message}"
        if self.status is not None:
            base = f"{base} (status={self.status})"
        return base
