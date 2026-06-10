"""Smoke test for M0 plumbing."""

import pytest
from cocola_agent_runtime.agent_provider import AgentOptions
from cocola_agent_runtime.echo_provider import EchoProvider


@pytest.mark.asyncio
async def test_echo_provider_emits_text_and_done():
    p = EchoProvider()
    opts = AgentOptions(user_id="u1", session_id="s1")
    events = [e async for e in p.query("hi", opts)]
    assert len(events) == 2
    assert events[0].kind == "text"
    assert "hi" in events[0].data["text"]
    assert events[1].kind == "done"
