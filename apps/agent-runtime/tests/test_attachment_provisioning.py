"""Attachment pre-provisioning tests (P0 file upload, ADR-0017).

Hermetic: no gRPC server, no Docker. We drive AgentRuntimeServicer.Query with a
StaticSandboxBinder + StaticSandboxExecutor and assert the push model:

  - each uploaded file is written into ./uploads/ under the workspace cwd
    (resolved via `pwd`), binary-safe, and a prompt preamble listing the paths
    is prepended to the user's prompt the provider sees.
  - `mkdir -p uploads` runs before any write (WriteFile does not create dirs).
  - filenames are sanitized: path traversal / separators cannot escape uploads.
  - a provisioning failure becomes a terminal `error` event; the provider never
    runs (we do not run the agent against files that never arrived).
  - no executor/sandbox (local dev) -> files land in a per-session HOST
    workspace and that dir is threaded to the provider as its cwd.
"""

from dataclasses import dataclass, field

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
    content: bytes
    mime: str = ""


@dataclass
class FakeRequest:
    user_id: str = "U1"
    session_id: str = "S1"
    prompt: str = "summarize the files"
    sandbox_id: str = ""
    max_turns: int = 0
    attachments: list = field(default_factory=list)


class FakeContext:
    def __init__(self):
        self.written = []

    async def write(self, event):
        self.written.append(event)


class RecordingProvider:
    def __init__(self, events=None):
        self._events = events or [AgentEvent(kind="done", data={})]
        self.seen_prompt: str | None = None
        self.seen_options: AgentOptions | None = None
        self.ran = False

    async def query(self, prompt, options):
        self.ran = True
        self.seen_prompt = prompt
        self.seen_options = options
        for e in self._events:
            yield e


def _pwd_executor():
    """Executor whose `pwd` returns the session workspace cwd."""
    return StaticSandboxExecutor(
        exec_handler=lambda sid, cmd: ExecOutcome(
            exit_code=0, stdout="/workspace/S9\n"
        )
    )


async def test_attachments_are_written_and_preamble_prepended():
    prov = RecordingProvider()
    binder = StaticSandboxBinder()
    ex = _pwd_executor()
    ctx = FakeContext()
    req = FakeRequest(
        session_id="S9",
        prompt="what do these say?",
        attachments=[
            FakeAttachment("notes.txt", b"hello"),
            FakeAttachment("pic.png", b"\x89PNG\x00\x01"),
        ],
    )
    await AgentRuntimeServicer(prov, binder=binder, executor=ex).Query(req, ctx)

    # mkdir ran before writes, in the workspace cwd (empty cwd == container WD).
    assert ex.exec_calls and ex.exec_calls[0]["cmd"] == [
        "sh",
        "-c",
        "mkdir -p uploads && pwd",
    ]
    # Both files landed under the resolved absolute uploads dir, binary-safe.
    written_paths = {p for (_sid, p, _data) in ex.byte_writes}
    assert written_paths == {
        "/workspace/S9/uploads/notes.txt",
        "/workspace/S9/uploads/pic.png",
    }
    png = next(d for (_s, p, d) in ex.byte_writes if p.endswith("pic.png"))
    assert png == b"\x89PNG\x00\x01"  # bytes preserved, not utf-8 mangled

    # The provider saw a preamble listing relative paths, then the user prompt.
    assert prov.ran is True
    assert "./uploads/notes.txt" in prov.seen_prompt
    assert "./uploads/pic.png" in prov.seen_prompt
    assert prov.seen_prompt.endswith("what do these say?")


async def test_filename_is_sanitized_against_traversal():
    prov = RecordingProvider()
    binder = StaticSandboxBinder()
    ex = _pwd_executor()
    ctx = FakeContext()
    req = FakeRequest(
        session_id="S9",
        attachments=[FakeAttachment("../../etc/passwd", b"x")],
    )
    await AgentRuntimeServicer(prov, binder=binder, executor=ex).Query(req, ctx)

    paths = [p for (_s, p, _d) in ex.byte_writes]
    # Cannot escape the uploads dir; basename only.
    assert paths == ["/workspace/S9/uploads/passwd"]


