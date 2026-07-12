"""AgentRuntimeServicer tests.

Hermetic: no gRPC server, no socket. We call the servicer\'s Query coroutine
directly with a fake provider, a fake streaming context that records written
proto events, and a plain request object. We assert (a) generic AgentEvents map
onto proto AgentEvents with non-string data flattened, (b) a provider error
becomes a terminal proto `error` event instead of propagating, and (c) enabled
skills are folded into the AgentOptions the provider receives.
"""

import io
import json
import re
import subprocess
import sys
import zipfile
from dataclasses import dataclass, field
from types import SimpleNamespace

import grpc
import pytest
from cocola_agent_runtime.agent_provider import AgentEvent, AgentOptions
from cocola_agent_runtime.checkpoint import CheckpointConfig, CheckpointManager
from cocola_agent_runtime.prompt_loader import PromptMarker, StaticPromptCatalog
from cocola_agent_runtime.sandbox_binder import (
    ExecOutcome,
    SandboxGoneError,
    StaticSandboxBinder,
    StaticSandboxExecutor,
)
from cocola_agent_runtime.server import (
    _SKILLS_BATCH_INSTALL_SCRIPT,
    AgentRuntimeServicer,
    _product_traceparent,
    event_to_proto,
)
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

    def __init__(self, metadata=()):
        self.written = []
        self.metadata = metadata

    def invocation_metadata(self):
        return self.metadata

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


def test_invalid_run_timeout_fails_servicer_startup(monkeypatch):
    monkeypatch.setenv("COCOLA_AGENT_RUN_TIMEOUT_SECS", "0")
    with pytest.raises(RuntimeError, match="positive integer"):
        AgentRuntimeServicer(ListProvider([]))


async def test_missing_active_sandbox_aborts_query_as_unavailable(monkeypatch):
    class GoneBinder(StaticSandboxBinder):
        async def heartbeat(self, *, sandbox_id: str) -> None:
            raise SandboxGoneError(sandbox_id)

    class AbortContext:
        aborted = None

        async def abort(self, code, detail):
            self.aborted = (code, detail)

    async def no_delay(_seconds):
        return None

    monkeypatch.setattr("cocola_agent_runtime.server.asyncio.sleep", no_delay)
    context = AbortContext()
    servicer = AgentRuntimeServicer(ListProvider([]), binder=GoneBinder())
    await servicer._heartbeat_sandbox("box-gone", context, None)
    assert context.aborted == (grpc.StatusCode.UNAVAILABLE, "active sandbox was reclaimed")


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
        self.deleted = []
        self.delete_errors = {}

    def get(self, key: str) -> bytes:
        value = self.puts[key]
        if isinstance(value, tuple):
            return value[0]
        return value

    def put(self, key: str, data: bytes, mime: str) -> None:
        self.puts[key] = (data, mime)

    def list(self, prefix: str) -> list[str]:
        return sorted(key for key in self.puts if key.startswith(prefix))

    def delete(self, key: str) -> None:
        if error := self.delete_errors.get(key):
            raise error
        self.deleted.append(key)
        self.puts.pop(key, None)


def test_product_traceparent_wins_over_otel_transport_parent():
    otel_parent = "00-" + "a" * 32 + "-" + "1" * 16 + "-01"
    product_parent = "00-" + "a" * 32 + "-" + "2" * 16 + "-01"
    context = FakeContext(
        (
            SimpleNamespace(key="traceparent", value=otel_parent),
            SimpleNamespace(key="x-cocola-product-traceparent", value=product_parent),
        )
    )

    assert _product_traceparent(context) == product_parent


def test_product_traceparent_falls_back_to_standard_header():
    traceparent = "00-" + "a" * 32 + "-" + "1" * 16 + "-01"
    context = FakeContext((SimpleNamespace(key="traceparent", value=traceparent),))

    assert _product_traceparent(context) == traceparent


