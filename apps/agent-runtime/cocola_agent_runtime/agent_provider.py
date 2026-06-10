"""Defines the AgentProvider Protocol.

Concrete providers (`ClaudeAgentSDKProvider`, `EchoProvider`, …) MUST implement
this Protocol. The runtime server depends on the Protocol only, never on a
concrete class — this is what makes the runtime LLM-agnostic and testable.
"""

from __future__ import annotations

from collections.abc import AsyncIterator
from dataclasses import dataclass
from typing import Protocol


@dataclass
class AgentOptions:
    user_id: str
    session_id: str
    sandbox_id: str | None = None
    system_prompt: str | None = None
    max_turns: int = 30


@dataclass
class AgentEvent:
    """Streamed back to the gateway. Kept intentionally generic."""

    kind: str  # text | tool_use | tool_result | error | done
    data: dict


class AgentProvider(Protocol):
    async def query(
        self,
        prompt: str,
        options: AgentOptions,
    ) -> AsyncIterator[AgentEvent]: ...
