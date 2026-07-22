"""Backend-pull tests for large (key-only) attachments (ADR-0017 P1a).

Delivery is always push: the gateway uploads every file to the object store and
ships large ones as an ``oss_key`` with empty inline ``content``. The servicer
must materialize those by pulling their bytes from the object-store fetcher
before provisioning, so both delivery routes see "filename + bytes in hand".

Hermetic: a fake Fetcher stands in for MinIO; a StaticSandboxBinder +
StaticSandboxExecutor capture what lands in /workspace/uploads/.
"""

from dataclasses import dataclass

from cocola_agent_runtime.agent_provider import AgentEvent, AgentOptions
from cocola_agent_runtime.sandbox_binder import (
    ExecOutcome,
    StaticSandboxBinder,
    StaticSandboxExecutor,
)
from cocola_agent_runtime.server import AgentRuntimeServicer


@dataclass
class FakeAttachment:
    filename: str
    content: bytes = b""
    mime: str = ""
    oss_key: str = ""


@dataclass
class FakeRequest:
    user_id: str = "U1"
    session_id: str = "S1"
    prompt: str = "read the files"
    sandbox_id: str = ""
    max_turns: int = 0
    attachments: list = None

    def __post_init__(self):
        if self.attachments is None:
            self.attachments = []


class FakeContext:
    def __init__(self):
        self.written = []

    def invocation_metadata(self):
        return ()

    async def write(self, event):
        self.written.append(event)


class RecordingProvider:
    def __init__(self):
        self.ran = False
        self.seen_prompt = None
        self.seen_options: AgentOptions | None = None

    async def query(self, prompt, options):
        self.ran = True
        self.seen_prompt = prompt
        self.seen_options = options
        yield AgentEvent(kind="done", data={})


class FakeFetcher:
    """In-memory object store: key -> bytes. Records every get for assertions."""

    def __init__(self, objects):
        self._objects = objects
        self.gets = []

    def get(self, key):
        self.gets.append(key)
        return self._objects[key]


def _pwd_executor():
    return StaticSandboxExecutor(
        exec_handler=lambda sid, cmd: ExecOutcome(exit_code=0, stdout="/workspace\n")
    )


async def _run(prov, req, ctx, *, executor=None, objstore=None):
    servicer = AgentRuntimeServicer(
        prov, binder=StaticSandboxBinder(), executor=executor, objstore=objstore
    )
    await servicer.Query(req, ctx)


async def test_key_only_attachment_is_pulled_and_written():
    prov = RecordingProvider()
    ex = _pwd_executor()
    ctx = FakeContext()
    fetcher = FakeFetcher({"attachments/S9/uuid-big.bin": b"\x89PNG\x00pulled"})
    req = FakeRequest(
        session_id="S9",
        attachments=[FakeAttachment("big.bin", content=b"", oss_key="attachments/S9/uuid-big.bin")],
    )
    await _run(prov, req, ctx, executor=ex, objstore=fetcher)

    # Bytes were pulled from the store by key ...
    assert fetcher.gets == ["attachments/S9/uuid-big.bin"]
    # ... and landed under /workspace/uploads/ binary-safe.
    writes = {p: d for (_s, p, d) in ex.byte_writes}
    assert writes == {"/workspace/uploads/big.bin": b"\x89PNG\x00pulled"}
    assert prov.ran is True
    assert "/workspace/uploads/big.bin" in prov.seen_prompt


async def test_inline_attachment_is_not_pulled():
    prov = RecordingProvider()
    ex = _pwd_executor()
    ctx = FakeContext()
    fetcher = FakeFetcher({})  # would KeyError if consulted
    # Small file: inline content present, oss_key set too (source of truth), but
    # we already hold the bytes, so no fetch should happen.
    req = FakeRequest(
        session_id="S9",
        attachments=[
            FakeAttachment("a.txt", content=b"inline", oss_key="attachments/S9/uuid-a.txt")
        ],
    )
    await _run(prov, req, ctx, executor=ex, objstore=fetcher)

    assert fetcher.gets == []
    writes = {p: d for (_s, p, d) in ex.byte_writes}
    assert writes == {"/workspace/uploads/a.txt": b"inline"}


async def test_key_only_without_fetcher_is_terminal_error():
    prov = RecordingProvider()
    ex = _pwd_executor()
    ctx = FakeContext()
    req = FakeRequest(
        session_id="S9",
        attachments=[FakeAttachment("big.bin", content=b"", oss_key="attachments/S9/uuid-big.bin")],
    )
    # No objstore wired: a key-only file cannot be materialized.
    await _run(prov, req, ctx, executor=ex)

    kinds = [e.kind for e in ctx.written]
    assert kinds[-1] == "error"
    assert "attachment provisioning failed" in ctx.written[-1].data["error"]
    assert prov.ran is False
    assert ex.byte_writes == []


async def test_mixed_small_and_large_on_host(tmp_path, monkeypatch):
    # Local dev (no executor): a small inline file and a large pulled file both
    # land in the per-session HOST workspace uploads dir.
    monkeypatch.setenv("COCOLA_LOCAL_WORKSPACE_ROOT", str(tmp_path))
    prov = RecordingProvider()
    ctx = FakeContext()
    fetcher = FakeFetcher({"attachments/S7/uuid-big.bin": b"BIGDATA"})
    req = FakeRequest(
        session_id="S7",
        attachments=[
            FakeAttachment("small.txt", content=b"hi"),
            FakeAttachment("big.bin", content=b"", oss_key="attachments/S7/uuid-big.bin"),
        ],
    )
    await _run(prov, req, ctx, objstore=fetcher)

    uploads = tmp_path / "S7" / "uploads"
    assert (uploads / "small.txt").read_bytes() == b"hi"
    assert (uploads / "big.bin").read_bytes() == b"BIGDATA"
    assert fetcher.gets == ["attachments/S7/uuid-big.bin"]
    assert [e.kind for e in ctx.written][-1] == "done"