class FakeSessionMap:
    def __init__(self, *, fail_delete: bool = False):
        self.deleted = []
        self.fail_delete = fail_delete
        self.bindings = {}
        self.checkpoints = {}
        self.put_checkpoint_calls = []

    async def get(self, session_id: str, *, user_id: str = ""):
        binding = await self.get_binding(session_id, user_id=user_id)
        return binding.claude_session_id if binding else None

    async def get_binding(self, session_id: str, *, user_id: str = ""):
        binding = self.bindings.get(session_id)
        if binding and binding.user_id and binding.user_id != user_id:
            return None
        return binding

    async def put(
        self, session_id: str, claude_session_id: str, *, user_id: str = "", sandbox_id: str = ""
    ):
        self.bindings[session_id] = SessionBinding(
            claude_session_id=claude_session_id,
            user_id=user_id,
            sandbox_id=sandbox_id,
            checkpoint_object_key=self.checkpoints.get(session_id, ""),
        )

    async def get_checkpoint(self, session_id: str, *, user_id: str = ""):
        return self.checkpoints.get(session_id)

    async def put_checkpoint(
        self, session_id: str, object_key: str, *, user_id: str = "", size_bytes: int = 0
    ):
        self.put_checkpoint_calls.append((session_id, object_key))
        self.checkpoints[session_id] = object_key

    async def put_checkpoint_failure(self, session_id: str, error: str, *, user_id: str = ""):
        return None

    async def delete(self, session_id: str, *, user_id: str = ""):
        binding = self.bindings.get(session_id)
        if binding and binding.user_id and binding.user_id != user_id:
            return
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


def skill_bundle(**files: str) -> bytes:
    out = io.BytesIO()
    with zipfile.ZipFile(out, "w") as archive:
        for path, content in files.items():
            archive.writestr(path, content)
    return out.getvalue()


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
    cat = StaticSkillCatalog(
        [Skill(id="web", name="Web Search", version="1.2", skill_md="# Web Search")]
    )
    await AgentRuntimeServicer(
        prov,
        skills=cat,
        executor=StaticSandboxExecutor(),
    ).Query(FakeRequest(sandbox_id="box-1"), FakeContext())
    assert prov.seen_options.system_prompt is not None
    assert "Web Search" in prov.seen_options.system_prompt
    assert prov.seen_options.environment_skills == [
        {"id": "web", "name": "Web Search", "version": "1.2"}
    ]


async def test_fresh_environment_snapshot_reports_loaded_skills_without_mcp():
    prov = ListProvider([AgentEvent(kind="done", data={})])
    cat = StaticSkillCatalog(
        [Skill(id="web", name="Web Search", version="1.2", skill_md="# Web Search")]
    )
    ctx = FakeContext()

    await AgentRuntimeServicer(
        prov,
        skills=cat,
        binder=StaticSandboxBinder(),
        executor=StaticSandboxExecutor(),
    ).Query(FakeRequest(), ctx)

    snapshots = [
        json.loads(event.data["snapshot"])
        for event in ctx.written
        if event.kind == "environment_prepare"
    ]
    assert [snapshot["state"] for snapshot in snapshots] == ["preparing", "ready"]
    ready = snapshots[-1]
    assert all(component["kind"] != "mcp" for component in ready["components"])
    skills = next(component for component in ready["components"] if component["kind"] == "skills")
    assert skills["count"] == 1
    assert skills["metadata"]["items"] == [{"id": "web", "label": "Web Search", "version": "1.2"}]


async def test_skill_sync_uses_one_archive_write_and_one_exec():
    executor = StaticSandboxExecutor()
    store = FakeObjectStore()
    store.puts["bundle-a"] = skill_bundle(**{"SKILL.md": "# A", "scripts/run.py": "pass"})
    skills = [
        Skill(id="bundle-a", name="A", bundle_object_key="bundle-a"),
        Skill(id="markdown-b", name="B", skill_md="# B"),
    ]
    servicer = AgentRuntimeServicer(
        ListProvider([]),
        executor=executor,
        objstore=store,
    )

    await servicer._sync_skills_into_sandbox("box-1", skills)

    assert len(executor.byte_writes) == 1
    assert len(executor.exec_calls) == 1
    sandbox_id, archive_path, batch_data = executor.byte_writes[0]
    assert sandbox_id == "box-1"
    assert archive_path.startswith("/tmp/cocola-skills-")
    with zipfile.ZipFile(io.BytesIO(batch_data)) as batch:
        manifest = json.loads(batch.read("manifest.json"))
        assert manifest == [
            {"id": "bundle-a", "kind": "bundle", "member": "payloads/0000.zip"},
            {"id": "markdown-b", "kind": "markdown", "member": "payloads/0001.md"},
        ]
        with zipfile.ZipFile(io.BytesIO(batch.read("payloads/0000.zip"))) as bundle:
            assert bundle.read("SKILL.md") == b"# A"
        assert batch.read("payloads/0001.md") == b"# B"
    call = executor.exec_calls[0]
    assert call["cmd"][:3] == ["python3", "-c", _SKILLS_BATCH_INSTALL_SCRIPT]
    assert call["cmd"][3] == archive_path


