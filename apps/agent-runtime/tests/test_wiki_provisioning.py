"""Hermetic Wiki reference provisioning tests."""

import hashlib
from dataclasses import dataclass, field

from cocola_agent_runtime.agent_provider import AgentEvent
from cocola_agent_runtime.sandbox_binder import (
    ExecOutcome,
    StaticSandboxBinder,
    StaticSandboxExecutor,
)
from cocola_agent_runtime.server import (
    MAX_WIKI_REFERENCE_BYTES_PER_TURN,
    MAX_WIKI_REFERENCES_PER_TURN,
    AgentRuntimeServicer,
)


@dataclass
class FakeWikiReference:
    logical_path: str
    filename: str
    oss_key: str
    size: int
    sha256: str
    mime: str = "text/markdown"


@dataclass
class FakeRequest:
    user_id: str = "U1"
    session_id: str = "S1"
    prompt: str = "use the referenced context"
    sandbox_id: str = ""
    max_turns: int = 0
    attachments: list = field(default_factory=list)
    wiki_references: list = field(default_factory=list)


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
        self.prompt = ""

    async def query(self, prompt, _options):
        self.ran = True
        self.prompt = prompt
        yield AgentEvent(kind="done", data={})


class FakeFetcher:
    def __init__(self, objects):
        self.objects = objects
        self.gets = []

    def get(self, key):
        self.gets.append(key)
        return self.objects[key]


def _reference(path: str, key: str, content: bytes) -> FakeWikiReference:
    return FakeWikiReference(
        logical_path=path,
        filename=path.rsplit("/", 1)[-1],
        oss_key=key,
        size=len(content),
        sha256=hashlib.sha256(content).hexdigest(),
    )


def _executor() -> StaticSandboxExecutor:
    return StaticSandboxExecutor(
        exec_handler=lambda _sandbox_id, _cmd: ExecOutcome(exit_code=0, stdout="/workspace\n")
    )


async def test_wiki_reference_preserves_tree_and_pins_verified_bytes():
    content = b"# Product brief\n"
    reference = _reference("Product/Research/brief.md", "wiki/node/version", content)
    fetcher = FakeFetcher({"wiki/node/version": content})
    executor = _executor()
    provider = RecordingProvider()
    context = FakeContext()

    await AgentRuntimeServicer(
        provider,
        binder=StaticSandboxBinder(),
        executor=executor,
        objstore=fetcher,
    ).Query(FakeRequest(wiki_references=[reference]), context)

    assert fetcher.gets == ["wiki/node/version"]
    assert executor.byte_writes == [
        ("box-S1", "/workspace/wiki/Product/Research/brief.md", content)
    ]
    assert "/workspace/wiki/Product/Research/brief.md" in provider.prompt
    assert "cocola-wiki-read xlsx-range" in provider.prompt
    assert provider.prompt.endswith("use the referenced context")


async def test_wiki_reference_rejects_path_traversal():
    content = b"safe"
    reference = _reference("../../etc/brief.md", "wiki/safe", content)
    executor = _executor()
    provider = RecordingProvider()
    context = FakeContext()

    await AgentRuntimeServicer(
        provider,
        binder=StaticSandboxBinder(),
        executor=executor,
        objstore=FakeFetcher({"wiki/safe": content}),
    ).Query(FakeRequest(wiki_references=[reference]), context)

    assert provider.ran is False
    assert executor.byte_writes == []
    assert context.written[-1].kind == "error"
    assert context.written[-1].data["code"] == "WIKI_PROVISION_FAILED"


async def test_wiki_reference_preserves_leading_dot_names():
    hidden = b"# hidden"
    visible = b"# visible"
    references = [
        _reference(".brief.md", "wiki/hidden", hidden),
        _reference("brief.md", "wiki/visible", visible),
    ]
    executor = _executor()

    await AgentRuntimeServicer(
        RecordingProvider(),
        binder=StaticSandboxBinder(),
        executor=executor,
        objstore=FakeFetcher({"wiki/hidden": hidden, "wiki/visible": visible}),
    ).Query(FakeRequest(wiki_references=references), FakeContext())

    assert executor.byte_writes == [
        ("box-S1", "/workspace/wiki/.brief.md", hidden),
        ("box-S1", "/workspace/wiki/brief.md", visible),
    ]


async def test_wiki_reference_rejects_conflicting_target_paths_before_writing():
    first = b"first"
    second = b"second"
    references = [
        _reference("brief.md", "wiki/first", first),
        _reference("brief.md", "wiki/second", second),
    ]
    executor = _executor()
    provider = RecordingProvider()
    context = FakeContext()

    await AgentRuntimeServicer(
        provider,
        binder=StaticSandboxBinder(),
        executor=executor,
        objstore=FakeFetcher({"wiki/first": first, "wiki/second": second}),
    ).Query(FakeRequest(wiki_references=references), context)

    assert provider.ran is False
    assert executor.byte_writes == []
    assert context.written[-1].kind == "error"
    assert context.written[-1].data["code"] == "WIKI_PROVISION_FAILED"


async def test_wiki_integrity_failure_is_terminal_and_skips_agent():
    reference = FakeWikiReference(
        logical_path="brief.md",
        filename="brief.md",
        oss_key="wiki/tampered",
        size=8,
        sha256=hashlib.sha256(b"expected").hexdigest(),
    )
    provider = RecordingProvider()
    context = FakeContext()
    executor = _executor()

    await AgentRuntimeServicer(
        provider,
        binder=StaticSandboxBinder(),
        executor=executor,
        objstore=FakeFetcher({"wiki/tampered": b"tampered"}),
    ).Query(FakeRequest(wiki_references=[reference]), context)

    assert provider.ran is False
    assert executor.byte_writes == []
    assert context.written[-1].kind == "error"
    assert context.written[-1].data["code"] == "WIKI_PROVISION_FAILED"


async def test_wiki_reference_count_limit_fails_before_object_fetch():
    references = [
        _reference(f"{index}.md", f"wiki/{index}", b"x")
        for index in range(MAX_WIKI_REFERENCES_PER_TURN + 1)
    ]
    fetcher = FakeFetcher({})
    provider = RecordingProvider()
    context = FakeContext()

    await AgentRuntimeServicer(
        provider,
        binder=StaticSandboxBinder(),
        executor=_executor(),
        objstore=fetcher,
    ).Query(FakeRequest(wiki_references=references), context)

    assert provider.ran is False
    assert fetcher.gets == []
    assert context.written[-1].kind == "error"
    assert context.written[-1].data["code"] == "WIKI_PROVISION_FAILED"


async def test_wiki_reference_byte_limit_fails_before_object_fetch():
    references = [
        _reference("small.md", "wiki/small", b"x"),
        FakeWikiReference(
            logical_path="large.pdf",
            filename="large.pdf",
            oss_key="wiki/large",
            size=MAX_WIKI_REFERENCE_BYTES_PER_TURN,
            sha256="",
            mime="application/pdf",
        ),
    ]
    fetcher = FakeFetcher({"wiki/small": b"x"})
    provider = RecordingProvider()
    context = FakeContext()

    await AgentRuntimeServicer(
        provider,
        binder=StaticSandboxBinder(),
        executor=_executor(),
        objstore=fetcher,
    ).Query(FakeRequest(wiki_references=references), context)

    assert provider.ran is False
    assert fetcher.gets == []
    assert context.written[-1].kind == "error"
    assert context.written[-1].data["code"] == "WIKI_PROVISION_FAILED"
