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
    "cwd":           str | null,      # optional, default $COCOLA_AGENT_CWD
    "permission_mode": str | null,    # optional, default "bypassPermissions"
    "mcp_servers":   object | null,   # optional runtime MCP configuration
    "result_contract": object | null, # selected Skill result contract
    "result_policy": str | null       # none | required | optional
  }

Auth/routing come from the exec environment injected by the provider. The shim
does not read credentials from the JSON request, so they never transit the
prompt channel or logs.

Execute runs use the full native Claude Code toolset inside the container.
Plan runs keep Claude's native read-only permission mode, remove user MCP
servers, disable write/subagent tools, and install one trusted in-process Cocola
control server. permission_mode defaults to "bypassPermissions" because Execute
runs have no interactive permission prompt; their security boundary is the
container plus the network egress allowlist.
"""

from __future__ import annotations

import asyncio
import contextlib
import hashlib
import json
import os
import re
import signal
import sys
import time
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit, urlunsplit

from jsonschema import Draft202012Validator
from jsonschema.exceptions import SchemaError, ValidationError


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


def _build_options(
    req: dict[str, Any],
    *,
    run_control: _CocolaRunControl | None = None,
):
    """Translate a Request into ClaudeAgentOptions.

    Imported lazily so `--selfcheck` can run without the SDK installed.
    """
    import claude_agent_sdk

    cwd = (
        req.get("cwd")
        or os.environ.get("COCOLA_AGENT_CWD")
        or os.environ.get("COCOLA_WORKSPACE")
        or os.getcwd()
    )
    permission_mode = req.get("permission_mode") or "bypassPermissions"
    plan_mode = permission_mode == "plan"

    kwargs: dict[str, Any] = dict(
        max_turns=int(req.get("max_turns") or 20),
        cwd=cwd,
        permission_mode=permission_mode,
        disallowed_tools=["AskUserQuestion"],
    )

    # The renamed Claude *Agent* SDK (>=0.2) no longer enables Claude Code's
    # default behaviour implicitly: when `tools`/`system_prompt` are left unset
    # it passes `--system-prompt ""` and ships *no* tool preset, so the model
    # loses both its native Bash/Read/Write toolset and the `<env>` block that
    # tells it the real cwd. That regression is exactly what produced the
    # "I don't have a Bash tool available" reply and the hallucinated host cwd.
    # Opt back into the `claude_code` presets so the in-sandbox agent has hands.
    kwargs["tools"] = {"type": "preset", "preset": "claude_code"}
    # Plan Mode must not load workspace-controlled settings because those
    # settings can register hooks or MCP servers with external side effects.
    kwargs["setting_sources"] = [] if plan_mode else ["user", "project"]
    kwargs["skills"] = "all"

    # System prompt: default to Claude Code's preset (which injects the live
    # <env> block, including the genuine current cwd). A caller-supplied prompt
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
    control = run_control
    if control is not None:
        mcp_servers = (
            {}
            if plan_mode
            else dict(req["mcp_servers"])
            if isinstance(req.get("mcp_servers"), dict)
            else {}
        )
        mcp_servers[_PLAN_CONTROL_SERVER] = control.sdk_server(claude_agent_sdk)
        kwargs["mcp_servers"] = mcp_servers
        kwargs["strict_mcp_config"] = True
        kwargs["hooks"] = control.sdk_hooks(claude_agent_sdk)
        if plan_mode:
            kwargs["allowed_tools"] = sorted(control.tool_names)
            kwargs["disallowed_tools"] = list(_PLAN_DISALLOWED_TOOLS)
            kwargs["can_use_tool"] = control.can_use_tool
    elif isinstance(req.get("mcp_servers"), dict) and req["mcp_servers"]:
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
_PLAN_MAX_BYTES = 128 * 1024
_PLAN_INVALID_ERROR = "Claude did not return a reviewable plan. Refine the request and try again."
_PLAN_CONTROL_SERVER = "cocola_control"
_PLAN_CONTROL_TOOLS = (
    "cocola_submit_plan",
    "cocola_request_user_input",
    "cocola_get_runtime_info",
)
_RESULT_CONTROL_TOOL = "cocola_submit_result"
_BUILTIN_RESULT_TOOL_RENDERERS = {
    "cocola_present_summary": "summary",
    "cocola_present_table": "table",
    "cocola_present_list": "list",
    "cocola_present_metrics": "metrics",
}
_BUILTIN_RESULT_TOOL_DESCRIPTIONS = {
    "cocola_present_summary": (
        "Present a compact set of labeled facts when a card is clearer than prose, "
        "then stop this run."
    ),
    "cocola_present_table": (
        "Present comparable records as rows and columns when a table is clearer than prose, "
        "then stop this run."
    ),
    "cocola_present_list": (
        "Present a homogeneous collection of items when a list card is clearer than prose, "
        "then stop this run."
    ),
    "cocola_present_metrics": (
        "Present labeled measurements when a metrics card is clearer than prose, "
        "then stop this run."
    ),
}
_RESULT_CONTROL_TOOLS = (
    _RESULT_CONTROL_TOOL,
    *_BUILTIN_RESULT_TOOL_RENDERERS,
)
_PLAN_CONTROL_TOOL_NAMES = tuple(
    f"mcp__{_PLAN_CONTROL_SERVER}__{name}" for name in _PLAN_CONTROL_TOOLS
)
_TERMINAL_CONTROL_TOOL_NAMES = frozenset(
    {
        f"mcp__{_PLAN_CONTROL_SERVER}__cocola_submit_plan",
        f"mcp__{_PLAN_CONTROL_SERVER}__cocola_request_user_input",
        *(f"mcp__{_PLAN_CONTROL_SERVER}__{tool_name}" for tool_name in _RESULT_CONTROL_TOOLS),
    }
)
_PLAN_DISALLOWED_TOOLS = (
    "ExitPlanMode",
    "AskUserQuestion",
    "Write",
    "Edit",
    "MultiEdit",
    "NotebookEdit",
    "Agent",
    "Task",
)
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


class _ClaudeTaskProgress:
    """Collapse Claude Code's task tools into one bounded progress snapshot."""

    _MAX_TASKS = 100
    _MAX_PENDING_CALLS = 256
    _TASK_TOOLS = {"taskcreate", "taskupdate", "tasklist", "taskget"}
    _STATUSES = {"pending", "in_progress", "completed"}

    def __init__(self) -> None:
        self._tasks: list[dict[str, str]] = []
        self._pending: dict[str, dict[str, Any]] = {}

    @staticmethod
    def _canonical_name(name: Any) -> str:
        return re.sub(r"[^a-z0-9]", "", str(name or "").lower())

    @staticmethod
    def _text(value: Any) -> str:
        return value.strip() if isinstance(value, str) else ""

    def _snapshot(self) -> dict[str, Any]:
        return {
            "type": "progress",
            "id": "todo-list",
            "items": [dict(task) for task in self._tasks],
        }

    def _find(self, task_id: str) -> int:
        return next(
            (index for index, task in enumerate(self._tasks) if task["id"] == task_id),
            -1,
        )

    def _remove(self, task_id: str) -> bool:
        index = self._find(task_id)
        if index < 0:
            return False
        self._tasks.pop(index)
        return True

    def _upsert(self, raw: dict[str, Any]) -> bool:
        task_id = self._text(raw.get("id") or raw.get("taskId"))
        subject = self._text(raw.get("subject") or raw.get("content"))
        if not task_id:
            return False
        index = self._find(task_id)
        if index < 0:
            if not subject or len(self._tasks) >= self._MAX_TASKS:
                return False
            self._tasks.append(
                {
                    "id": task_id,
                    "content": subject,
                    "status": "pending",
                    "activeForm": self._text(raw.get("activeForm")),
                }
            )
            index = len(self._tasks) - 1

        task = self._tasks[index]
        if subject:
            task["content"] = subject
        active_form = self._text(raw.get("activeForm"))
        if active_form:
            task["activeForm"] = active_form
        status = self._text(raw.get("status")).lower()
        if status in self._STATUSES:
            task["status"] = status
        return True

    def _promote_created_task(
        self,
        provisional_id: str,
        task_id: str,
        subject: str,
    ) -> None:
        provisional_index = self._find(provisional_id)
        existing_index = self._find(task_id)
        if existing_index >= 0 and existing_index != provisional_index:
            if provisional_index >= 0:
                provisional = self._tasks.pop(provisional_index)
                existing_index = self._find(task_id)
                if not self._tasks[existing_index].get("content"):
                    self._tasks[existing_index]["content"] = provisional["content"]
            if subject:
                self._tasks[existing_index]["content"] = subject
            return
        if provisional_index >= 0:
            self._tasks[provisional_index]["id"] = task_id
            if subject:
                self._tasks[provisional_index]["content"] = subject
            return
        self._upsert({"id": task_id, "subject": subject, "status": "pending"})

    @classmethod
    def _objects(cls, value: Any, depth: int = 0):
        if depth > 4:
            return
        if isinstance(value, dict):
            yield value
            for child in value.values():
                yield from cls._objects(child, depth + 1)
        elif isinstance(value, list):
            for child in value[: cls._MAX_TASKS]:
                yield from cls._objects(child, depth + 1)
        elif isinstance(value, str) and len(value) <= 100_000:
            candidate = value.strip()
            if candidate.startswith(("{", "[")):
                try:
                    parsed = json.loads(candidate)
                except (TypeError, ValueError):
                    return
                yield from cls._objects(parsed, depth + 1)

    @classmethod
    def _result_text(cls, content: Any) -> str:
        if isinstance(content, str):
            return content
        texts: list[str] = []
        if isinstance(content, list):
            for item in content[: cls._MAX_TASKS]:
                if isinstance(item, str):
                    texts.append(item)
                elif isinstance(item, dict) and isinstance(item.get("text"), str):
                    texts.append(item["text"])
        return "\n".join(texts)

    @classmethod
    def _created_task(cls, content: Any) -> dict[str, Any] | None:
        for obj in cls._objects(content):
            task = obj.get("task")
            if isinstance(task, dict) and cls._text(task.get("id")):
                return task
        match = re.search(
            r"Task #([^\s]+) created successfully:\s*(.+)",
            cls._result_text(content),
        )
        if match:
            return {"id": match.group(1), "subject": match.group(2).strip()}
        return None

    @classmethod
    def _listed_tasks(cls, content: Any) -> list[dict[str, Any]] | None:
        for obj in cls._objects(content):
            tasks = obj.get("tasks")
            if isinstance(tasks, list):
                return [task for task in tasks[: cls._MAX_TASKS] if isinstance(task, dict)]

        text = cls._result_text(content).strip()
        if text == "No tasks found":
            return []
        tasks: list[dict[str, Any]] = []
        for line in text.splitlines()[: cls._MAX_TASKS]:
            match = re.match(r"^#([^\s]+) \[(pending|in_progress|completed)\] (.+)$", line)
            if not match:
                continue
            subject = re.sub(r"\s+\[blocked by .+\]$", "", match.group(3)).strip()
            tasks.append({"id": match.group(1), "status": match.group(2), "subject": subject})
        return tasks or None

    @classmethod
    def _fetched_task(cls, content: Any) -> dict[str, Any] | None:
        for obj in cls._objects(content):
            task = obj.get("task")
            if isinstance(task, dict) and cls._text(task.get("id")):
                return task
        text = cls._result_text(content)
        match = re.search(
            r"^Task #([^:]+):\s*(.+?)\nStatus:\s*(pending|in_progress|completed)\b",
            text,
            re.MULTILINE,
        )
        if match:
            return {"id": match.group(1), "subject": match.group(2), "status": match.group(3)}
        return None

    @classmethod
    def _update_succeeded(cls, content: Any) -> bool:
        for obj in cls._objects(content):
            if obj.get("success") is False:
                return False
        return "task not found" not in cls._result_text(content).lower()

    def handle_tool_use(
        self,
        name: Any,
        tool_id: Any,
        tool_input: Any,
    ) -> tuple[bool, dict[str, Any] | None]:
        canonical = self._canonical_name(name)
        call_id = self._text(tool_id)
        if canonical == "todowrite":
            items = tool_input.get("todos") if isinstance(tool_input, dict) else None
            if not call_id or not isinstance(items, list):
                return False, None
            if len(self._pending) >= self._MAX_PENDING_CALLS:
                return False, None
            self._pending[call_id] = {"kind": "todowrite"}
            return True, {"type": "progress", "id": "todo-list", "items": items}

        if canonical not in self._TASK_TOOLS or not call_id or not isinstance(tool_input, dict):
            return False, None
        if len(self._pending) >= self._MAX_PENDING_CALLS:
            return False, None

        if canonical == "taskcreate":
            subject = self._text(tool_input.get("subject"))
            if not subject or len(self._tasks) >= self._MAX_TASKS:
                return False, None
            provisional_id = f"pending:{call_id}"
            self._upsert(
                {
                    "id": provisional_id,
                    "subject": subject,
                    "status": "pending",
                    "activeForm": tool_input.get("activeForm"),
                }
            )
            self._pending[call_id] = {
                "kind": canonical,
                "input": dict(tool_input),
                "provisional_id": provisional_id,
            }
            return True, self._snapshot()

        task_id = self._text(tool_input.get("taskId"))
        if canonical in {"taskupdate", "taskget"} and not task_id:
            return False, None
        self._pending[call_id] = {"kind": canonical, "input": dict(tool_input)}
        return True, None

    def handle_tool_result(
        self,
        tool_use_id: Any,
        is_error: bool,
        content: Any,
    ) -> tuple[bool, dict[str, Any] | None]:
        call_id = self._text(tool_use_id)
        call = self._pending.pop(call_id, None)
        if call is None:
            return False, None
        kind = call["kind"]
        if kind == "todowrite":
            return True, None
        if is_error:
            changed = kind == "taskcreate" and self._remove(call["provisional_id"])
            return True, self._snapshot() if changed else None

        tool_input = call.get("input", {})
        if kind == "taskcreate":
            task = self._created_task(content)
            if task is None:
                return True, None
            task_id = self._text(task.get("id") or task.get("taskId"))
            if not task_id:
                return True, None
            subject = self._text(task.get("subject")) or self._text(tool_input.get("subject"))
            self._promote_created_task(call["provisional_id"], task_id, subject)
            return True, self._snapshot()

        if kind == "taskupdate":
            if not self._update_succeeded(content):
                return True, None
            task_id = self._text(tool_input.get("taskId"))
            if self._text(tool_input.get("status")).lower() == "deleted":
                changed = self._remove(task_id)
            else:
                changed = self._upsert({"id": task_id, **tool_input})
            return True, self._snapshot() if changed else None

        if kind == "tasklist":
            tasks = self._listed_tasks(content)
            if tasks is None:
                return True, None
            had_tasks = bool(self._tasks)
            self._tasks = []
            for task in tasks:
                self._upsert(task)
            return True, self._snapshot() if self._tasks or had_tasks else None

        task = self._fetched_task(content)
        changed = task is not None and self._upsert(task)
        return True, self._snapshot() if changed else None