async def test_skill_batch_installer_links_shared_and_installs_local_payloads(tmp_path):
    executor = StaticSandboxExecutor()
    store = FakeObjectStore()
    store.puts["local-bundle"] = skill_bundle(**{"SKILL.md": "# Local"})
    store.puts["shared-bundle"] = skill_bundle(**{"SKILL.md": "# Stale copy"})
    skills = [
        Skill(id="local", name="Local", bundle_object_key="local-bundle"),
        Skill(id="markdown", name="Markdown", skill_md="# Markdown"),
        Skill(id="shared", name="Shared", bundle_object_key="shared-bundle"),
    ]
    servicer = AgentRuntimeServicer(ListProvider([]), executor=executor, objstore=store)
    await servicer._sync_skills_into_sandbox("box-1", skills)
    batch_data = executor.byte_writes[0][2]

    archive = tmp_path / "skills.zip"
    archive.write_bytes(batch_data)
    base = tmp_path / "claude"
    shared_root = tmp_path / "shared"
    (shared_root / "shared").mkdir(parents=True)
    (shared_root / "shared" / "SKILL.md").write_text("# Shared", encoding="utf-8")

    result = subprocess.run(
        [
            sys.executable,
            "-c",
            _SKILLS_BATCH_INSTALL_SCRIPT,
            str(archive),
            str(base),
            str(shared_root),
        ],
        check=False,
        capture_output=True,
        text=True,
    )

    assert result.returncode == 0, result.stderr
    assert (base / "skills" / "local" / "SKILL.md").read_text() == "# Local"
    assert (base / "skills" / "markdown" / "SKILL.md").read_text() == "# Markdown"
    assert (base / "skills" / "shared").is_symlink()
    assert (base / "skills" / "shared" / "SKILL.md").read_text() == "# Shared"
    assert not archive.exists()


async def test_skill_batch_installer_rejects_unsafe_bundle_before_replacing_targets(tmp_path):
    executor = StaticSandboxExecutor()
    store = FakeObjectStore()
    store.puts["bad-bundle"] = skill_bundle(
        **{"SKILL.md": "# Bad", "../escaped.txt": "not allowed"}
    )
    servicer = AgentRuntimeServicer(ListProvider([]), executor=executor, objstore=store)
    await servicer._sync_skills_into_sandbox(
        "box-1",
        [Skill(id="bad", name="Bad", bundle_object_key="bad-bundle")],
    )

    archive = tmp_path / "skills.zip"
    archive.write_bytes(executor.byte_writes[0][2])
    base = tmp_path / "claude"
    existing = base / "skills" / "bad"
    existing.mkdir(parents=True)
    (existing / "SKILL.md").write_text("# Existing", encoding="utf-8")
    shared_root = tmp_path / "shared"
    shared_root.mkdir()

    result = subprocess.run(
        [
            sys.executable,
            "-c",
            _SKILLS_BATCH_INSTALL_SCRIPT,
            str(archive),
            str(base),
            str(shared_root),
        ],
        check=False,
        capture_output=True,
        text=True,
    )

    assert result.returncode != 0
    assert "unsafe skill archive path" in result.stderr
    assert (existing / "SKILL.md").read_text() == "# Existing"
    assert not (base / ".cocola" / "escaped.txt").exists()


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
    assert mcp_trace.data["category"] == "agent_init"


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
    assert prompt_trace.data["category"] == "agent_init"
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
    session_map.bindings["sess-7"] = SessionBinding(
        claude_session_id="claude-1", user_id="U1", sandbox_id="box-sess-7"
    )

    await AgentRuntimeServicer(
        ListProvider([]),
        binder=binder,
        session_map=session_map,
    ).ReleaseSession(FakeRequest(session_id="sess-7"), FakeContext())

    assert binder.released == ["sess-7"]
    assert session_map.deleted == ["sess-7"]


