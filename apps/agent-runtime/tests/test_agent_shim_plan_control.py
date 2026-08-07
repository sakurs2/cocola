"""Contract tests for Cocola-owned Claude Plan Mode control."""

from __future__ import annotations

import asyncio
import importlib.util
import json
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


def _native_plan_fixture(monkeypatch, tmp_path, module, *, plan_path=None):
    session_config = tmp_path / "session" / "runtime" / "claude"
    projects = session_config / "projects" / "-session-workspace"
    projects.mkdir(parents=True)
    configured = tmp_path / "home" / "cocola" / ".claude"
    configured.parent.mkdir(parents=True)
    configured.symlink_to(session_config, target_is_directory=True)
    expected_plan = configured / "plans" / "trusted-plan.md"
    transcript = projects / "session.jsonl"
    transcript.write_text(
        json.dumps(
            {
                "type": "attachment",
                "attachment": {
                    "type": "plan_mode",
                    "planFilePath": str(plan_path or expected_plan),
                    "planExists": False,
                },
            }
        )
        + "\n",
        encoding="utf-8",
    )
    monkeypatch.setenv("CLAUDE_CONFIG_DIR", str(configured))
    monkeypatch.setattr(module, "_NATIVE_PLAN_SESSION_CONFIG", str(session_config))
    return transcript, expected_plan, session_config


def test_plan_options_preserve_native_plan_mode_and_install_trusted_controls(monkeypatch):
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
        "mcp__cocola_control__cocola_request_user_input",
        "mcp__cocola_control__cocola_get_runtime_info",
        "mcp__cocola_control__cocola_update_plan",
    }
    assert options["permission_mode"] == "plan"
    assert options["disallowed_tools"] == [
        "AskUserQuestion",
        "Edit",
        "NotebookEdit",
        "Write",
    ]
    assert "ExitPlanMode" not in options["disallowed_tools"]
    assert options["can_use_tool"] == control.can_use_tool
    assert options["env"] == {"ENABLE_TOOL_SEARCH": "1"}
    plan_instructions = options["extra_args"]["plan-mode-instructions"]
    assert "cocola_update_plan" in plan_instructions
    assert "ToolSearch" in plan_instructions
    assert "select:ExitPlanMode" in plan_instructions
    assert "max_results 1" in plan_instructions
    assert "ExitPlanMode" in plan_instructions
    assert "Do not use Skill" in plan_instructions
    assert "Never use Bash, mkdir, touch" in plan_instructions
    assert "owns both directory creation and persistence" in plan_instructions
    assert set(options["hooks"]) == {
        "PreToolUse",
        "PostToolUse",
        "PostToolUseFailure",
        "Stop",
    }


def test_reasoning_effort_maps_to_claude_agent_options(monkeypatch):
    captured: dict[str, object] = {}
    monkeypatch.setitem(sys.modules, "claude_agent_sdk", _fake_sdk(captured))
    module = _load_shim("cocola_agent_shim_reasoning_effort")

    module._build_options({"prompt": "solve it", "reasoning_effort": "max"})

    assert captured["options"]["effort"] == "max"


async def test_plan_permission_callback_never_grants_additional_permissions():
    module = _load_shim("cocola_agent_shim_plan_permission_callback")
    control = module._CocolaRunControl()
    context = types.SimpleNamespace(tool_use_id="bash-1")

    denial = await control.can_use_tool("Bash", {"command": "git status"}, context)

    assert denial.message == "Cocola Plan Mode does not grant interactive tool permissions."
    assert denial.interrupt is False


async def test_plan_permission_callback_captures_native_exit_before_denial():
    module = _load_shim("cocola_agent_shim_plan_exit_permission_callback")
    control = module._CocolaRunControl()
    context = types.SimpleNamespace(tool_use_id="exit-1")

    denial = await control.can_use_tool(
        "ExitPlanMode",
        {"plan": "## Plan\n\n- Implement safely"},
        context,
    )

    assert denial.message == "Cocola Plan Mode does not grant interactive tool permissions."
    assert control.final_event() == {
        "type": "plan_ready",
        "content_markdown": "## Plan\n\n- Implement safely",
    }


