"""Session<->sandbox binding tests (step 3).

Hermetic: no gRPC server, no Docker. We drive AgentRuntimeServicer.Query
directly with a StaticSandboxBinder and assert the binding lifecycle:

  - with a binder and no caller-pinned sandbox, Query acquires a sandbox for the
    session, injects its id into the provider's AgentOptions, and emits an
    observable `sandbox` event before the agent output.
  - a caller-pinned sandbox_id is respected verbatim (no acquire).
  - an acquire failure becomes a terminal `error` event and the provider never
    runs (the agent does not execute without its sandbox).
  - the SandboxManagerBinder bridges the blocking SandboxClient to async via a
    thread, returning a transport-neutral BoundSandbox.
"""

import json
from dataclasses import dataclass, field

import grpc
from cocola_agent_runtime.agent_provider import AgentEvent, AgentOptions
from cocola_agent_runtime.sandbox_binder import (
    BoundSandbox,
    SandboxManagerBinder,
    StaticSandboxBinder,
)
from cocola_agent_runtime.server import AgentRuntimeServicer


@dataclass
class FakeRequest:
    user_id: str = "U1"
    session_id: str = "S1"
    prompt: str = "hi"
    sandbox_id: str = ""
    max_turns: int = 0
    attachments: list = field(default_factory=list)


class FakeContext:
    def __init__(self):
        self.written = []

    async def write(self, event):
        self.written.append(event)

    def invocation_metadata(self):
        return []


class RecordingProvider:
    def __init__(self, events=None):
        self._events = events or [AgentEvent(kind="done", data={})]
        self.seen_options: AgentOptions | None = None
        self.ran = False

    async def query(self, prompt, options):
        self.ran = True
        self.seen_options = options
        for e in self._events:
            yield e


async def test_query_acquires_sandbox_when_unpinned():
    prov = RecordingProvider(
        [AgentEvent(kind="text", data={"text": "ok"}), AgentEvent(kind="done", data={})]
    )
    binder = StaticSandboxBinder()
    ctx = FakeContext()
    await AgentRuntimeServicer(prov, binder=binder).Query(FakeRequest(session_id="S9"), ctx)

    # Acquired exactly once for this session.
    assert binder.acquired == ["S9"]
    # The provider received the bound sandbox id.
    assert prov.seen_options.sandbox_id == "box-S9"
    # The sandbox acquire trace is emitted internally before the observable binding.
    kinds = [e.kind for e in ctx.written]
    assert kinds[0] == "trace"
    assert ctx.written[0].data["name"] == "sandbox.create"
    assert ctx.written[0].data["category"] == "sandbox"
    assert ctx.written[0].data["sandbox_id"] == "box-S9"
    assert ctx.written[0].data["endpoint"] == "inmem://local"
    assert ctx.written[0].data["reused"] == "false"
    assert kinds[1] == "environment_prepare"
    preparing = json.loads(ctx.written[1].data["snapshot"])
    assert preparing["schema_version"] == 1
    assert preparing["state"] == "preparing"
    assert preparing["components"][0]["kind"] == "sandbox"
    assert kinds[2] == "sandbox"
    assert ctx.written[2].data["sandbox_id"] == "box-S9"
    assert ctx.written[2].data["endpoint"] == "inmem://local"
    ready = json.loads(ctx.written[3].data["snapshot"])
    assert ready["state"] == "ready"
    assert all(component["kind"] != "mcp" for component in ready["components"])
    # Then the agent output, ending clean.
    assert kinds == [
        "trace",
        "environment_prepare",
        "sandbox",
        "environment_prepare",
        "text",
        "done",
    ]


async def test_caller_pinned_sandbox_is_respected():
    prov = RecordingProvider()
    binder = StaticSandboxBinder()
    ctx = FakeContext()
    await AgentRuntimeServicer(prov, binder=binder).Query(FakeRequest(sandbox_id="pinned-box"), ctx)
    # No acquire happened; the pinned id flows through unchanged.
    assert binder.acquired == []
    assert prov.seen_options.sandbox_id == "pinned-box"
    assert [e.kind for e in ctx.written] == ["done"]


async def test_no_binder_keeps_passthrough_behavior():
    prov = RecordingProvider()
    ctx = FakeContext()
    await AgentRuntimeServicer(prov).Query(FakeRequest(sandbox_id="x"), ctx)
    assert prov.seen_options.sandbox_id == "x"
    # No sandbox event without a binder.
    assert all(e.kind != "sandbox" for e in ctx.written)


async def test_acquire_failure_is_terminal_and_skips_provider():
    prov = RecordingProvider()
    binder = StaticSandboxBinder(fail_with=RuntimeError("manager down"))
    ctx = FakeContext()
    await AgentRuntimeServicer(prov, binder=binder).Query(FakeRequest(), ctx)

    # One internal acquire trace plus a terminal error event, provider never ran.
    kinds = [e.kind for e in ctx.written]
    assert kinds == ["trace", "error"]
    assert ctx.written[0].data["name"] == "sandbox.acquire"
    assert ctx.written[0].data["status"] == "error"
    assert "manager down" in ctx.written[1].data["error"]
    assert prov.ran is False


