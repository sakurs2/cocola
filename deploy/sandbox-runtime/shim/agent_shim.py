#!/usr/bin/env python3
"""cocola in-sandbox Agent Runtime dispatcher (see ADR-0009 and ADR-0022).

This process is the in-sandbox endpoint of the cocola control plane. It runs
*inside* the user's own container, where the selected runtime and its tools
live. The control-plane router (agent-runtime) invokes it via:

    docker exec -i <ctr> /opt/cocola/shim/entrypoint.sh   # local Docker (M1)
    kubectl exec -i  <pod> -- /opt/cocola/shim/entrypoint.sh   # K8s+gVisor

Protocol -- deliberately STDIO, never a listening socket (cocola hard rule:
a sandbox must not bind a network port):

  stdin  : exactly one JSON object (a "Request", schema below), then EOF.
  stdout : a stream of NDJSON "events" -- one compact JSON object per line --
           terminated by a final {"type":"done", ...} line.
  stderr : human-readable diagnostics (never parsed by the caller).
  exit   : 0 on a clean run (even if the model erred -- that surfaces as an
           event), non-zero only on a shim-level failure.

Request schema:
  {
    "runtime_id":    str,             # claude-code | codex
    "prompt":        str,             # required, the user turn
    "skill_id":      str | null,      # optional effective skill selected for this turn
    "system_prompt": str | null,      # optional
    "max_turns":     int | null,      # optional, default 20
    "resume":        str | null,      # optional session_id to --resume
    "cwd":           str | null,      # optional, default $COCOLA_WORKSPACE
    "permission_mode": str | null,    # optional, default "bypassPermissions"
    "mcp_servers":   object | null    # optional runtime MCP configuration
  }

Auth/routing come from the exec environment injected by the provider. The shim
does not read credentials from the JSON request, so they never transit the
prompt channel or logs.

The agent runs with the FULL native Claude Code toolset (no MCP forwarding, no
disallowed_tools): native Bash/Read/Write/Edit are isolated to this container
by construction. permission_mode defaults to "bypassPermissions" because there
is no interactive human in the sandbox to approve a prompt -- the security
boundary is the container + network egress allowlist, not a per-tool prompt.
"""

from __future__ import annotations

import asyncio
import contextlib
import json
import os
import re
import signal
import sys
import time
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit, urlunsplit


def _emit(obj: dict[str, Any]) -> None:
    """Write one compact NDJSON event line and flush immediately."""
    sys.stdout.write(json.dumps(obj, ensure_ascii=False, separators=(",", ":")) + "\n")
    sys.stdout.flush()


def _read_json_object() -> dict[str, Any]:
    raw = sys.stdin.read()
    if not raw.strip():
        raise ValueError("empty request on stdin")
    req = json.loads(raw)
    if not isinstance(req, dict):
        raise ValueError("request must be a JSON object")
    return req


def _read_request() -> dict[str, Any]:
    req = _read_json_object()
    if not req.get("prompt"):
        raise ValueError("request.prompt is required")
    return req


def _build_options(req: dict[str, Any]):
    """Translate a Request into ClaudeAgentOptions.

    Imported lazily so `--selfcheck` can run without the SDK installed.
    """
    import claude_agent_sdk

    cwd = req.get("cwd") or os.environ.get("COCOLA_WORKSPACE") or os.getcwd()
    permission_mode = req.get("permission_mode") or "bypassPermissions"

    kwargs: dict[str, Any] = dict(
        max_turns=int(req.get("max_turns") or 20),
        cwd=cwd,
        permission_mode=permission_mode,
    )

    # The renamed Claude *Agent* SDK (>=0.2) no longer enables Claude Code's
    # default behaviour implicitly: when `tools`/`system_prompt` are left unset
    # it passes `--system-prompt ""` and ships *no* tool preset, so the model
    # loses both its native Bash/Read/Write toolset and the `<env>` block that
    # tells it the real cwd. That regression is exactly what produced the
    # "I don't have a Bash tool available" reply and the hallucinated host cwd.
    # Opt back into the `claude_code` presets so the in-sandbox agent has hands.
    kwargs["tools"] = {"type": "preset", "preset": "claude_code"}
    kwargs["setting_sources"] = ["user", "project"]
    kwargs["skills"] = "all"

    # System prompt: default to Claude Code's preset (which injects the live
    # <env> block, incl. the genuine /workspace cwd). A caller-supplied prompt
    # is *appended* to the preset rather than replacing it, so callers can add
    # instructions without amputating the agent's environment awareness.
    caller_prompt = req.get("system_prompt")
    if caller_prompt:
        kwargs["system_prompt"] = {
            "type": "preset",
            "preset": "claude_code",
            "append": caller_prompt,
        }
    else:
        kwargs["system_prompt"] = {"type": "preset", "preset": "claude_code"}
    # Resume rebuilds the brain from the on-disk session (persisted under the
    # session workspace), which is how a hibernated sandbox restores state
    # without a RAM snapshot.
    if req.get("resume"):
        kwargs["resume"] = req["resume"]
    if isinstance(req.get("mcp_servers"), dict) and req["mcp_servers"]:
        kwargs["mcp_servers"] = req["mcp_servers"]
        kwargs["strict_mcp_config"] = True

    return claude_agent_sdk.ClaudeAgentOptions(**kwargs)


