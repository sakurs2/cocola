"""Unit-test the Route-A sandbox shim option translation without real SDK calls."""

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


def test_agent_shim_passes_mcp_servers_to_claude_options(monkeypatch):
    captured = {}

    class FakeClaudeAgentOptions:
        def __init__(self, **kwargs):
            captured.update(kwargs)

    fake_sdk = types.SimpleNamespace(ClaudeAgentOptions=FakeClaudeAgentOptions)
    monkeypatch.setitem(sys.modules, "claude_agent_sdk", fake_sdk)

    module = _load_shim("cocola_agent_shim_test")

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
    assert captured["setting_sources"] == ["user", "project"]
    assert captured["skills"] == "all"


async def test_agent_shim_streams_mcp_status_without_blocking_query(monkeypatch):
    captured: dict[str, object] = {}
    calls: list[str] = []

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

        async def get_mcp_status(self):
            calls.append("status")
            return {
                "mcpServers": [
                    {
                        "name": "maps",
                        "status": "connected",
                        "serverInfo": {"name": "Amap", "version": "1.0"},
                        "tools": [{"name": "weather"}],
                    }
                ]
            }

        async def query(self, prompt):
            calls.append("query")
            captured["prompt"] = prompt
            await asyncio.sleep(0)

        async def receive_response(self):
            result_type = type("ResultMessage", (), {})
            result = result_type()
            result.is_error = False
            result.num_turns = 1
            result.total_cost_usd = 0
            result.session_id = "claude-session"
            result.result = "done"
            yield result

    fake_sdk = types.SimpleNamespace(
        ClaudeAgentOptions=FakeClaudeAgentOptions,
        ClaudeSDKClient=FakeClaudeSDKClient,
    )
    monkeypatch.setitem(sys.modules, "claude_agent_sdk", fake_sdk)

    module = _load_shim("cocola_agent_shim_status_test")
    emitted: list[dict] = []
    monkeypatch.setattr(module, "_emit", emitted.append)

    await module._run(
        {
            "prompt": "weather?",
            "skill_id": "weather",
            "mcp_servers": {"maps": {"type": "http", "url": "https://mcp.example.test/mcp"}},
        }
    )

    assert captured["prompt"] == "/weather\n\nweather?"
    assert calls[:2] == ["query", "status"]
    snapshots = [event for event in emitted if event.get("type") == "environment_status"]
    assert [snapshot["phase"] for snapshot in snapshots] == ["preparing", "ready"]
    assert snapshots[-1]["components"] == [
        {
            "kind": "mcp",
            "id": "maps",
            "label": "Amap",
            "status": "connected",
            "tool_count": 1,
        }
    ]
    assert emitted[-1]["type"] == "done"
    assert emitted[-1]["session_id"] == "claude-session"


async def test_agent_shim_skips_mcp_status_on_resumed_turn(monkeypatch):
    captured: dict[str, object] = {}

    class FakeClaudeAgentOptions:
        def __init__(self, **kwargs):
            captured["options"] = kwargs

    class UnexpectedClaudeSDKClient:
        def __init__(self, **_kwargs):
            raise AssertionError("resumed turns must use the one-shot SDK path")

    async def fake_query(*, prompt, options):
        captured["prompt"] = prompt
        captured["client_options"] = options
        result_type = type("ResultMessage", (), {})
        result = result_type()
        result.is_error = False
        result.num_turns = 1
        result.total_cost_usd = 0
        result.session_id = "claude-session"
        result.result = "done"
        yield result

    fake_sdk = types.SimpleNamespace(
        ClaudeAgentOptions=FakeClaudeAgentOptions,
        ClaudeSDKClient=UnexpectedClaudeSDKClient,
        query=fake_query,
    )
    monkeypatch.setitem(sys.modules, "claude_agent_sdk", fake_sdk)

    module = _load_shim("cocola_agent_shim_resume_status_test")
    emitted: list[dict] = []
    monkeypatch.setattr(module, "_emit", emitted.append)

    await module._run(
        {
            "prompt": "and tomorrow?",
            "resume": "claude-session",
            "mcp_servers": {"maps": {"type": "http", "url": "https://mcp.example.test/mcp"}},
        }
    )

    assert captured["prompt"] == "and tomorrow?"
    assert captured["options"]["resume"] == "claude-session"
    assert not [event for event in emitted if event.get("type") == "environment_status"]
    assert emitted[-1]["type"] == "done"
    assert emitted[-1]["session_id"] == "claude-session"


def test_agent_shim_maps_todo_write_to_one_progress_node():
    module = _load_shim("cocola_agent_shim_todo_test")
    task_progress = module._ClaudeTaskProgress()

    tool = type("ToolUseBlock", (), {})()
    tool.id = "todo-call-1"
    tool.name = "TodoWrite"
    tool.input = {
        "todos": [
            {"content": "Inspect the project", "status": "completed"},
            {"content": "Implement the change", "status": "in_progress"},
        ]
    }
    assistant = type("AssistantMessage", (), {})()
    assistant.content = [tool]

    result = type("ToolResultBlock", (), {})()
    result.tool_use_id = "todo-call-1"
    result.is_error = False
    result.content = "Todos have been modified successfully"
    user = type("UserMessage", (), {})()
    user.content = [result]

    assert module._message_to_events(assistant, task_progress) == [
        {
            "type": "progress",
            "id": "todo-list",
            "items": tool.input["todos"],
        }
    ]
    assert module._message_to_events(user, task_progress) == []