def test_plan_control_exposes_one_path_bound_plan_writer():
    module = _load_shim("cocola_agent_shim_plan_writer_tool")
    control = module._CocolaRunControl()
    server = control.sdk_server(_fake_sdk({}))
    tools = {tool.name: tool for tool in server["tools"]}

    assert "cocola_update_plan" in tools
    assert tools["cocola_update_plan"].annotations["readOnlyHint"] is False
    assert tools["cocola_get_runtime_info"].annotations["readOnlyHint"] is True


async def test_native_exit_plan_mode_emits_exactly_one_validated_terminal_event():
    module = _load_shim("cocola_agent_shim_structured_plan_terminal")
    control = module._CocolaRunControl()

    decision = await control.pre_tool_use(
        {
            "tool_name": "ExitPlanMode",
            "tool_use_id": "exit-1",
            "tool_input": {"plan": "## Plan\n\n- Inspect\n- Implement"},
        },
        "exit-1",
        {},
    )

    assert decision["hookSpecificOutput"]["permissionDecision"] == "deny"
    assert control.final_event() == {
        "type": "plan_ready",
        "content_markdown": "## Plan\n\n- Inspect\n- Implement",
    }
    duplicate = await control.request_user_input({"question": "Which branch?"})
    assert duplicate["is_error"] is True
    assert control.final_event()["code"] == "PLAN_OUTPUT_INVALID"


async def test_native_plan_hook_survives_empty_exit_plan_message_input():
    module = _load_shim("cocola_agent_shim_injected_native_plan")
    control = module._CocolaRunControl()
    await control.pre_tool_use(
        {
            "tool_name": "ExitPlanMode",
            "tool_use_id": "exit-1",
            "tool_input": {
                "plan": "## Injected plan\n\n- Implement safely",
                "planFilePath": "/home/cocola/.claude/plans/safe.md",
            },
        },
        "exit-1",
        {},
    )
    assistant = type(
        "AssistantMessage",
        (),
        {
            "content": [
                type(
                    "ToolUseBlock",
                    (),
                    {"id": "exit-1", "name": "ExitPlanMode", "input": {}},
                )()
            ]
        },
    )()

    assert control.message_events(assistant, module._ClaudeTaskProgress()) == []
    assert control.final_event() == {
        "type": "plan_ready",
        "content_markdown": "## Injected plan\n\n- Implement safely",
    }


def test_native_exit_plan_mode_message_capture_is_idempotent_and_hidden():
    module = _load_shim("cocola_agent_shim_native_plan_message")
    control = module._CocolaRunControl()
    assistant = type(
        "AssistantMessage",
        (),
        {
            "content": [
                type(
                    "ToolUseBlock",
                    (),
                    {
                        "id": "exit-1",
                        "name": "ExitPlanMode",
                        "input": {"plan": "## Initial plan"},
                    },
                )(),
                type(
                    "ToolUseBlock",
                    (),
                    {
                        "id": "exit-2",
                        "name": "ExitPlanMode",
                        "input": {"plan": "## Revised plan\n\n- Implement safely"},
                    },
                )(),
            ]
        },
    )()

    assert control.message_events(assistant, module._ClaudeTaskProgress()) == []
    assert control.final_event() == {
        "type": "plan_ready",
        "content_markdown": "## Revised plan\n\n- Implement safely",
    }


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

    assert ordinary == {}
    assert terminal["hookSpecificOutput"]["permissionDecision"] == "deny"
    assert control.final_event()["code"] == "QUESTION_OUTPUT_INVALID"
    assert control.should_interrupt() is True


