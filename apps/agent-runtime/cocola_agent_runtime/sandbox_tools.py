"""sandbox_tools — expose the bound sandbox to the agent as in-process MCP tools.

This is the seam that makes "the sandbox actually gets used": the Claude Agent
SDK owns the ReAct loop and *decides* when to run a command or touch a file, but
those tool calls must land inside the session's real sandbox — not on the
agent-runtime host. The SDK's in-process MCP mechanism is exactly the reuse-not-
reinvent path for that:

    tool(...)                  -> declare a tool (name, schema, async handler)
    create_sdk_mcp_server(...) -> bundle tools into an in-process MCP server
                                  (runs in THIS process, no subprocess, no port)
    ClaudeAgentOptions(mcp_servers=..., allowed_tools=...) -> mount it

So we register three sandbox-backed tools — bash / read_file / write_file — whose
handlers call the injected `SandboxExecutor` against the session's bound
`sandbox_id`. The handlers return the SDK's tool-result shape
(`{"content": [{"type": "text", "text": ...}], "is_error": bool}`), so the agent
sees real stdout/stderr/file contents and a clean error on failure.

Hermetic by construction: the executor is a Protocol (StaticSandboxExecutor in
tests), and building the tool/handler set needs no SDK import — only mounting
them onto ClaudeAgentOptions does. That keeps the mapping logic unit-testable
with no subprocess, no socket, no model.
"""

from __future__ import annotations

from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from typing import Any

from cocola_agent_runtime.sandbox_binder import ExecOutcome, SandboxExecutor

# The MCP server name. The SDK namespaces tools as ``mcp__<server>__<tool>``,
# so this is what the ``allowed_tools`` entries below are derived from.
SERVER_NAME = "cocola_sandbox"

# A tool handler: receives the parsed tool input dict, returns an SDK tool result.
ToolHandler = Callable[[dict[str, Any]], Awaitable[dict[str, Any]]]


@dataclass(frozen=True)
class ToolDef:
    """SDK-agnostic description of one tool.

    Kept free of any SDK type so the handler/mapping logic is unit-testable
    without importing claude_agent_sdk. `to_sdk_tool` turns it into the SDK's
    `SdkMcpTool` only at mount time.
    """

    name: str
    description: str
    input_schema: dict[str, Any]
    handler: ToolHandler


def _ok(text: str) -> dict[str, Any]:
    return {"content": [{"type": "text", "text": text}], "is_error": False}


def _err(text: str) -> dict[str, Any]:
    return {"content": [{"type": "text", "text": text}], "is_error": True}


def _render_exec(out: ExecOutcome) -> dict[str, Any]:
    """Render an ExecOutcome as an SDK tool result.

    A sandbox-level failure (`error` set) is a tool error. A command that ran but
    exited non-zero is NOT a tool error — the agent should see the exit code and
    streams and decide what to do, mirroring how a shell behaves.
    """
    if out.error:
        return _err(f"sandbox exec failed: {out.error}")
    parts = [f"exit_code: {out.exit_code}"]
    if out.stdout:
        parts.append("stdout:\n" + out.stdout)
    if out.stderr:
        parts.append("stderr:\n" + out.stderr)
    return {
        "content": [{"type": "text", "text": "\n".join(parts)}],
        "is_error": False,
    }


def sandbox_tool_defs(executor: SandboxExecutor, sandbox_id: str) -> list[ToolDef]:
    """Build the bash / read_file / write_file tool defs bound to one sandbox.

    The `sandbox_id` is closed over, so the agent never sees (or can spoof) which
    sandbox it runs in: every call is pinned to the session's bound sandbox.
    """

    async def _bash(args: dict[str, Any]) -> dict[str, Any]:
        command = str(args.get("command", "")).strip()
        if not command:
            return _err("bash: 'command' is required")
        timeout = int(args.get("timeout_secs", 0) or 0)
        try:
            out = await executor.exec(
                sandbox_id=sandbox_id,
                cmd=["bash", "-lc", command],
                timeout_secs=timeout,
            )
        except Exception as exc:  # noqa: BLE001 - surface transport faults as a tool error
            return _err(f"bash: {exc}")
        return _render_exec(out)

    async def _read_file(args: dict[str, Any]) -> dict[str, Any]:
        path = str(args.get("path", "")).strip()
        if not path:
            return _err("read_file: 'path' is required")
        try:
            data = await executor.read_file(sandbox_id=sandbox_id, path=path)
        except Exception as exc:  # noqa: BLE001
            return _err(f"read_file: {exc}")
        return _ok(data)

    async def _write_file(args: dict[str, Any]) -> dict[str, Any]:
        path = str(args.get("path", "")).strip()
        if not path:
            return _err("write_file: 'path' is required")
        content = str(args.get("content", ""))
        try:
            await executor.write_file(sandbox_id=sandbox_id, path=path, content=content)
        except Exception as exc:  # noqa: BLE001
            return _err(f"write_file: {exc}")
        return _ok(f"wrote {len(content)} bytes to {path}")

    return [
        ToolDef(
            name="bash",
            description=(
                "Run a shell command inside the session's sandbox and return its "
                "exit code, stdout and stderr."
            ),
            input_schema={"command": str, "timeout_secs": int},
            handler=_bash,
        ),
        ToolDef(
            name="read_file",
            description="Read a UTF-8 text file from the session's sandbox.",
            input_schema={"path": str},
            handler=_read_file,
        ),
        ToolDef(
            name="write_file",
            description="Write a UTF-8 text file into the session's sandbox.",
            input_schema={"path": str, "content": str},
            handler=_write_file,
        ),
    ]


def tool_names(defs: list[ToolDef], *, server: str = SERVER_NAME) -> list[str]:
    """The fully-qualified tool names to pass to `allowed_tools`.

    The SDK exposes in-process MCP tools as ``mcp__<server>__<tool>``; we must
    allow-list them explicitly or the agent cannot call them.
    """
    return [f"mcp__{server}__{d.name}" for d in defs]


def build_sandbox_mcp_server(executor: SandboxExecutor, sandbox_id: str):
    """Build the in-process MCP server config + its allowed tool names.

    Returns ``(server_config, allowed_tool_names)``. Imports the SDK lazily so
    this module (and the tool-def/handler logic) stays importable without the SDK
    installed — only callers that actually mount the server need it.
    """
    from claude_agent_sdk import create_sdk_mcp_server, tool

    defs = sandbox_tool_defs(executor, sandbox_id)
    sdk_tools = [tool(d.name, d.description, d.input_schema)(d.handler) for d in defs]
    server = create_sdk_mcp_server(SERVER_NAME, "0.1.0", sdk_tools)
    return server, tool_names(defs)
