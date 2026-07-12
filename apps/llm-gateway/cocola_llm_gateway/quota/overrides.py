"""Per-subject quota overrides: ask "does this subject have a custom cap?".

The static env caps (QuotaPolicy: user-daily / tenant-monthly) are the fleet
default. The admin-api lets an operator set a *per-subject* override that
supersedes that default for one user_id or tenant_id (PUT /admin/quotas). This
module is the gateway-side seam that reads those overrides on the quota path, so
the Enforcer can apply a subject-specific limit instead of the env default.

Semantics deliberately match the admin-api QuotaOverride:
  - no override        -> None  (fall back to the static policy cap)
  - override present    -> int   (the per-subject cap; 0 means EXPLICITLY
                                  unlimited for that subject, mirroring the
                                  policy's "limit <= 0 == unlimited")

So the return type is `int | None`: None and 0 are different answers. None means
"I have nothing to say, use the default"; 0 means "this subject is uncapped".

Storage-agnostic Protocol (mirrors QuotaStore / RevocationStore):
MemoryOverrideStore for hermetic tests, RedisOverrideStore
for a shared table the admin-api writes and every gateway replica reads.
TTLCachedOverrides wraps either so the hot path is an in-process dict lookup
most of the time, with a few-seconds staleness bound on a fresh override.

Admin API writes and LLM Gateway reads the same Redis hash.
"""

from __future__ import annotations

import asyncio
import time
from typing import Protocol, runtime_checkable


@runtime_checkable
class OverrideStore(Protocol):
    async def get(self, scope: str, subject: str) -> int | None:
        """Return the per-subject cap, or None if there is no override."""
        ...

    async def set(self, scope: str, subject: str, limit: int) -> None:
        """Upsert a per-subject cap (limit 0 == explicitly unlimited)."""
        ...

    async def delete(self, scope: str, subject: str) -> None:
        """Remove an override (idempotent); the subject reverts to the default."""
        ...

    async def aclose(self) -> None: ...


def _key(scope: str, subject: str) -> str:
    return scope + "/" + subject


class MemoryOverrideStore:
    """In-process override table. Task-safe via an asyncio lock."""

    def __init__(self, overrides: dict[tuple[str, str], int] | None = None) -> None:
        self._d: dict[str, int] = {}
        if overrides:
            for (scope, subject), limit in overrides.items():
                self._d[_key(scope, subject)] = limit
        self._lock = asyncio.Lock()

    async def get(self, scope: str, subject: str) -> int | None:
        if not subject:
            return None
        async with self._lock:
            return self._d.get(_key(scope, subject))

    async def set(self, scope: str, subject: str, limit: int) -> None:
        if not subject:
            return
        async with self._lock:
            self._d[_key(scope, subject)] = int(limit)

    async def delete(self, scope: str, subject: str) -> None:
        if not subject:
            return
        async with self._lock:
            self._d.pop(_key(scope, subject), None)

    async def aclose(self) -> None:  # pragma: no cover - nothing to release
        return None


_PREFIX = "cocola:quota:override"


class RedisOverrideStore:
    """Redis-backed override table as a single HASH of scope/subject -> limit.

    Key: cocola:quota:override  HASH field "scope/subject" -> str(limit).
    get is HGET; set is HSET; delete is HDEL. The admin-api writes overrides on
    PUT /admin/quotas; every gateway replica reads the same hash, so an override
    is visible fleet-wide without a redeploy.
    """

    def __init__(self, client, key: str = _PREFIX) -> None:
        self._r = client
        self._key = key

    @classmethod
    def from_url(cls, url: str) -> RedisOverrideStore:
        from redis import asyncio as aioredis

        client = aioredis.from_url(url, encoding="utf-8", decode_responses=True)
        return cls(client)

    async def get(self, scope: str, subject: str) -> int | None:
        if not subject:
            return None
        raw = await self._r.hget(self._key, _key(scope, subject))
        if raw is None:
            return None
        try:
            return int(raw)
        except (TypeError, ValueError):
            return None

    async def set(self, scope: str, subject: str, limit: int) -> None:
        if not subject:
            return
        await self._r.hset(self._key, _key(scope, subject), str(int(limit)))

    async def delete(self, scope: str, subject: str) -> None:
        if not subject:
            return
        await self._r.hdel(self._key, _key(scope, subject))

    async def aclose(self) -> None:
        await self._r.aclose()


class TTLCachedOverrides:
    """Wrap an OverrideStore with a tiny in-process TTL cache.

    Keeps the quota path fast: most get() calls hit the local cache instead of
    the backend. The TTL bounds staleness (a fresh override takes effect within
    `ttl_s`). Both "has an override" and "no override" results are cached, so a
    subject with no custom cap does not hit the backend on every request.
    """

    _MISS = object()

    def __init__(self, inner: OverrideStore, ttl_s: float = 5.0) -> None:
        self._inner = inner
        self._ttl = max(0.0, ttl_s)
        self._cache: dict[str, tuple[int | None, float]] = {}
        self._lock = asyncio.Lock()

    async def get(self, scope: str, subject: str, *, now: float | None = None) -> int | None:
        if not subject:
            return None
        k = _key(scope, subject)
        t = time.monotonic() if now is None else now
        async with self._lock:
            hit = self._cache.get(k)
            if hit is not None and hit[1] > t:
                return hit[0]
        val = await self._inner.get(scope, subject)
        async with self._lock:
            self._cache[k] = (val, t + self._ttl)
        return val

    async def set(self, scope: str, subject: str, limit: int) -> None:
        await self._inner.set(scope, subject, limit)
        async with self._lock:
            self._cache.pop(_key(scope, subject), None)

    async def delete(self, scope: str, subject: str) -> None:
        await self._inner.delete(scope, subject)
        async with self._lock:
            self._cache.pop(_key(scope, subject), None)

    async def aclose(self) -> None:
        await self._inner.aclose()