def test_agent_shim_collapses_claude_task_tools_into_one_progress_node():
    module = _load_shim("cocola_agent_shim_task_tools_test")
    task_progress = module._ClaudeTaskProgress()

    create_one = type("ToolUseBlock", (), {})()
    create_one.id = "create-1"
    create_one.name = "TaskCreate"
    create_one.input = {
        "subject": "Inspect the project",
        "description": "Find the relevant files",
        "activeForm": "Inspecting the project",
    }
    create_two = type("ToolUseBlock", (), {})()
    create_two.id = "create-2"
    create_two.name = "TaskCreate"
    create_two.input = {
        "subject": "Implement the change",
        "description": "Update the code",
        "activeForm": "Implementing the change",
    }
    assistant = type("AssistantMessage", (), {})()
    assistant.content = [create_one, create_two]

    create_events = module._message_to_events(assistant, task_progress)
    assert [event["type"] for event in create_events] == ["progress", "progress"]
    assert [item["content"] for item in create_events[-1]["items"]] == [
        "Inspect the project",
        "Implement the change",
    ]

    result_one = type("ToolResultBlock", (), {})()
    result_one.tool_use_id = "create-1"
    result_one.is_error = False
    result_one.content = "Task #1 created successfully: Inspect the project"
    result_two = type("ToolResultBlock", (), {})()
    result_two.tool_use_id = "create-2"
    result_two.is_error = False
    result_two.content = "Task #2 created successfully: Implement the change"
    user = type("UserMessage", (), {})()
    user.content = [result_one, result_two]

    result_events = module._message_to_events(user, task_progress)
    assert [item["id"] for item in result_events[-1]["items"]] == ["1", "2"]

    update = type("ToolUseBlock", (), {})()
    update.id = "update-1"
    update.name = "TaskUpdate"
    update.input = {"taskId": "1", "status": "completed"}
    assistant.content = [update]
    assert module._message_to_events(assistant, task_progress) == []

    update_result = type("ToolResultBlock", (), {})()
    update_result.tool_use_id = "update-1"
    update_result.is_error = False
    update_result.content = "Updated task #1 status"
    user.content = [update_result]
    update_events = module._message_to_events(user, task_progress)
    assert update_events == [
        {
            "type": "progress",
            "id": "todo-list",
            "items": [
                {
                    "id": "1",
                    "content": "Inspect the project",
                    "status": "completed",
                    "activeForm": "Inspecting the project",
                },
                {
                    "id": "2",
                    "content": "Implement the change",
                    "status": "pending",
                    "activeForm": "Implementing the change",
                },
            ],
        }
    ]


def test_agent_shim_restores_task_progress_from_task_list_and_get_results():
    module = _load_shim("cocola_agent_shim_task_snapshot_test")
    task_progress = module._ClaudeTaskProgress()
    assistant = type("AssistantMessage", (), {})()
    user = type("UserMessage", (), {})()

    task_list = type("ToolUseBlock", (), {})()
    task_list.id = "list-1"
    task_list.name = "TaskList"
    task_list.input = {}
    assistant.content = [task_list]
    assert module._message_to_events(assistant, task_progress) == []

    list_result = type("ToolResultBlock", (), {})()
    list_result.tool_use_id = "list-1"
    list_result.is_error = False
    list_result.content = (
        "#1 [completed] Inspect the project\n#2 [in_progress] Implement the change"
    )
    user.content = [list_result]
    events = module._message_to_events(user, task_progress)
    assert [(item["id"], item["status"]) for item in events[0]["items"]] == [
        ("1", "completed"),
        ("2", "in_progress"),
    ]

    task_get = type("ToolUseBlock", (), {})()
    task_get.id = "get-2"
    task_get.name = "TaskGet"
    task_get.input = {"taskId": "2"}
    assistant.content = [task_get]
    assert module._message_to_events(assistant, task_progress) == []

    get_result = type("ToolResultBlock", (), {})()
    get_result.tool_use_id = "get-2"
    get_result.is_error = False
    get_result.content = [
        {
            "type": "text",
            "text": '{"task":{"id":"2","subject":"Implement and verify","status":"in_progress"}}',
        }
    ]
    user.content = [get_result]
    events = module._message_to_events(user, task_progress)
    assert events[0]["items"][1]["content"] == "Implement and verify"

    empty_list = type("ToolUseBlock", (), {})()
    empty_list.id = "list-2"
    empty_list.name = "TaskList"
    empty_list.input = {}
    assistant.content = [empty_list]
    module._message_to_events(assistant, task_progress)
    list_result.tool_use_id = "list-2"
    list_result.content = "No tasks found"
    user.content = [list_result]
    assert module._message_to_events(user, task_progress)[0]["items"] == []


def test_agent_shim_preserves_invalid_todo_write_as_a_tool_call():
    module = _load_shim("cocola_agent_shim_invalid_todo_test")
    task_progress = module._ClaudeTaskProgress()
    tool = type("ToolUseBlock", (), {})()
    tool.id = "todo-call-2"
    tool.name = "TodoWrite"
    tool.input = {"todos": "not-a-list"}

    assert module._block_to_event(tool, task_progress) == {
        "type": "tool_use",
        "id": "todo-call-2",
        "name": "TodoWrite",
        "input": {"todos": "not-a-list"},
    }
