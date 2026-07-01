"""ClaudeAgentSDKProvider — drives the Claude Code Agent SDK against cocola.

The whole point of cocola's M3 design is that we do NOT build our own agent
loop. The Claude Agent SDK *is* the agent: it owns the ReAct loop, tool calling,
context management, and turn orchestration. Our job is only to point it at the
cocola LLM gateway instead of api.anthropic.com.

How the redirection works
-------------------------
`claude_agent_sdk.query()` spawns the Claude Code CLI as a child process and
talks to it over stdio. That CLI is what actually opens HTTP connections to the
model endpoint, and it reads the endpoint from the environment:

    ANTHROPIC_BASE_URL   -> where to send /v1/messages   (our gateway)
    ANTHROPIC_AUTH_TOKEN -> bearer the gateway validates  (cocola-issued)

So redirecting the SDK to cocola is purely an env injection — zero SDK code
changes, zero monkeypatching. Point the base URL at the gateway and every model
call flows through cocola's routing + metering + billing.

Testability
-----------
The SDK spawns a real subprocess and opens real sockets, which we cannot do in
a hermetic/no-port test. So the concrete `query` callable is injectable
(`query_fn`); production uses the real SDK, tests inject a fake that yields
SDK-shaped messages (optionally driving the gateway over ASGI in-process). The
mapping logic below is identical either way.
"""

from __future__ import annotations

import atexit
import json
import pathlib
import shutil
import tempfile
from collections.abc import AsyncIterator, Callable
from dataclasses import dataclass, field
from typing import Any

from cocola_agent_runtime.agent_provider import AgentEvent, AgentOptions


@dataclass
class ClaudeSDKConfig:
    """Where the SDK should send model traffic, injected from config/env.

    `base_url` MUST point at the cocola gateway's HTTP root (the SDK appends
    `/v1/messages`). `api_key` is the cocola-issued credential the gateway
    checks — never a real Anthropic key. `model` is a caller-facing alias the
    gateway's registry resolves to a real upstream model.
    """

    base_url: str
    model: str
    api_key: str = "cocola-local"
    max_turns: int = 30
    extra_env: dict[str, str] = field(default_factory=dict)


# Signature of claude_agent_sdk.query (the bits we use). Kept as a callable so
# tests can substitute a fake without importing/spawning the real CLI.
QueryFn = Callable[..., AsyncIterator[Any]]


