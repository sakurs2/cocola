"""Gateway service layer: resolve -> (quota gate) -> stream -> meter + commit.

This is the single orchestration seam the HTTP layer calls. It is deliberately
the ONLY place that knows about all collaborators (registry, resilient streaming,
ledger, quota) at once; each collaborator stays unaware of the others.

Flow for one request:
  0. check_quota(identity)               -> raise QuotaExceeded (HTTP 429) early
  1. registry.resolve(route_id)          -> (route, provider)
  2. ResilientStreamer(provider).stream  -> normalized StreamEvent stream
  3. pass events through UNCHANGED, accumulating Usage
  4. at stream end: write ONE UsageRecord (billing) AND commit the token total to
     the quota counters (M4)

Metering + quota commit are *hooks around* the stream, not logic inside any
provider — this keeps both uniform across vendors (a standing project rule).

Neither billing nor quota may break the user's stream: a ledger or counter write
failure is logged and swallowed. Records/commits happen even on error/partial
streams so usage is captured for whatever the upstream already produced.
"""

from __future__ import annotations

import asyncio
import json
import time
import uuid
from collections.abc import AsyncIterator
from typing import Protocol

from cocola_common import CocolaError, ErrorCode, get_logger

from cocola_llm_gateway.auth.jwt import Identity
from cocola_llm_gateway.billing.ledger import Ledger, UsageRecord
from cocola_llm_gateway.conversation_trace import ConversationTraceStore, TraceContext, utc_now
from cocola_llm_gateway.middleware import RateLimiter, ResiliencePolicy, ResilientStreamer
from cocola_llm_gateway.quota import Enforcer, QuotaStatus
from cocola_llm_gateway.registry import ModelRoute, Registry
from cocola_llm_gateway.types import (
    ChatMessage,
    ChatParams,
    ChatRequest,
    StreamEvent,
    StreamEventType,
    Usage,
)
from cocola_llm_gateway.upstream.errors import UpstreamError

log = get_logger("cocola.llm-gateway.service")


class RegistrySource(Protocol):
    async def acquire_registry(self, *, force_refresh: bool = False) -> Registry: ...

    async def release_registry(self, registry: Registry) -> None: ...

    async def aclose(self) -> None: ...


class StaticRegistrySource:
    def __init__(self, registry: Registry):
        self._registry = registry

    async def acquire_registry(self, *, force_refresh: bool = False) -> Registry:
        return self._registry

    async def release_registry(self, registry: Registry) -> None:
        return None

    async def aclose(self) -> None:
        await self._registry.aclose()