async def test_provisioning_failure_is_terminal_and_skips_provider():
    prov = RecordingProvider()
    binder = StaticSandboxBinder()
    # mkdir/pwd returns a non-zero exit -> provisioning must fail cleanly.
    ex = StaticSandboxExecutor(
        exec_handler=lambda sid, cmd: ExecOutcome(exit_code=1, error="mkdir denied")
    )
    ctx = FakeContext()
    req = FakeRequest(attachments=[FakeAttachment("a.txt", b"x")])
    await AgentRuntimeServicer(prov, binder=binder, executor=ex).Query(req, ctx)

    kinds = [e.kind for e in ctx.written]
    # sandbox event, then a terminal error; provider never ran.
    assert kinds[-1] == "error"
    assert "attachment provisioning failed" in ctx.written[-1].data["error"]
    assert prov.ran is False
    assert ex.byte_writes == []


async def test_no_executor_lands_on_host_and_sets_cwd(tmp_path, monkeypatch):
    # Local dev: no executor/sandbox, so the brain runs IN-PROCESS. Attachments
    # must land in a per-session HOST workspace under the configured root, and
    # that dir must be handed to the provider as its cwd so ./uploads/ resolves.
    monkeypatch.setenv("COCOLA_LOCAL_WORKSPACE_ROOT", str(tmp_path))
    prov = RecordingProvider()
    binder = StaticSandboxBinder()
    ctx = FakeContext()
    req = FakeRequest(
        session_id="S7",
        prompt="what does it say?",
        attachments=[FakeAttachment("a.txt", b"hi"), FakeAttachment("p.png", b"\x89P")],
    )
    # No executor wired.
    await AgentRuntimeServicer(prov, binder=binder).Query(req, ctx)

    workspace = tmp_path / "S7"
    uploads = workspace / "uploads"
    assert (uploads / "a.txt").read_bytes() == b"hi"
    assert (uploads / "p.png").read_bytes() == b"\x89P"  # binary-safe

    assert prov.ran is True
    # Preamble prepended, then the user prompt; workspace threaded as cwd.
    assert "./uploads/a.txt" in prov.seen_prompt
    assert prov.seen_prompt.endswith("what does it say?")
    assert prov.seen_options.workspace == str(workspace)
    assert [e.kind for e in ctx.written][-1] == "done"


async def test_no_executor_defaults_workspace_root(tmp_path, monkeypatch):
    # Without the env override the root falls back to a stable dir under the
    # OS temp dir; the session subdir + uploads/ layout is unchanged.
    monkeypatch.delenv("COCOLA_LOCAL_WORKSPACE_ROOT", raising=False)
    monkeypatch.setattr("tempfile.gettempdir", lambda: str(tmp_path))
    prov = RecordingProvider()
    ctx = FakeContext()
    req = FakeRequest(
        session_id="S8", attachments=[FakeAttachment("d.txt", b"z")]
    )
    await AgentRuntimeServicer(prov, binder=StaticSandboxBinder()).Query(req, ctx)

    expected = tmp_path / "cocola-workspaces" / "S8"
    assert (expected / "uploads" / "d.txt").read_bytes() == b"z"
    assert prov.seen_options.workspace == str(expected)


async def test_no_attachments_is_a_noop():
    prov = RecordingProvider()
    binder = StaticSandboxBinder()
    ex = _pwd_executor()
    ctx = FakeContext()
    await AgentRuntimeServicer(prov, binder=binder, executor=ex).Query(
        FakeRequest(prompt="plain"), ctx
    )
    # No exec/write happened; prompt untouched.
    assert ex.exec_calls == []
    assert ex.byte_writes == []
    assert prov.seen_prompt == "plain"
