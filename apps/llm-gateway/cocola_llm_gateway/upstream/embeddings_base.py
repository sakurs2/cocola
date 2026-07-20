"""Provider contract for the OpenAI Embeddings wire protocol."""

from __future__ import annotations

from typing import Protocol, runtime_checkable


@runtime_checkable
class EmbeddingsProvider(Protocol):
    name: str

    async def create_embeddings(self, payload: dict) -> dict: ...

    async def aclose(self) -> None: ...
