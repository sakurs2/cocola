#!/usr/bin/env python3
"""cocola in-sandbox stdio shim (Route A, see ADR-0009).

This process is the in-sandbox endpoint of the cocola control plane. It runs
*inside* the user's own container, where the Claude Code brain and hands both
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
    "prompt":        str,             # required, the user turn
    "system_prompt": str | null,      # optional
    "max_turns":     int | null,      # optional, default 20
    "resume":        str | null,      # optional session_id to --resume
    "cwd":           str | null,      # optional, default $COCOLA_WORKSPACE
    "permission_mode": str | null     # optional, default "bypassPermissions"
  }

Auth/routing come from the ENV the container was started with (injected by the
provider, ADR-0009 sec.2): ANTHROPIC_BASE_URL, ANTHROPIC_AUTH_TOKEN,
CLAUDE_CONFIG_DIR (-> hidden session-local Claude config). The shim does NOT read
credentials from the request, so they never transit the prompt channel.

The agent runs with the FULL native Claude Code toolset (no MCP forwarding, no
disallowed_tools): native Bash/Read/Write/Edit are isolated to this container
by construction. permission_mode defaults to "bypassPermissions" because there
is no interactive human in the sandbox to approve a prompt -- the security
boundary is the container + network egress allowlist, not a per-tool prompt.
"""

from __future__ import annotations

import asyncio
import json
import os
import sys
import time
from typing import Any


def _emit(obj: dict[str, Any]) -> None:
    """Write one compact NDJSON event line and flush immediately."""
    sys.stdout.write(json.dumps(obj, ensure_ascii=False, separators=(",", ":")) + "\n")
    sys.stdout.flush()


def _read_request() -> dict[str, Any]:
    raw = sys.stdin.read()
    if not raw.strip():
        raise ValueError("empty request on stdin")
    req = json.loads(raw)
    if not isinstance(req, dict):
        raise ValueError("request must be a JSON object")
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

    return claude_agent_sdk.ClaudeAgentOptions(**kwargs)


def _message_to_events(message: Any) -> list[dict[str, Any]]:
    """Map an SDK message to transport-neutral NDJSON events.

    Mirrors the taxonomy agent-runtime already uses (shim_provider.py),
    so the router relays these straight through to the gateway/web SSE.
    """
    events: list[dict[str, Any]] = []
    cls = type(message).__name__

    if cls == "AssistantMessage":
        for block in getattr(message, "content", []) or []:
            bcls = type(block).__name__
            if bcls == "TextBlock":
                events.append({"type": "text", "text": getattr(block, "text", "")})
            elif bcls == "ThinkingBlock":
                events.append({"type": "thinking", "text": getattr(block, "thinking", "")})
            elif bcls == "ToolUseBlock":
                events.append({
                    "type": "tool_use",
                    "name": getattr(block, "name", None),
                    "id": getattr(block, "id", None),
                    "input": getattr(block, "input", None),
                })
            elif bcls == "ToolResultBlock":
                events.append({
                    "type": "tool_result",
                    "tool_use_id": getattr(block, "tool_use_id", None),
                    "is_error": bool(getattr(block, "is_error", False)),
                })
    elif cls == "ResultMessage":
        events.append({
            "type": "result",
            "is_error": bool(getattr(message, "is_error", False)),
            "num_turns": getattr(message, "num_turns", None),
            "total_cost_usd": getattr(message, "total_cost_usd", None),
            "session_id": getattr(message, "session_id", None),
            "result": getattr(message, "result", None),
        })
    elif cls == "SystemMessage":
        events.append({"type": "system", "subtype": getattr(message, "subtype", None)})

    return events


async def _run(req: dict[str, Any]) -> int:
    import claude_agent_sdk

    options = _build_options(req)
    _emit({"type": "start", "ts": time.time()})

    last_session_id: str | None = None
    async for message in claude_agent_sdk.query(prompt=req["prompt"], options=options):
        for ev in _message_to_events(message):
            if ev.get("type") == "result" and ev.get("session_id"):
                last_session_id = ev["session_id"]
            _emit(ev)

    # The final done event carries the session_id so the caller can persist the
    # session<->sandbox binding and later --resume it.
    _emit({"type": "done", "session_id": last_session_id, "ts": time.time()})
    return 0


def _selfcheck() -> int:
    """Environment sanity probe used by the verification script.

    Reports the runtime facts the Route-A image must satisfy, as a single JSON
    line, WITHOUT importing the SDK or making any network call.
    """
    import shutil
    import subprocess

    info: dict[str, Any] = {
        "type": "selfcheck",
        "python": sys.version.split()[0],
        "node": None,
        "claude_cli": None,
        "claude_agent_sdk": None,
        "config_dir": os.environ.get("CLAUDE_CONFIG_DIR"),
        "workspace": os.environ.get("COCOLA_WORKSPACE"),
        "base_url_set": bool(os.environ.get("ANTHROPIC_BASE_URL")),
        "auth_token_set": bool(os.environ.get("ANTHROPIC_AUTH_TOKEN")),
    }
    if shutil.which("node"):
        try:
            info["node"] = subprocess.check_output(["node", "-v"], text=True).strip()
        except Exception as e:  # noqa: BLE001
            info["node"] = f"error: {e}"
    if shutil.which("claude"):
        try:
            info["claude_cli"] = subprocess.check_output(
                ["claude", "--version"], text=True
            ).strip()
        except Exception as e:  # noqa: BLE001
            info["claude_cli"] = f"error: {e}"
    try:
        import claude_agent_sdk  # noqa: F401

        info["claude_agent_sdk"] = getattr(
            claude_agent_sdk, "__version__", "installed"
        )
    except Exception as e:  # noqa: BLE001
        info["claude_agent_sdk"] = f"missing: {e}"

    _emit(info)
    ok = (
        info["node"]
        and not str(info["node"]).startswith("error")
        and info["claude_cli"]
        and not str(info["claude_cli"]).startswith("error")
        and not str(info["claude_agent_sdk"]).startswith("missing")
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
        _emit({"type": "error", "stage": "run", "error": str(e)})
        return 1


if __name__ == "__main__":
    sys.exit(main())