def _claude_prompt(req: dict[str, Any]) -> str:
    prompt = str(req["prompt"])
    skill_id = str(req.get("skill_id") or "").strip()
    return f"/{skill_id}\n\n{prompt}" if skill_id else prompt


# Cap on tool_result content forwarded to the UI. Tool outputs (Read of a big
# file, Bash flooding stdout) can be huge; the browser only needs enough to
# render a status node or a search-result list, so we truncate hard here to keep
# the SSE stream and client memory bounded.
_TOOL_RESULT_MAX_CHARS = 4000
_MCP_STATUS_TIMEOUT_SECONDS = 8.0
_MCP_STATUS_POLL_SECONDS = (0.5, 1.0, 2.0)
_MCP_TERMINAL_STATUSES = {"connected", "failed", "needs-auth", "disabled"}


def _tool_result_content(content: Any) -> str:
    """Flatten an SDK ToolResultBlock.content to a bounded string.

    content is ``str | list[dict] | None``. Web search / fetch return a list of
    content blocks; everything else is usually a plain string. We JSON-encode
    lists so the browser can parse structured results, pass strings through, and
    truncate either form to _TOOL_RESULT_MAX_CHARS.
    """
    if content is None:
        return ""
    if isinstance(content, str):
        text = content
    else:
        try:
            text = json.dumps(content, ensure_ascii=False, default=str)
        except (TypeError, ValueError):
            text = str(content)
    if len(text) > _TOOL_RESULT_MAX_CHARS:
        return text[:_TOOL_RESULT_MAX_CHARS] + "…[truncated]"
    return text


def _block_to_event(block: Any) -> dict[str, Any] | None:
    """Map one SDK content block to a transport event, or None to skip.

    Handles both client-side tools (ToolUseBlock/ToolResultBlock) and the
    server-side variants (ServerToolUseBlock/ServerToolResultBlock, used by
    web_search/web_fetch). Crucially, ToolResultBlock arrives inside a
    UserMessage: the SDK synthesizes a user turn to carry the result back
    to the model, so this mapper is applied to UserMessage content too.
    Without that, tool_result events never reach the UI and a tool node
    spinner never stops even after the model has moved on.
    """
    bcls = type(block).__name__
    if bcls == "TextBlock":
        return {"type": "text", "text": getattr(block, "text", "")}
    if bcls == "ThinkingBlock":
        return {"type": "thinking", "text": getattr(block, "thinking", "")}
    if bcls in ("ToolUseBlock", "ServerToolUseBlock"):
        return {
            "type": "tool_use",
            "name": getattr(block, "name", None),
            "id": getattr(block, "id", None),
            "input": getattr(block, "input", None),
        }
    if bcls in ("ToolResultBlock", "ServerToolResultBlock"):
        return {
            "type": "tool_result",
            "tool_use_id": getattr(block, "tool_use_id", None),
            "is_error": bool(getattr(block, "is_error", False)),
            "content": _tool_result_content(getattr(block, "content", None)),
        }
    return None