async def test_release_session_wrong_owner_does_not_release_sandbox():
    binder = StaticSandboxBinder()
    session_map = FakeSessionMap()
    session_map.bindings["shared"] = SessionBinding(
        claude_session_id="claude-u1", user_id="U1", sandbox_id="box-u1"
    )

    await AgentRuntimeServicer(
        ListProvider([]), binder=binder, session_map=session_map
    ).ReleaseSession(FakeRequest(user_id="U2", session_id="shared"), FakeContext())

    assert binder.released == []
    assert session_map.deleted == []


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
    executor = StaticSandboxExecutor()

    def exec_handler(sandbox_id, cmd):
        assert sandbox_id == "box-sess-7"
        # The archive command writes a zstd tar to an on-disk temp path inside
        # the sandbox and prints an "archived" marker; the payload is then read
        # back over the binary file channel, never via stdout.
        match = re.search(r"(/tmp/cocola-checkpoint-[0-9a-f]+\.tar\.zst)", cmd[2])
        assert match, cmd[2]
        executor.byte_files[(sandbox_id, match.group(1))] = archive
        return ExecOutcome(exit_code=0, stdout="archived")

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


async def test_checkpoint_upload_deletes_only_superseded_session_snapshots():
    archive = b"checkpoint-bytes"
    executor = StaticSandboxExecutor()

    def exec_handler(sandbox_id, cmd):
        match = re.search(r"(/tmp/cocola-checkpoint-[0-9a-f]+\.tar\.zst)", cmd[2])
        assert match, cmd[2]
        executor.byte_files[(sandbox_id, match.group(1))] = archive
        return ExecOutcome(exit_code=0, stdout="archived")

    executor = StaticSandboxExecutor(exec_handler=exec_handler)
    store = FakeObjectStore()
    old_key = "checkpoints/U1/S1/old.tar.zst"
    unrelated_key = "checkpoints/U1/S2/keep.tar.zst"
    store.puts[old_key] = b"old"
    store.puts[unrelated_key] = b"unrelated"
    session_map = FakeSessionMap()
    checkpoint = CheckpointManager(
        objstore=store,
        executor=executor,
        session_map=session_map,
        config=CheckpointConfig(),
    )

    key = await checkpoint.checkpoint_on_reclaim(
        sandbox_id="box-S1",
        user_id="U1",
        session_id="S1",
    )

    assert key is not None
    assert old_key not in store.puts
    assert store.deleted == [old_key]
    assert key in store.puts
    assert unrelated_key in store.puts
    assert session_map.put_checkpoint_calls == [("S1", key)]


async def test_checkpoint_cleanup_failure_keeps_new_snapshot_usable():
    archive = b"checkpoint-bytes"
    executor = StaticSandboxExecutor()

    def exec_handler(sandbox_id, cmd):
        match = re.search(r"(/tmp/cocola-checkpoint-[0-9a-f]+\.tar\.zst)", cmd[2])
        assert match, cmd[2]
        executor.byte_files[(sandbox_id, match.group(1))] = archive
        return ExecOutcome(exit_code=0, stdout="archived")

    executor = StaticSandboxExecutor(exec_handler=exec_handler)
    store = FakeObjectStore()
    old_key = "checkpoints/U1/S1/old.tar.zst"
    store.puts[old_key] = b"old"
    store.delete_errors[old_key] = RuntimeError("minio unavailable")
    session_map = FakeSessionMap()
    checkpoint = CheckpointManager(
        objstore=store,
        executor=executor,
        session_map=session_map,
        config=CheckpointConfig(),
    )

    key = await checkpoint.checkpoint_on_reclaim(
        sandbox_id="box-S1",
        user_id="U1",
        session_id="S1",
    )

    assert key is not None
    assert key in store.puts
    assert old_key in store.puts
    assert session_map.put_checkpoint_calls == [("S1", key)]


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
    # The archive bytes are delivered over the binary file channel, then the
    # unpack command reads them from that on-disk path -- no stdin base64 blob.
    assert first["stdin"] == ""
    match = re.search(r"(/tmp/cocola-checkpoint-[0-9a-f]+\.tar\.zst)", first["cmd"][2])
    assert match, first["cmd"][2]
    assert executor.byte_files[("box-S1", match.group(1))] == b"checkpoint-bytes"
    assert executor.byte_writes == [("box-S1", match.group(1), b"checkpoint-bytes")]


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