async def test_repeated_acquire_reports_reuse():
    binder = StaticSandboxBinder()
    await binder.acquire(session_id="S1", user_id="u")
    second = await binder.acquire(session_id="S1", user_id="u")
    assert second.reused is True
    assert second.id == "box-S1"


async def test_reused_sandbox_does_not_emit_environment_preparation():
    binder = StaticSandboxBinder()
    await binder.acquire(session_id="S1", user_id="U1")
    ctx = FakeContext()

    await AgentRuntimeServicer(RecordingProvider(), binder=binder).Query(FakeRequest(), ctx)

    assert all(event.kind != "environment_prepare" for event in ctx.written)


# --- SandboxManagerBinder: the production thread-bridge over SandboxClient ----


async def test_manager_binder_bridges_blocking_client(monkeypatch):
    """SandboxManagerBinder offloads the blocking client and returns BoundSandbox.

    We patch SandboxClient (imported into the binder module) with a fake context
    manager so no gRPC channel is opened; this proves the anyio thread bridge and
    the proto->BoundSandbox mapping without a sandbox-manager.
    """
    import cocola_agent_runtime.sandbox_binder as mod

    @dataclass
    class _Box:
        id: str
        endpoint: str

    @dataclass
    class _Acq:
        sandbox: object
        reused: bool

    captured = {}

    class FakeClient:
        def __init__(self, addr=""):
            captured["addr"] = addr

        def __enter__(self):
            return self

        def __exit__(self, *a):
            return False

        def acquire(self, session_id, user_id="", image="", env=None):
            captured["acquire"] = (session_id, user_id, image, env)
            return _Acq(sandbox=_Box(id="real-box", endpoint="tcp://h:1"), reused=True)

        def release(self, session_id):
            captured["release"] = session_id

    monkeypatch.setattr(mod, "SandboxClient", FakeClient)

    binder = SandboxManagerBinder("127.0.0.1:50051")
    box = await binder.acquire(session_id="S2", user_id="emp-1", image="img")
    assert isinstance(box, BoundSandbox)
    assert box.id == "real-box" and box.endpoint == "tcp://h:1" and box.reused is True
    assert captured["addr"] == "127.0.0.1:50051"
    assert captured["acquire"] == ("S2", "emp-1", "img", {})

    await binder.release(session_id="S2")
    assert captured["release"] == "S2"


async def test_manager_binder_applies_provisioning_defaults(monkeypatch):
    """Route A defaults (image + injected creds) flow into Acquire.

    A binder configured with a default image/env must apply them when the caller
    pins neither -- this is the seam that makes a session sandbox the Claude-Code
    brain image and carries ANTHROPIC_* into the sandbox ENV at creation.
    """
    import cocola_agent_runtime.sandbox_binder as mod

    @dataclass
    class _Box:
        id: str
        endpoint: str

    @dataclass
    class _Acq:
        sandbox: object
        reused: bool

    captured = {}

    class FakeClient:
        def __init__(self, addr=""):
            pass

        def __enter__(self):
            return self

        def __exit__(self, *a):
            return False

        def acquire(self, session_id, user_id="", image="", env=None):
            captured["image"] = image
            captured["env"] = env
            return _Acq(sandbox=_Box(id="b", endpoint="e"), reused=False)

    monkeypatch.setattr(mod, "SandboxClient", FakeClient)

    binder = SandboxManagerBinder(
        "addr",
        default_image="cocola/sandbox-runtime:dev",
        default_env={"ANTHROPIC_BASE_URL": "http://gw:8081", "ANTHROPIC_MODEL": "cocola-default"},
    )
    await binder.acquire(session_id="S3", user_id="u")
    # Defaults applied when the caller pins neither image nor env.
    assert captured["image"] == "cocola/sandbox-runtime:dev"
    assert captured["env"]["ANTHROPIC_BASE_URL"] == "http://gw:8081"
    assert captured["env"]["ANTHROPIC_MODEL"] == "cocola-default"

    # An explicit per-call value overrides the default; env is merged.
    await binder.acquire(
        session_id="S4", user_id="u", image="other:img", env={"ANTHROPIC_MODEL": "fast"}
    )
    assert captured["image"] == "other:img"
    assert captured["env"]["ANTHROPIC_BASE_URL"] == "http://gw:8081"  # default kept
    assert captured["env"]["ANTHROPIC_MODEL"] == "fast"  # per-call wins


async def test_manager_binder_maps_capacity_exhausted(monkeypatch):
    import cocola_agent_runtime.sandbox_binder as mod

    class CapacityRpcError(grpc.RpcError):
        def code(self):
            return grpc.StatusCode.RESOURCE_EXHAUSTED

    class FakeClient:
        def __init__(self, addr=""):
            pass

        def __enter__(self):
            return self

        def __exit__(self, *a):
            return False

        def acquire(self, session_id, user_id="", image="", env=None):
            raise CapacityRpcError()

    monkeypatch.setattr(mod, "SandboxClient", FakeClient)

    binder = SandboxManagerBinder("addr")
    try:
        await binder.acquire(session_id="S5", user_id="u")
    except RuntimeError as exc:
        assert str(exc) == "current resources are busy; no sandbox capacity available"
    else:
        raise AssertionError("expected RuntimeError")
