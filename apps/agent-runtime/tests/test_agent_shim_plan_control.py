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

    class FakeHookMatcher:
        def __init__(self, *, matcher=None, hooks=None, timeout=None):
            self.matcher = matcher
            self.hooks = hooks or []
            self.timeout = timeout

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
        HookMatcher=FakeHookMatcher,
        PermissionResultDeny=FakePermissionResultDeny,
        ToolAnnotations=lambda **kwargs: kwargs,
        create_sdk_mcp_server=fake_server,
        tool=fake_tool,
    )


def test_plan_options_install_only_the_trusted_control_server(monkeypatch):
    captured: dict[str, object] = {}
    monkeypatch.setitem(sys.modules, "claude_agent_sdk", _fake_sdk(captured))
    module = _load_shim("cocola_agent_shim_structured_plan_options")

    control = module._CocolaRunControl()
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
        run_control=control,
    )

    options = captured["options"]
    assert set(options["mcp_servers"]) == {"cocola_control"}
    assert options["strict_mcp_config"] is True
    assert set(options["allowed_tools"]) == {
        "mcp__cocola_control__cocola_submit_plan",
        "mcp__cocola_control__cocola_request_user_input",
        "mcp__cocola_control__cocola_get_runtime_info",
    }
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
    assert set(options["hooks"]) == {
        "PreToolUse",
        "PostToolUse",
        "PostToolUseFailure",
    }


async def test_plan_control_emits_exactly_one_validated_terminal_event():
    module = _load_shim("cocola_agent_shim_structured_plan_terminal")
    control = module._CocolaRunControl()

    await control.submit_plan({"content_markdown": "## Plan\n\n- Inspect\n- Implement"})

    assert control.final_event() == {
        "type": "plan_ready",
        "content_markdown": "## Plan\n\n- Inspect\n- Implement",
    }
    duplicate = await control.request_user_input({"question": "Which branch?"})
    assert duplicate["is_error"] is True
    assert control.final_event()["code"] == "PLAN_OUTPUT_INVALID"


async def test_terminal_control_tool_is_rejected_while_an_ordinary_tool_is_active():
    module = _load_shim("cocola_agent_shim_terminal_tool_gate")
    control = module._CocolaRunControl(plan_mode=False, user_input_enabled=True)

    ordinary = await control.pre_tool_use(
        {"tool_name": "Bash", "tool_use_id": "tool-1"},
        "tool-1",
        {},
    )
    terminal = await control.pre_tool_use(
        {
            "tool_name": "mcp__cocola_control__cocola_request_user_input",
            "tool_use_id": "tool-2",
        },
        "tool-2",
        {},
    )

    assert ordinary["hookSpecificOutput"]["permissionDecision"] == "allow"
    assert terminal["hookSpecificOutput"]["permissionDecision"] == "deny"
    assert control.final_event()["code"] == "QUESTION_OUTPUT_INVALID"
    assert control.should_interrupt() is True


async def test_ordinary_tools_are_rejected_after_a_terminal_tool_is_reserved():
    module = _load_shim("cocola_agent_shim_post_terminal_tool_gate")
    control = module._CocolaRunControl(plan_mode=False, user_input_enabled=True)
    terminal_name = "mcp__cocola_control__cocola_request_user_input"

    terminal = await control.pre_tool_use(
        {"tool_name": terminal_name, "tool_use_id": "tool-1"},
        "tool-1",
        {},
    )
    assert terminal["hookSpecificOutput"]["permissionDecision"] == "allow"
    await control.request_user_input({"question": "Which database?"})
    ordinary = await control.pre_tool_use(
        {"tool_name": "Bash", "tool_use_id": "tool-2"},
        "tool-2",
        {},
    )

    assert ordinary["hookSpecificOutput"]["permissionDecision"] == "deny"
    assert control.final_event()["code"] == "QUESTION_OUTPUT_INVALID"


async def test_completed_ordinary_tool_releases_the_terminal_tool_gate():
    module = _load_shim("cocola_agent_shim_released_terminal_tool_gate")
    control = module._CocolaRunControl(plan_mode=False, user_input_enabled=True)
    terminal_name = "mcp__cocola_control__cocola_request_user_input"

    await control.pre_tool_use(
        {"tool_name": "Read", "tool_use_id": "tool-1"},
        "tool-1",
        {},
    )
    await control.post_tool_use(
        {"tool_name": "Read", "tool_use_id": "tool-1"},
        "tool-1",
        {},
    )
    terminal = await control.pre_tool_use(
        {"tool_name": terminal_name, "tool_use_id": "tool-2"},
        "tool-2",
        {},
    )
    await control.request_user_input({"question": "Which database?"})

    assert terminal["hookSpecificOutput"]["permissionDecision"] == "allow"
    assert control.final_event()["type"] == "question_required"


