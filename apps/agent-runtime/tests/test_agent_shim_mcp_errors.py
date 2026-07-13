from __future__ import annotations

import asyncio
import importlib.util
import signal
from pathlib import Path

import pytest


def _load_shim():
    root = Path(__file__).resolve().parents[3]
    shim_path = root / "deploy" / "sandbox-runtime" / "shim" / "agent_shim.py"
    spec = importlib.util.spec_from_file_location("cocola_agent_shim_mcp_errors", shim_path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_agent_error_redacts_mcp_url_headers_and_env():
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

    message = shim._sanitize_agent_error(
        error,
        {"mcp_servers": {"remote": config}},
    )

    assert message == (
        "RuntimeError: request to https://mcp.example.test/api with [redacted] "
        "and [redacted] failed"
    )
    assert "url-secret" not in message
    assert "header-secret" not in message
    assert "env-secret" not in message
    assert "user:pass" not in message


def test_agent_error_unwraps_nested_exception_group_and_redacts_secrets():
    shim = _load_shim()
    config = {
        "type": "http",
        "url": "https://mcp.example.test/api?token=url-secret",
        "headers": {"Authorization": "Bearer header-secret"},
    }
    error = ExceptionGroup(
        "unhandled errors in a TaskGroup",
        [
            ExceptionGroup(
                "request failed",
                [
                    RuntimeError(
                        "HTTP 401 from https://mcp.example.test/api?token=url-secret "
                        "using Bearer header-secret"
                    )
                ],
            )
        ],
    )

    message = shim._sanitize_agent_error(
        error,
        {"mcp_servers": {"remote": config}},
    )

    assert message == ("RuntimeError: HTTP 401 from https://mcp.example.test/api using [redacted]")
    assert "ExceptionGroup" not in message
    assert "url-secret" not in message
    assert "header-secret" not in message


def test_resume_not_found_is_classified_only_for_sdk_process_errors():
    shim = _load_shim()

    class ProcessError(Exception):
        def __init__(self, message: str):
            self.stderr = message
            super().__init__(message)

    error = ProcessError("No conversation found with session ID: stale")

    assert shim._agent_error_code(error, {"resume": "stale"}) == "SESSION_NOT_FOUND"
    assert shim._agent_error_code(error, {}) == ""
    assert shim._agent_error_code(RuntimeError(str(error)), {"resume": "stale"}) == ""


def test_environment_status_redacts_failed_mcp_and_marks_timeout():
    shim = _load_shim()
    request = {
        "mcp_servers": {
            "remote": {
                "type": "http",
                "url": "https://user:pass@mcp.example.test/api?token=url-secret",
                "headers": {"Authorization": "Bearer header-secret"},
            }
        }
    }
    failed = shim._environment_status_event(
        request,
        {
            "mcpServers": [
                {
                    "name": "remote",
                    "status": "failed",
                    "error": (
                        "HTTP 401 from https://user:pass@mcp.example.test/api?token=url-secret "
                        "using Bearer header-secret"
                    ),
                }
            ]
        },
    )

    assert failed["phase"] == "degraded"
    assert failed["components"][0]["error"] == (
        "HTTP 401 from https://mcp.example.test/api using [redacted]"
    )
    assert "url-secret" not in str(failed)
    assert "header-secret" not in str(failed)
    assert "user:pass" not in str(failed)

    timed_out = shim._environment_status_event(request, timed_out=True)
    assert timed_out["phase"] == "degraded"
    assert timed_out["components"][0]["status"] == "timeout"


async def test_unknown_runtime_is_rejected_instead_of_falling_back_to_claude(monkeypatch):
    shim = _load_shim()
    events: list[dict] = []
    monkeypatch.setattr(shim, "_emit", events.append)

    exit_code = await shim._run({"runtime_id": "unknown", "prompt": "hello"})

    assert exit_code == 2
    assert events == [
        {
            "type": "error",
            "stage": "prepare",
            "code": "UNSUPPORTED_RUNTIME",
            "error": "Agent Runtime is not supported",
        }
    ]


async def test_codex_reports_configured_mcp_then_verifies_it_on_first_use(monkeypatch):
    shim = _load_shim()
    emitted: list[dict] = []

    class Stdin:
        def write(self, data):
            assert data

        async def drain(self):
            return None

        def close(self):
            return None

    class Stdout:
        def __init__(self):
            self.lines = [
                b'{"type":"start","session_id":"thread-1"}\n',
                (
                    b'{"type":"tool_use","id":"tool-1","name":"github.search",'
                    b'"input":{},"_cocola_mcp_server":"github"}\n'
                ),
                b'{"type":"done","session_id":"thread-1"}\n',
            ]

        async def readline(self):
            return self.lines.pop(0) if self.lines else b""

    class Stderr:
        async def read(self, size):
            return b""

    class Process:
        pid = 321
        returncode = 0
        stdin = Stdin()
        stdout = Stdout()
        stderr = Stderr()

        async def wait(self):
            return self.returncode

    async def create_process(*args, **kwargs):
        assert kwargs["start_new_session"] is True
        return Process()

    monkeypatch.setattr(shim.asyncio, "create_subprocess_exec", create_process)
    monkeypatch.setattr(shim, "_emit", emitted.append)

    exit_code = await shim._run_codex(
        {
            "prompt": "search repositories",
            "mcp_servers": {
                "github": {
                    "type": "stdio",
                    "command": "github-mcp-server",
                }
            },
        }
    )

    assert exit_code == 0
    assert emitted[0]["phase"] == "ready"
    assert emitted[0]["components"] == [
        {
            "kind": "mcp",
            "id": "github",
            "label": "github",
            "status": "configured",
            "tool_count": 0,
        }
    ]
    assert emitted[2]["components"][0]["status"] == "connected"
    assert emitted[3] == {
        "type": "tool_use",
        "id": "tool-1",
        "name": "github.search",
        "input": {},
    }
    assert "_cocola_mcp_server" not in emitted[3]


async def test_codex_cancellation_terminates_the_child_process_group(monkeypatch):
    shim = _load_shim()
    stopped = asyncio.Event()
    killed: list[tuple[int, signal.Signals]] = []

    class Stdin:
        def write(self, data):
            assert data

        async def drain(self):
            await asyncio.sleep(0)

        def close(self):
            return None

    class Reader:
        async def readline(self):
            await asyncio.Future()

        async def read(self, size):
            await asyncio.Future()

    class Process:
        pid = 321
        returncode = None
        stdin = Stdin()
        stdout = Reader()
        stderr = Reader()

        async def wait(self):
            await stopped.wait()
            return self.returncode

    process = Process()

    async def create_process(*args, **kwargs):
        assert kwargs["start_new_session"] is True
        return process

    def killpg(pid, sig):
        killed.append((pid, sig))
        process.returncode = -int(sig)
        stopped.set()

    monkeypatch.setattr(shim.asyncio, "create_subprocess_exec", create_process)
    monkeypatch.setattr(shim.os, "killpg", killpg)

    task = asyncio.create_task(shim._run_codex({"prompt": "long task"}))
    await asyncio.sleep(0)
    await asyncio.sleep(0)
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task

    assert killed == [(321, signal.SIGTERM)]
