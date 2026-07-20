"""Exact OpenAI Embeddings upstream; deliberately has no chat capability."""

from __future__ import annotations

from dataclasses import dataclass

import httpx
from cocola_common import ErrorCode

from cocola_llm_gateway.upstream.errors import UpstreamError


@dataclass
class OpenAIEmbeddingsConfig:
    base_url: str = "https://api.openai.com/v1"
    api_key: str = ""
    timeout_s: float = 60.0
    connect_timeout_s: float = 10.0


class OpenAIEmbeddingsUpstream:
    name = "openai_embeddings"

    def __init__(self, cfg: OpenAIEmbeddingsConfig):
        if not cfg.api_key:
            raise UpstreamError(ErrorCode.INVALID_ARGUMENT, "embedding provider requires api_key")
        self._client = httpx.AsyncClient(
            base_url=cfg.base_url.rstrip("/") + "/",
            timeout=httpx.Timeout(cfg.timeout_s, connect=cfg.connect_timeout_s),
            headers={
                "authorization": f"Bearer {cfg.api_key}",
                "content-type": "application/json",
                "accept": "application/json",
            },
        )

    async def create_embeddings(self, payload: dict) -> dict:
        try:
            response = await self._client.post("embeddings", json=payload)
        except httpx.TimeoutException as exc:
            raise UpstreamError(
                ErrorCode.UNAVAILABLE, "embedding upstream timeout", retryable=True
            ) from exc
        except httpx.HTTPError as exc:
            raise UpstreamError(
                ErrorCode.UNAVAILABLE,
                "embedding upstream transport error",
                retryable=True,
            ) from exc
        if response.status_code >= 400:
            raise UpstreamError(
                ErrorCode.UNAVAILABLE,
                f"embedding upstream failed ({response.status_code})",
                status=response.status_code,
                retryable=response.status_code == 429 or response.status_code >= 500,
            )
        try:
            return response.json()
        except ValueError as exc:
            raise UpstreamError(
                ErrorCode.UNAVAILABLE, "embedding upstream returned invalid JSON"
            ) from exc

    async def aclose(self) -> None:
        await self._client.aclose()