def _message_to_events(message: Any) -> list[dict[str, Any]]:
    """Map an SDK message to transport-neutral NDJSON events.

    Mirrors the taxonomy agent-runtime already uses (shim_provider.py),
    so the router relays these straight through to the gateway/web SSE.
    """
    events: list[dict[str, Any]] = []
    cls = type(message).__name__

    if cls in ("AssistantMessage", "UserMessage"):
        # UserMessage.content may be a bare string (the human prompt): skip
        # that; only its block list carries tool results worth relaying.
        content = getattr(message, "content", None)
        if isinstance(content, list):
            for block in content:
                ev = _block_to_event(block)
                if ev is not None:
                    events.append(ev)
    elif cls == "ResultMessage":
        events.append(
            {
                "type": "result",
                "is_error": bool(getattr(message, "is_error", False)),
                "num_turns": getattr(message, "num_turns", None),
                "total_cost_usd": getattr(message, "total_cost_usd", None),
                "session_id": getattr(message, "session_id", None),
                "result": getattr(message, "result", None),
            }
        )
    elif cls == "SystemMessage":
        events.append({"type": "system", "subtype": getattr(message, "subtype", None)})

    return events


def _mcp_configs(req: dict[str, Any]) -> dict[str, dict[str, Any]]:
    servers = req.get("mcp_servers")
    if not isinstance(servers, dict):
        return {}
    return {
        str(name): config
        for name, config in servers.items()
        if isinstance(name, str) and isinstance(config, dict)
    }


def _environment_status_event(
    req: dict[str, Any],
    result: dict[str, Any] | None = None,
    *,
    timed_out: bool = False,
    default_status: str = "pending",
    status_overrides: dict[str, str] | None = None,
) -> dict[str, Any]:
    """Build one secret-safe, idempotent session environment snapshot."""
    configs = _mcp_configs(req)
    statuses = {
        str(server.get("name") or ""): server
        for server in (result or {}).get("mcpServers", [])
        if isinstance(server, dict)
    }
    components: list[dict[str, Any]] = []
    for name, config in sorted(configs.items()):
        server = statuses.get(name, {})
        status = str(server.get("status") or (status_overrides or {}).get(name) or default_status)
        if timed_out and status == "pending":
            status = "timeout"
        info = server.get("serverInfo") if isinstance(server.get("serverInfo"), dict) else {}
        tools = server.get("tools") if isinstance(server.get("tools"), list) else []
        component: dict[str, Any] = {
            "kind": "mcp",
            "id": name,
            "label": str(info.get("name") or name),
            "status": status,
            "tool_count": len(tools),
        }
        error = str(server.get("error") or "").strip()
        if error:
            component["error"] = _redact_mcp_message(error, config)[:500]
        components.append(component)

    statuses_seen = {str(component["status"]) for component in components}
    if statuses_seen & {"failed", "needs-auth", "timeout"}:
        phase = "degraded"
    elif "pending" in statuses_seen:
        phase = "preparing"
    else:
        phase = "ready"
    return {
        "type": "environment_status",
        "version": 1,
        "phase": phase,
        "components": components,
        "ts": time.time(),
    }


async def _watch_mcp_status(client: Any, req: dict[str, Any]) -> None:
    """Observe MCP startup until it reaches a terminal state or a short deadline."""
    if not _mcp_configs(req):
        return
    deadline = time.monotonic() + _MCP_STATUS_TIMEOUT_SECONDS
    last_snapshot = ""
    last_result: dict[str, Any] = {}
    poll_index = 0
    try:
        while True:
            try:
                result = await client.get_mcp_status()
            except Exception as error:  # noqa: BLE001 - status is best-effort per turn
                result = {
                    "mcpServers": [
                        {"name": name, "status": "failed", "error": str(error)}
                        for name in _mcp_configs(req)
                    ]
                }
            last_result = result
            snapshot = _environment_status_event(req, result)
            serialized = json.dumps(
                {"phase": snapshot["phase"], "components": snapshot["components"]},
                sort_keys=True,
                default=str,
            )
            if serialized != last_snapshot:
                _emit(snapshot)
                last_snapshot = serialized
            statuses = {str(component.get("status") or "") for component in snapshot["components"]}
            if statuses and statuses <= _MCP_TERMINAL_STATUSES:
                return
            if time.monotonic() >= deadline:
                _emit(_environment_status_event(req, result, timed_out=True))
                return
            delay = _MCP_STATUS_POLL_SECONDS[min(poll_index, len(_MCP_STATUS_POLL_SECONDS) - 1)]
            poll_index += 1
            await asyncio.sleep(min(delay, max(deadline - time.monotonic(), 0)))
    except asyncio.CancelledError:
        _emit(_environment_status_event(req, last_result, timed_out=True))
        raise