async def test_plan_control_uses_structured_question_and_rejects_missing_terminal():
    module = _load_shim("cocola_agent_shim_structured_clarification")
    clarification = module._CocolaRunControl()

    await clarification.request_user_input(
        {
            "question": "Which package should the plan cover?",
            "options": ["Gateway", "Web"],
        }
    )

    assert clarification.final_event() == {
        "type": "question_required",
        "question": "Which package should the plan cover?",
        "options": ["Gateway", "Web"],
    }

    missing = module._CocolaRunControl()
    assert missing.final_event() == {
        "type": "error",
        "stage": "plan",
        "code": "PLAN_OUTPUT_INVALID",
        "error": "Claude did not return a reviewable plan. Refine the request and try again.",
    }


async def test_plan_permission_denial_has_a_structured_tool_outcome():
    module = _load_shim("cocola_agent_shim_structured_tool_outcome")
    control = module._CocolaRunControl()
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
    result = await module._CocolaRunControl().get_runtime_info({})

    assert calls == [
        ("claude", "--version"),
        ("node", "--version"),
        ("npm", "--version"),
    ]
    assert result["is_error"] is False


async def test_resumed_turn_explicitly_switches_the_sdk_permission_mode(monkeypatch):
    captured: dict[str, object] = {}
    calls: list[tuple[str, str]] = []
    emitted: list[dict[str, object]] = []

    class FakeClaudeAgentOptions:
        def __init__(self, **kwargs):
            captured["options"] = kwargs

    class SystemMessage:
        def __init__(self, subtype):
            self.subtype = subtype

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
            yield SystemMessage("init")
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
        SystemMessage=SystemMessage,
        query=forbidden_query,
    )
    monkeypatch.setitem(sys.modules, "claude_agent_sdk", fake_sdk)
    module = _load_shim("cocola_agent_shim_explicit_resume_permission")
    monkeypatch.setattr(module, "_emit", emitted.append)

    await module._run(
        {
            "prompt": "execute the approved plan",
            "resume": "claude-session",
            "permission_mode": "bypassPermissions",
            "user_input_enabled": False,
        }
    )

    assert calls == [
        ("permission", "bypassPermissions"),
        ("query", "execute the approved plan"),
    ]
    assert captured["options"]["resume"] == "claude-session"
    assert [event["type"] for event in emitted] == [
        "start",
        "run_accepted",
        "system",
        "result",
        "done",
    ]
    assert captured["options"]["disallowed_tools"] == ["AskUserQuestion"]


async def test_runtime_is_not_accepted_before_the_sdk_init_message(monkeypatch):
    emitted: list[dict[str, object]] = []

    class FakeClaudeAgentOptions:
        def __init__(self, **_kwargs):
            pass

    class SystemMessage:
        def __init__(self, subtype):
            self.subtype = subtype

    class FakeClaudeSDKClient:
        def __init__(self, *, options):
            self.options = options

        async def __aenter__(self):
            return self

        async def __aexit__(self, *_args):
            return None

        async def set_permission_mode(self, _mode):
            pass

        async def query(self, _prompt):
            pass

        async def receive_response(self):
            result = type(
                "ResultMessage",
                (),
                {
                    "is_error": True,
                    "num_turns": 0,
                    "total_cost_usd": 0,
                    "session_id": "",
                    "result": "resume failed",
                },
            )()
            yield result

    fake_sdk = types.SimpleNamespace(
        ClaudeAgentOptions=FakeClaudeAgentOptions,
        ClaudeSDKClient=FakeClaudeSDKClient,
        SystemMessage=SystemMessage,
    )
    monkeypatch.setitem(sys.modules, "claude_agent_sdk", fake_sdk)
    module = _load_shim("cocola_agent_shim_no_early_acceptance")
    monkeypatch.setattr(module, "_emit", emitted.append)

    await module._run(
        {
            "prompt": "continue",
            "resume": "missing-session",
            "permission_mode": "bypassPermissions",
            "user_input_enabled": False,
        }
    )

    assert "run_accepted" not in [event["type"] for event in emitted]


def test_plan_prompt_uses_only_cocola_control_tools():
    from cocola_agent_runtime.server import PLAN_SYSTEM_PROMPT

    assert "cocola_submit_plan" in PLAN_SYSTEM_PROMPT
    assert "cocola_request_user_input" in PLAN_SYSTEM_PROMPT
    assert "<cocola_plan>" not in PLAN_SYSTEM_PROMPT
    assert "ExitPlanMode" not in PLAN_SYSTEM_PROMPT
    assert "AskUserQuestion" not in PLAN_SYSTEM_PROMPT