class GatewayService:
    def __init__(
        self,
        registry: Registry,
        ledger: Ledger,
        policy: ResiliencePolicy | None = None,
        enforcer: Enforcer | None = None,
        registry_source: RegistrySource | None = None,
        trace_store: ConversationTraceStore | None = None,
    ):
        self._registry = registry
        self._registry_source = registry_source or StaticRegistrySource(registry)
        self._ledger = ledger
        self._policy = policy or ResiliencePolicy()
        self._enforcer = enforcer
        self._trace_store = trace_store
        # One shared limiter so per-tenant buckets persist across requests.
        self._limiter = RateLimiter(self._policy.rate_limit_rps, self._policy.rate_burst)

    @property
    def registry(self) -> Registry:
        return self._registry

    @property
    def ledger(self) -> Ledger:
        return self._ledger

    @property
    def enforcer(self) -> Enforcer | None:
        return self._enforcer

    async def resolve_model(self, requested_alias: str | None) -> str:
        """Expose the resolved real model id (used by the front-end to stamp the
        outgoing message's `model` field). Raises CocolaError(NOT_FOUND)."""
        registry = await self._registry_source.acquire_registry()
        try:
            self._registry = registry
            route, _ = registry.resolve(requested_alias)
            return route.real_model
        finally:
            await self._registry_source.release_registry(registry)

    async def validate_chat_request(self, req: ChatRequest, requested_alias: str | None) -> None:
        registry = await self._registry_source.acquire_registry()
        try:
            route, _ = registry.resolve_chat(requested_alias)
            validate_reasoning_effort(route, anthropic_reasoning_effort(req))
        finally:
            await self._registry_source.release_registry(registry)

    async def registry_status(self) -> tuple[str, list[str]]:
        """Return a stable health snapshot without exposing a leased registry."""
        registry = await self._registry_source.acquire_registry()
        try:
            return registry.default_alias, registry.aliases()
        finally:
            await self._registry_source.release_registry(registry)

    async def memory_embedding_configured(self) -> bool:
        """Report route availability without executing or billing a model call."""
        registry = await self._registry_source.acquire_registry(force_refresh=True)
        try:
            route_id = registry.memory_embedding_route_id
            if not route_id:
                return False
            try:
                registry.resolve_embeddings(route_id)
            except CocolaError:
                return False
            return True
        finally:
            await self._registry_source.release_registry(registry)

    async def memory_chat_completion(self, payload: dict) -> dict:
        """Serve OpenViking's narrow text-only extraction contract.

        This path deliberately bypasses user quota while still recording usage
        under the platform ``memory-service`` identity.
        """
        registry = await self._registry_source.acquire_registry(force_refresh=True)
        route = None
        request_id = f"memory_{uuid.uuid4().hex[:16]}"
        usage = Usage()
        try:
            self._registry = registry
            route_id = registry.memory_extraction_route_id
            if not route_id:
                raise CocolaError(
                    ErrorCode.UNAVAILABLE,
                    "memory extraction model is not configured",
                )

            response_format = payload.get("response_format")
            messages = [
                ChatMessage(role=item["role"], content=item["content"])
                for item in payload["messages"]
            ]
            if response_format:
                messages.insert(
                    0,
                    ChatMessage(
                        role="system",
                        content=_structured_output_instruction(response_format),
                    ),
                )

            route, provider = registry.resolve_chat(route_id)
            req = ChatRequest(
                model=route.real_model,
                messages=messages,
                params=ChatParams(
                    max_tokens=int(payload.get("max_tokens") or 1024),
                    temperature=payload.get("temperature"),
                    stream=True,
                ),
                user_id="memory-service",
                session_id="memory-service",
            )
            chunks: list[str] = []
            streamer = ResilientStreamer(provider, self._policy, self._limiter)
            async for event in streamer.chat_stream(req):
                if event.type is StreamEventType.ERROR:
                    raise UpstreamError(
                        ErrorCode.UNAVAILABLE,
                        "memory extraction upstream failed",
                        retryable=False,
                    )
                text_delta = _memory_stream_text_delta(event)
                if text_delta:
                    chunks.append(text_delta)
                if event.usage is not None and event.type in {
                    StreamEventType.MESSAGE_START,
                    StreamEventType.MESSAGE_DELTA,
                }:
                    usage.merge(event.usage)
            text = "".join(chunks)

            if not text:
                raise UpstreamError(
                    ErrorCode.UNAVAILABLE,
                    "memory extraction upstream returned no text",
                    retryable=False,
                )
            await self._write_usage_record(
                request_id=request_id,
                user_id="memory-service",
                session_id="memory-service",
                route=route,
                usage=usage,
                status="ok",
                error="",
            )
            return {
                "id": request_id,
                "object": "chat.completion",
                "created": int(time.time()),
                "model": "cocola-memory-extraction",
                "choices": [
                    {
                        "index": 0,
                        "message": {"role": "assistant", "content": text},
                        "finish_reason": "stop",
                    }
                ],
                "usage": {
                    "prompt_tokens": usage.prompt_tokens,
                    "completion_tokens": usage.completion_tokens,
                    "total_tokens": usage.total_tokens,
                },
            }
        finally:
            await self._registry_source.release_registry(registry)

    async def memory_embeddings(self, payload: dict) -> dict:
        registry = await self._registry_source.acquire_registry(force_refresh=True)
        route = None
        try:
            self._registry = registry
            route_id = registry.memory_embedding_route_id
            if not route_id:
                raise CocolaError(ErrorCode.UNAVAILABLE, "memory embedding model is not configured")
            route, provider = registry.resolve_embeddings(route_id)
            upstream_payload = {
                "model": route.real_model,
                "input": payload["input"],
                "encoding_format": "float",
            }
            response = await self._embeddings_create_with_retry(provider, upstream_payload)
            _validate_embedding_dimensions(response, route.embedding_dimension)
            raw_usage = response.get("usage") if isinstance(response.get("usage"), dict) else {}
            usage = Usage(prompt_tokens=int(raw_usage.get("prompt_tokens") or 0))
            await self._write_usage_record(
                request_id=f"memory_embedding_{uuid.uuid4().hex[:16]}",
                user_id="memory-service",
                session_id="memory-service",
                route=route,
                usage=usage,
                status="ok",
                error="",
            )
            return {**response, "model": "cocola-memory-embedding"}
        finally:
            await self._registry_source.release_registry(registry)

    async def check_quota(self, identity: Identity | None) -> None:
        """Pre-call gate. Raises QuotaExceeded if the caller is over budget.

        No-op when no enforcer is configured or identity is missing.
        """
        if self._enforcer is None or identity is None:
            return
        await self._enforcer.check(identity)

    async def quota_status(self, identity: Identity | None) -> list[QuotaStatus]:
        if self._enforcer is None or identity is None:
            return []
        return await self._enforcer.status(identity)

    async def chat_stream(
        self,
        req: ChatRequest,
        *,
        requested_alias: str | None = None,
        identity: Identity | None = None,
        trace_context: TraceContext | None = None,
    ) -> AsyncIterator[StreamEvent]:
        """Resolve, stream with resilience, meter, and commit quota.

        `req.model` is expected to already be the resolved real model (the codec
        sets it). `requested_alias` is the caller-facing alias used for routing &
        billing attribution. `identity` drives the post-call quota commit.
        """
        alias = requested_alias or req.metadata.get("requested_model") or None
        registry = await self._registry_source.acquire_registry()
        try:
            self._registry = registry
            route, provider = registry.resolve(alias)
            validate_reasoning_effort(route, anthropic_reasoning_effort(req))
        except BaseException:
            await self._registry_source.release_registry(registry)
            raise

        request_id = req.metadata.get("request_id") or f"req_{uuid.uuid4().hex[:16]}"
        streamer = ResilientStreamer(provider, self._policy, self._limiter)

        usage = Usage()
        status = "ok"
        error = ""
        started_at = utc_now()
        started_mono = time.monotonic()
        ttft_ms = 0
        saw_first_output = False

        try:
            async for ev in streamer.chat_stream(req):
                if not saw_first_output and ev.type in (
                    StreamEventType.MESSAGE_START,
                    StreamEventType.CONTENT_DELTA,
                ):
                    saw_first_output = True
                    ttft_ms = int((time.monotonic() - started_mono) * 1000)
                if ev.usage is not None and ev.type in (
                    StreamEventType.MESSAGE_START,
                    StreamEventType.MESSAGE_DELTA,
                ):
                    usage.merge(ev.usage)
                elif ev.type is StreamEventType.ERROR:
                    status = "error"
                    error = ev.error
                    log.warning(
                        "upstream stream error",
                        error=ev.error,
                        code=getattr(ev, "code", None),
                    )
                yield ev
        finally:
            try:
                # Always record + commit, even on partial/error streams.
                await self._write_record(req, route, request_id, usage, status, error)
                await self._commit_quota(identity, usage)
                if self._trace_store is not None and trace_context is not None:
                    try:
                        await self._trace_store.record_model_call(
                            trace_context,
                            started_at=started_at,
                            duration_us=int((time.monotonic() - started_mono) * 1_000_000),
                            ttft_ms=ttft_ms,
                            status="error" if status == "error" else "success",
                            model_alias=route.alias,
                            real_model=route.real_model,
                            provider=route.provider_name,
                            input_tokens=usage.prompt_tokens,
                            output_tokens=usage.completion_tokens,
                            error_code="upstream_error" if error else "",
                        )
                    except Exception as exc:  # noqa: BLE001 - tracing never breaks inference
                        log.warning("conversation trace write failed", error=repr(exc))
            finally:
                await self._registry_source.release_registry(registry)

    async def _embeddings_create_with_retry(self, provider, payload: dict) -> dict:
        for attempt in range(self._policy.max_retries + 1):
            try:
                async with asyncio.timeout(self._policy.timeout_s):
                    return await provider.create_embeddings(payload)
            except TimeoutError as exc:
                if attempt >= self._policy.max_retries:
                    raise UpstreamError(
                        ErrorCode.UNAVAILABLE,
                        "embedding upstream timeout",
                        retryable=True,
                    ) from exc
            except UpstreamError as exc:
                if not exc.retryable or attempt >= self._policy.max_retries:
                    raise
            await asyncio.sleep(self._policy.backoff_base_s * (2**attempt))
        raise RuntimeError("unreachable")

    async def _write_record(self, req, route, request_id, usage, status, error) -> None:
        await self._write_usage_record(
            request_id=request_id,
            user_id=req.user_id,
            session_id=req.session_id,
            route=route,
            usage=usage,
            status=status,
            error=error,
        )

    async def _write_usage_record(
        self, *, request_id, user_id, session_id, route, usage, status, error
    ) -> None:
        cost = route.pricing.cost(usage.prompt_tokens, usage.completion_tokens)
        rec = UsageRecord(
            request_id=request_id,
            user_id=user_id,
            session_id=session_id,
            alias=route.alias,
            real_model=route.real_model,
            provider=route.provider_name,
            prompt_tokens=usage.prompt_tokens,
            completion_tokens=usage.completion_tokens,
            cost_usd=cost,
            status=status,
            error=error[:500],
        )
        try:
            await self._ledger.record(rec)
        except Exception as e:  # never break the user's request on a billing error
            log.warning("ledger write failed", error=repr(e), request_id=request_id)

    async def _commit_quota(self, identity: Identity | None, usage: Usage) -> None:
        """Add the real token total to the caller's quota counters (best-effort)."""
        if self._enforcer is None or identity is None:
            return
        await self._enforcer.commit(identity, usage.total_tokens)

    async def aclose(self) -> None:
        await self._registry_source.aclose()
        await self._ledger.aclose()
        if self._enforcer is not None:
            await self._enforcer.store.aclose()
        if self._trace_store is not None:
            await self._trace_store.aclose()