async def test_plan_hook_rejects_general_workspace_writes():
    module = _load_shim("cocola_agent_shim_native_plan_permissions")
    control = module._CocolaRunControl()

    decision = await control.pre_tool_use(
        {
            "tool_name": "Write",
            "tool_use_id": "write-1",
            "tool_input": {
                "file_path": "/session/workspace/todo.html",
                "content": "must not run while planning",
            },
        },
        "write-1",
        {},
    )

    assert decision["hookSpecificOutput"]["permissionDecision"] == "deny"
    assert "cocola_update_plan" in decision["hookSpecificOutput"]["permissionDecisionReason"]
    assert control.final_event()["code"] == "PLAN_OUTPUT_INVALID"


async def test_plan_update_writes_native_file_then_preserves_exit_plan_mode(monkeypatch, tmp_path):
    module = _load_shim("cocola_agent_shim_trusted_plan_update")
    transcript, plan_path, _session_config = _native_plan_fixture(monkeypatch, tmp_path, module)
    control = module._CocolaRunControl()
    update_name = "mcp__cocola_control__cocola_update_plan"

    decision = await control.pre_tool_use(
        {
            "tool_name": update_name,
            "tool_use_id": "plan-update-1",
            "transcript_path": str(transcript),
            "tool_input": {"content_markdown": "## Plan\n\n- Inspect\n- Implement"},
        },
        "plan-update-1",
        {},
    )
    result = await control.update_plan({"content_markdown": "## Plan\n\n- Inspect\n- Implement"})
    await control.post_tool_use(
        {"tool_use_id": "plan-update-1"},
        "plan-update-1",
        {},
    )
    terminal = await control.pre_tool_use(
        {
            "tool_name": "ExitPlanMode",
            "tool_use_id": "exit-1",
            "tool_input": {"plan": "## Plan\n\n- Inspect\n- Implement"},
        },
        "exit-1",
        {},
    )

    assert decision["hookSpecificOutput"]["permissionDecision"] == "allow"
    assert result["is_error"] is False
    assert plan_path.read_text(encoding="utf-8") == "## Plan\n\n- Inspect\n- Implement\n"
    assert terminal["hookSpecificOutput"]["permissionDecision"] == "deny"
    assert control.final_event() == {
        "type": "plan_ready",
        "content_markdown": "## Plan\n\n- Inspect\n- Implement",
    }


async def test_plan_update_and_exit_cannot_run_in_parallel(monkeypatch, tmp_path):
    module = _load_shim("cocola_agent_shim_parallel_plan_update")
    transcript, _plan_path, _session_config = _native_plan_fixture(monkeypatch, tmp_path, module)
    control = module._CocolaRunControl()

    await control.pre_tool_use(
        {
            "tool_name": "mcp__cocola_control__cocola_update_plan",
            "tool_use_id": "plan-update-1",
            "transcript_path": str(transcript),
            "tool_input": {"content_markdown": "## Plan"},
        },
        "plan-update-1",
        {},
    )
    terminal = await control.pre_tool_use(
        {
            "tool_name": "ExitPlanMode",
            "tool_use_id": "exit-1",
            "tool_input": {"plan": "## Stale plan"},
        },
        "exit-1",
        {},
    )

    assert terminal["hookSpecificOutput"]["permissionDecision"] == "deny"
    assert control.final_event()["code"] == "PLAN_OUTPUT_INVALID"


