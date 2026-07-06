"""AgentRuntimeServicer tests.

Hermetic: no gRPC server, no socket. We call the servicer\'s Query coroutine
directly with a fake provider, a fake streaming context that records written
proto events, and a plain request object. We assert (a) generic AgentEvents map
onto proto AgentEvents with non-string data flattened, (b) a provider error
becomes a terminal proto `error` event instead of propagating, and (c) enabled
skills are folded into the AgentOptions the provider receives.
"""

import base64
import json
from dataclasses import dataclass, field

from cocola_agent_runtime.agent_provider import AgentEvent, AgentOptions
from cocola_agent_runtime.checkpoint import CheckpointConfig, CheckpointManager
from cocola_agent_runtime.prompt_loader import PromptMarker, StaticPromptCatalog
from cocola_agent_runtime.sandbox_binder import (
    ExecOutcome,
    StaticSandboxBinder,
    StaticSandboxExecutor,
)
from cocola_agent_runtime.server import AgentRuntimeServicer, event_to_proto
from cocola_agent_runtime.session_map import SessionBinding
from cocola_agent_runtime.skill_loader import Skill, StaticSkillCatalog


@dataclass
class FakeRequest:
    user_id: str = "U1"
    session_id: str = "S1"
    prompt: str = "hi"
    sandbox_id: str = ""
    max_turns: int = 0
    attachments: list = field(default_factory=list)


class FakeContext:
    """Records proto events the servicer streams via context.write()."""

    def __init__(self):
        self.written = []

    def invocation_metadata(self):
        return ()

    async def write(self, event):
        self.written.append(event)


class ListProvider:
    """AgentProvider yielding a fixed list; records the options it was given."""

    def __init__(self, events):
        self._events = events
        self.seen_options: AgentOptions | None = None

    async def query(self, prompt, options):
        self.seen_options = options
        for e in self._events:
            yield e


class BoomProvider:
    def __init__(self):
        self.seen_options = None

    async def query(self, prompt, options):
        self.seen_options = options
        yield AgentEvent(kind="text", data={"text": "partial"})
        raise RuntimeError("provider exploded")


class FakeObjectStore:
    def __init__(self):
        self.puts = {}

    def get(self, key: str) -> bytes:
        value = self.puts[key]
        if isinstance(value, tuple):
            return value[0]
        return value

    def put(self, key: str, data: bytes, mime: str) -> None:
        self.puts[key] = (data, mime)


class FakeSessionMap:
    def __init__(self, *, fail_delete: bool = False):
        self.deleted = []
        self.fail_delete = fail_delete
        self.bindings = {}
        self.checkpoints = {}
        self.put_checkpoint_calls = []

    async def get(self, session_id: str):
        binding = await self.get_binding(session_id)
        return binding.claude_session_id if binding else None

    async def get_binding(self, session_id: str):
        return self.bindings.get(session_id)

    async def put(
        self, session_id: str, claude_session_id: str, *, user_id: str = "", sandbox_id: str = ""
    ):
        self.bindings[session_id] = SessionBinding(
            claude_session_id=claude_session_id,
            sandbox_id=sandbox_id,
            checkpoint_object_key=self.checkpoints.get(session_id, ""),
        )

    async def get_checkpoint(self, session_id: str):
        return self.checkpoints.get(session_id)

    async def put_checkpoint(self, session_id: str, object_key: str, *, size_bytes: int = 0):
        self.put_checkpoint_calls.append((session_id, object_key))
        self.checkpoints[session_id] = object_key

    async def put_checkpoint_failure(self, session_id: str, error: str):
        return None

    async def delete(self, session_id: str):
        self.deleted.append(session_id)
        if self.fail_delete:
            raise RuntimeError("delete failed")
        self.bindings.pop(session_id, None)
        self.checkpoints.pop(session_id, None)

    async def aclose(self):
        return None


