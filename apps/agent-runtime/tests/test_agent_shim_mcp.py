"""Unit-test the Route-A sandbox shim option translation without real SDK calls."""

from __future__ import annotations

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
