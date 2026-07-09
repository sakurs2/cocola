"""Defines the AgentProvider Protocol.

Concrete providers (`InSandboxShimProvider`, `EchoProvider`, …) MUST implement
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
    # Host working directory for an in-process provider (one whose brain runs
    # IN THIS PROCESS). When set, such a provider points its cwd here so native
    # Read/Bash tools resolve relative paths like ./uploads/. Unused by Route A
    # (the in-sandbox brain has its own cwd) and by EchoProvider.
    workspace: str | None = None
    system_prompt: str | None = None
    max_turns: int = 30
    model_alias: str | None = None
    mcp_servers: dict[str, dict] | None = None
    # Per-user cocola token minted by the gateway for THIS turn (sub=user,
    # ten=tenant). A Route A provider injects it into the sandbox as
    # ANTHROPIC_AUTH_TOKEN so the in-sandbox brain calls the llm-gateway as the
    # real user (per-user quota / usage / revocation), overriding the static
    # token baked at sandbox creation. None => keep the baked default token.
    auth_token: str | None = None


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