class OutputWritingProvider:
    def __init__(self, executor: StaticSandboxExecutor):
        self._executor = executor
        self.seen_options: AgentOptions | None = None

    async def query(self, prompt, options):
        self.seen_options = options
        await self._executor.write_bytes(
            sandbox_id=options.sandbox_id,
            path="outputs/report.txt",
            data=b"hello world",
        )
        await self._executor.write_bytes(
            sandbox_id=options.sandbox_id,
            path="scratch.txt",
            data=b"not public",
        )
        yield AgentEvent(kind="text", data={"text": "done"})
        yield AgentEvent(kind="done", data={})


class StaticMCPCatalog:
    def __init__(self, servers):
        self.servers = servers
        self.seen_user_id = ""

    def effective_mcp_servers(self, user_id: str = ""):
        self.seen_user_id = user_id
        return dict(self.servers)


def outputs_snapshot_executor() -> StaticSandboxExecutor:
    ex: StaticSandboxExecutor

    def exec_handler(sandbox_id, cmd):
        if cmd[:2] == ["python3", "-c"]:
            out = {}
            for (sid, path), data in ex.byte_files.items():
                if sid == sandbox_id and path.startswith("outputs/"):
                    out[path] = {"size": len(data), "mtime_ns": len(data)}
            return ExecOutcome(exit_code=0, stdout=json.dumps(out))
        return ExecOutcome(exit_code=0)

    ex = StaticSandboxExecutor(exec_handler=exec_handler)
    return ex


def test_event_to_proto_flattens_non_strings():
    proto = event_to_proto(
        AgentEvent(
            kind="tool_use",
            data={
                "name": "bash",
                "input": {"cmd": "ls"},
                "n": 3,
                "nothing": None,
            },
        )
    )
    assert proto.kind == "tool_use"
    assert proto.data["name"] == "bash"
    assert json.loads(proto.data["input"]) == {"cmd": "ls"}
    assert proto.data["n"] == "3"
    assert proto.data["nothing"] == ""


async def test_query_streams_mapped_events():
    prov = ListProvider(
        [
            AgentEvent(kind="text", data={"text": "hello"}),
            AgentEvent(kind="done", data={}),
        ]
    )
    ctx = FakeContext()
    await AgentRuntimeServicer(prov).Query(FakeRequest(), ctx)
    kinds = [e.kind for e in ctx.written]
    assert kinds == ["text", "done"]
    assert ctx.written[0].data["text"] == "hello"


async def test_query_error_becomes_terminal_event():
    ctx = FakeContext()
    await AgentRuntimeServicer(BoomProvider()).Query(FakeRequest(), ctx)
    kinds = [e.kind for e in ctx.written]
    assert kinds == ["text", "error"]
    assert "provider exploded" in ctx.written[-1].data["error"]


async def test_query_folds_enabled_skills_into_options():
    prov = ListProvider([AgentEvent(kind="done", data={})])
    cat = StaticSkillCatalog([Skill(id="web", name="Web Search")])
    await AgentRuntimeServicer(prov, skills=cat).Query(FakeRequest(), FakeContext())
    assert prov.seen_options.system_prompt is not None
    assert "Web Search" in prov.seen_options.system_prompt


async def test_query_loads_mcp_servers_into_options_and_trace():
    prov = ListProvider([AgentEvent(kind="done", data={})])
    mcps = StaticMCPCatalog({"amap": {"type": "http", "url": "https://mcp.example.com/mcp"}})
    ctx = FakeContext()

    await AgentRuntimeServicer(prov, mcps=mcps).Query(FakeRequest(user_id="alice"), ctx)

    assert mcps.seen_user_id == "alice"
    assert prov.seen_options.mcp_servers == {
        "amap": {"type": "http", "url": "https://mcp.example.com/mcp"}
    }
    traces = [event for event in ctx.written if event.kind == "trace"]
    mcp_trace = next(event for event in traces if event.data["name"] == "sandbox.mcp_config_load")
    assert mcp_trace.data["mcp_count"] == "1"
    assert json.loads(mcp_trace.data["mcp_names"]) == ["amap"]