async def test_query_reports_missing_checkpoint_for_existing_session():
    prov = ListProvider([AgentEvent(kind="done", data={})])
    executor = StaticSandboxExecutor()
    store = FakeObjectStore()
    session_map = FakeSessionMap()
    session_map.bindings["S1"] = SessionBinding(
        claude_session_id="claude-old",
        sandbox_id="box-old",
    )
    checkpoint = CheckpointManager(
        objstore=store,
        executor=executor,
        session_map=session_map,
        config=CheckpointConfig(),
    )
    ctx = FakeContext()

    await AgentRuntimeServicer(
        prov,
        binder=StaticSandboxBinder(),
        executor=executor,
        objstore=store,
        session_map=session_map,
        checkpoint=checkpoint,
    ).Query(FakeRequest(), ctx)

    snapshots = [
        json.loads(event.data["snapshot"])
        for event in ctx.written
        if event.kind == "environment_prepare"
    ]
    assert [snapshot["state"] for snapshot in snapshots] == ["preparing", "degraded"]
    restore = next(
        component for component in snapshots[-1]["components"] if component["kind"] == "checkpoint"
    )
    assert restore == {
        "kind": "checkpoint",
        "status": "failed",
        "label": "Session restore",
        "summary": "Saved session data is unavailable",
    }


async def test_query_does_not_report_missing_checkpoint_for_new_session():
    prov = ListProvider([AgentEvent(kind="done", data={})])
    executor = StaticSandboxExecutor()
    store = FakeObjectStore()
    session_map = FakeSessionMap()
    checkpoint = CheckpointManager(
        objstore=store,
        executor=executor,
        session_map=session_map,
        config=CheckpointConfig(),
    )
    ctx = FakeContext()

    await AgentRuntimeServicer(
        prov,
        binder=StaticSandboxBinder(),
        executor=executor,
        objstore=store,
        session_map=session_map,
        checkpoint=checkpoint,
    ).Query(FakeRequest(), ctx)

    snapshots = [
        json.loads(event.data["snapshot"])
        for event in ctx.written
        if event.kind == "environment_prepare"
    ]
    assert [snapshot["state"] for snapshot in snapshots] == ["preparing", "ready"]
    assert all(component["kind"] != "checkpoint" for component in snapshots[-1]["components"])


async def test_query_reports_checkpoint_restore_failure():
    prov = ListProvider([AgentEvent(kind="done", data={})])
    executor = StaticSandboxExecutor()
    store = FakeObjectStore()
    session_map = FakeSessionMap()
    session_map.bindings["S1"] = SessionBinding(
        claude_session_id="claude-old",
        sandbox_id="box-old",
        checkpoint_object_key="missing-object",
    )
    session_map.checkpoints["S1"] = "missing-object"
    checkpoint = CheckpointManager(
        objstore=store,
        executor=executor,
        session_map=session_map,
        config=CheckpointConfig(),
    )
    ctx = FakeContext()

    await AgentRuntimeServicer(
        prov,
        binder=StaticSandboxBinder(),
        executor=executor,
        objstore=store,
        session_map=session_map,
        checkpoint=checkpoint,
    ).Query(FakeRequest(), ctx)

    snapshots = [
        json.loads(event.data["snapshot"])
        for event in ctx.written
        if event.kind == "environment_prepare"
    ]
    assert snapshots[-1]["state"] == "degraded"
    restore = next(
        component for component in snapshots[-1]["components"] if component["kind"] == "checkpoint"
    )
    assert restore["status"] == "failed"
    assert restore["summary"] == "Could not restore saved session data"


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
    assert kinds == [
        "trace",
        "environment_prepare",
        "sandbox",
        "environment_prepare",
        "text",
        "file",
        "done",
    ]
    assert ctx.written[0].data["name"] == "sandbox.create"
    file_event = ctx.written[5]
    assert file_event.data["filename"] == "report.txt"
    assert file_event.data["mime"] == "text/plain"
    key = file_event.data["object_key"]
    assert key.startswith("artifacts/U1/S1/")
    assert store.puts[key] == (b"hello world", "text/plain")
    assert "Only files in ./outputs/" in prov.seen_options.system_prompt
