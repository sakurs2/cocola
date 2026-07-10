from __future__ import annotations

import asyncio
import importlib.util
from pathlib import Path
from types import SimpleNamespace

import mcp
import mcp.client.sse
import mcp.client.stdio
import mcp.client.streamable_http
import pytest


def _load_shim():
    root = Path(__file__).resolve().parents[3]
    shim_path = root / "deploy" / "sandbox-runtime" / "shim" / "agent_shim.py"
    spec = importlib.util.spec_from_file_location("cocola_agent_shim_mcp_check", shim_path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class _ClientContext:
    def __init__(self, transport: str, calls: list[str]):
        self.transport = transport
        self.calls = calls

    async def __aenter__(self):
        self.calls.append(self.transport)
        return object(), object(), lambda: None

    async def __aexit__(self, *_args):
        return None


class _Session:
    def __init__(self, *_streams):
        pass

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_args):
        return None

    async def initialize(self):
        return SimpleNamespace(serverInfo=SimpleNamespace(name="demo", version="1.2.3"))

    async def list_tools(self):
        return SimpleNamespace(tools=[object(), object()])


@pytest.mark.parametrize(
    ("config", "expected_transport"),
    [
        ({"type": "stdio", "command": "demo", "args": ["--serve"]}, "stdio"),
        ({"type": "http", "url": "https://mcp.example.test/api?token=secret"}, "http"),
        ({"type": "sse", "url": "https://mcp.example.test/events"}, "sse"),
    ],
)
def test_mcp_check_initializes_and_lists_tools(monkeypatch, config, expected_transport):
    shim = _load_shim()
    calls: list[str] = []
    monkeypatch.setattr(mcp, "ClientSession", _Session)
    monkeypatch.setattr(
        mcp.client.stdio,
        "stdio_client",
        lambda *_args, **_kwargs: _ClientContext("stdio", calls),
    )
    monkeypatch.setattr(
        mcp.client.streamable_http,
        "streamablehttp_client",
        lambda *_args, **_kwargs: _ClientContext("http", calls),
    )
    monkeypatch.setattr(
        mcp.client.sse,
        "sse_client",
        lambda *_args, **_kwargs: _ClientContext("sse", calls),
    )

    result = asyncio.run(shim._initialize_mcp(config))

    assert calls == [expected_transport]
    assert result == {
        "status": "connected",
        "server_name": "demo",
        "server_version": "1.2.3",
        "tool_count": 2,
    }


def test_mcp_error_redacts_url_query_headers_and_env():
    shim = _load_shim()
    config = {
        "type": "http",
        "url": "https://user:pass@mcp.example.test/api?token=url-secret#private",
        "headers": {"Authorization": "Bearer header-secret"},
        "env": {"TOKEN": "env-secret"},
    }
    error = RuntimeError(
        "request to https://user:pass@mcp.example.test/api?token=url-secret#private "
        "with Bearer header-secret and env-secret failed"
    )

    message = shim._sanitize_mcp_error(error, config)

    assert message == (
        "RuntimeError: request to https://mcp.example.test/api with [redacted] "
        "and [redacted] failed"
    )
    assert "url-secret" not in message
    assert "header-secret" not in message
    assert "env-secret" not in message
    assert "user:pass" not in message

    runtime_message = shim._sanitize_agent_error(
        error,
        {"mcp_servers": {"remote": config}},
    )
    assert runtime_message == message
