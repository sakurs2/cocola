"""Canonical error envelope for Python services. Mirrors go-common/errors."""
from __future__ import annotations

from enum import Enum


class ErrorCode(str, Enum):
    UNKNOWN = "UNKNOWN"
    INVALID_ARGUMENT = "INVALID_ARGUMENT"
    NOT_FOUND = "NOT_FOUND"
    PERMISSION_DENIED = "PERMISSION_DENIED"
    UNAVAILABLE = "UNAVAILABLE"
    INTERNAL = "INTERNAL"


class CocolaError(Exception):
    """Domain error carrying a stable code and an optional cause."""

    def __init__(self, code: ErrorCode, message: str, *, cause: Exception | None = None):
        super().__init__(message)
        self.code = code
        self.message = message
        self.cause = cause

    def __str__(self) -> str:  # pragma: no cover - trivial
        if self.cause is not None:
            return f"{self.code.value}: {self.message}: {self.cause!r}"
        return f"{self.code.value}: {self.message}"