async def test_plan_update_rejects_missing_or_untrusted_native_path(monkeypatch, tmp_path):
    module = _load_shim("cocola_agent_shim_untrusted_plan_update")
    transcript, trusted_plan, _session_config = _native_plan_fixture(
        monkeypatch,
        tmp_path,
        module,
        plan_path=tmp_path / "workspace" / "forged.md",
    )
    invalid_attachment = transcript.read_text(encoding="utf-8")
    transcript.write_text(
        json.dumps(
            {
                "type": "attachment",
                "attachment": {
                    "type": "plan_mode",
                    "planFilePath": str(trusted_plan),
                },
            }
        )
        + "\n"
        + invalid_attachment,
        encoding="utf-8",
    )
    control = module._CocolaRunControl()

    decision = await control.pre_tool_use(
        {
            "tool_name": "mcp__cocola_control__cocola_update_plan",
            "tool_use_id": "plan-update-1",
            "transcript_path": str(transcript),
            "tool_input": {"content_markdown": "## Forged"},
        },
        "plan-update-1",
        {},
    )
    result = await control.update_plan({"content_markdown": "## Forged"})

    assert decision["hookSpecificOutput"]["permissionDecision"] == "deny"
    assert result["is_error"] is True
    assert not (tmp_path / "workspace" / "forged.md").exists()


async def test_plan_update_rejects_symlinked_plan_directory(monkeypatch, tmp_path):
    module = _load_shim("cocola_agent_shim_symlinked_plan_update")
    transcript, _plan_path, session_config = _native_plan_fixture(monkeypatch, tmp_path, module)
    escaped = tmp_path / "escaped-plans"
    escaped.mkdir()
    (session_config / "plans").symlink_to(escaped, target_is_directory=True)
    control = module._CocolaRunControl()

    decision = await control.pre_tool_use(
        {
            "tool_name": "mcp__cocola_control__cocola_update_plan",
            "tool_use_id": "plan-update-1",
            "transcript_path": str(transcript),
            "tool_input": {"content_markdown": "## Must stay contained"},
        },
        "plan-update-1",
        {},
    )
    result = await control.update_plan({"content_markdown": "## Must stay contained"})

    assert decision["hookSpecificOutput"]["permissionDecision"] == "allow"
    assert result["is_error"] is True
    assert list(escaped.iterdir()) == []


async def test_denied_tool_result_releases_custom_terminal_gate():
    module = _load_shim("cocola_agent_shim_denied_plan_tool")
    control = module._CocolaRunControl()
    await control.pre_tool_use(
        {"tool_name": "Write", "tool_use_id": "write-1"},
        "write-1",
        {},
    )
    denied_result = type(
        "UserMessage",
        (),
        {
            "content": [
                type(
                    "ToolResultBlock",
                    (),
                    {
                        "tool_use_id": "write-1",
                        "content": "Permission denied",
                        "is_error": True,
                    },
                )()
            ]
        },
    )()

    control.message_events(denied_result, module._ClaudeTaskProgress())
    terminal = await control.pre_tool_use(
        {
            "tool_name": "mcp__cocola_control__cocola_request_user_input",
            "tool_use_id": "question-1",
        },
        "question-1",
        {},
    )
    await control.request_user_input({"question": "Which package?"})

    assert terminal["hookSpecificOutput"]["permissionDecision"] == "allow"
    assert control.final_event() == {
        "type": "question_required",
        "question": "Which package?",
        "options": [],
    }


async def test_native_plan_completion_ignores_denied_write_still_in_hook_flight():
    module = _load_shim("cocola_agent_shim_native_plan_after_denial")
    control = module._CocolaRunControl()
    await control.pre_tool_use(
        {"tool_name": "Write", "tool_use_id": "write-1"},
        "write-1",
        {},
    )

    terminal = await control.pre_tool_use(
        {
            "tool_name": "ExitPlanMode",
            "tool_use_id": "exit-1",
            "tool_input": {"plan": "## Plan\n\n- Implement safely"},
        },
        "exit-1",
        {},
    )

    assert terminal["hookSpecificOutput"]["permissionDecision"] == "deny"
    assert control.final_event() == {
        "type": "plan_ready",
        "content_markdown": "## Plan\n\n- Implement safely",
    }


