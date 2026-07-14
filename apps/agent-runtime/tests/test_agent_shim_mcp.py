"""Unit-test the Route-A sandbox shim option translation without real SDK calls."""

from __future__ import annotations

import asyncio
import importlib.util
import pathlib
import sys
import types


def test_agent_shim_passes_mcp_servers_to_claude_options(monkeypatch):
    captured = {}

    class FakeClaudeAgentOptions:
        def __init__(self, **kwargs):
            captured.update(kwargs)

    fake_sdk = types.SimpleNamespace(ClaudeAgentOptions=FakeClaudeAgentOptions)
    monkeypatch.setitem(sys.modules, "claude_agent_sdk", fake_sdk)

    root = pathlib.Path(__file__).resolve().parents[3]
    shim_path = root / "deploy" / "sandbox-runtime" / "shim" / "agent_shim.py"
    spec = importlib.util.spec_from_file_location("cocola_agent_shim_test", shim_path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)

    module._build_options(
        {
            "prompt": "hi",
            "mcp_servers": {
                "github": {
                    "type": "stdio",
                    "command": "npx",
                    "env": {"GITHUB_TOKEN": "secret"},
                }
            },
        }
    )

    assert captured["mcp_servers"]["github"]["command"] == "npx"
    assert captured["mcp_servers"]["github"]["env"]["GITHUB_TOKEN"] == "secret"
    assert captured["strict_mcp_config"] is True
    assert captured["setting_sources"] == ["user", "project"]
    assert captured["skills"] == "all"


async def test_agent_shim_streams_mcp_status_without_blocking_query(monkeypatch):
    captured: dict[str, object] = {}
    calls: list[str] = []

    class FakeClaudeAgentOptions:
        def __init__(self, **kwargs):
            captured["options"] = kwargs

    class FakeClaudeSDKClient:
        def __init__(self, *, options):
            captured["client_options"] = options

        async def __aenter__(self):
            return self

        async def __aexit__(self, *_args):
            return None

        async def get_mcp_status(self):
            calls.append("status")
            return {
                "mcpServers": [
                    {
                        "name": "maps",
                        "status": "connected",
                        "serverInfo": {"name": "Amap", "version": "1.0"},
                        "tools": [{"name": "weather"}],
                    }
                ]
            }

        async def query(self, prompt):
            calls.append("query")
            captured["prompt"] = prompt
            await asyncio.sleep(0)

        async def receive_response(self):
            result_type = type("ResultMessage", (), {})
            result = result_type()
            result.is_error = False
            result.num_turns = 1
            result.total_cost_usd = 0
            result.session_id = "claude-session"
            result.result = "done"
            yield result

    fake_sdk = types.SimpleNamespace(
        ClaudeAgentOptions=FakeClaudeAgentOptions,
        ClaudeSDKClient=FakeClaudeSDKClient,
    )
    monkeypatch.setitem(sys.modules, "claude_agent_sdk", fake_sdk)

    root = pathlib.Path(__file__).resolve().parents[3]
    shim_path = root / "deploy" / "sandbox-runtime" / "shim" / "agent_shim.py"
    spec = importlib.util.spec_from_file_location("cocola_agent_shim_status_test", shim_path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    emitted: list[dict] = []
    monkeypatch.setattr(module, "_emit", emitted.append)

    await module._run(
        {
            "prompt": "weather?",
            "skill_id": "weather",
            "mcp_servers": {"maps": {"type": "http", "url": "https://mcp.example.test/mcp"}},
        }
    )

    assert captured["prompt"] == "/weather\n\nweather?"
    assert calls[:2] == ["query", "status"]
    snapshots = [event for event in emitted if event.get("type") == "environment_status"]
    assert [snapshot["phase"] for snapshot in snapshots] == ["preparing", "ready"]
    assert snapshots[-1]["components"] == [
        {
            "kind": "mcp",
            "id": "maps",
            "label": "Amap",
            "status": "connected",
            "tool_count": 1,
        }
    ]
    assert emitted[-1]["type"] == "done"
    assert emitted[-1]["session_id"] == "claude-session"


async def test_agent_shim_skips_mcp_status_on_resumed_turn(monkeypatch):
    captured: dict[str, object] = {}

    class FakeClaudeAgentOptions:
        def __init__(self, **kwargs):
            captured["options"] = kwargs

    class UnexpectedClaudeSDKClient:
        def __init__(self, **_kwargs):
            raise AssertionError("resumed turns must use the one-shot SDK path")

    async def fake_query(*, prompt, options):
        captured["prompt"] = prompt
        captured["client_options"] = options
        result_type = type("ResultMessage", (), {})
        result = result_type()
        result.is_error = False
        result.num_turns = 1
        result.total_cost_usd = 0
        result.session_id = "claude-session"
        result.result = "done"
        yield result

    fake_sdk = types.SimpleNamespace(
        ClaudeAgentOptions=FakeClaudeAgentOptions,
        ClaudeSDKClient=UnexpectedClaudeSDKClient,
        query=fake_query,
    )
    monkeypatch.setitem(sys.modules, "claude_agent_sdk", fake_sdk)

    root = pathlib.Path(__file__).resolve().parents[3]
    shim_path = root / "deploy" / "sandbox-runtime" / "shim" / "agent_shim.py"
    spec = importlib.util.spec_from_file_location("cocola_agent_shim_resume_status_test", shim_path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    emitted: list[dict] = []
    monkeypatch.setattr(module, "_emit", emitted.append)

    await module._run(
        {
            "prompt": "and tomorrow?",
            "resume": "claude-session",
            "mcp_servers": {"maps": {"type": "http", "url": "https://mcp.example.test/mcp"}},
        }
    )

    assert captured["prompt"] == "and tomorrow?"
    assert captured["options"]["resume"] == "claude-session"
    assert not [event for event in emitted if event.get("type") == "environment_status"]
    assert emitted[-1]["type"] == "done"
    assert emitted[-1]["session_id"] == "claude-session"