async def test_query_loads_admin_prompt_into_options_and_trace():
    prov = ListProvider([AgentEvent(kind="done", data={})])
    prompts = StaticPromptCatalog(
        "Prefer concise answers.",
        [PromptMarker(id="global", version=4, content_length=23)],
    )
    ctx = FakeContext()

    await AgentRuntimeServicer(prov, prompts=prompts).Query(FakeRequest(user_id="alice"), ctx)

    assert prov.seen_options.system_prompt is not None
    assert "Administrator-configured system instructions:" in prov.seen_options.system_prompt
    assert "Prefer concise answers." in prov.seen_options.system_prompt
    traces = [event for event in ctx.written if event.kind == "trace"]
    prompt_trace = next(
        event for event in traces if event.data["name"] == "agent.prompt_config_load"
    )
    assert prompt_trace.data["prompt_count"] == "1"
    assert json.loads(prompt_trace.data["prompt_ids"]) == ["global"]
    assert json.loads(prompt_trace.data["prompt_versions"]) == [4]
    assert "Prefer concise answers." not in json.dumps(dict(prompt_trace.data))


async def test_query_maps_request_fields_to_options():
    prov = ListProvider([AgentEvent(kind="done", data={})])
    req = FakeRequest(user_id="emp-9", session_id="sess-7", sandbox_id="box-1", max_turns=5)
    await AgentRuntimeServicer(prov).Query(req, FakeContext())
    o = prov.seen_options
    assert o.user_id == "emp-9" and o.session_id == "sess-7"
    assert o.sandbox_id == "box-1" and o.max_turns == 5


async def test_release_session_calls_binder_and_session_map():
    binder = StaticSandboxBinder()
    session_map = FakeSessionMap()

    await AgentRuntimeServicer(
        ListProvider([]),
        binder=binder,
        session_map=session_map,
    ).ReleaseSession(FakeRequest(session_id="sess-7"), FakeContext())

    assert binder.released == ["sess-7"]
    assert session_map.deleted == ["sess-7"]


async def test_release_session_without_binder_succeeds_and_delete_failure_is_best_effort():
    session_map = FakeSessionMap(fail_delete=True)

    resp = await AgentRuntimeServicer(
        ListProvider([]),
        session_map=session_map,
    ).ReleaseSession(FakeRequest(session_id="sess-8"), FakeContext())

    assert resp is not None
    assert session_map.deleted == ["sess-8"]


async def test_release_session_checkpoints_before_releasing_sandbox():
    archive = b"checkpoint-bytes"

    def exec_handler(sandbox_id, cmd):
        assert sandbox_id == "box-sess-7"
        return ExecOutcome(exit_code=0, stdout=base64.b64encode(archive).decode("ascii"))

    binder = StaticSandboxBinder()
    executor = StaticSandboxExecutor(exec_handler=exec_handler)
    store = FakeObjectStore()
    session_map = FakeSessionMap()
    session_map.bindings["sess-7"] = SessionBinding(
        claude_session_id="claude-1",
        sandbox_id="box-sess-7",
    )
    checkpoint = CheckpointManager(
        objstore=store,
        executor=executor,
        session_map=session_map,
        config=CheckpointConfig(),
    )

    await AgentRuntimeServicer(
        ListProvider([]),
        binder=binder,
        executor=executor,
        objstore=store,
        session_map=session_map,
        checkpoint=checkpoint,
    ).ReleaseSession(FakeRequest(session_id="sess-7"), FakeContext())

    assert binder.released == ["sess-7"]
    assert session_map.deleted == ["sess-7"]
    assert len(store.puts) == 1
    key = next(iter(store.puts))
    assert key.startswith("checkpoints/U1/sess-7/")
    assert store.puts[key] == (archive, "application/zstd")
    assert session_map.put_checkpoint_calls == [("sess-7", key)]


