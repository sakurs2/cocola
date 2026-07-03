"""AgentRuntimeServicer tests.

Hermetic: no gRPC server, no socket. We call the servicer\'s Query coroutine
directly with a fake provider, a fake streaming context that records written
proto events, and a plain request object. We assert (a) generic AgentEvents map
onto proto AgentEvents with non-string data flattened, (b) a provider error
becomes a terminal proto `error` event instead of propagating, and (c) enabled
skills are folded into the AgentOptions the provider receives.
"""

import json
from dataclasses import dataclass, field

from cocola_agent_runtime.agent_provider import AgentEvent, AgentOptions
from cocola_agent_runtime.sandbox_binder import (
    ExecOutcome,
    StaticSandboxBinder,
    StaticSandboxExecutor,
)
from cocola_agent_runtime.server import AgentRuntimeServicer, event_to_proto
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
        return self.puts[key]

    def put(self, key: str, data: bytes, mime: str) -> None:
        self.puts[key] = (data, mime)


class FakeSessionMap:
    def __init__(self, *, fail_delete: bool = False):
        self.deleted = []
        self.fail_delete = fail_delete

    async def get(self, session_id: str):
        return None

    async def put(
        self, session_id: str, claude_session_id: str, *, user_id: str = "", sandbox_id: str = ""
    ):
        return None

    async def delete(self, session_id: str):
        self.deleted.append(session_id)
        if self.fail_delete:
            raise RuntimeError("delete failed")

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
    assert kinds == ["sandbox", "text", "file", "done"]
    file_event = ctx.written[2]
    assert file_event.data["filename"] == "report.txt"
    assert file_event.data["mime"] == "text/plain"
    key = file_event.data["object_key"]
    assert key.startswith("artifacts/U1/S1/")
    assert store.puts[key] == (b"hello world", "text/plain")
    assert "Only files in ./outputs/" in prov.seen_options.system_prompt