async def _run_claude(req: dict[str, Any]) -> int:
    import claude_agent_sdk

    options = _build_options(req)
    prompt = _claude_prompt(req)
    _emit({"type": "start", "ts": time.time()})

    last_session_id: str | None = None

    async def relay(messages: Any) -> None:
        nonlocal last_session_id
        async for message in messages:
            for ev in _message_to_events(message):
                if ev.get("type") == "result" and ev.get("session_id"):
                    last_session_id = ev["session_id"]
                _emit(ev)

    if req.get("resume"):
        await relay(claude_agent_sdk.query(prompt=prompt, options=options))
    else:
        _emit(_environment_status_event(req))
        status_task: asyncio.Task[None] | None = None
        async with claude_agent_sdk.ClaudeSDKClient(options=options) as client:
            if _mcp_configs(req):
                status_task = asyncio.create_task(_watch_mcp_status(client, req))
            try:
                await client.query(prompt)
                await relay(client.receive_response())
            finally:
                if status_task is not None:
                    if not status_task.done():
                        status_task.cancel()
                    await asyncio.gather(status_task, return_exceptions=True)

    # The final done event carries the session_id so the caller can persist the
    # session<->sandbox binding and later --resume it.
    _emit({"type": "done", "session_id": last_session_id, "ts": time.time()})
    return 0


async def _run_codex(req: dict[str, Any]) -> int:
    """Run the Node Codex adapter while preserving the shim's NDJSON stream."""
    report_environment = not req.get("resume")
    mcp_configs = _mcp_configs(req)
    connected_mcp_servers: set[str] = set()
    if report_environment:
        _emit(_environment_status_event(req, default_status="configured"))

    process = await asyncio.create_subprocess_exec(
        "node",
        "/opt/cocola/shim/codex_adapter.mjs",
        stdin=asyncio.subprocess.PIPE,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
        start_new_session=True,
    )
    assert process.stdin is not None
    assert process.stdout is not None
    assert process.stderr is not None
    stderr_tail = ""

    async def drain_stderr() -> None:
        nonlocal stderr_tail
        while chunk := await process.stderr.read(1024):
            stderr_tail = (stderr_tail + chunk.decode(errors="replace"))[-4000:]

    stderr_task = asyncio.create_task(drain_stderr())
    loop = asyncio.get_running_loop()
    current_task = asyncio.current_task()
    installed_signals: list[signal.Signals] = []
    if current_task is not None:
        for sig in (signal.SIGTERM, signal.SIGINT):
            with contextlib.suppress(NotImplementedError, RuntimeError):
                loop.add_signal_handler(sig, current_task.cancel)
                installed_signals.append(sig)

    async def terminate() -> None:
        if process.returncode is not None:
            return
        with contextlib.suppress(ProcessLookupError):
            os.killpg(process.pid, signal.SIGTERM)
        try:
            await asyncio.wait_for(process.wait(), timeout=3)
        except TimeoutError:
            with contextlib.suppress(ProcessLookupError):
                os.killpg(process.pid, signal.SIGKILL)
            await process.wait()

    emitted_error = False
    try:
        process.stdin.write(json.dumps(req, ensure_ascii=False).encode())
        await process.stdin.drain()
        process.stdin.close()
        while line := await process.stdout.readline():
            event = json.loads(line)
            mcp_server = str(event.pop("_cocola_mcp_server", ""))
            if (
                report_environment
                and mcp_server in mcp_configs
                and mcp_server not in connected_mcp_servers
            ):
                connected_mcp_servers.add(mcp_server)
                _emit(
                    _environment_status_event(
                        req,
                        default_status="configured",
                        status_overrides={name: "connected" for name in connected_mcp_servers},
                    )
                )
            emitted_error = emitted_error or event.get("type") == "error"
            _emit(event)
        code = await process.wait()
        await stderr_task
    except asyncio.CancelledError:
        await terminate()
        raise
    except BaseException:
        await terminate()
        raise
    finally:
        for sig in installed_signals:
            loop.remove_signal_handler(sig)
        if not stderr_task.done():
            stderr_task.cancel()
        await asyncio.gather(stderr_task, return_exceptions=True)
    if code != 0 and not emitted_error:
        _emit({"type": "error", "stage": "run", "error": stderr_tail[:500] or "Codex failed"})
    return code