async def test_plan_stop_hook_reprompts_once_for_native_completion():
    module = _load_shim("cocola_agent_shim_plan_stop_hook")
    control = module._CocolaRunControl()

    first_stop = await control.stop(
        {"hook_event_name": "Stop", "stop_hook_active": False},
        None,
        {},
    )
    repeated_stop = await control.stop(
        {"hook_event_name": "Stop", "stop_hook_active": True},
        None,
        {},
    )

    assert first_stop["decision"] == "block"
    assert "cocola_update_plan" in first_stop["reason"]
    assert "ToolSearch" in first_stop["reason"]
    assert "select:ExitPlanMode" in first_stop["reason"]
    assert "ExitPlanMode" in first_stop["reason"]
    assert repeated_stop == {}


async def test_plan_stop_hook_only_requests_native_exit_after_plan_persistence(
    monkeypatch, tmp_path
):
    module = _load_shim("cocola_agent_shim_persisted_plan_stop_hook")
    transcript, _plan_path, _session_config = _native_plan_fixture(monkeypatch, tmp_path, module)
    control = module._CocolaRunControl()
    await control.pre_tool_use(
        {
            "tool_name": "mcp__cocola_control__cocola_update_plan",
            "tool_use_id": "plan-update-1",
            "transcript_path": str(transcript),
        },
        "plan-update-1",
        {},
    )
    result = await control.update_plan({"content_markdown": "## Plan\n\n- Implement"})
    await control.post_tool_use({"tool_use_id": "plan-update-1"}, "plan-update-1", {})

    stopped = await control.stop({"stop_hook_active": False}, None, {})

    assert result["is_error"] is False
    assert stopped["decision"] == "block"
    assert "already persisted" in stopped["reason"]
    assert "ToolSearch" in stopped["reason"]
    assert "select:ExitPlanMode" in stopped["reason"]
    assert "ExitPlanMode" in stopped["reason"]
    assert "Call cocola_update_plan" not in stopped["reason"]


async def test_plan_stop_hook_allows_native_plan_or_question_terminal():
    module = _load_shim("cocola_agent_shim_completed_plan_stop_hook")
    plan = module._CocolaRunControl()
    await plan.pre_tool_use(
        {
            "tool_name": "ExitPlanMode",
            "tool_use_id": "exit-1",
            "tool_input": {"plan": "## Plan\n\n- Ship it"},
        },
        "exit-1",
        {},
    )
    question = module._CocolaRunControl()
    await question.request_user_input({"question": "Which package?"})

    assert await plan.stop({"stop_hook_active": False}, None, {}) == {}
    assert await question.stop({"stop_hook_active": False}, None, {}) == {}


async def test_execute_mode_stop_hook_never_reprompts():
    module = _load_shim("cocola_agent_shim_execute_stop_hook")
    control = module._CocolaRunControl(plan_mode=False)

    assert await control.stop({"stop_hook_active": False}, None, {}) == {}


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


async def test_native_exit_plan_mode_rejects_oversized_plan():
    module = _load_shim("cocola_agent_shim_native_plan_size_limit")
    control = module._CocolaRunControl()
    await control.pre_tool_use(
        {
            "tool_name": "ExitPlanMode",
            "tool_use_id": "exit-large",
            "tool_input": {"plan": "x" * (128 * 1024 + 1)},
        },
        "exit-large",
        {},
    )

    assert control.final_event()["code"] == "PLAN_OUTPUT_INVALID"


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


def test_plan_prompt_uses_claude_native_plan_completion():
    from cocola_agent_runtime.server import PLAN_SYSTEM_PROMPT

    assert "cocola_submit_plan" not in PLAN_SYSTEM_PROMPT
    assert "cocola_update_plan" in PLAN_SYSTEM_PROMPT
    assert "cocola_request_user_input" in PLAN_SYSTEM_PROMPT
    assert "native plan file" in PLAN_SYSTEM_PROMPT
    assert "ToolSearch" in PLAN_SYSTEM_PROMPT
    assert "Never use Skill" in PLAN_SYSTEM_PROMPT
    assert "<cocola_plan>" not in PLAN_SYSTEM_PROMPT
    assert "ExitPlanMode" in PLAN_SYSTEM_PROMPT
    assert "never call Write" in PLAN_SYSTEM_PROMPT
    assert "Never use Bash, mkdir, touch" in PLAN_SYSTEM_PROMPT
    assert "owns both directory creation and persistence" in PLAN_SYSTEM_PROMPT
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
        result_policy="required",
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
        result_policy="required",
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
        result_policy="required",
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


