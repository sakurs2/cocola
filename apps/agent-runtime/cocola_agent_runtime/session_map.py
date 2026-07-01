"""Session -> Claude on-disk session index (M7, ADR-0008).

Route A's `--resume` continuation needs the *claude_session_id* that the
in-sandbox shim emitted on a previous turn. Two facts shape this module:

  1. The SUFFICIENT condition for a real resume is the on-disk Claude session
     file (``~/.claude/projects/<proj>/<uuid>.jsonl``) living on the agent's
     persistent volume. Without that file, no id can revive the conversation.
  2. This table is therefore a pure *INDEX*: cocola's session_id -> the
     claude_session_id to pass as ``resume``. It survives an agent-runtime
     restart so a follow-up turn still knows which on-disk session to reopen;
     it is NOT itself the conversation state.

The store is a tiny protocol so the provider does not care where the index
lives. Two implementations:

``MemorySessionMap``
    In-process dict. Zero-dependency default so a dev boot needs no Postgres
    (matches the COCOLA_PG_DSN gating used in llm-gateway / admin-api). Resume
    works within a single process lifetime only.

``PostgresSessionMap``
    Durable index in the ``session_map`` table (schema owned by
    ``db/migrations``; this module only reads/writes). ``put`` is an idempotent
    ``INSERT ... ON CONFLICT DO UPDATE`` keyed by ``session_id``. Survives a
    restart, so paired with the persistent ``~/.claude`` volume a follow-up
    turn resumes the real conversation.
"""

from __future__ import annotations

import asyncio
from typing import Protocol, runtime_checkable

from cocola_common import get_logger
from psycopg_pool import AsyncConnectionPool

log = get_logger("cocola.agent-runtime.session-map")


@runtime_checkable
class SessionMap(Protocol):
    """session_id -> claude_session_id index for --resume continuation."""

    async def get(self, session_id: str) -> str | None:
        """Return the claude_session_id to resume, or None if unknown."""

    async def put(
        self, session_id: str, claude_session_id: str, *, user_id: str = "", sandbox_id: str = ""
    ) -> None:
        """Record the latest claude_session_id for a cocola session."""

    async def delete(self, session_id: str) -> None:
        """Forget a session's binding (e.g. a dangling/stale resume id)."""

    async def aclose(self) -> None:
        """Release any backing resources."""


class MemorySessionMap:
    """In-process index; resume works within one process lifetime only."""

    def __init__(self) -> None:
        self._d: dict[str, str] = {}

    async def get(self, session_id: str) -> str | None:
        return self._d.get(session_id)

    async def put(
        self, session_id: str, claude_session_id: str, *, user_id: str = "", sandbox_id: str = ""
    ) -> None:
        if claude_session_id:
            self._d[session_id] = claude_session_id

    async def delete(self, session_id: str) -> None:
        self._d.pop(session_id, None)

    async def aclose(self) -> None:
        return None


_GET = "SELECT claude_session_id FROM session_map WHERE session_id = %s"

_PUT = """
INSERT INTO session_map (session_id, claude_session_id, user_id, sandbox_id, updated_at)
VALUES (%s, %s, %s, %s, now())
ON CONFLICT (session_id)
DO UPDATE SET claude_session_id = EXCLUDED.claude_session_id,
             user_id = EXCLUDED.user_id,
             sandbox_id = EXCLUDED.sandbox_id,
             updated_at = now()
"""

_DELETE = "DELETE FROM session_map WHERE session_id = %s"


class PostgresSessionMap:
    """Durable session_id -> claude_session_id index over a connection pool.

    The pool opens lazily on first use so the synchronous composition root can
    construct the backend without a running event loop.
    """

    def __init__(self, dsn: str):
        self._pool = AsyncConnectionPool(dsn, open=False, kwargs={"autocommit": True})
        self._lock = asyncio.Lock()
        self._opened = False

    async def _ready(self) -> None:
        if self._opened:
            return
        async with self._lock:
            if not self._opened:
                await self._pool.open()
                self._opened = True

    async def get(self, session_id: str) -> str | None:
        await self._ready()
        async with self._pool.connection() as conn:
            cur = await conn.execute(_GET, (session_id,))
            row = await cur.fetchone()
        if not row:
            return None
        cid = row[0]
        return cid or None

    async def put(
        self, session_id: str, claude_session_id: str, *, user_id: str = "", sandbox_id: str = ""
    ) -> None:
        if not claude_session_id:
            return
        await self._ready()
        async with self._pool.connection() as conn:
            await conn.execute(_PUT, (session_id, claude_session_id, user_id, sandbox_id))

    async def delete(self, session_id: str) -> None:
        await self._ready()
        async with self._pool.connection() as conn:
            await conn.execute(_DELETE, (session_id,))

    async def aclose(self) -> None:
        if self._opened:
            await self._pool.close()
            self._opened = False
