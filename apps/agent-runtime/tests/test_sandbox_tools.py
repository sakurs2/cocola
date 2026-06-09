"""sandbox_tools tests — the agent's bash/file tools land inside the sandbox.

Hermetic: no SDK subprocess, no socket, no model. We drive the tool *handlers*
directly with a StaticSandboxExecutor and assert (a) each tool routes to the
right executor method with the right args, (b) results map to the SDK tool-result
shape, and (c) errors become tool errors without raising. We also assert the
provider mounts these as an in-process MCP server only when both an executor and
a bound sandbox are present.
"""

from __future__ import annotations

from cocola_agent_runtime.agent_provider import AgentOptions
from cocola_agent_runtime.claude_sdk_provider import (
    ClaudeAgentSDKProvider,
    ClaudeSDKConfig,
)
from cocola_agent_runtime.sandbox_binder import ExecOutcome, StaticSandboxExecutor
from cocola_agent_runtime.sandbox_tools import (
    SERVER_NAME,
    sandbox_tool_defs,
    tool_names,
)


def _by_name(defs):
    return {d.name: d for d in defs}


async def test_bash_tool_runs_in_bound_sandbox():
    ex = StaticSandboxExecutor(
        exec_handler=lambda sid, cmd: ExecOutcome(exit_code=0, stdout="hi\n")
    )
    defs = sandbox_tool_defs(ex, "box-S1")
    res = await _by_name(defs)["bash"].handler({"command": "echo hi"})

    assert res["is_error"] is False
    text = res["content"][0]["text"]
    assert "exit_code: 0" in text and "hi" in text
    # routed to THIS sandbox, wrapped in a login shell
    call = ex.exec_calls[0]
    assert call["sandbox_id"] == "box-S1"
    assert call["cmd"] == ["bash", "-lc", "echo hi"]


async def test_bash_nonzero_exit_is_not_a_tool_error():
    ex = StaticSandboxExecutor(
        exec_handler=lambda sid, cmd: ExecOutcome(exit_code=2, stderr="boom\n")
    )
    defs = sandbox_tool_defs(ex, "box-S1")
    res = await _by_name(defs)["bash"].handler({"command": "false"})
    # A command that ran but failed is surfaced, not hidden as a tool error.
    assert res["is_error"] is False
    assert "exit_code: 2" in res["content"][0]["text"]
    assert "boom" in res["content"][0]["text"]


async def test_bash_sandbox_error_is_a_tool_error():
    ex = StaticSandboxExecutor(
        exec_handler=lambda sid, cmd: ExecOutcome(error="sandbox gone")
    )
    defs = sandbox_tool_defs(ex, "box-S1")
    res = await _by_name(defs)["bash"].handler({"command": "ls"})
    assert res["is_error"] is True
    assert "sandbox gone" in res["content"][0]["text"]


async def test_bash_requires_command():
    ex = StaticSandboxExecutor()
    defs = sandbox_tool_defs(ex, "box-S1")
    res = await _by_name(defs)["bash"].handler({"command": "   "})
    assert res["is_error"] is True
    assert ex.exec_calls == []  # never reached the sandbox


async def test_write_then_read_roundtrip():
    ex = StaticSandboxExecutor()
    defs = _by_name(sandbox_tool_defs(ex, "box-S1"))

    w = await defs["write_file"].handler({"path": "/work/a.txt", "content": "data"})
    assert w["is_error"] is False
    assert ex.writes == [("box-S1", "/work/a.txt", "data")]

    r = await defs["read_file"].handler({"path": "/work/a.txt"})
    assert r["is_error"] is False
    assert r["content"][0]["text"] == "data"
    assert ex.reads == [("box-S1", "/work/a.txt")]


async def test_read_missing_file_is_a_tool_error():
    ex = StaticSandboxExecutor()
    defs = _by_name(sandbox_tool_defs(ex, "box-S1"))
    res = await defs["read_file"].handler({"path": "/nope"})
    assert res["is_error"] is True


async def test_transport_failure_becomes_tool_error():
    ex = StaticSandboxExecutor(fail_with=RuntimeError("grpc down"))
    defs = _by_name(sandbox_tool_defs(ex, "box-S1"))
    res = await defs["bash"].handler({"command": "ls"})
    assert res["is_error"] is True
    assert "grpc down" in res["content"][0]["text"]


def test_tool_names_are_mcp_namespaced():
    ex = StaticSandboxExecutor()
    defs = sandbox_tool_defs(ex, "box-S1")
    names = tool_names(defs)
    assert names == [
        f"mcp__{SERVER_NAME}__bash",
        f"mcp__{SERVER_NAME}__read_file",
        f"mcp__{SERVER_NAME}__write_file",
    ]


# --- provider mounting -----------------------------------------------------


def test_provider_mounts_sandbox_server_when_bound():
    ex = StaticSandboxExecutor()
    prov = ClaudeAgentSDKProvider(
        ClaudeSDKConfig(base_url="http://gw", model="default"),
        query_fn=lambda **kw: iter(()),
        executor=ex,
    )
    opts = AgentOptions(user_id="U1", session_id="S1", sandbox_id="box-S1")
    sdk_opts = prov._build_options(opts)
    assert SERVER_NAME in sdk_opts.mcp_servers
    assert f"mcp__{SERVER_NAME}__bash" in sdk_opts.allowed_tools


def test_provider_skips_server_without_bound_sandbox():
    ex = StaticSandboxExecutor()
    prov = ClaudeAgentSDKProvider(
        ClaudeSDKConfig(base_url="http://gw", model="default"),
        query_fn=lambda **kw: iter(()),
        executor=ex,
    )
    opts = AgentOptions(user_id="U1", session_id="S1", sandbox_id=None)
    sdk_opts = prov._build_options(opts)
    assert not sdk_opts.mcp_servers


def test_provider_skips_server_without_executor():
    prov = ClaudeAgentSDKProvider(
        ClaudeSDKConfig(base_url="http://gw", model="default"),
        query_fn=lambda **kw: iter(()),
    )
    opts = AgentOptions(user_id="U1", session_id="S1", sandbox_id="box-S1")
    sdk_opts = prov._build_options(opts)
    assert not sdk_opts.mcp_servers