async def test_execute_control_validates_and_emits_structured_result():
    module = _load_shim("cocola_agent_shim_structured_result")
    contract = {
        "version": 1,
        "renderer": "metrics",
        "contract_hash": "sha256:" + "a" * 64,
        "schema": {
            "type": "object",
            "properties": {"title": {"type": "string"}, "metrics": {"type": "array"}},
            "required": ["metrics"],
        },
    }
    control = module._CocolaRunControl(
        plan_mode=False,
        user_input_enabled=True,
        result_contract=contract,
    )

    await control.submit_result(
        {
            "title": "Build health",
            "metrics": [{"label": "Passed", "value": 42}],
        }
    )

    assert control.final_event() == {
        "type": "structured_result_ready",
        "renderer": "metrics",
        "renderer_version": 1,
        "contract_hash": contract["contract_hash"],
        "title": "Build health",
        "data": {
            "title": "Build health",
            "metrics": [{"label": "Passed", "value": 42}],
        },
    }
    assert control.should_interrupt() is True
    assert control.should_interrupt() is False


async def test_execute_question_is_terminal_and_suppresses_interrupt_result_error():
    module = _load_shim("cocola_agent_shim_execute_question_terminal")
    control = module._CocolaRunControl(plan_mode=False, user_input_enabled=True)
    await control.request_user_input(
        {"question": "Which database?", "options": ["PostgreSQL", "SQLite"]}
    )
    result = type(
        "ResultMessage",
        (),
        {
            "is_error": True,
            "num_turns": 2,
            "total_cost_usd": 0,
            "session_id": "claude-session",
            "result": "interrupted",
        },
    )()

    events = control.message_events(result, module._ClaudeTaskProgress())

    assert events[0]["is_error"] is False
    assert control.final_event()["type"] == "question_required"


async def test_structured_result_enforces_renderer_limits():
    module = _load_shim("cocola_agent_shim_structured_result_limits")
    contract = {
        "version": 1,
        "renderer": "table",
        "contract_hash": "sha256:" + "a" * 64,
        "schema": {"type": "object"},
    }
    control = module._CocolaRunControl(
        plan_mode=False,
        user_input_enabled=False,
        result_contract=contract,
    )

    response = await control.submit_result(
        {
            "columns": [f"column-{index}" for index in range(21)],
            "rows": [],
        }
    )

    assert response["is_error"] is True
    assert control.final_event()["code"] == "STRUCTURED_RESULT_INVALID"


async def test_structured_result_is_revalidated_against_the_contract_schema():
    module = _load_shim("cocola_agent_shim_structured_result_schema")
    contract = {
        "version": 1,
        "renderer": "metrics",
        "contract_hash": "sha256:" + "a" * 64,
        "schema": {
            "type": "object",
            "properties": {"metrics": {"type": "array"}},
            "required": ["metrics"],
            "additionalProperties": False,
        },
    }
    control = module._CocolaRunControl(
        plan_mode=False,
        user_input_enabled=False,
        result_contract=contract,
    )

    response = await control.submit_result({"title": "Missing metrics"})

    assert response["is_error"] is True
    assert response["content"][0]["text"] == (
        "Structured result does not match the Skill result schema."
    )
    assert control.final_event()["code"] == "STRUCTURED_RESULT_INVALID"


def test_result_contract_rejects_remote_schema_references():
    module = _load_shim("cocola_agent_shim_remote_schema_ref")
    contract = {
        "version": 1,
        "renderer": "summary",
        "contract_hash": "sha256:" + "a" * 64,
        "schema": {
            "type": "object",
            "properties": {"value": {"$ref": "https://example.invalid/value.json"}},
        },
    }

    try:
        module._normalized_result_contract(contract)
    except ValueError as error:
        assert str(error) == "result_contract schema or hash is invalid"
    else:
        raise AssertionError("remote schema reference must be rejected")


def test_execute_options_merge_control_with_user_mcps(monkeypatch):
    captured: dict[str, object] = {}
    sdk = _fake_sdk(captured)
    monkeypatch.setitem(sys.modules, "claude_agent_sdk", sdk)
    module = _load_shim("cocola_agent_shim_execute_control")
    control = module._CocolaRunControl(plan_mode=False, user_input_enabled=True)

    module._build_options(
        {
            "prompt": "implement it",
            "permission_mode": "bypassPermissions",
            "mcp_servers": {"github": {"type": "http", "url": "https://example.invalid"}},
        },
        run_control=control,
    )

    options = captured["options"]
    assert set(options["mcp_servers"]) == {"github", "cocola_control"}
    assert options["disallowed_tools"] == ["AskUserQuestion"]