def test_builtin_structured_output_is_optional_and_registers_typed_tools():
    captured: dict[str, object] = {}
    sdk = _fake_sdk(captured)
    module = _load_shim("cocola_agent_shim_builtin_structured_output")
    control = module._CocolaRunControl(
        plan_mode=False,
        user_input_enabled=False,
        result_policy="optional",
    )

    server = control.sdk_server(sdk)
    tools = {tool.name: tool for tool in server["tools"]}

    assert set(tools) == {
        "cocola_present_summary",
        "cocola_present_table",
        "cocola_present_list",
        "cocola_present_metrics",
    }
    assert tools["cocola_present_table"].input_schema["required"] == ["columns", "rows"]
    assert control.final_event() is None


async def test_builtin_table_submission_emits_structured_result():
    module = _load_shim("cocola_agent_shim_builtin_table_result")
    control = module._CocolaRunControl(
        plan_mode=False,
        user_input_enabled=False,
        result_policy="optional",
    )

    await control.submit_result(
        {
            "title": "Options",
            "columns": ["Name", "Status"],
            "rows": [
                {"Name": "Alpha", "Status": "Ready"},
                {"Name": "Beta", "Status": "Blocked"},
            ],
        },
        "cocola_present_table",
    )

    event = control.final_event()
    assert event["type"] == "structured_result_ready"
    assert event["renderer"] == "table"
    assert event["renderer_version"] == 1
    assert event["title"] == "Options"
    assert event["contract_hash"].startswith("sha256:")
    assert event["data"]["rows"][1]["Status"] == "Blocked"


async def test_invalid_builtin_result_fails_without_markdown_fallback():
    module = _load_shim("cocola_agent_shim_builtin_result_invalid")
    control = module._CocolaRunControl(
        plan_mode=False,
        user_input_enabled=False,
        result_policy="optional",
    )

    response = await control.submit_result(
        {"title": "Missing rows", "columns": ["Name"]},
        "cocola_present_table",
    )

    assert response["is_error"] is True
    assert control.final_event()["code"] == "STRUCTURED_RESULT_INVALID"


async def test_builtin_result_rejects_non_json_numbers():
    module = _load_shim("cocola_agent_shim_builtin_non_json_number")
    control = module._CocolaRunControl(
        plan_mode=False,
        user_input_enabled=False,
        result_policy="optional",
    )

    response = await control.submit_result(
        {"metrics": [{"label": "Latency", "value": float("nan")}]},
        "cocola_present_metrics",
    )

    assert response["is_error"] is True
    assert response["content"][0]["text"] == "Structured result must be valid JSON."
    assert control.final_event()["code"] == "STRUCTURED_RESULT_INVALID"


def test_required_skill_result_and_builtin_results_are_mutually_exclusive():
    module = _load_shim("cocola_agent_shim_result_policy_conflict")
    contract = {
        "version": 1,
        "renderer": "summary",
        "contract_hash": "sha256:" + "a" * 64,
        "schema": {"type": "object"},
    }

    try:
        module._CocolaRunControl(
            plan_mode=False,
            result_contract=contract,
            result_policy="optional",
        )
    except ValueError as error:
        assert "valid only with required" in str(error)
    else:
        raise AssertionError("conflicting structured result policies must fail closed")


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
    assert "env" not in options
    assert "extra_args" not in options
    assert "can_use_tool" not in options
