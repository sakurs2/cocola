"""Token revocation denylist: ask "has this token id been revoked early?".

Tokens are stateless signed JWTs verified offline, so a token stays valid until
its `exp` even after an admin revokes it. To close that gap the admin-api keeps
a denylist keyed by the token id (the `jti` claim == TokenRecord.ID), and the
gateway consults a RevocationStore on the hot path: a verified-but-revoked token
is rejected with 401 before any work happens.

Storage-agnostic Protocol (mirrors QuotaStore): MemoryRevocationStore for
hermetic tests and RedisRevocationStore for the shared production denylist.
TTLCachedRevocation wraps either so
the per-request check is an in-process set membership most of the time, with a
short window before a fresh revocation takes effect (seconds, never past `exp`).

Admin API writes and LLM Gateway reads the same Redis keys.
"""

from __future__ import annotations

import asyncio
import time
from typing import Protocol, runtime_checkable


@runtime_checkable
class RevocationStore(Protocol):
    async def is_revoked(self, token_id: str) -> bool:
        """Return True if the token id is on the denylist."""
        ...

    async def revoke(self, token_id: str) -> None:
        """Add a token id to the denylist (idempotent)."""
        ...

    async def aclose(self) -> None: ...


class MemoryRevocationStore:
    """In-process denylist. Task-safe via an asyncio lock."""

    def __init__(self, revoked: set[str] | None = None) -> None:
        self._revoked: set[str] = set(revoked or ())
        self._lock = asyncio.Lock()

    async def is_revoked(self, token_id: str) -> bool:
        if not token_id:
            return False
        async with self._lock:
            return token_id in self._revoked

    async def revoke(self, token_id: str) -> None:
        if not token_id:
            return
        async with self._lock:
            self._revoked.add(token_id)

    async def aclose(self) -> None:  # pragma: no cover - nothing to release
        return None


_PREFIX = "cocola:revoked"


class RedisRevocationStore:
    """Redis-backed denylist as a single SET of revoked token ids.

    Key: cocola:revoked  SET of token ids (jti).
    is_revoked is a cheap SISMEMBER; revoke is SADD. The admin-api adds ids on
    revocation; every gateway replica reads the same set, so a revoke is visible
    fleet-wide without a redeploy.
    """

    def __init__(self, client, key: str = _PREFIX) -> None:
        self._r = client
        self._key = key

    @classmethod
    def from_url(cls, url: str) -> RedisRevocationStore:
        from redis import asyncio as aioredis

        client = aioredis.from_url(url, encoding="utf-8", decode_responses=True)
        return cls(client)

    async def is_revoked(self, token_id: str) -> bool:
        if not token_id:
            return False
        return bool(await self._r.sismember(self._key, token_id))

    async def revoke(self, token_id: str) -> None:
        if not token_id:
            return
        await self._r.sadd(self._key, token_id)

    async def aclose(self) -> None:
        await self._r.aclose()


class TTLCachedRevocation:
    """Wrap a RevocationStore with a tiny in-process TTL cache.

    Keeps the hot path fast: most is_revoked calls hit the local cache instead of
    the backend. The TTL bounds staleness (a fresh revoke takes effect within
    `ttl_s`), which is acceptable because the token still expires at `exp` and the
    window is seconds. Both positive and negative results are cached.
    """

    def __init__(self, inner: RevocationStore, ttl_s: float = 5.0) -> None:
        self._inner = inner
        self._ttl = max(0.0, ttl_s)
        self._cache: dict[str, tuple[bool, float]] = {}
        self._lock = asyncio.Lock()

    async def is_revoked(self, token_id: str, *, now: float | None = None) -> bool:
        if not token_id:
            return False
        t = time.monotonic() if now is None else now
        async with self._lock:
            hit = self._cache.get(token_id)
            if hit is not None and hit[1] > t:
                return hit[0]
        revoked = await self._inner.is_revoked(token_id)
        async with self._lock:
            self._cache[token_id] = (revoked, t + self._ttl)
        return revoked

    async def revoke(self, token_id: str) -> None:
        await self._inner.revoke(token_id)
        async with self._lock:
            self._cache.pop(token_id, None)

    async def aclose(self) -> None:
        await self._inner.aclose()