async def test_checkpoint_failure_does_not_block_release():
    def exec_handler(sandbox_id, cmd):
        return ExecOutcome(exit_code=1, stderr="tar failed")

    binder = StaticSandboxBinder()
    executor = StaticSandboxExecutor(exec_handler=exec_handler)
    store = FakeObjectStore()
    session_map = FakeSessionMap()
    session_map.bindings["sess-9"] = SessionBinding(
        claude_session_id="claude-1",
        sandbox_id="box-sess-9",
    )
    checkpoint = CheckpointManager(
        objstore=store,
        executor=executor,
        session_map=session_map,
        config=CheckpointConfig(),
    )

    await AgentRuntimeServicer(
        ListProvider([]),
        binder=binder,
        executor=executor,
        objstore=store,
        session_map=session_map,
        checkpoint=checkpoint,
    ).ReleaseSession(FakeRequest(session_id="sess-9"), FakeContext())

    assert binder.released == ["sess-9"]
    assert session_map.deleted == ["sess-9"]
    assert store.puts == {}


async def test_query_restores_checkpoint_for_fresh_sandbox_before_agent_runs():
    prov = ListProvider([AgentEvent(kind="done", data={})])
    executor = StaticSandboxExecutor()
    store = FakeObjectStore()
    store.puts["ck-latest"] = b"checkpoint-bytes"
    session_map = FakeSessionMap()
    session_map.checkpoints["S1"] = "ck-latest"
    checkpoint = CheckpointManager(
        objstore=store,
        executor=executor,
        session_map=session_map,
        config=CheckpointConfig(),
    )

    await AgentRuntimeServicer(
        prov,
        binder=StaticSandboxBinder(),
        executor=executor,
        objstore=store,
        session_map=session_map,
        checkpoint=checkpoint,
    ).Query(FakeRequest(), FakeContext())

    assert executor.exec_calls
    first = executor.exec_calls[0]
    assert first["sandbox_id"] == "box-S1"
    assert "zstd -d -c" in first["cmd"][2]
    assert base64.b64decode(first["stdin"]) == b"checkpoint-bytes"


async def test_query_does_not_restore_checkpoint_for_reused_sandbox():
    prov = ListProvider([AgentEvent(kind="done", data={})])
    binder = StaticSandboxBinder()
    await binder.acquire(session_id="S1", user_id="U1")
    executor = StaticSandboxExecutor()
    store = FakeObjectStore()
    store.puts["ck-latest"] = b"checkpoint-bytes"
    session_map = FakeSessionMap()
    session_map.checkpoints["S1"] = "ck-latest"
    checkpoint = CheckpointManager(
        objstore=store,
        executor=executor,
        session_map=session_map,
        config=CheckpointConfig(),
    )

    await AgentRuntimeServicer(
        prov,
        binder=binder,
        executor=executor,
        objstore=store,
        session_map=session_map,
        checkpoint=checkpoint,
    ).Query(FakeRequest(), FakeContext())

    assert not any("zstd -d -c" in call["cmd"][2] for call in executor.exec_calls)


async def test_query_publishes_outputs_artifacts():
    ex = outputs_snapshot_executor()
    store = FakeObjectStore()
    prov = OutputWritingProvider(ex)
    ctx = FakeContext()

    await AgentRuntimeServicer(
        prov,
        binder=StaticSandboxBinder(),
        executor=ex,
        objstore=store,
    ).Query(FakeRequest(), ctx)

    kinds = [e.kind for e in ctx.written]
    assert kinds == ["trace", "sandbox", "text", "file", "done"]
    assert ctx.written[0].data["name"] == "sandbox.create"
    file_event = ctx.written[3]
    assert file_event.data["filename"] == "report.txt"
    assert file_event.data["mime"] == "text/plain"
    key = file_event.data["object_key"]
    assert key.startswith("artifacts/U1/S1/")
    assert store.puts[key] == (b"hello world", "text/plain")
    assert "Only files in ./outputs/" in prov.seen_options.system_prompt
