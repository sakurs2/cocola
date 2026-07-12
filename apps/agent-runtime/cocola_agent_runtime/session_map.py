"""Session -> Claude on-disk session index (M7, ADR-0008).

Route A's `--resume` continuation needs the *claude_session_id* that the
in-sandbox shim emitted on a previous turn. Two facts shape this module:

  1. The SUFFICIENT condition for a real resume is the on-disk Claude session
     file (``~/.claude/projects/<proj>/<uuid>.jsonl``) in the active sandbox.
     When a sandbox is replaced, that file is restored from the latest MinIO
     checkpoint; no session persistent volume is assumed.
  2. This table is therefore a pure *INDEX*: cocola's session_id -> the
     claude_session_id to pass as ``resume``. It survives an agent-runtime
     restart so a follow-up turn still knows which on-disk session to reopen;
     it is NOT itself the conversation state.

The store is a tiny protocol so the provider does not care where the index
lives. Two implementations:

``MemorySessionMap``
    In-process test implementation. Production always uses Postgres.

``PostgresSessionMap``
    Durable index in the ``session_map`` table (schema owned by
    ``db/migrations``; this module only reads/writes). ``put`` is an idempotent
    ``INSERT ... ON CONFLICT DO UPDATE`` keyed by ``session_id``. Survives a
    restart, so paired with checkpoint restore a follow-up turn resumes the real
    conversation even after the previous sandbox was destroyed.
"""

from __future__ import annotations

import asyncio
from dataclasses import dataclass
from typing import Protocol, runtime_checkable

from cocola_common import get_logger
from psycopg_pool import AsyncConnectionPool

log = get_logger("cocola.agent-runtime.session-map")


@dataclass(frozen=True)
class SessionBinding:
    claude_session_id: str
    user_id: str = ""
    sandbox_id: str = ""
    checkpoint_object_key: str = ""


@runtime_checkable
class SessionMap(Protocol):
    """session_id -> claude_session_id index for --resume continuation."""

    async def get(self, session_id: str, *, user_id: str = "") -> str | None:
        """Return the claude_session_id to resume, or None if unknown."""

    async def get_binding(self, session_id: str, *, user_id: str = "") -> SessionBinding | None:
        """Return the stored resume binding, including the sandbox it belongs to."""

    async def put(
        self, session_id: str, claude_session_id: str, *, user_id: str = "", sandbox_id: str = ""
    ) -> None:
        """Record the latest claude_session_id for a cocola session."""

    async def get_checkpoint(self, session_id: str, *, user_id: str = "") -> str | None:
        """Return the latest checkpoint object key for a cocola session."""

    async def delete(self, session_id: str, *, user_id: str = "") -> None:
        """Forget a session's binding (e.g. a dangling/stale resume id)."""

    async def aclose(self) -> None:
        """Release any backing resources."""


class MemorySessionMap:
    """In-process index; resume works within one process lifetime only."""

    def __init__(self) -> None:
        self._d: dict[str, SessionBinding] = {}

    async def get(self, session_id: str, *, user_id: str = "") -> str | None:
        binding = await self.get_binding(session_id, user_id=user_id)
        if not binding or not binding.claude_session_id:
            return None
        return binding.claude_session_id

    async def get_binding(self, session_id: str, *, user_id: str = "") -> SessionBinding | None:
        if not user_id:
            return None
        binding = self._d.get(session_id)
        if not binding or not binding.claude_session_id or binding.user_id != user_id:
            return None
        return SessionBinding(
            claude_session_id=binding.claude_session_id,
            user_id=binding.user_id,
            sandbox_id=binding.sandbox_id,
            checkpoint_object_key=binding.checkpoint_object_key,
        )

    async def put(
        self, session_id: str, claude_session_id: str, *, user_id: str = "", sandbox_id: str = ""
    ) -> None:
        if claude_session_id:
            if not user_id:
                raise PermissionError("session owner required")
            current = self._d.get(session_id)
            if current and current.user_id and current.user_id != user_id:
                raise PermissionError("session owner mismatch")
            self._d[session_id] = SessionBinding(
                claude_session_id=claude_session_id,
                user_id=user_id,
                sandbox_id=sandbox_id,
                checkpoint_object_key=current.checkpoint_object_key if current else "",
            )

    async def get_checkpoint(self, session_id: str, *, user_id: str = "") -> str | None:
        if not user_id:
            return None
        binding = self._d.get(session_id)
        if binding and binding.user_id != user_id:
            return None
        key = binding.checkpoint_object_key if binding else ""
        return key or None

    async def delete(self, session_id: str, *, user_id: str = "") -> None:
        if not user_id:
            return
        binding = self._d.get(session_id)
        if binding and binding.user_id != user_id:
            return
        self._d.pop(session_id, None)

    async def aclose(self) -> None:
        return None


_GET = """
SELECT claude_session_id, sandbox_id, checkpoint_object_key, user_id
FROM session_map
WHERE session_id = %s AND user_id = %s
"""

_PUT = """
INSERT INTO session_map (session_id, claude_session_id, user_id, sandbox_id, updated_at)
VALUES (%s, %s, %s, %s, now())
ON CONFLICT (session_id)
DO UPDATE SET claude_session_id = EXCLUDED.claude_session_id,
             user_id = EXCLUDED.user_id,
             sandbox_id = EXCLUDED.sandbox_id,
             updated_at = now()
WHERE session_map.user_id = EXCLUDED.user_id
RETURNING session_id
"""

_GET_CHECKPOINT = """
SELECT checkpoint_object_key
FROM session_map
WHERE session_id = %s AND user_id = %s
"""

_DELETE = "DELETE FROM session_map WHERE session_id = %s AND user_id = %s"


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

    async def get(self, session_id: str, *, user_id: str = "") -> str | None:
        binding = await self.get_binding(session_id, user_id=user_id)
        return binding.claude_session_id if binding else None

    async def get_binding(self, session_id: str, *, user_id: str = "") -> SessionBinding | None:
        if not user_id:
            return None
        await self._ready()
        async with self._pool.connection() as conn:
            cur = await conn.execute(_GET, (session_id, user_id))
            row = await cur.fetchone()
        if not row:
            return None
        cid = row[0]
        if not cid:
            return None
        return SessionBinding(
            claude_session_id=cid,
            user_id=row[3] or "",
            sandbox_id=row[1] or "",
            checkpoint_object_key=row[2] or "",
        )

    async def put(
        self, session_id: str, claude_session_id: str, *, user_id: str = "", sandbox_id: str = ""
    ) -> None:
        if not claude_session_id:
            return
        if not user_id:
            raise PermissionError("session owner required")
        await self._ready()
        async with self._pool.connection() as conn:
            cur = await conn.execute(_PUT, (session_id, claude_session_id, user_id, sandbox_id))
            if await cur.fetchone() is None:
                raise PermissionError("session owner mismatch")

    async def get_checkpoint(self, session_id: str, *, user_id: str = "") -> str | None:
        if not user_id:
            return None
        await self._ready()
        async with self._pool.connection() as conn:
            cur = await conn.execute(_GET_CHECKPOINT, (session_id, user_id))
            row = await cur.fetchone()
        if not row:
            return None
        return row[0] or None

    async def delete(self, session_id: str, *, user_id: str = "") -> None:
        if not user_id:
            return
        await self._ready()
        async with self._pool.connection() as conn:
            await conn.execute(_DELETE, (session_id, user_id))

    async def aclose(self) -> None:
        if self._opened:
            await self._pool.close()
            self._opened = False
