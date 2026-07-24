"""Contract tests for Cocola-owned Claude Plan Mode control."""

from __future__ import annotations

import asyncio
import importlib.util
import pathlib
import sys
import types


def _load_shim(name: str):
    root = pathlib.Path(__file__).resolve().parents[3]
    shim_path = root / "deploy" / "sandbox-runtime" / "shim" / "agent_shim.py"
    spec = importlib.util.spec_from_file_location(name, shim_path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _fake_sdk(captured: dict[str, object]):
    class FakeClaudeAgentOptions:
        def __init__(self, **kwargs):
            captured["options"] = kwargs

    class FakePermissionResultDeny:
        def __init__(self, *, message: str, interrupt: bool = False):
            self.message = message
            self.interrupt = interrupt

    def fake_tool(name, description, input_schema, annotations=None):
        def decorate(handler):
            handler.name = name
            handler.description = description
            handler.input_schema = input_schema
            handler.annotations = annotations
            return handler

        return decorate

    def fake_server(*, name, version, tools):
        return {"name": name, "version": version, "tools": tools}

    return types.SimpleNamespace(
        ClaudeAgentOptions=FakeClaudeAgentOptions,
        PermissionResultDeny=FakePermissionResultDeny,
        ToolAnnotations=lambda **kwargs: kwargs,
        create_sdk_mcp_server=fake_server,
        tool=fake_tool,
    )


def test_plan_options_install_only_the_trusted_control_server(monkeypatch):
    captured: dict[str, object] = {}
    monkeypatch.setitem(sys.modules, "claude_agent_sdk", _fake_sdk(captured))
    module = _load_shim("cocola_agent_shim_structured_plan_options")

    control = module._ClaudePlanControl()
    module._build_options(
        {
            "prompt": "plan the change",
            "permission_mode": "plan",
            "mcp_servers": {
                "github": {
                    "type": "stdio",
                    "command": "npx",
                }
            },
        },
        plan_control=control,
    )

    options = captured["options"]
    assert set(options["mcp_servers"]) == {"cocola_control"}
    assert options["strict_mcp_config"] is True
    assert options["allowed_tools"] == [
        "mcp__cocola_control__cocola_submit_plan",
        "mcp__cocola_control__cocola_request_clarification",
        "mcp__cocola_control__cocola_get_runtime_info",
    ]
    assert set(options["disallowed_tools"]) >= {
        "ExitPlanMode",
        "AskUserQuestion",
        "Write",
        "Edit",
        "MultiEdit",
        "NotebookEdit",
        "Agent",
        "Task",
    }
    assert options["can_use_tool"] == control.can_use_tool


async def test_plan_control_emits_exactly_one_validated_terminal_event():
    module = _load_shim("cocola_agent_shim_structured_plan_terminal")
    control = module._ClaudePlanControl()

    await control.submit_plan({"content_markdown": "## Plan\n\n- Inspect\n- Implement"})

    assert control.final_event() == {
        "type": "plan_ready",
        "content_markdown": "## Plan\n\n- Inspect\n- Implement",
    }
    duplicate = await control.request_clarification({"question": "Which branch?"})
    assert duplicate["is_error"] is True
    assert control.final_event()["type"] == "plan_ready"


async def test_plan_control_uses_structured_clarification_and_rejects_missing_terminal():
    module = _load_shim("cocola_agent_shim_structured_clarification")
    clarification = module._ClaudePlanControl()

    await clarification.request_clarification(
        {
            "question": "Which package should the plan cover?",
            "options": ["Gateway", "Web"],
        }
    )

    assert clarification.final_event() == {
        "type": "clarification_required",
        "question": "Which package should the plan cover?",
        "options": ["Gateway", "Web"],
        "text": "Which package should the plan cover?\n\n- Gateway\n- Web",
    }

    missing = module._ClaudePlanControl()
    assert missing.final_event() == {
        "type": "error",
        "stage": "plan",
        "code": "PLAN_OUTPUT_INVALID",
        "error": "Claude did not return a reviewable plan. Refine the request and try again.",
    }


async def test_plan_permission_denial_has_a_structured_tool_outcome():
    module = _load_shim("cocola_agent_shim_structured_tool_outcome")
    control = module._ClaudePlanControl()
    context = types.SimpleNamespace(tool_use_id="tool-1")

    denial = await control.can_use_tool("Bash", {"command": "npm --version"}, context)
    assert denial.message == "This tool is not permitted in Cocola Plan Mode."

    block = type(
        "ToolResultBlock",
        (),
        {"tool_use_id": "tool-1", "is_error": True, "content": denial.message},
    )()
    assert module._block_to_event(block, tool_outcomes=control)["outcome"] == "permission_denied"


async def test_runtime_info_uses_only_fixed_argv_without_a_shell(monkeypatch):
    module = _load_shim("cocola_agent_shim_trusted_runtime_info")
    calls: list[tuple[str, ...]] = []

    class FakeProcess:
        returncode = 0

        async def communicate(self):
            return b"1.2.3\n", b""

    async def fake_subprocess(*argv, **kwargs):
        calls.append(tuple(argv))
        assert set(kwargs) == {"stdin", "stdout", "stderr"}
        return FakeProcess()

    monkeypatch.setattr(asyncio, "create_subprocess_exec", fake_subprocess)
    result = await module._ClaudePlanControl().get_runtime_info({})

    assert calls == [
        ("claude", "--version"),
        ("node", "--version"),
        ("npm", "--version"),
    ]
    assert result["is_error"] is False


async def test_resumed_turn_explicitly_switches_the_sdk_permission_mode(monkeypatch):
    captured: dict[str, object] = {}
    calls: list[tuple[str, str]] = []

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

        async def set_permission_mode(self, mode):
            calls.append(("permission", mode))

        async def query(self, prompt):
            calls.append(("query", prompt))

        async def receive_response(self):
            result = type(
                "ResultMessage",
                (),
                {
                    "is_error": False,
                    "num_turns": 1,
                    "total_cost_usd": 0,
                    "session_id": "claude-session",
                    "result": "done",
                },
            )()
            yield result

    async def forbidden_query(**_kwargs):
        raise AssertionError("resumed turns must use ClaudeSDKClient")
        yield

    fake_sdk = types.SimpleNamespace(
        ClaudeAgentOptions=FakeClaudeAgentOptions,
        ClaudeSDKClient=FakeClaudeSDKClient,
        query=forbidden_query,
    )
    monkeypatch.setitem(sys.modules, "claude_agent_sdk", fake_sdk)
    module = _load_shim("cocola_agent_shim_explicit_resume_permission")
    monkeypatch.setattr(module, "_emit", lambda _event: None)

    await module._run(
        {
            "prompt": "execute the approved plan",
            "resume": "claude-session",
            "permission_mode": "bypassPermissions",
        }
    )

    assert calls == [
        ("permission", "bypassPermissions"),
        ("query", "execute the approved plan"),
    ]
    assert captured["options"]["resume"] == "claude-session"


def test_plan_prompt_uses_only_cocola_control_tools():
    from cocola_agent_runtime.server import PLAN_SYSTEM_PROMPT

    assert "cocola_submit_plan" in PLAN_SYSTEM_PROMPT
    assert "cocola_request_clarification" in PLAN_SYSTEM_PROMPT
    assert "<cocola_plan>" not in PLAN_SYSTEM_PROMPT
    assert "ExitPlanMode" not in PLAN_SYSTEM_PROMPT
    assert "AskUserQuestion" not in PLAN_SYSTEM_PROMPT
