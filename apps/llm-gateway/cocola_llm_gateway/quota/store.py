"""Quota counter storage: read current usage, atomically add tokens.

Storage-agnostic Protocol (mirrors the Ledger design): MemoryQuotaStore for
hermetic tests + single-process dev, RedisQuotaStore for shared, durable,
period-windowed counters in production.

The counter for a scope is keyed by (scope, subject, period). `add` is an atomic
increment that also (re)applies the window TTL so the key disappears after the
period ends — rollover needs no cron. `get` is a cheap read used both for the
pre-call check and the `/v1/quota` ops surface.
"""

from __future__ import annotations

import asyncio
from collections import defaultdict
from typing import Protocol, runtime_checkable


@runtime_checkable
class QuotaStore(Protocol):
    async def get(self, scope: str, subject: str, period: str) -> int:
        """Return tokens used by (scope, subject) in `period`. 0 if absent."""
        ...

    async def add(self, scope: str, subject: str, period: str, tokens: int, ttl_s: int) -> int:
        """Atomically add `tokens` to the counter and return the new total.

        Applies/refreshes a TTL of `ttl_s` so the key self-expires after the
        period rolls over.
        """
        ...

    async def aclose(self) -> None: ...


class MemoryQuotaStore:
    """In-process counters. Task-safe via an asyncio lock."""

    def __init__(self) -> None:
        self._counts: dict[tuple[str, str, str], int] = defaultdict(int)
        self._lock = asyncio.Lock()

    async def get(self, scope: str, subject: str, period: str) -> int:
        async with self._lock:
            return self._counts.get((scope, subject, period), 0)

    async def add(self, scope: str, subject: str, period: str, tokens: int, ttl_s: int) -> int:
        async with self._lock:
            key = (scope, subject, period)
            self._counts[key] += tokens
            return self._counts[key]

    async def aclose(self) -> None:  # pragma: no cover - nothing to release
        return None


_PREFIX = "cocola:quota:"


class RedisQuotaStore:
    """Redis-backed counters: INCRBY + EXPIRE, period embedded in the key.

    Key: cocola:quota:{scope}:{subject}:{period}  STRING (integer token count)
    The increment and TTL refresh run in one MULTI/EXEC so a crash can't leave a
    counted-but-never-expiring key.
    """

    def __init__(self, client) -> None:
        self._r = client

    @classmethod
    def from_url(cls, url: str) -> RedisQuotaStore:
        from redis import asyncio as aioredis

        client = aioredis.from_url(url, encoding="utf-8", decode_responses=True)
        return cls(client)

    @staticmethod
    def _key(scope: str, subject: str, period: str) -> str:
        return f"{_PREFIX}{scope}:{subject}:{period}"

    async def get(self, scope: str, subject: str, period: str) -> int:
        raw = await self._r.get(self._key(scope, subject, period))
        return int(raw) if raw else 0

    async def add(self, scope: str, subject: str, period: str, tokens: int, ttl_s: int) -> int:
        key = self._key(scope, subject, period)
        pipe = self._r.pipeline(transaction=True)
        pipe.incrby(key, tokens)
        pipe.expire(key, ttl_s)
        new_total, _ = await pipe.execute()
        return int(new_total)

    async def aclose(self) -> None:
        await self._r.aclose()
