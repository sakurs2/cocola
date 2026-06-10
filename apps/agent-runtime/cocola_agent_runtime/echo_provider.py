"""Trivial AgentProvider used in M0 to prove wiring without external deps."""

from __future__ import annotations

from collections.abc import AsyncIterator

from cocola_agent_runtime.agent_provider import AgentEvent, AgentOptions


class EchoProvider:
    async def query(
        self,
        prompt: str,
        options: AgentOptions,
    ) -> AsyncIterator[AgentEvent]:
        yield AgentEvent(kind="text", data={"text": f"echo({options.session_id}): {prompt}"})
        yield AgentEvent(kind="done", data={})