def _block_to_event(
    block: Any,
    task_progress: _ClaudeTaskProgress | None = None,
    tool_outcomes: _CocolaRunControl | None = None,
) -> dict[str, Any] | None:
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
        name = getattr(block, "name", None)
        tool_input = getattr(block, "input", None)
        if tool_outcomes is not None:
            tool_outcomes.observe_tool_use(
                getattr(block, "id", None),
                name,
            )
        if task_progress is not None:
            handled, event = task_progress.handle_tool_use(
                name,
                getattr(block, "id", None),
                tool_input,
            )
            if handled:
                return event
        return {
            "type": "tool_use",
            "name": name,
            "id": getattr(block, "id", None),
            "input": tool_input,
        }
    if bcls in ("ToolResultBlock", "ServerToolResultBlock"):
        tool_use_id = str(getattr(block, "tool_use_id", None) or "")
        is_error = bool(getattr(block, "is_error", False))
        content = getattr(block, "content", None)
        if task_progress is not None:
            handled, event = task_progress.handle_tool_result(tool_use_id, is_error, content)
            if handled:
                return event
        return {
            "type": "tool_result",
            "tool_use_id": tool_use_id or None,
            "is_error": is_error,
            "outcome": (
                tool_outcomes.tool_outcome(tool_use_id, is_error)
                if tool_outcomes is not None
                else ("failed" if is_error else "success")
            ),
            "content": _tool_result_content(content),
        }
    return None