async def _run(req: dict[str, Any]) -> int:
    runtime_id = str(req.get("runtime_id") or "claude-code")
    adapters = {
        "claude-code": _run_claude,
        "codex": _run_codex,
    }
    adapter = adapters.get(runtime_id)
    if adapter is None:
        _emit(
            {
                "type": "error",
                "stage": "prepare",
                "code": "UNSUPPORTED_RUNTIME",
                "error": "Agent Runtime is not supported",
            }
        )
        return 2
    return await adapter(req)


def _safe_remote_url(raw_url: str) -> str:
    try:
        parsed = urlsplit(raw_url)
    except ValueError:
        return "remote MCP server"
    if not parsed.scheme or not parsed.hostname:
        return "remote MCP server"
    host = parsed.hostname
    if ":" in host and not host.startswith("["):
        host = f"[{host}]"
    try:
        port = parsed.port
    except ValueError:
        return "remote MCP server"
    if port:
        host = f"{host}:{port}"
    return urlunsplit((parsed.scheme, host, parsed.path, "", ""))


def _redact_mcp_message(message: str, config: dict[str, Any]) -> str:
    raw_url = str(config.get("url") or "")
    replacements: list[tuple[str, str]] = []
    if raw_url:
        replacements.append((raw_url, _safe_remote_url(raw_url)))
    for field in ("headers", "env"):
        values = config.get(field)
        if isinstance(values, dict):
            for value in values.values():
                secret = str(value or "")
                if secret:
                    replacements.append((secret, "[redacted]"))
    for secret, replacement in sorted(replacements, key=lambda item: len(item[0]), reverse=True):
        message = message.replace(secret, replacement)

    # Libraries may normalize or shorten a URL before including it in an
    # exception. Strip credentials, query and fragment from any remaining URL.
    message = re.sub(
        r"https?://[^\s\"'<>]+",
        lambda match: _safe_remote_url(match.group(0)),
        message,
    )
    return message


def _exception_detail(error: BaseException) -> str:
    """Return the first useful leaf instead of a TaskGroup wrapper."""
    leaves = _exception_leaves(error)
    fallback = f"{type(error).__name__}: {error}"
    for current in leaves:
        detail = str(current).strip()
        if detail:
            return f"{type(current).__name__}: {detail}"
        fallback = type(current).__name__
    return fallback


def _exception_leaves(error: BaseException) -> list[BaseException]:
    pending: list[BaseException] = [error]
    leaves: list[BaseException] = []
    while pending:
        current = pending.pop(0)
        if isinstance(current, BaseExceptionGroup):
            pending[0:0] = list(current.exceptions)
        else:
            leaves.append(current)
    return leaves


_RESUME_NOT_FOUND_MARKERS = (
    "no conversation found with session id",
    "no conversation found",
    "session id not found",
    "could not find session",
    "no session found",
    "session not found",
)


def _agent_error_code(error: BaseException, req: dict[str, Any]) -> str:
    """Normalize SDK compatibility errors at the shim protocol boundary."""
    if not req.get("resume"):
        return ""
    for leaf in _exception_leaves(error):
        if type(leaf).__name__ != "ProcessError":
            continue
        detail = f"{leaf}\n{getattr(leaf, 'stderr', '')}".lower()
        if any(marker in detail for marker in _RESUME_NOT_FOUND_MARKERS):
            return "SESSION_NOT_FOUND"
    return ""


def _sanitize_agent_error(error: Exception, req: dict[str, Any]) -> str:
    message = _exception_detail(error)
    servers = req.get("mcp_servers")
    if isinstance(servers, dict):
        for config in servers.values():
            if isinstance(config, dict):
                message = _redact_mcp_message(message, config)
    return message[:500]


