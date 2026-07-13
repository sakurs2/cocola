"""Gateway service layer: resolve -> (quota gate) -> stream -> meter + commit.

This is the single orchestration seam the HTTP layer calls. It is deliberately
the ONLY place that knows about all collaborators (registry, resilient streaming,
ledger, quota) at once; each collaborator stays unaware of the others.

Flow for one request:
  0. check_quota(identity)               -> raise QuotaExceeded (HTTP 429) early
  1. registry.resolve(alias)             -> (route, provider)
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
from dataclasses import dataclass
from typing import Protocol

from cocola_common import ErrorCode, get_logger

from cocola_llm_gateway.auth.jwt import Identity
from cocola_llm_gateway.billing.ledger import Ledger, UsageRecord
from cocola_llm_gateway.conversation_trace import ConversationTraceStore, TraceContext, utc_now
from cocola_llm_gateway.middleware import RateLimiter, ResiliencePolicy, ResilientStreamer
from cocola_llm_gateway.quota import Enforcer, QuotaStatus
from cocola_llm_gateway.registry import Registry
from cocola_llm_gateway.types import ChatRequest, StreamEvent, StreamEventType, Usage
from cocola_llm_gateway.upstream.errors import UpstreamError

log = get_logger("cocola.llm-gateway.service")


@dataclass
class ResponsesUsage:
    input_tokens: int = 0
    cached_input_tokens: int = 0
    output_tokens: int = 0
    reasoning_tokens: int = 0

    def quota_usage(self) -> Usage:
        # Cached and reasoning tokens are subsets of the Responses API input
        # and output totals; do not double-count them for quota or billing.
        return Usage(prompt_tokens=self.input_tokens, completion_tokens=self.output_tokens)


def responses_usage(payload: dict) -> ResponsesUsage:
    response = payload.get("response") if isinstance(payload.get("response"), dict) else payload
    raw = response.get("usage") if isinstance(response, dict) else None
    if not isinstance(raw, dict):
        return ResponsesUsage()
    input_details = raw.get("input_tokens_details") or {}
    output_details = raw.get("output_tokens_details") or {}
    return ResponsesUsage(
        input_tokens=int(raw.get("input_tokens") or 0),
        cached_input_tokens=int(input_details.get("cached_tokens") or 0),
        output_tokens=int(raw.get("output_tokens") or 0),
        reasoning_tokens=int(output_details.get("reasoning_tokens") or 0),
    )


class ResponsesSSEMeter:
    def __init__(self) -> None:
        self._buffer = b""
        self.usage = ResponsesUsage()
        self.completed = False
        self.terminal_type = ""
        self.response_id = ""

    def feed(self, chunk: bytes) -> None:
        self._buffer = (self._buffer + chunk).replace(b"\r\n", b"\n")
        while b"\n\n" in self._buffer:
            event_bytes, self._buffer = self._buffer.split(b"\n\n", 1)
            event = event_bytes.decode("utf-8", errors="replace")
            data = "\n".join(
                line[5:].lstrip() for line in event.splitlines() if line.startswith("data:")
            )
            if not data or data == "[DONE]":
                continue
            try:
                payload = json.loads(data)
            except json.JSONDecodeError:
                continue
            event_type = str(payload.get("type") or "")
            if event_type in {
                "response.completed",
                "response.incomplete",
                "response.failed",
            }:
                self.terminal_type = event_type
                self.completed = event_type == "response.completed"
                self.usage = responses_usage(payload)
                response = payload.get("response")
                if isinstance(response, dict):
                    self.response_id = str(response.get("id") or "")


class RegistrySource(Protocol):
    async def acquire_registry(self) -> Registry: ...

    async def release_registry(self, registry: Registry) -> None: ...

    async def aclose(self) -> None: ...


class StaticRegistrySource:
    def __init__(self, registry: Registry):
        self._registry = registry

    async def acquire_registry(self) -> Registry:
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

    async def resolve_responses_model(self, requested_alias: str | None) -> str:
        registry = await self._registry_source.acquire_registry()
        try:
            self._registry = registry
            route, _ = registry.resolve_responses(requested_alias)
            return route.real_model
        finally:
            await self._registry_source.release_registry(registry)

    async def registry_status(self) -> tuple[str, list[str]]:
        """Return a stable health snapshot without exposing a leased registry."""
        registry = await self._registry_source.acquire_registry()
        try:
            return registry.default_alias, registry.aliases()
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

    async def responses_create(
        self,
        payload: dict,
        *,
        requested_alias: str,
        identity: Identity,
        session_id: str,
        trace_context: TraceContext | None = None,
    ) -> dict:
        await self._check_responses_rate_limit(identity)
        registry = await self._registry_source.acquire_registry()
        route = None
        request_id = f"req_{uuid.uuid4().hex[:16]}"
        usage = ResponsesUsage()
        status = "error"
        error = "upstream Responses request failed"
        started_at = utc_now()
        started_mono = time.monotonic()
        try:
            self._registry = registry
            route, provider = registry.resolve_responses(requested_alias)
            upstream_payload = {**payload, "model": route.real_model, "stream": False}
            response = await self._responses_create_with_retry(provider, upstream_payload)
            usage = responses_usage(response)
            request_id = str(response.get("id") or request_id)
            status, error = "ok", ""
            return response
        finally:
            try:
                if route is not None:
                    quota_usage = usage.quota_usage()
                    await self._write_usage_record(
                        request_id=request_id,
                        user_id=identity.user_id,
                        session_id=session_id,
                        route=route,
                        usage=quota_usage,
                        status=status,
                        error=error,
                    )
                    await self._commit_quota(identity, quota_usage)
                    await self._record_responses_trace(
                        trace_context,
                        route=route,
                        started_at=started_at,
                        started_mono=started_mono,
                        ttft_ms=int((time.monotonic() - started_mono) * 1000),
                        status=status,
                        usage=usage,
                        error_code="responses_upstream_error" if error else "",
                    )
            finally:
                await self._registry_source.release_registry(registry)

    async def responses_stream(
        self,
        payload: dict,
        *,
        requested_alias: str,
        identity: Identity,
        session_id: str,
        trace_context: TraceContext | None = None,
    ) -> AsyncIterator[bytes]:
        await self._check_responses_rate_limit(identity)
        registry = await self._registry_source.acquire_registry()
        route = None
        status = "error"
        error = "responses stream incomplete"
        meter = ResponsesSSEMeter()
        started_at = utc_now()
        started_mono = time.monotonic()
        ttft_ms = 0
        saw_first_chunk = False
        try:
            self._registry = registry
            route, provider = registry.resolve_responses(requested_alias)
            upstream_payload = {**payload, "model": route.real_model, "stream": True}
            async for chunk in self._responses_stream_with_retry(provider, upstream_payload):
                if not saw_first_chunk:
                    saw_first_chunk = True
                    ttft_ms = int((time.monotonic() - started_mono) * 1000)
                meter.feed(chunk)
                yield chunk
            if meter.completed:
                status, error = "ok", ""
            elif meter.terminal_type:
                error = f"Responses stream ended with {meter.terminal_type}"
        except Exception:
            error = "upstream Responses stream failed"
            raise
        finally:
            try:
                if route is not None:
                    quota_usage = meter.usage.quota_usage()
                    await self._write_usage_record(
                        request_id=meter.response_id or f"req_{uuid.uuid4().hex[:16]}",
                        user_id=identity.user_id,
                        session_id=session_id,
                        route=route,
                        usage=quota_usage,
                        status=status,
                        error=error,
                    )
                    await self._commit_quota(identity, quota_usage)
                    await self._record_responses_trace(
                        trace_context,
                        route=route,
                        started_at=started_at,
                        started_mono=started_mono,
                        ttft_ms=ttft_ms,
                        status=status,
                        usage=meter.usage,
                        error_code=meter.terminal_type
                        or ("responses_upstream_error" if error else ""),
                    )
            finally:
                await self._registry_source.release_registry(registry)

    async def _check_responses_rate_limit(self, identity: Identity) -> None:
        key = identity.user_id or identity.tenant_id or "anonymous"
        if await self._limiter.allow(key):
            return
        raise UpstreamError(
            ErrorCode.UNAVAILABLE,
            "rate limit exceeded",
            status=429,
            retryable=False,
        )

    async def _record_responses_trace(
        self,
        trace_context: TraceContext | None,
        *,
        route,
        started_at,
        started_mono: float,
        ttft_ms: int,
        status: str,
        usage: ResponsesUsage,
        error_code: str,
    ) -> None:
        if self._trace_store is None or trace_context is None:
            return
        try:
            await self._trace_store.record_model_call(
                trace_context,
                started_at=started_at,
                duration_us=int((time.monotonic() - started_mono) * 1_000_000),
                ttft_ms=ttft_ms,
                status="success" if status == "ok" else "error",
                model_alias=route.alias,
                real_model=route.real_model,
                provider=route.provider_name,
                input_tokens=usage.input_tokens,
                output_tokens=usage.output_tokens,
                error_code=error_code,
            )
        except Exception as exc:  # noqa: BLE001 - tracing never breaks inference
            log.warning("conversation trace write failed", error=repr(exc))

    async def _responses_create_with_retry(self, provider, payload: dict) -> dict:
        for attempt in range(self._policy.max_retries + 1):
            try:
                async with asyncio.timeout(self._policy.timeout_s):
                    return await provider.create_response(payload)
            except TimeoutError as exc:
                if attempt >= self._policy.max_retries:
                    raise UpstreamError(
                        ErrorCode.UNAVAILABLE,
                        "upstream timeout",
                        retryable=True,
                    ) from exc
                await asyncio.sleep(self._policy.backoff_base_s * (2**attempt))
            except UpstreamError as exc:
                if not exc.retryable or attempt >= self._policy.max_retries:
                    raise
                await asyncio.sleep(self._policy.backoff_base_s * (2**attempt))
        raise RuntimeError("unreachable")

    async def _responses_stream_with_retry(self, provider, payload: dict) -> AsyncIterator[bytes]:
        for attempt in range(self._policy.max_retries + 1):
            stream = provider.stream_response(payload)
            produced = False
            try:
                async with asyncio.timeout(self._policy.timeout_s):
                    first = await anext(stream)
                    produced = True
                    yield first
                    async for chunk in stream:
                        yield chunk
                return
            except StopAsyncIteration:
                if attempt >= self._policy.max_retries:
                    raise UpstreamError(
                        ErrorCode.UNAVAILABLE,
                        "upstream returned an empty stream",
                        retryable=True,
                    ) from None
                await asyncio.sleep(self._policy.backoff_base_s * (2**attempt))
            except TimeoutError as exc:
                if produced or attempt >= self._policy.max_retries:
                    raise UpstreamError(
                        ErrorCode.UNAVAILABLE,
                        "upstream timeout",
                        retryable=True,
                    ) from exc
                await asyncio.sleep(self._policy.backoff_base_s * (2**attempt))
            except UpstreamError as exc:
                if produced or attempt >= self._policy.max_retries or not exc.retryable:
                    raise
                await asyncio.sleep(self._policy.backoff_base_s * (2**attempt))
            finally:
                close = getattr(stream, "aclose", None)
                if close is not None:
                    await close()

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
