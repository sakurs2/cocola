"""Gateway service layer: resolve -> stream -> meter.

This is the single orchestration seam the HTTP layer calls. It is deliberately
the ONLY place that knows about all three collaborators (registry, resilient
streaming, ledger) at once; each collaborator stays unaware of the others.

Flow for one request:
  1. registry.resolve(alias)            -> (route, provider)
  2. ResilientStreamer(provider).stream -> normalized StreamEvent stream
  3. pass events through to the caller UNCHANGED, accumulating Usage
  4. at stream end, compute cost from route.pricing and write ONE UsageRecord

The metering is a *hook around* the stream, not logic inside any provider —
this keeps billing uniform across vendors and is why business logic lives in the
service/hook, not the Provider (a standing project rule).

Billing must never break the user's stream: if the ledger write fails we log and
swallow. Records are written even on error/partial streams (status='error') so
usage is captured for whatever the upstream already produced.
"""
from __future__ import annotations

import uuid
from collections.abc import AsyncIterator

from cocola_common import get_logger
from cocola_llm_gateway.billing.ledger import Ledger, UsageRecord
from cocola_llm_gateway.middleware import RateLimiter, ResiliencePolicy, ResilientStreamer
from cocola_llm_gateway.registry import Registry
from cocola_llm_gateway.types import ChatRequest, StreamEvent, StreamEventType, Usage

log = get_logger("cocola.llm-gateway.service")


class GatewayService:
    def __init__(
        self,
        registry: Registry,
        ledger: Ledger,
        policy: ResiliencePolicy | None = None,
    ):
        self._registry = registry
        self._ledger = ledger
        self._policy = policy or ResiliencePolicy()
        # One shared limiter so per-tenant buckets persist across requests.
        self._limiter = RateLimiter(self._policy.rate_limit_rps, self._policy.rate_burst)

    @property
    def registry(self) -> Registry:
        return self._registry

    @property
    def ledger(self) -> Ledger:
        return self._ledger

    def resolve_model(self, requested_alias: str | None) -> str:
        """Expose the resolved real model id (used by the front-end to stamp the
        outgoing message's `model` field). Raises CocolaError(NOT_FOUND)."""
        route, _ = self._registry.resolve(requested_alias)
        return route.real_model

    async def chat_stream(
        self, req: ChatRequest, *, requested_alias: str | None = None
    ) -> AsyncIterator[StreamEvent]:
        """Resolve, stream with resilience, and meter. Yields normalized events.

        `req.model` is expected to already be the resolved real model (the codec
        sets it). `requested_alias` is the caller-facing alias used for routing &
        billing attribution; defaults to req.metadata['requested_model'].
        """
        alias = requested_alias or req.metadata.get("requested_model") or None
        route, provider = self._registry.resolve(alias)

        request_id = req.metadata.get("request_id") or f"req_{uuid.uuid4().hex[:16]}"
        streamer = ResilientStreamer(provider, self._policy, self._limiter)

        usage = Usage()
        status = "ok"
        error = ""

        try:
            async for ev in streamer.chat_stream(req):
                if ev.type is StreamEventType.MESSAGE_START and ev.usage is not None:
                    usage.merge(ev.usage)
                elif ev.type is StreamEventType.MESSAGE_DELTA and ev.usage is not None:
                    usage.merge(ev.usage)
                elif ev.type is StreamEventType.ERROR:
                    status = "error"
                    error = ev.error
                yield ev
        finally:
            # Always record, even on partial/error streams.
            await self._write_record(req, route, request_id, usage, status, error)

    async def _write_record(self, req, route, request_id, usage, status, error) -> None:
        cost = route.pricing.cost(usage.prompt_tokens, usage.completion_tokens)
        rec = UsageRecord(
            request_id=request_id,
            user_id=req.user_id,
            session_id=req.session_id,
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

    async def aclose(self) -> None:
        await self._registry.aclose()
        await self._ledger.aclose()
