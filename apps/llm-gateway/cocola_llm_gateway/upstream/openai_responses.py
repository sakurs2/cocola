"""Transparent OpenAI Responses API upstream."""

from __future__ import annotations

from collections.abc import AsyncIterator
from dataclasses import dataclass

import httpx
from cocola_common import ErrorCode

from cocola_llm_gateway.upstream.errors import UpstreamError


@dataclass
class OpenAIResponsesConfig:
    base_url: str = "https://api.openai.com/v1"
    api_key: str = ""
    timeout_s: float = 600.0
    connect_timeout_s: float = 10.0


class OpenAIResponsesUpstream:
    name = "openai_responses"

    def __init__(self, cfg: OpenAIResponsesConfig):
        if not cfg.api_key:
            raise UpstreamError(
                ErrorCode.INVALID_ARGUMENT,
                "OpenAIResponsesUpstream requires an api_key",
            )
        self._client = httpx.AsyncClient(
            base_url=cfg.base_url.rstrip("/") + "/",
            timeout=httpx.Timeout(cfg.timeout_s, connect=cfg.connect_timeout_s),
            headers={
                "authorization": f"Bearer {cfg.api_key}",
                "content-type": "application/json",
                "accept": "application/json",
            },
        )

    async def create_response(self, payload: dict) -> dict:
        try:
            response = await self._client.post("responses", json=payload)
        except httpx.TimeoutException as exc:
            raise UpstreamError(ErrorCode.UNAVAILABLE, "upstream timeout", retryable=True) from exc
        except httpx.HTTPError as exc:
            raise UpstreamError(
                ErrorCode.UNAVAILABLE, "upstream transport error", retryable=True
            ) from exc
        if response.status_code >= 400:
            raise UpstreamError(
                ErrorCode.UNAVAILABLE,
                f"upstream responses request failed ({response.status_code})",
                status=response.status_code,
                retryable=response.status_code == 429 or response.status_code >= 500,
            )
        try:
            return response.json()
        except ValueError as exc:
            raise UpstreamError(ErrorCode.UNAVAILABLE, "upstream returned invalid JSON") from exc

    async def stream_response(self, payload: dict) -> AsyncIterator[bytes]:
        try:
            async with self._client.stream("POST", "responses", json=payload) as response:
                if response.status_code >= 400:
                    raise UpstreamError(
                        ErrorCode.UNAVAILABLE,
                        f"upstream responses request failed ({response.status_code})",
                        status=response.status_code,
                        retryable=response.status_code == 429 or response.status_code >= 500,
                    )
                # httpx transparently decodes Content-Encoding here. Forwarding
                # aiter_raw() would send gzip bytes after this gateway has
                # dropped the upstream Content-Encoding header.
                async for chunk in response.aiter_bytes():
                    if chunk:
                        yield chunk
        except UpstreamError:
            raise
        except httpx.TimeoutException as exc:
            raise UpstreamError(ErrorCode.UNAVAILABLE, "upstream timeout", retryable=True) from exc
        except httpx.HTTPError as exc:
            raise UpstreamError(
                ErrorCode.UNAVAILABLE, "upstream transport error", retryable=True
            ) from exc

    async def aclose(self) -> None:
        await self._client.aclose()