def _selfcheck() -> int:
    """Environment sanity probe used by the verification script.

    Reports the runtime facts the image must satisfy as a single JSON line,
    without making any network call.
    """
    import shutil
    import subprocess

    def cmd_version(*cmd: str) -> str:
        exe = cmd[0]
        if not shutil.which(exe):
            return "missing"
        try:
            out = subprocess.check_output(list(cmd), text=True, stderr=subprocess.STDOUT).strip()
        except Exception as e:  # noqa: BLE001
            return f"error: {e}"
        return out.splitlines()[0] if out else "installed"

    info: dict[str, Any] = {
        "type": "selfcheck",
        "python": sys.version.split()[0],
        "node": None,
        "claude_cli": None,
        "claude_agent_sdk": None,
        "codex_cli": cmd_version("codex", "--version"),
        "codex_sdk": None,
        "pnpm": cmd_version("pnpm", "--version"),
        "yarn": cmd_version("yarn", "--version"),
        "playwright": cmd_version("playwright", "--version"),
        "chromium": cmd_version("chromium", "--version"),
        "fd": cmd_version("fd", "--version"),
        "jq": cmd_version("jq", "--version"),
        "yq": cmd_version("yq", "--version"),
        "tree": cmd_version("tree", "--version"),
        "file": cmd_version("file", "--version"),
        "make": cmd_version("make", "--version"),
        "imagemagick": cmd_version("convert", "-version"),
        "pdftotext": cmd_version("pdftotext", "-v"),
        "rsvg_convert": cmd_version("rsvg-convert", "--version"),
        "config_dir": os.environ.get("CLAUDE_CONFIG_DIR"),
        "codex_home": os.environ.get("CODEX_HOME"),
        "workspace": os.environ.get("COCOLA_WORKSPACE"),
        "base_url_set": bool(os.environ.get("ANTHROPIC_BASE_URL")),
        "responses_base_url_set": bool(os.environ.get("COCOLA_LLM_BASE_URL")),
        "auth_token_set": bool(os.environ.get("ANTHROPIC_AUTH_TOKEN")),
        "codex_auth_token_set": bool(os.environ.get("CODEX_API_KEY")),
    }
    if shutil.which("node"):
        try:
            info["node"] = subprocess.check_output(["node", "-v"], text=True).strip()
        except Exception as e:  # noqa: BLE001
            info["node"] = f"error: {e}"
    if shutil.which("claude"):
        try:
            info["claude_cli"] = subprocess.check_output(["claude", "--version"], text=True).strip()
        except Exception as e:  # noqa: BLE001
            info["claude_cli"] = f"error: {e}"
    try:
        import claude_agent_sdk  # noqa: F401

        info["claude_agent_sdk"] = getattr(claude_agent_sdk, "__version__", "installed")
    except Exception as e:  # noqa: BLE001
        info["claude_agent_sdk"] = f"missing: {e}"
    try:
        package = Path("/opt/cocola/node_modules/@openai/codex-sdk/package.json")
        info["codex_sdk"] = str(json.loads(package.read_text())["version"])
    except Exception as e:  # noqa: BLE001
        info["codex_sdk"] = f"missing: {e}"

    _emit(info)
    required_tools = [
        "pnpm",
        "yarn",
        "playwright",
        "chromium",
        "fd",
        "jq",
        "yq",
        "tree",
        "file",
        "make",
        "imagemagick",
        "pdftotext",
        "rsvg_convert",
    ]
    ok = (
        info["node"]
        and not str(info["node"]).startswith("error")
        and info["claude_cli"]
        and not str(info["claude_cli"]).startswith("error")
        and not str(info["claude_agent_sdk"]).startswith("missing")
        and not str(info["codex_cli"]).startswith(("missing", "error"))
        and not str(info["codex_sdk"]).startswith("missing")
        and all(not str(info[name]).startswith(("missing", "error")) for name in required_tools)
    )
    return 0 if ok else 1


def main() -> int:
    if "--selfcheck" in sys.argv[1:]:
        return _selfcheck()
    try:
        req = _read_request()
    except Exception as e:  # noqa: BLE001
        _emit({"type": "error", "stage": "request", "error": str(e)})
        return 2
    try:
        return asyncio.run(_run(req))
    except Exception as e:  # noqa: BLE001
        event = {"type": "error", "stage": "run", "error": _sanitize_agent_error(e, req)}
        if code := _agent_error_code(e, req):
            event["code"] = code
        _emit(event)
        return 1


if __name__ == "__main__":
    sys.exit(main())