def _message_to_events(
    message: Any,
    task_progress: _ClaudeTaskProgress | None = None,
) -> list[dict[str, Any]]:
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
                ev = _block_to_event(block, task_progress)
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


def _valid_structured_result(value: Any, depth: int = 0) -> bool:
    if depth > 12:
        return False
    if value is None or isinstance(value, (bool, int, float)):
        return True
    if isinstance(value, str):
        return len(value.encode("utf-8")) <= 4 * 1024
    if isinstance(value, list):
        return len(value) <= 200 and all(
            _valid_structured_result(item, depth + 1) for item in value
        )
    if isinstance(value, dict):
        return len(value) <= 200 and all(
            isinstance(key, str) and len(key) <= 256 and _valid_structured_result(item, depth + 1)
            for key, item in value.items()
        )
    return False


def _valid_structured_renderer_shape(renderer: str, value: Any) -> bool:
    if not isinstance(value, dict):
        return False
    if renderer == "table":
        columns = value.get("columns")
        rows = value.get("rows")
        return (
            isinstance(columns, list)
            and isinstance(rows, list)
            and len(columns) <= 20
            and len(rows) <= 200
        )
    if renderer == "list":
        items = value.get("items")
        return isinstance(items, list) and len(items) <= 200
    if renderer == "metrics":
        metrics = value.get("metrics")
        if isinstance(metrics, (dict, list)):
            return len(metrics) <= 20
        return len(value) <= 21
    return True


