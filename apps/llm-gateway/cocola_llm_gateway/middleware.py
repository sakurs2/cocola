"""Cross-cutting resilience that wraps any UpstreamProvider stream.

Providers stay dumb (one vendor call); everything policy-related lives here so it
applies uniformly to all vendors and stays testable with the Fake provider.

Responsibilities:
  - Rate limiting: per-(user|session) token bucket, applied before the call.
  - Retry: ONLY before the first content byte. Once we've streamed any text to
    the client we cannot safely replay, so a mid-stream failure is surfaced as a
    terminal ERROR event rather than retried. This is the critical correctness
    rule for streaming retries.
  - Timeout: a wall-clock budget for the whole stream; exceeding it emits a
    terminal ERROR.
  - Error normalization: any exception becomes a single ERROR StreamEvent so the
    SSE layer can always close cleanly.

Composition: `stream = ResilientStreamer(provider, policy).chat_stream(req)`.
The result is itself an async iterator of StreamEvent, so it is a drop-in for a
raw provider from the server's perspective.
"""
from __future__ import annotations

import asyncio
import time
from collections.abc import AsyncIterator
from dataclasses import dataclass

from cocola_common import get_logger
from cocola_llm_gateway.types import ChatRequest, StreamEvent, StreamEventType
from cocola_llm_gateway.upstream.base import UpstreamProvider
from cocola_llm_gateway.upstream.errors import UpstreamError

log = get_logger("cocola.llm-gateway.middleware")


@dataclass
class ResiliencePolicy:
    timeout_s: float = 90.0
    max_retries: int = 2          # attempts beyond the first, pre-first-byte only
    backoff_base_s: float = 0.2   # exponential: base * 2**attempt
    rate_limit_rps: float = 0.0   # 0 disables; otherwise tokens/sec per key
    rate_burst: int = 0           # bucket capacity; defaults to ceil(rps) if 0


class _TokenBucket:
    """Simple monotonic token bucket. Not perfectly fair but adequate for
    coarse per-tenant throttling; precise distributed limiting is an M7 concern.
    """

    def __init__(self, rate: float, burst: int):
        self.rate = rate
        self.capacity = float(max(1, burst))
        self.tokens = self.capacity
        self.updated = time.monotonic()
        self._lock = asyncio.Lock()

    async def allow(self) -> bool:
        async with self._lock:
            now = time.monotonic()
            self.tokens = min(self.capacity, self.tokens + (now - self.updated) * self.rate)
            self.updated = now
            if self.tokens >= 1.0:
                self.tokens -= 1.0
                return True
            return False


class RateLimiter:
    def __init__(self, rate: float, burst: int):
        self._rate = rate
        self._burst = burst if burst > 0 else max(1, int(rate + 0.999))
        self._buckets: dict[str, _TokenBucket] = {}
        self._lock = asyncio.Lock()

    async def allow(self, key: str) -> bool:
        if self._rate <= 0:
            return True
        async with self._lock:
            bucket = self._buckets.get(key)
            if bucket is None:
                bucket = _TokenBucket(self._rate, self._burst)
                self._buckets[key] = bucket
        return await bucket.allow()


def _rate_key(req: ChatRequest) -> str:
    return req.user_id or req.session_id or "anonymous"


class ResilientStreamer:
    def __init__(self, provider: UpstreamProvider, policy: ResiliencePolicy,
                 limiter: RateLimiter | None = None):
        self._provider = provider
        self._policy = policy
        self._limiter = limiter or RateLimiter(policy.rate_limit_rps, policy.rate_burst)

    async def chat_stream(self, req: ChatRequest) -> AsyncIterator[StreamEvent]:
        # 1) Rate limit (before any upstream work).
        if not await self._limiter.allow(_rate_key(req)):
            yield StreamEvent(
                StreamEventType.ERROR,
                error="rate limit exceeded; retry later",
                code="rate_limited",
            )
            return

        # 2) Attempt with pre-first-byte retry.
        attempt = 0
        while True:
            produced_content = False
            try:
                async for ev in self._run_once(req):
                    if ev.type in (StreamEventType.CONTENT_DELTA, StreamEventType.MESSAGE_START):
                        produced_content = True
                    if ev.type is StreamEventType.ERROR:
                        # Retry only if nothing has been emitted yet and budget remains.
                        if not produced_content and attempt < self._policy.max_retries:
                            raise _RetrySignal(ev.error)
                        yield ev
                        return
                    yield ev
                return  # clean completion
            except _RetrySignal as rs:
                attempt += 1
                delay = self._policy.backoff_base_s * (2 ** (attempt - 1))
                log.info("retrying upstream", attempt=attempt, delay_s=delay, reason=str(rs))
                await asyncio.sleep(delay)
                continue
            except asyncio.TimeoutError:
                yield StreamEvent(StreamEventType.ERROR,
                                  error=f"gateway timeout after {self._policy.timeout_s}s",
                                  code="timeout")
                return
            except UpstreamError as e:
                if not produced_content and e.retryable and attempt < self._policy.max_retries:
                    attempt += 1
                    await asyncio.sleep(self._policy.backoff_base_s * (2 ** (attempt - 1)))
                    continue
                yield StreamEvent(StreamEventType.ERROR, error=str(e), code=e.code.value)
                return
            except Exception as e:  # normalize anything else
                log.warning("unexpected upstream error", error=repr(e))
                yield StreamEvent(StreamEventType.ERROR, error=f"internal error: {e}", code="internal")
                return

    async def _run_once(self, req: ChatRequest) -> AsyncIterator[StreamEvent]:
        """One provider attempt under a wall-clock deadline.

        We enforce the deadline per-event using asyncio.wait_for around the
        iterator's __anext__, so a stalled stream is bounded without buffering.
        """
        deadline = time.monotonic() + self._policy.timeout_s
        agen = self._provider.chat_stream(req).__aiter__()
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise asyncio.TimeoutError()
            try:
                ev = await asyncio.wait_for(agen.__anext__(), timeout=remaining)
            except StopAsyncIteration:
                return
            yield ev


class _RetrySignal(Exception):
    pass
