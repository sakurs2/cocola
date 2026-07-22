"""Defines the AgentProvider Protocol.

Concrete providers such as `InSandboxShimProvider` MUST implement this
Protocol. The runtime server depends on the Protocol only, never on a
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
    runtime_id: str = "claude-code"
    sandbox_id: str | None = None
    # Host working directory for an in-process provider (one whose brain runs
    # IN THIS PROCESS). When set, such a provider points its cwd here so native
    # Read/Bash tools resolve relative paths like ./uploads/. Unused by Route A
    # (the in-sandbox brain has its own cwd).
    workspace: str | None = None
    # Working directory inside the bound Sandbox. Project runs use the isolated
    # Git worktree while ordinary conversations retain the platform root.
    working_directory: str = "/workspace"
    system_prompt: str | None = None
    max_turns: int = 30
    model_route_id: str | None = None
    # Effective skill explicitly selected for this turn. The in-sandbox
    # adapter converts it to the selected runtime's native invocation syntax.
    selected_skill_id: str | None = None
    mcp_servers: dict[str, dict] | None = None
    # Secret-free metadata for skills that were successfully materialized in
    # this sandbox. The Route-A provider folds these into the same environment
    # snapshot as MCP status; they never enter the model prompt.
    environment_skills: list[dict[str, str]] | None = None
    # Per-user cocola token minted by the gateway for this turn. The provider
    # injects it under the selected runtime's auth variable so the in-sandbox
    # brain calls llm-gateway as the real user. None means no token is injected.
    auth_token: str | None = None
    # W3C context for Cocola-owned downstream services. It is injected only
    # into requests to the configured Cocola LLM gateway, never into MCP calls.
    traceparent: str | None = None
    # Run-scoped, HMAC-signed capability used only by the Cocola SCM broker.
    # It is distinct from the one-shot clone token and is injected only into
    # the current Agent process.
    project_credential: str | None = None
    project_provider: str | None = None
    project_repository: str | None = None
    project_broker_url: str | None = None
    project_task_branch: str | None = None


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
