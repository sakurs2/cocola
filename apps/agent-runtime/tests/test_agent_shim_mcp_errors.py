from __future__ import annotations

import importlib.util
from pathlib import Path


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

    assert shim._agent_error_code(error, {"resume": "stale"}) == "RESUME_NOT_FOUND"
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
