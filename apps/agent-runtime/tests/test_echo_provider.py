"""Smoke test for M0 plumbing."""

import pytest
from cocola_agent_runtime.__main__ import _build_provider
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


def test_real_mode_requires_sandbox_executor(monkeypatch):
    monkeypatch.delenv("COCOLA_AGENT_MODE", raising=False)
    with pytest.raises(RuntimeError, match="requires COCOLA_SANDBOX_ADDR"):
        _build_provider(None, None)


def test_echo_mode_is_explicit(monkeypatch):
    monkeypatch.setenv("COCOLA_AGENT_MODE", "echo")
    assert isinstance(_build_provider(None, None), EchoProvider)
