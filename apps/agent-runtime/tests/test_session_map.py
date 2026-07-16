"""SessionMap tests: the conversation/runtime -> native session index.

Two legs:

  - The MemorySessionMap contract runs everywhere (hermetic, no DB).
  - The Postgres parity leg is gated on COCOLA_TEST_PG_DSN. It points at a real
    database with the db/migrations schema applied and asserts the durable index
    behaves like the in-memory one AND survives being reconstructed (the restart
    proxy: a new store instance over the same DSN must still read the binding).

The on-disk runtime session is the sufficient condition, but the index must
outlive an agent-runtime restart so a follow-up turn knows which native session
to reopen without crossing runtime or user boundaries.
"""

from __future__ import annotations

import os

import pytest
from cocola_agent_runtime.session_map import MemorySessionMap

PG_DSN = os.getenv("COCOLA_TEST_PG_DSN", "").strip()
pg_only = pytest.mark.skipif(not PG_DSN, reason="COCOLA_TEST_PG_DSN not set")


async def _contract(store):
    runtime_id = "claude-code"
    # Unknown session -> None.
    assert await store.get("S-unknown", user_id="U1", runtime_id=runtime_id) is None
    # Put then get round-trips.
    await store.put("S1", "claude-aaa", user_id="U1", sandbox_id="box-1", runtime_id=runtime_id)
    assert await store.get("S1", user_id="U1", runtime_id=runtime_id) == "claude-aaa"
    binding = await store.get_binding("S1", user_id="U1", runtime_id=runtime_id)
    assert binding is not None
    assert binding.runtime_session_id == "claude-aaa"
    assert binding.runtime_id == runtime_id
    assert binding.sandbox_id == "box-1"
    # Idempotent overwrite: the latest native session ID wins.
    await store.put("S1", "claude-bbb", user_id="U1", sandbox_id="box-2", runtime_id=runtime_id)
    assert await store.get("S1", user_id="U1", runtime_id=runtime_id) == "claude-bbb"
    binding = await store.get_binding("S1", user_id="U1", runtime_id=runtime_id)
    assert binding is not None
    assert binding.runtime_session_id == "claude-bbb"
    assert binding.sandbox_id == "box-2"
    # Empty native session ID is a no-op (never clobbers a good binding).
    await store.put("S1", "", user_id="U1", runtime_id=runtime_id)
    assert await store.get("S1", user_id="U1", runtime_id=runtime_id) == "claude-bbb"
    assert await store.get("S1", user_id="U2", runtime_id=runtime_id) is None
    assert await store.get("S1", user_id="U1", runtime_id="codex") is None
    # delete forgets a binding (used to drop a dangling/stale resume id).
    await store.delete("S1", user_id="U1", runtime_id=runtime_id)
    assert await store.get("S1", user_id="U1", runtime_id=runtime_id) is None
    assert await store.get_binding("S1", user_id="U1", runtime_id=runtime_id) is None
    # delete is idempotent: forgetting an unknown session is a no-op.
    await store.delete("S-unknown", user_id="U1", runtime_id=runtime_id)


async def test_memory_session_map_contract():
    store = MemorySessionMap()
    await _contract(store)
    await store.aclose()


async def test_memory_session_map_rejects_cross_owner_overwrite_and_delete():
    store = MemorySessionMap()
    await store.put(
        "shared",
        "claude-u1",
        user_id="U1",
        sandbox_id="box-u1",
        runtime_id="claude-code",
    )
    with pytest.raises(PermissionError, match="owner mismatch"):
        await store.put(
            "shared",
            "claude-u2",
            user_id="U2",
            sandbox_id="box-u2",
            runtime_id="claude-code",
        )
    with pytest.raises(PermissionError, match="runtime mismatch"):
        await store.put(
            "shared",
            "codex-u1",
            user_id="U1",
            sandbox_id="box-codex",
            runtime_id="codex",
        )
    await store.delete("shared", user_id="U2", runtime_id="claude-code")
    await store.delete("shared", user_id="U1", runtime_id="codex")
    assert await store.get("shared", user_id="U1", runtime_id="claude-code") == "claude-u1"
    assert await store.get("shared", user_id="U2", runtime_id="claude-code") is None


async def _truncate(dsn: str) -> None:
    import psycopg

    async with await psycopg.AsyncConnection.connect(dsn, autocommit=True) as conn:
        await conn.execute("TRUNCATE session_map")


@pg_only
async def test_postgres_session_map_parity():
    from cocola_agent_runtime.session_map import PostgresSessionMap

    await _truncate(PG_DSN)
    store = PostgresSessionMap(PG_DSN)
    try:
        await _contract(store)
    finally:
        await store.aclose()


@pg_only
async def test_postgres_session_map_survives_restart():
    from cocola_agent_runtime.session_map import PostgresSessionMap

    await _truncate(PG_DSN)
    writer = PostgresSessionMap(PG_DSN)
    try:
        await writer.put(
            "S-restart",
            "claude-persist",
            user_id="U9",
            sandbox_id="box-9",
            runtime_id="claude-code",
        )
    finally:
        await writer.aclose()

    # A brand-new store instance models an agent-runtime restart: the binding
    # must come back from the durable table, not from any in-process state.
    reader = PostgresSessionMap(PG_DSN)
    try:
        assert (
            await reader.get("S-restart", user_id="U9", runtime_id="claude-code")
            == "claude-persist"
        )
        binding = await reader.get_binding("S-restart", user_id="U9", runtime_id="claude-code")
        assert binding is not None
        assert binding.sandbox_id == "box-9"
    finally:
        await reader.aclose()
