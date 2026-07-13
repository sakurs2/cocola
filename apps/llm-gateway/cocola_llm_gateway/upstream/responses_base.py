"""Provider contract for the OpenAI Responses wire protocol."""

from __future__ import annotations

from collections.abc import AsyncIterator
from typing import Protocol, runtime_checkable


@runtime_checkable
class ResponsesProvider(Protocol):
    name: str

    async def create_response(self, payload: dict) -> dict:
        """Create one non-streaming response using the upstream wire shape."""

    def stream_response(self, payload: dict) -> AsyncIterator[bytes]:
        """Yield upstream Responses SSE bytes without translating events."""

    async def aclose(self) -> None: ...
