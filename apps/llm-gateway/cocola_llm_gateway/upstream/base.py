"""The UpstreamProvider contract.

This mirrors the role `SandboxProvider` plays in sandbox-manager: the service
layer depends ONLY on this Protocol, never on a concrete vendor client. Swapping
Anthropic for an OpenAI-compatible endpoint (or a Fake in tests) is therefore
invisible above this line.

A provider has exactly one job: take a normalized `ChatRequest` and yield a
stream of normalized `StreamEvent`s. Everything cross-cutting — routing,
retries, rate limiting, billing — lives OUTSIDE the provider (in middleware /
hooks). Providers stay dumb and vendor-specific; business logic stays in hooks.
"""
from __future__ import annotations

from collections.abc import AsyncIterator
from typing import Protocol, runtime_checkable

from cocola_llm_gateway.types import ChatRequest, StreamEvent


@runtime_checkable
class UpstreamProvider(Protocol):
    """Vendor adapter. Implementations MUST be safe for concurrent use across
    many sessions (the gateway shares one provider instance process-wide)."""

    name: str

    def chat_stream(self, req: ChatRequest) -> AsyncIterator[StreamEvent]:
        """Stream a completion.

        Implementations should:
        - translate `req` into the vendor wire format,
        - emit MESSAGE_START (with prompt-token Usage if known) first,
        - emit CONTENT_DELTA events as text arrives,
        - emit MESSAGE_DELTA carrying output-token Usage and finish_reason,
        - emit MESSAGE_STOP last.
        On failure, emit a single ERROR event (normalized) instead of raising,
        so the SSE stream can always be closed cleanly. Connection-level
        failures before the first byte MAY raise UpstreamError; middleware will
        translate it.
        """
        ...

    async def aclose(self) -> None:
        """Release any pooled connections. Safe to call multiple times."""
        ...