def _has_remote_schema_ref(value: Any) -> bool:
    if isinstance(value, dict):
        for key, item in value.items():
            if key == "$ref" and isinstance(item, str) and item and not item.startswith("#"):
                return True
            if _has_remote_schema_ref(item):
                return True
    elif isinstance(value, list):
        return any(_has_remote_schema_ref(item) for item in value)
    return False


def _normalized_result_contract(value: Any) -> dict[str, Any] | None:
    if value is None:
        return None
    if not isinstance(value, dict):
        raise ValueError("result_contract must be an object")
    renderer = str(value.get("renderer") or "").strip()
    version = value.get("version")
    schema = value.get("schema")
    contract_hash = str(value.get("contract_hash") or "").strip()
    if renderer not in {"summary", "table", "list", "metrics"} or version != 1:
        raise ValueError("result_contract renderer or version is invalid")
    if (
        not isinstance(schema, dict)
        or schema.get("type") != "object"
        or re.fullmatch(r"sha256:[0-9a-f]{64}", contract_hash) is None
        or _has_remote_schema_ref(schema)
    ):
        raise ValueError("result_contract schema or hash is invalid")
    encoded = json.dumps(schema, ensure_ascii=False, separators=(",", ":"))
    if len(encoded.encode("utf-8")) > 64 * 1024:
        raise ValueError("result_contract schema exceeds 64 KiB")
    try:
        Draft202012Validator.check_schema(schema)
    except SchemaError as error:
        raise ValueError("result_contract schema is invalid") from error
    return {
        "renderer": renderer,
        "version": 1,
        "schema": schema,
        "contract_hash": contract_hash,
    }