class ClaudeAgentSDKProvider:
    """AgentProvider backed by the Claude Agent SDK, routed through cocola.

    Implements the runtime's `AgentProvider` Protocol (`query`). The runtime
    server depends only on that Protocol, so swapping EchoProvider for this is a
    composition-root change, not a server change.
    """

    name = "claude-agent-sdk"

    def __init__(
        self,
        config: ClaudeSDKConfig,
        *,
        query_fn: QueryFn | None = None,
    ):
        self._config = config
        self._iso_config_dir: str | None = None
        # Lazy import so the package imports cleanly even if the SDK/CLI is not
        # installed (e.g. in unit tests that inject query_fn).
        if query_fn is not None:
            self._query: QueryFn = query_fn
        else:  # pragma: no cover - exercised only with the real SDK + CLI
            import claude_agent_sdk

            self._query = claude_agent_sdk.query

    def _isolated_config_dir(self) -> str:
        """Return a private, empty Claude config dir, creating it once.

        Why this exists (the 503 bug): Claude Code applies the `env` block from
        the user's *global* ``~/.claude/settings.json`` with HIGHER precedence
        than the process environment we inject. A developer running cocola will
        almost always have a real Anthropic/proxy endpoint pinned there
        (``ANTHROPIC_BASE_URL``, ``ANTHROPIC_AUTH_TOKEN``, model overrides...),
        so our ``ANTHROPIC_BASE_URL=<cocola gateway>`` got silently overridden
        and every model call shot straight past cocola to that endpoint —
        which doesn't know cocola's model aliases and returns 503.

        Empirically (and per the SDK/CLI source) neither ``--setting-sources=``
        nor ``--settings <json>`` overrides that global env block. The only
        reliable fix is to point the CLI at a *different* config dir that has no
        settings, via ``CLAUDE_CONFIG_DIR`` / ``ANTHROPIC_CONFIG_DIR``. Then our
        injected env is authoritative and traffic flows through cocola.
        """
        if self._iso_config_dir is None:
            d = tempfile.mkdtemp(prefix="cocola-claude-config-")
            (pathlib.Path(d) / "settings.json").write_text(json.dumps({}))
            self._iso_config_dir = d
            atexit.register(shutil.rmtree, d, ignore_errors=True)
        return self._iso_config_dir

    def _build_env(self) -> dict[str, str]:
        # Claude Code distinguishes two auth modes that map to different HTTP
        # headers (verified against the CLI docs):
        #   ANTHROPIC_API_KEY    -> sent as `x-api-key`        (direct Anthropic)
        #   ANTHROPIC_AUTH_TOKEN -> sent as `Authorization: Bearer`
        #                                                      (custom gateway)
        # cocola routes the SDK at its own gateway with a cocola-issued JWT, not
        # a real `sk-ant-` key. Putting that JWT in ANTHROPIC_API_KEY makes the
        # CLI treat it as an Anthropic key and reject/retry it (HTTP 401). The
        # gateway-proxy mode is exactly what ANTHROPIC_AUTH_TOKEN is for, so we
        # present the credential there. The gateway's _bearer() accepts both
        # `Authorization` and `x-api-key`, so this stays compatible.
        iso = self._isolated_config_dir()
        env = {
            "ANTHROPIC_BASE_URL": self._config.base_url,
            "ANTHROPIC_AUTH_TOKEN": self._config.api_key,
            # Isolate from the user's global ~/.claude/settings.json `env` block,
            # which otherwise overrides ANTHROPIC_BASE_URL and breaks routing.
            "CLAUDE_CONFIG_DIR": iso,
            "ANTHROPIC_CONFIG_DIR": iso,
            # The isolated config drops the user's disable flag; re-assert it so
            # the CLI does not phone home to api.anthropic.com for telemetry.
            "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
        }
        env.update(self._config.extra_env)
        return env

    def _build_options(self, options: AgentOptions):  # pragma: no cover - real SDK path
        # Imported here (not module top) so unit tests need no SDK install.
        import claude_agent_sdk

        kwargs: dict[str, Any] = dict(
            model=self._config.model,
            system_prompt=options.system_prompt,
            max_turns=options.max_turns or self._config.max_turns,
            env=self._build_env(),
        )
        # This provider's brain runs IN THIS PROCESS, not in the user's sandbox.
        # When the server has landed uploaded attachments in a local per-session
        # workspace (local-dev push model, ADR-0017) it passes that dir here so
        # the SDK's native Read/Bash tools resolve ./uploads/ against it.
        if options.workspace:
            kwargs["cwd"] = options.workspace
        # Route A (ADR-0009) runs the whole Claude Code brain inside the user's
        # own sandbox, so its NATIVE Bash/Read/Write tools are already isolated.
        # The old Route-B MCP forwarding seam (sandbox_tools.py) that proxied
        # the agent's tools into a remote sandbox has been removed — the agent
        # here runs with its built-in toolset only.
        return claude_agent_sdk.ClaudeAgentOptions(**kwargs)

    async def query(
        self,
        prompt: str,
        options: AgentOptions,
    ) -> AsyncIterator[AgentEvent]:
        """Run one SDK query and re-emit its messages as generic AgentEvents.

        We translate the SDK's rich message/block taxonomy into the runtime's
        small, transport-neutral `AgentEvent` vocabulary so the rest of cocola
        never depends on the SDK's types.
        """
        sdk_options = self._maybe_build_options(options)
        async for message in self._query(prompt=prompt, options=sdk_options):
            for event in _message_to_events(message):
                yield event
        yield AgentEvent(kind="done", data={})

    def _maybe_build_options(self, options: AgentOptions):
        # When a fake query_fn is injected for tests it ignores `options`, so we
        # avoid importing the SDK just to construct an options object.
        try:
            return self._build_options(options)
        except ImportError:
            return options


def _message_to_events(message: Any) -> list[AgentEvent]:
    """Map a single SDK message into zero or more AgentEvents.

    Uses duck-typing on class names so the mapping works against both the real
    SDK dataclasses and lightweight fakes that mimic their shape.
    """
    events: list[AgentEvent] = []
    cls = type(message).__name__

    if cls == "AssistantMessage":
        for block in getattr(message, "content", []) or []:
            events.extend(_block_to_events(block))
        if getattr(message, "error", None):
            events.append(AgentEvent(kind="error", data={"error": str(message.error)}))
    elif cls == "ResultMessage":
        events.append(
            AgentEvent(
                kind="result",
                data={
                    "is_error": bool(getattr(message, "is_error", False)),
                    "num_turns": getattr(message, "num_turns", None),
                    "total_cost_usd": getattr(message, "total_cost_usd", None),
                    "session_id": getattr(message, "session_id", None),
                    "result": getattr(message, "result", None),
                },
            )
        )
    elif cls == "SystemMessage":
        events.append(
            AgentEvent(
                kind="system",
                data={
                    "subtype": getattr(message, "subtype", ""),
                    "data": getattr(message, "data", {}),
                },
            )
        )
    # UserMessage (tool results fed back) is internal to the loop; we skip it.
    return events


def _block_to_events(block: Any) -> list[AgentEvent]:
    cls = type(block).__name__
    if cls == "TextBlock":
        return [AgentEvent(kind="text", data={"text": getattr(block, "text", "")})]
    if cls == "ThinkingBlock":
        return [AgentEvent(kind="thinking", data={"thinking": getattr(block, "thinking", "")})]
    if cls == "ToolUseBlock":
        return [
            AgentEvent(
                kind="tool_use",
                data={
                    "id": getattr(block, "id", ""),
                    "name": getattr(block, "name", ""),
                    "input": getattr(block, "input", {}),
                },
            )
        ]
    if cls == "ToolResultBlock":
        return [
            AgentEvent(
                kind="tool_result",
                data={
                    "tool_use_id": getattr(block, "tool_use_id", ""),
                    "content": getattr(block, "content", None),
                    "is_error": bool(getattr(block, "is_error", False)),
                },
            )
        ]
    return []
