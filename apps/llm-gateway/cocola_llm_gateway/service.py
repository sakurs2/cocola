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

import uuid
from collections.abc import AsyncIterator

from cocola_common import get_logger
from cocola_llm_gateway.auth.jwt import Identity
from cocola_llm_gateway.billing.ledger import Ledger, UsageRecord
from cocola_llm_gateway.middleware import RateLimiter, ResiliencePolicy, ResilientStreamer
from cocola_llm_gateway.quota import Enforcer, QuotaStatus
from cocola_llm_gateway.registry import Registry
from cocola_llm_gateway.types import ChatRequest, StreamEvent, StreamEventType, Usage

log = get_logger("cocola.llm-gateway.service")


class GatewayService:
    def __init__(
        self,
        registry: Registry,
        ledger: Ledger,
        policy: ResiliencePolicy | None = None,
        enforcer: Enforcer | None = None,
    ):
        self._registry = registry
        self._ledger = ledger
        self._policy = policy or ResiliencePolicy()
        self._enforcer = enforcer
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

    def resolve_model(self, requested_alias: str | None) -> str:
        """Expose the resolved real model id (used by the front-end to stamp the
        outgoing message's `model` field). Raises CocolaError(NOT_FOUND)."""
        route, _ = self._registry.resolve(requested_alias)
        return route.real_model

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
    ) -> AsyncIterator[StreamEvent]:
        """Resolve, stream with resilience, meter, and commit quota.

        `req.model` is expected to already be the resolved real model (the codec
        sets it). `requested_alias` is the caller-facing alias used for routing &
        billing attribution. `identity` drives the post-call quota commit.
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
                    log.warning(
                        "upstream stream error",
                        error=ev.error,
                        code=getattr(ev, "code", None),
                    )
                yield ev
        finally:
            # Always record + commit, even on partial/error streams.
            await self._write_record(req, route, request_id, usage, status, error)
            await self._commit_quota(identity, usage)

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

    async def _commit_quota(self, identity: Identity | None, usage: Usage) -> None:
        """Add the real token total to the caller's quota counters (best-effort)."""
        if self._enforcer is None or identity is None:
            return
        await self._enforcer.commit(identity, usage.total_tokens)

    async def aclose(self) -> None:
        await self._registry.aclose()
        await self._ledger.aclose()
        if self._enforcer is not None:
            await self._enforcer.store.aclose()