def _builtin_result_contract(renderer: str, schema: dict[str, Any]) -> dict[str, Any]:
    hash_input = json.dumps(
        {"version": 1, "renderer": renderer, "schema": schema},
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode()
    return {
        "renderer": renderer,
        "version": 1,
        "schema": schema,
        "contract_hash": "sha256:" + hashlib.sha256(hash_input).hexdigest(),
    }


_BUILTIN_RESULT_CONTRACTS = {
    "cocola_present_summary": _builtin_result_contract(
        "summary",
        {
            "type": "object",
            "properties": {
                "title": {"type": "string", "maxLength": 4096},
                "summary": {"type": "string", "maxLength": 4096},
                "status": {"type": "string", "maxLength": 4096},
                "details": {
                    "type": "array",
                    "maxItems": 20,
                    "items": {
                        "type": "object",
                        "properties": {
                            "label": {"type": "string", "maxLength": 4096},
                            "value": {},
                        },
                        "required": ["label", "value"],
                        "additionalProperties": False,
                    },
                },
            },
            "required": ["summary"],
            "additionalProperties": False,
        },
    ),
    "cocola_present_table": _builtin_result_contract(
        "table",
        {
            "type": "object",
            "properties": {
                "title": {"type": "string", "maxLength": 4096},
                "columns": {
                    "type": "array",
                    "minItems": 1,
                    "maxItems": 20,
                    "items": {
                        "oneOf": [
                            {"type": "string", "maxLength": 4096},
                            {
                                "type": "object",
                                "properties": {
                                    "key": {"type": "string", "maxLength": 256},
                                    "label": {"type": "string", "maxLength": 4096},
                                },
                                "required": ["key", "label"],
                                "additionalProperties": False,
                            },
                        ]
                    },
                },
                "rows": {
                    "type": "array",
                    "maxItems": 200,
                    "items": {
                        "oneOf": [
                            {"type": "array", "maxItems": 20},
                            {"type": "object"},
                        ]
                    },
                },
            },
            "required": ["columns", "rows"],
            "additionalProperties": False,
        },
    ),
    "cocola_present_list": _builtin_result_contract(
        "list",
        {
            "type": "object",
            "properties": {
                "title": {"type": "string", "maxLength": 4096},
                "items": {
                    "type": "array",
                    "minItems": 1,
                    "maxItems": 200,
                    "items": {},
                },
            },
            "required": ["items"],
            "additionalProperties": False,
        },
    ),
    "cocola_present_metrics": _builtin_result_contract(
        "metrics",
        {
            "type": "object",
            "properties": {
                "title": {"type": "string", "maxLength": 4096},
                "metrics": {
                    "type": "array",
                    "minItems": 1,
                    "maxItems": 20,
                    "items": {
                        "type": "object",
                        "properties": {
                            "label": {"type": "string", "maxLength": 4096},
                            "value": {},
                            "unit": {"type": "string", "maxLength": 4096},
                            "trend": {"type": "string", "maxLength": 4096},
                        },
                        "required": ["label", "value"],
                        "additionalProperties": False,
                    },
                },
            },
            "required": ["metrics"],
            "additionalProperties": False,
        },
    ),
}


class _CocolaRunControl:
    """Own the trusted, structured terminal protocol for one Claude run."""

    def __init__(
        self,
        *,
        plan_mode: bool = True,
        user_input_enabled: bool = True,
        result_contract: dict[str, Any] | None = None,
        result_policy: str = "none",
    ) -> None:
        if result_policy not in {"none", "required", "optional"}:
            raise ValueError("result_policy must be none, required, or optional")
        if plan_mode and result_policy != "none":
            raise ValueError("structured results are available only in Execute mode")
        if result_policy == "required":
            if result_contract is None:
                raise ValueError("required result_policy needs a Skill result contract")
        elif result_contract is not None:
            raise ValueError("a Skill result contract is valid only with required result_policy")
        self._plan_mode = plan_mode
        self._user_input_enabled = user_input_enabled
        self._result_policy = result_policy
        self._result_contracts: dict[str, dict[str, Any]] = {}
        if result_policy == "required":
            self._result_contracts[_RESULT_CONTROL_TOOL] = result_contract
        elif result_policy == "optional":
            self._result_contracts.update(_BUILTIN_RESULT_CONTRACTS)
        self._terminal_event: dict[str, Any] | None = None
        self._protocol_error: dict[str, Any] | None = None
        self._terminal_reserved_name = ""
        self._ordinary_tool_ids: set[str] = set()
        self._tool_gate_lock = asyncio.Lock()
        self._interrupt_requested = False
        self._runtime_failed = False
        self._internal_tool_ids: set[str] = set()
        self._tool_outcomes: dict[str, str] = {}
        names: set[str] = set()
        if plan_mode:
            names.update(_PLAN_CONTROL_TOOL_NAMES)
        elif user_input_enabled:
            names.add(f"mcp__{_PLAN_CONTROL_SERVER}__cocola_request_user_input")
        names.update(
            f"mcp__{_PLAN_CONTROL_SERVER}__{tool_name}" for tool_name in self._result_contracts
        )
        self._tool_names = names
        self._terminal_tool_names = names & _TERMINAL_CONTROL_TOOL_NAMES

    @property
    def tool_names(self) -> frozenset[str]:
        return frozenset(self._tool_names)

    @staticmethod
    def _tool_result(message: str, *, is_error: bool = False) -> dict[str, Any]:
        return {
            "content": [{"type": "text", "text": message}],
            "is_error": is_error,
        }

    def _set_terminal(
        self,
        event: dict[str, Any],
        terminal_tool_name: str = "",
    ) -> dict[str, Any]:
        if not self._terminal_reserved_name:
            if terminal_tool_name:
                suffix = terminal_tool_name
            else:
                event_type = str(event.get("type") or "")
                if event_type == "structured_result_ready":
                    suffix = _RESULT_CONTROL_TOOL
                elif event_type == "question_required":
                    suffix = "cocola_request_user_input"
                else:
                    suffix = "cocola_submit_plan"
            self._terminal_reserved_name = f"mcp__{_PLAN_CONTROL_SERVER}__{suffix}"
        if self._terminal_event is not None or self._protocol_error is not None:
            self._record_protocol_error(self._terminal_reserved_name)
            return self._tool_result(
                "A run can submit only one terminal control result.",
                is_error=True,
            )
        self._terminal_event = event
        return self._tool_result("Cocola recorded the run result.")

    @staticmethod
    def _protocol_error_for(tool_name: str) -> dict[str, Any]:
        if any(tool_name.endswith(result_tool) for result_tool in _RESULT_CONTROL_TOOLS):
            return {
                "type": "error",
                "stage": "result",
                "code": "STRUCTURED_RESULT_INVALID",
                "error": "Claude returned an invalid structured result.",
            }
        if tool_name.endswith("cocola_request_user_input"):
            return {
                "type": "error",
                "stage": "question",
                "code": "QUESTION_OUTPUT_INVALID",
                "error": "Claude returned an invalid question.",
            }
        return {
            "type": "error",
            "stage": "plan",
            "code": "PLAN_OUTPUT_INVALID",
            "error": _PLAN_INVALID_ERROR,
        }

    def _record_protocol_error(self, tool_name: str) -> None:
        if self._protocol_error is None:
            self._protocol_error = self._protocol_error_for(tool_name)

    def _reject_terminal(self, tool_name: str, message: str) -> dict[str, Any]:
        self._record_protocol_error(tool_name)
        return self._tool_result(message, is_error=True)

    @staticmethod
    def _pre_tool_decision(allow: bool, reason: str = "") -> dict[str, Any]:
        output: dict[str, Any] = {
            "hookEventName": "PreToolUse",
            "permissionDecision": "allow" if allow else "deny",
        }
        if reason:
            output["permissionDecisionReason"] = reason
        return {"hookSpecificOutput": output}

    async def pre_tool_use(
        self,
        hook_input: dict[str, Any],
        tool_use_id: str | None,
        _context: Any,
    ) -> dict[str, Any]:
        tool_name = str(hook_input.get("tool_name") or "")
        identifier = str(hook_input.get("tool_use_id") or tool_use_id or "")
        async with self._tool_gate_lock:
            if tool_name in self._terminal_tool_names:
                if (
                    self._ordinary_tool_ids
                    or self._terminal_reserved_name
                    or self._terminal_event is not None
                    or self._protocol_error is not None
                ):
                    self._record_protocol_error(tool_name)
                    return self._pre_tool_decision(
                        False,
                        "A terminal Cocola control tool must be the run's only active tool.",
                    )
                self._terminal_reserved_name = tool_name
                return self._pre_tool_decision(True)
            if (
                self._terminal_reserved_name
                or self._terminal_event is not None
                or self._protocol_error is not None
            ):
                self._record_protocol_error(self._terminal_reserved_name)
                return self._pre_tool_decision(
                    False,
                    "No tools may run after a terminal Cocola control tool.",
                )
            if identifier:
                self._ordinary_tool_ids.add(identifier)
            return self._pre_tool_decision(True)

    async def post_tool_use(
        self,
        hook_input: dict[str, Any],
        tool_use_id: str | None,
        _context: Any,
    ) -> dict[str, Any]:
        identifier = str(hook_input.get("tool_use_id") or tool_use_id or "")
        if identifier:
            async with self._tool_gate_lock:
                self._ordinary_tool_ids.discard(identifier)
        return {}

    def sdk_hooks(self, sdk: Any) -> dict[str, list[Any]]:
        return {
            "PreToolUse": [sdk.HookMatcher(matcher=None, hooks=[self.pre_tool_use])],
            "PostToolUse": [sdk.HookMatcher(matcher=None, hooks=[self.post_tool_use])],
            "PostToolUseFailure": [sdk.HookMatcher(matcher=None, hooks=[self.post_tool_use])],
        }

    async def submit_plan(self, args: dict[str, Any]) -> dict[str, Any]:
        content = args.get("content_markdown")
        if not isinstance(content, str):
            return self._reject_terminal(
                f"mcp__{_PLAN_CONTROL_SERVER}__cocola_submit_plan",
                "content_markdown must be a non-empty Markdown string.",
            )
        content = content.strip()
        if not content or len(content.encode("utf-8")) > _PLAN_MAX_BYTES:
            return self._reject_terminal(
                f"mcp__{_PLAN_CONTROL_SERVER}__cocola_submit_plan",
                _PLAN_INVALID_ERROR,
            )
        return self._set_terminal(
            {
                "type": "plan_ready",
                "content_markdown": content,
            }
        )

    async def request_user_input(self, args: dict[str, Any]) -> dict[str, Any]:
        tool_name = f"mcp__{_PLAN_CONTROL_SERVER}__cocola_request_user_input"
        question = args.get("question")
        if not isinstance(question, str) or not question.strip():
            return self._reject_terminal(
                tool_name,
                "question must be a non-empty string.",
            )
        question = question.strip()
        if len(question.encode("utf-8")) > 16 * 1024:
            return self._reject_terminal(tool_name, "question is too large.")

        raw_options = args.get("options", [])
        if raw_options is None:
            raw_options = []
        if not isinstance(raw_options, list) or len(raw_options) > 8:
            return self._reject_terminal(
                tool_name,
                "options must be an array containing at most eight strings.",
            )
        options: list[str] = []
        seen_options: set[str] = set()
        for raw_option in raw_options:
            if not isinstance(raw_option, str) or not raw_option.strip():
                return self._reject_terminal(
                    tool_name,
                    "each question option must be a non-empty string.",
                )
            option = raw_option.strip()
            if len(option.encode("utf-8")) > 1024:
                return self._reject_terminal(
                    tool_name,
                    "each question option must not exceed 1 KiB.",
                )
            if option in seen_options:
                return self._reject_terminal(
                    tool_name,
                    "question options must be unique.",
                )
            seen_options.add(option)
            options.append(option)

        return self._set_terminal(
            {
                "type": "question_required",
                "question": question,
                "options": options,
            }
        )

    async def submit_result(
        self,
        args: dict[str, Any],
        result_tool_name: str = _RESULT_CONTROL_TOOL,
    ) -> dict[str, Any]:
        tool_name = f"mcp__{_PLAN_CONTROL_SERVER}__{result_tool_name}"
        contract = self._result_contracts.get(result_tool_name)
        if contract is None:
            return self._reject_terminal(
                tool_name,
                "This run does not accept a structured result.",
            )
        try:
            encoded = json.dumps(
                args,
                ensure_ascii=False,
                allow_nan=False,
                separators=(",", ":"),
            )
        except (TypeError, ValueError):
            return self._reject_terminal(tool_name, "Structured result must be valid JSON.")
        if (
            len(encoded.encode("utf-8")) > 128 * 1024
            or not _valid_structured_result(args)
            or not _valid_structured_renderer_shape(contract["renderer"], args)
        ):
            return self._reject_terminal(tool_name, "Structured result exceeds Cocola limits.")
        try:
            Draft202012Validator(contract["schema"]).validate(args)
        except ValidationError:
            schema_owner = (
                "Skill" if result_tool_name == _RESULT_CONTROL_TOOL else "Cocola presentation"
            )
            return self._reject_terminal(
                tool_name,
                f"Structured result does not match the {schema_owner} result schema.",
            )
        title = args.get("title")
        if not isinstance(title, str):
            title = ""
        return self._set_terminal(
            {
                "type": "structured_result_ready",
                "renderer": contract["renderer"],
                "renderer_version": contract["version"],
                "contract_hash": contract["contract_hash"],
                "title": title.strip()[: 4 * 1024],
                "data": args,
            },
            result_tool_name,
        )

    async def get_runtime_info(self, _args: dict[str, Any]) -> dict[str, Any]:
        versions: dict[str, dict[str, str]] = {
            "python": {"status": "available", "version": sys.version.split()[0]},
        }
        for name, argv in (
            ("claude_code", ("claude", "--version")),
            ("node", ("node", "--version")),
            ("npm", ("npm", "--version")),
        ):
            versions[name] = await self._fixed_command_version(argv)
        return self._tool_result(
            json.dumps(
                {
                    "cwd": os.getcwd(),
                    "versions": versions,
                },
                ensure_ascii=False,
                separators=(",", ":"),
            )
        )

    @staticmethod
    async def _fixed_command_version(argv: tuple[str, ...]) -> dict[str, str]:
        try:
            process = await asyncio.create_subprocess_exec(
                *argv,
                stdin=asyncio.subprocess.DEVNULL,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
        except FileNotFoundError:
            return {"status": "unavailable"}
        try:
            stdout, stderr = await asyncio.wait_for(process.communicate(), timeout=3.0)
        except TimeoutError:
            with contextlib.suppress(ProcessLookupError):
                process.kill()
            with contextlib.suppress(TimeoutError):
                await asyncio.wait_for(process.communicate(), timeout=1.0)
            return {"status": "timeout"}
        output = (stdout if process.returncode == 0 else stderr).decode(errors="replace")
        version = output.strip().splitlines()[0] if output.strip() else ""
        if process.returncode != 0:
            return {"status": "failed"}
        return {"status": "available", "version": version[:200]}

    def sdk_server(self, sdk: Any) -> Any:
        annotations = sdk.ToolAnnotations(
            readOnlyHint=True,
            destructiveHint=False,
            idempotentHint=True,
            openWorldHint=False,
        )

        tools: list[Any] = []
        if self._plan_mode:

            @sdk.tool(
                "cocola_submit_plan",
                "Submit one complete Markdown plan to Cocola for user review.",
                {
                    "type": "object",
                    "properties": {"content_markdown": {"type": "string"}},
                    "required": ["content_markdown"],
                    "additionalProperties": False,
                },
                annotations=annotations,
            )
            async def submit_plan(args: dict[str, Any]) -> dict[str, Any]:
                return await self.submit_plan(args)

            tools.append(submit_plan)

        if self._user_input_enabled:

            @sdk.tool(
                "cocola_request_user_input",
                "Ask the user one concise question, then stop this run.",
                {
                    "type": "object",
                    "properties": {
                        "question": {"type": "string"},
                        "options": {
                            "type": "array",
                            "items": {"type": "string"},
                            "maxItems": 8,
                        },
                    },
                    "required": ["question"],
                    "additionalProperties": False,
                },
                annotations=annotations,
            )
            async def request_user_input(args: dict[str, Any]) -> dict[str, Any]:
                return await self.request_user_input(args)

            tools.append(request_user_input)

        if self._plan_mode:

            @sdk.tool(
                "cocola_get_runtime_info",
                "Return trusted Cocola runtime versions without using shell tools.",
                {
                    "type": "object",
                    "properties": {},
                    "additionalProperties": False,
                },
                annotations=annotations,
            )
            async def get_runtime_info(args: dict[str, Any]) -> dict[str, Any]:
                return await self.get_runtime_info(args)

            tools.append(get_runtime_info)

        def result_tool(result_tool_name: str, contract: dict[str, Any]) -> Any:
            description = _BUILTIN_RESULT_TOOL_DESCRIPTIONS.get(
                result_tool_name,
                "Submit the final structured Skill result to Cocola, then stop this run.",
            )

            @sdk.tool(
                result_tool_name,
                description,
                contract["schema"],
                annotations=annotations,
            )
            async def submit_result(args: dict[str, Any]) -> dict[str, Any]:
                return await self.submit_result(args, result_tool_name)

            return submit_result

        tools.extend(
            result_tool(result_tool_name, contract)
            for result_tool_name, contract in self._result_contracts.items()
        )

        return sdk.create_sdk_mcp_server(
            name=_PLAN_CONTROL_SERVER,
            version="1.0.0",
            tools=tools,
        )

    async def can_use_tool(
        self,
        tool_name: str,
        tool_input: dict[str, Any],
        context: Any,
    ) -> Any:
        import claude_agent_sdk

        if tool_name in self._tool_names:
            return claude_agent_sdk.PermissionResultAllow(updated_input=tool_input)
        tool_use_id = str(getattr(context, "tool_use_id", "") or "")
        if tool_use_id:
            self._tool_outcomes[tool_use_id] = "permission_denied"
        return claude_agent_sdk.PermissionResultDeny(
            message="This tool is not permitted in Cocola Plan Mode."
        )

    def observe_tool_use(self, tool_use_id: Any, tool_name: Any) -> None:
        identifier = str(tool_use_id or "")
        name = str(tool_name or "")
        if not identifier:
            return
        if name in self._tool_names:
            self._internal_tool_ids.add(identifier)
        elif name in _PLAN_DISALLOWED_TOOLS:
            self._tool_outcomes[identifier] = "permission_denied"

    def is_internal_tool_use(self, block: Any) -> bool:
        name = str(getattr(block, "name", "") or "")
        identifier = str(getattr(block, "id", "") or "")
        if name not in self._tool_names:
            return False
        if identifier:
            self._internal_tool_ids.add(identifier)
        return True

    def is_internal_tool_result(self, block: Any) -> bool:
        identifier = str(getattr(block, "tool_use_id", "") or "")
        return bool(identifier and identifier in self._internal_tool_ids)

    def tool_outcome(self, tool_use_id: str, is_error: bool) -> str:
        if not is_error:
            return "success"
        return self._tool_outcomes.get(tool_use_id, "failed")

    def message_events(
        self,
        message: Any,
        task_progress: _ClaudeTaskProgress,
    ) -> list[dict[str, Any]]:
        events: list[dict[str, Any]] = []
        message_class = type(message).__name__
        if message_class in ("AssistantMessage", "UserMessage"):
            content = getattr(message, "content", None)
            if not isinstance(content, list):
                return events
            for block in content:
                block_class = type(block).__name__
                if (
                    self._plan_mode
                    and message_class == "AssistantMessage"
                    and block_class == "TextBlock"
                ):
                    continue
                if block_class in ("ToolUseBlock", "ServerToolUseBlock") and (
                    self.is_internal_tool_use(block)
                ):
                    continue
                if block_class in (
                    "ToolResultBlock",
                    "ServerToolResultBlock",
                ) and self.is_internal_tool_result(block):
                    continue
                event = _block_to_event(block, task_progress, self)
                if event is not None:
                    events.append(event)
            return events
        if message_class == "ResultMessage":
            result = getattr(message, "result", None)
            # Cocola deliberately interrupts the SDK query after a terminal
            # control tool succeeds. Some SDK transports describe that clean
            # stop as an error ResultMessage; the durable terminal event is the
            # authoritative outcome in that case.
            is_error = bool(getattr(message, "is_error", False)) and self._terminal_event is None
            self._runtime_failed = is_error
            events.append(
                {
                    "type": "result",
                    "is_error": is_error,
                    "num_turns": getattr(message, "num_turns", None),
                    "total_cost_usd": getattr(message, "total_cost_usd", None),
                    "session_id": getattr(message, "session_id", None),
                    "result": result,
                }
            )
            return events
        if message_class == "SystemMessage":
            events.append({"type": "system", "subtype": getattr(message, "subtype", None)})
        return events

    def final_event(self) -> dict[str, Any] | None:
        if self._protocol_error is not None:
            return dict(self._protocol_error)
        if self._terminal_event is not None:
            return dict(self._terminal_event)
        if self._terminal_reserved_name:
            return self._protocol_error_for(self._terminal_reserved_name)
        if self._runtime_failed:
            return None
        if not self._plan_mode:
            if self._result_policy != "required":
                return None
            return {
                "type": "error",
                "stage": "result",
                "code": "STRUCTURED_RESULT_INVALID",
                "error": "Claude returned an invalid structured result.",
            }
        return {
            "type": "error",
            "stage": "plan",
            "code": "PLAN_OUTPUT_INVALID",
            "error": _PLAN_INVALID_ERROR,
        }

    def should_interrupt(self) -> bool:
        if (
            self._terminal_event is None and self._protocol_error is None
        ) or self._interrupt_requested:
            return False
        self._interrupt_requested = True
        return True


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

    permission_mode = req.get("permission_mode") or "bypassPermissions"
    plan_mode = permission_mode == "plan"
    try:
        result_contract = _normalized_result_contract(req.get("result_contract"))
    except ValueError as error:
        _emit(
            {
                "type": "error",
                "stage": "result",
                "code": "STRUCTURED_RESULT_INVALID",
                "error": str(error),
            }
        )
        return 1
    raw_result_policy = req.get("result_policy")
    if raw_result_policy is None:
        # Accept requests from an older control-plane process during a rolling
        # deployment. New providers always send the policy explicitly.
        result_policy = "required" if result_contract is not None else "none"
    elif isinstance(raw_result_policy, str):
        result_policy = raw_result_policy
    else:
        _emit(
            {
                "type": "error",
                "stage": "result",
                "code": "STRUCTURED_RESULT_INVALID",
                "error": "result_policy must be none, required, or optional",
            }
        )
        return 1
    user_input_enabled = bool(req.get("user_input_enabled", plan_mode))
    try:
        run_control = (
            _CocolaRunControl(
                plan_mode=plan_mode,
                user_input_enabled=user_input_enabled,
                result_contract=result_contract,
                result_policy=result_policy,
            )
            if plan_mode or user_input_enabled or result_policy != "none"
            else None
        )
    except ValueError as error:
        _emit(
            {
                "type": "error",
                "stage": "result",
                "code": "STRUCTURED_RESULT_INVALID",
                "error": str(error),
            }
        )
        return 1
    options = _build_options(req, run_control=run_control)
    prompt = _claude_prompt(req)
    _emit({"type": "start", "ts": time.time()})

    last_session_id: str | None = None
    task_progress = _ClaudeTaskProgress()

    async def relay(client: Any, messages: Any) -> None:
        nonlocal last_session_id
        runtime_accepted = False
        async for message in messages:
            if (
                not runtime_accepted
                and isinstance(message, claude_agent_sdk.SystemMessage)
                and message.subtype == "init"
            ):
                runtime_accepted = True
                _emit({"type": "run_accepted"})
            events = (
                run_control.message_events(message, task_progress)
                if run_control is not None
                else _message_to_events(message, task_progress)
            )
            for ev in events:
                if ev.get("type") == "result" and ev.get("session_id"):
                    last_session_id = ev["session_id"]
                _emit(ev)
            if run_control is not None and run_control.should_interrupt():
                await client.interrupt()

    report_environment = not req.get("resume")
    if report_environment:
        _emit(_environment_status_event(req))
    status_task: asyncio.Task[None] | None = None
    async with claude_agent_sdk.ClaudeSDKClient(options=options) as client:
        await client.set_permission_mode(permission_mode)
        if report_environment and not plan_mode and _mcp_configs(req):
            status_task = asyncio.create_task(_watch_mcp_status(client, req))
        try:
            await client.query(prompt)
            await relay(client, client.receive_response())
        finally:
            if status_task is not None:
                if not status_task.done():
                    status_task.cancel()
                await asyncio.gather(status_task, return_exceptions=True)

    if run_control is not None and (final_event := run_control.final_event()):
        _emit(final_event)

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
        "openpyxl": None,
        "gh": cmd_version("gh", "--version"),
        "github_skill": Path("/opt/cocola/skills/cocola-github/SKILL.md").is_file(),
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
        "gopls": cmd_version("gopls", "version"),
        "clangd": cmd_version("clangd", "--version"),
        "shellcheck": cmd_version("shellcheck", "--version"),
        "shfmt": cmd_version("shfmt", "--version"),
        "java": cmd_version("java", "-version"),
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
        import openpyxl

        info["openpyxl"] = openpyxl.__version__
    except Exception as e:  # noqa: BLE001
        info["openpyxl"] = f"missing: {e}"
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
        "gopls",
        "clangd",
        "shellcheck",
        "shfmt",
        "java",
        "gh",
    ]
    ok = (
        info["node"]
        and not str(info["node"]).startswith("error")
        and info["claude_cli"]
        and not str(info["claude_cli"]).startswith("error")
        and not str(info["claude_agent_sdk"]).startswith("missing")
        and not str(info["codex_cli"]).startswith(("missing", "error"))
        and not str(info["codex_sdk"]).startswith("missing")
        and not str(info["openpyxl"]).startswith("missing")
        and info["github_skill"] is True
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