def _structured_output_instruction(response_format: dict) -> str:
    if response_format.get("type") == "json_schema":
        schema = response_format.get("json_schema") or {}
        return (
            "Return only valid JSON matching this JSON Schema. Do not include markdown fences: "
            + json.dumps(schema.get("schema") or {}, separators=(",", ":"))
        )
    return "Return only one valid JSON object. Do not include markdown fences or commentary."


def _memory_stream_text_delta(event: StreamEvent) -> str:
    """Extract text from normalized or lossless Anthropic stream events."""
    if event.type is StreamEventType.CONTENT_DELTA:
        return event.text
    if event.type is not StreamEventType.PASSTHROUGH:
        return ""
    frame = event.extra.get("frame")
    if not isinstance(frame, dict) or frame.get("type") != "content_block_delta":
        return ""
    delta = frame.get("delta")
    if not isinstance(delta, dict) or delta.get("type") != "text_delta":
        return ""
    text = delta.get("text")
    return text if isinstance(text, str) else ""


def anthropic_reasoning_effort(req: ChatRequest) -> str:
    output_config = req.params.output_config
    if output_config is None or output_config.get("effort") is None:
        return ""
    effort = output_config.get("effort")
    if not isinstance(effort, str):
        raise CocolaError(ErrorCode.INVALID_ARGUMENT, "reasoning effort must be a string")
    return effort.strip()


def validate_reasoning_effort(route: ModelRoute, effort: str) -> None:
    if not effort:
        return
    allowed = {"low", "medium", "high", "xhigh", "max"}
    if effort not in allowed or effort not in route.reasoning_efforts:
        raise CocolaError(
            ErrorCode.INVALID_ARGUMENT,
            f"reasoning effort '{effort}' is not supported by model route '{route.alias}'",
        )


def _validate_embedding_dimensions(payload: dict, expected: int) -> None:
    if expected <= 0:
        raise CocolaError(ErrorCode.INVALID_ARGUMENT, "embedding dimension is not configured")
    data = payload.get("data")
    if not isinstance(data, list) or not data:
        raise UpstreamError(
            ErrorCode.UNAVAILABLE,
            "embedding upstream returned no vectors",
            retryable=False,
        )
    for item in data:
        vector = item.get("embedding") if isinstance(item, dict) else None
        if not isinstance(vector, list) or len(vector) != expected:
            raise UpstreamError(
                ErrorCode.UNAVAILABLE,
                "embedding upstream returned an unexpected vector dimension",
                retryable=False,
            )
