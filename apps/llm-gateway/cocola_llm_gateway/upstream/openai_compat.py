"""OpenAI-compatible upstream (reserved, functional skeleton).

Many model vendors (and local servers like vLLM / Ollama / TGI) expose an
OpenAI `/chat/completions` endpoint. This adapter proves the UpstreamProvider
abstraction holds across a *different* wire format: it translates our normalized
ChatRequest into an OpenAI chat-completions request and maps the OpenAI SSE
`choices[].delta.content` stream back into our StreamEvent taxonomy.

Status: functional but NOT on the M3 critical path. The reference path is
Anthropic passthrough (what the Claude Agent SDK needs). This exists so adding a
third vendor later is copy-and-adapt, and so tests can assert the service layer
is truly provider-agnostic.

HARD CONSTRAINT (ADR-0004): base_url / api_key from config only; the default is
the public OpenAI URL purely for BYO-key local runs, never an internal endpoint.
"""
from __future__ import annotations

import json
from collections.abc import AsyncIterator
from dataclasses import dataclass

import httpx

from cocola_common import ErrorCode
from cocola_llm_gateway.types import ChatRequest, StreamEvent, StreamEventType, Usage
from cocola_llm_gateway.upstream.errors import UpstreamError


@dataclass
class OpenAICompatConfig:
    base_url: str = "https://api.openai.com/v1"
    api_key: str = ""
    timeout_s: float = 60.0
    connect_timeout_s: float = 10.0
    # Many OpenAI-compatible servers support usage in the final stream chunk via
    # this flag; off by default for broad compatibility.
    stream_usage: bool = True


def _build_payload(req: ChatRequest, *, stream_usage: bool) -> dict:
    payload: dict = {
        "model": req.model,
        "messages": [{"role": m.role, "content": m.content} for m in req.messages],
        "max_tokens": req.params.max_tokens,
        "stream": True,
    }
    if req.params.temperature is not None:
        payload["temperature"] = req.params.temperature
    if req.params.top_p is not None:
        payload["top_p"] = req.params.top_p
    if req.params.stop:
        payload["stop"] = req.params.stop
    if stream_usage:
        payload["stream_options"] = {"include_usage": True}
    return payload


class OpenAICompatUpstream:
    name = "openai_compat"

    def __init__(self, cfg: OpenAICompatConfig):
        if not cfg.api_key:
            raise UpstreamError(
                ErrorCode.INVALID_ARGUMENT,
                "OpenAICompatUpstream requires an api_key (set via config/env)",
            )
        self._cfg = cfg
        self._client = httpx.AsyncClient(
            base_url=cfg.base_url,
            timeout=httpx.Timeout(cfg.timeout_s, connect=cfg.connect_timeout_s),
            headers={
                "authorization": f"Bearer {cfg.api_key}",
                "content-type": "application/json",
            },
        )

    async def chat_stream(self, req: ChatRequest) -> AsyncIterator[StreamEvent]:
        payload = _build_payload(req, stream_usage=self._cfg.stream_usage)
        try:
            async with self._client.stream("POST", "/chat/completions", json=payload) as resp:
                if resp.status_code >= 400:
                    body = (await resp.aread()).decode(errors="replace")
                    yield StreamEvent(
                        StreamEventType.ERROR,
                        error=f"upstream {resp.status_code}: {body[:500]}",
                        code="upstream_http_error",
                    )
                    return
                async for ev in self._parse_stream(resp, req.model):
                    yield ev
        except httpx.TimeoutException as e:
            yield StreamEvent(StreamEventType.ERROR, error=f"upstream timeout: {e}", code="timeout")
        except httpx.HTTPError as e:
            yield StreamEvent(StreamEventType.ERROR, error=f"upstream transport error: {e}", code="transport")

    async def _parse_stream(self, resp: httpx.Response, model: str) -> AsyncIterator[StreamEvent]:
        """OpenAI streams `data: {json}` lines (no event: field), terminated by
        `data: [DONE]`. Map deltas to CONTENT_DELTA; finish_reason/usage to
        MESSAGE_DELTA."""
        yield StreamEvent(StreamEventType.MESSAGE_START, usage=Usage(), model=model)
        finish_reason = ""
        completion_tokens = 0
        prompt_tokens = 0
        saw_usage = False
        chunks = 0

        async for raw in resp.aiter_lines():
            if not raw.startswith("data:"):
                continue
            data_str = raw[len("data:") :].strip()
            if data_str == "[DONE]":
                break
            try:
                data = json.loads(data_str)
            except json.JSONDecodeError:
                continue

            usage = data.get("usage")
            if usage:
                saw_usage = True
                prompt_tokens = int(usage.get("prompt_tokens", 0))
                completion_tokens = int(usage.get("completion_tokens", 0))

            for choice in data.get("choices", []):
                delta = choice.get("delta", {})
                text = delta.get("content")
                if text:
                    chunks += 1
                    yield StreamEvent(StreamEventType.CONTENT_DELTA, text=str(text))
                if choice.get("finish_reason"):
                    finish_reason = str(choice["finish_reason"])

        # If the server didn't report usage, fall back to chunk count so billing
        # still records something deterministic.
        if not saw_usage:
            completion_tokens = chunks
        if prompt_tokens:
            yield StreamEvent(StreamEventType.MESSAGE_START, usage=Usage(prompt_tokens=prompt_tokens), model=model)
        yield StreamEvent(
            StreamEventType.MESSAGE_DELTA,
            usage=Usage(completion_tokens=completion_tokens),
            finish_reason=finish_reason or "stop",
        )
        yield StreamEvent(StreamEventType.MESSAGE_STOP)

    async def aclose(self) -> None:
        await self._client.aclose()
