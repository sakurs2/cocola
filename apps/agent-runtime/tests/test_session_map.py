"""SessionMap tests: the session_id -> claude_session_id resume index (M7).

Two legs:

  - The MemorySessionMap contract runs everywhere (hermetic, no DB).
  - The Postgres parity leg is gated on COCOLA_TEST_PG_DSN. It points at a real
    database with the db/migrations schema applied and asserts the durable index
    behaves like the in-memory one AND survives being reconstructed (the restart
    proxy: a new store instance over the same DSN must still read the binding).

This is the durability anchor for Route A --resume: the on-disk ~/.claude file
is the SUFFICIENT condition, but the index must outlive an agent-runtime restart
so a follow-up turn knows which claude_session_id to reopen.
"""

from __future__ import annotations

import os

import pytest
from cocola_agent_runtime.session_map import MemorySessionMap

PG_DSN = os.getenv("COCOLA_TEST_PG_DSN", "").strip()
pg_only = pytest.mark.skipif(not PG_DSN, reason="COCOLA_TEST_PG_DSN not set")


async def _contract(store):
    # Unknown session -> None.
    assert await store.get("S-unknown") is None
    assert await store.get_checkpoint("S-unknown") is None
    # Put then get round-trips.
    await store.put("S1", "claude-aaa", user_id="U1", sandbox_id="box-1")
    assert await store.get("S1") == "claude-aaa"
    binding = await store.get_binding("S1")
    assert binding is not None
    assert binding.claude_session_id == "claude-aaa"
    assert binding.sandbox_id == "box-1"
    assert binding.checkpoint_object_key == ""
    # Checkpoint metadata is independent from the Claude resume id.
    await store.put_checkpoint("S1", "checkpoints/U1/S1/a.tar.zst", user_id="U1")
    assert await store.get_checkpoint("S1") == "checkpoints/U1/S1/a.tar.zst"
    binding = await store.get_binding("S1")
    assert binding is not None
    assert binding.claude_session_id == "claude-aaa"
    assert binding.checkpoint_object_key == "checkpoints/U1/S1/a.tar.zst"
    # Idempotent overwrite: the latest claude_session_id wins.
    await store.put("S1", "claude-bbb", user_id="U1", sandbox_id="box-2")
    assert await store.get("S1") == "claude-bbb"
    binding = await store.get_binding("S1")
    assert binding is not None
    assert binding.claude_session_id == "claude-bbb"
    assert binding.sandbox_id == "box-2"
    assert binding.checkpoint_object_key == "checkpoints/U1/S1/a.tar.zst"
    # Checkpoint overwrite does not clobber the Claude resume id.
    await store.put_checkpoint("S1", "checkpoints/U1/S1/b.tar.zst", user_id="U1")
    assert await store.get_checkpoint("S1") == "checkpoints/U1/S1/b.tar.zst"
    assert await store.get("S1") == "claude-bbb"
    # Checkpoint-only sessions are valid: restore may happen before a Claude
    # resume id exists in a freshly recreated sandbox.
    await store.put_checkpoint("S-only-checkpoint", "checkpoints/U1/S-only/c.tar.zst")
    assert await store.get_checkpoint("S-only-checkpoint") == "checkpoints/U1/S-only/c.tar.zst"
    assert await store.get("S-only-checkpoint") is None
    # Empty claude_session_id is a no-op (never clobbers a good binding).
    await store.put("S1", "", user_id="U1")
    assert await store.get("S1") == "claude-bbb"
    # delete forgets a binding (used to drop a dangling/stale resume id).
    await store.delete("S1")
    assert await store.get("S1") is None
    assert await store.get_checkpoint("S1") is None
    assert await store.get_binding("S1") is None
    # delete is idempotent: forgetting an unknown session is a no-op.
    await store.delete("S-unknown")


async def test_memory_session_map_contract():
    store = MemorySessionMap()
    await _contract(store)
    await store.aclose()


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
        await writer.put("S-restart", "claude-persist", user_id="U9", sandbox_id="box-9")
    finally:
        await writer.aclose()

    # A brand-new store instance models an agent-runtime restart: the binding
    # must come back from the durable table, not from any in-process state.
    reader = PostgresSessionMap(PG_DSN)
    try:
        assert await reader.get("S-restart") == "claude-persist"
        binding = await reader.get_binding("S-restart")
        assert binding is not None
        assert binding.sandbox_id == "box-9"
    finally:
        await reader.aclose()
