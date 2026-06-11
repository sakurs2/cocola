"""Session<->sandbox binding: agent-runtime binds a session to a sandbox.

M2 closed the binding lifecycle on the *sandbox-manager* side (Acquire renews a
lease and converges concurrent calls for one session onto a single sandbox;
Heartbeat keeps it alive; Release tears it down). Step 3 wires that into the
agent-runtime's `Query` path so a session actually gets a real sandbox bound to
it before the agent runs, instead of merely passing through whatever sandbox_id
the caller supplied.

Design mirrors the rest of the runtime (agent_provider, skill_loader): a small
Protocol the server depends on, a production implementation, and a static one for
hermetic tests.

  - `SandboxBinder` (Protocol) — the only thing the server depends on.
  - `SandboxManagerBinder` — production: wraps the blocking `SandboxClient` and
    bridges it to the async server with `anyio.to_thread`, exactly as the client
    docstring foretold ("For the async Agent loop we will wrap these calls with
    anyio.to_thread"). One short-lived channel per acquire keeps it simple; the
    sandbox-manager's Acquire is create-or-reuse, so reconnecting per call is
    cheap and stateless.
  - `StaticSandboxBinder` — in-memory, for tests/dev (no sandbox-manager needed).

Step "make the sandbox actually used" adds a second, orthogonal seam: an
`SandboxExecutor` that turns the agent's tool calls (bash / file IO) into real
work inside the *bound* sandbox. The binder answers "which sandbox is this
session on?"; the executor answers "run this command / read this file in that
sandbox". Same shape as the binder — Protocol + production (gRPC, anyio-bridged)
+ static (in-memory) — so the SDK tool layer depends only on the Protocol.

Failure policy: acquiring a sandbox is best-effort at this layer's CALLER. The
binder itself raises on transport failure; the server decides whether a missing
sandbox is fatal (it emits a terminal `error` event rather than crashing). This
keeps the binder a thin, honest wrapper.
"""

from __future__ import annotations

from collections.abc import AsyncIterator
from dataclasses import dataclass
from typing import Protocol

import anyio
from cocola.sandbox.v1 import sandbox_pb2 as pb
from cocola_common import get_logger

from cocola_agent_runtime.sandbox_client import SandboxClient

log = get_logger("cocola.agent-runtime.sandbox")


@dataclass(frozen=True)
class BoundSandbox:
    """The sandbox bound to a session, as the runtime consumes it.

    A transport-neutral view of the proto `Sandbox` + the Acquire `reused` flag,
    so the server never depends on the generated proto types directly.
    """

    id: str
    endpoint: str = ""
    reused: bool = False


class SandboxBinder(Protocol):
    """The runtime depends on this Protocol only, never a concrete client."""

    async def acquire(
        self, *, session_id: str, user_id: str, image: str = "", env: dict | None = None
    ) -> BoundSandbox:
        """Bind the session to a sandbox (create-or-reuse), renewing its lease."""
        ...

    async def release(self, *, session_id: str) -> None:
        """Best-effort unbind+destroy of the session's sandbox."""
        ...


class SandboxManagerBinder:
    """SandboxBinder backed by sandbox-manager over gRPC.

    Wraps the blocking `SandboxClient` and offloads each call to a worker thread
    so it never blocks the asyncio event loop the gRPC server runs on. A fresh
    channel is opened per call (Acquire is idempotent create-or-reuse), keeping
    this object stateless and safe to share across concurrent sessions.
    """

    def __init__(
        self,
        addr: str,
        *,
        default_image: str = "",
        default_env: dict | None = None,
    ) -> None:
        self._addr = addr
        # Route A provisioning defaults (ADR-0009): when a caller does not pin an
        # image/env, the session sandbox is created from these. This is the seam
        # that makes a session sandbox the Claude-Code brain image and carries
        # the model credentials (ANTHROPIC_*) into the sandbox ENV at creation --
        # never via the prompt channel. An explicit per-call value still wins.
        self._default_image = default_image
        self._default_env = dict(default_env or {})

    async def acquire(
        self, *, session_id: str, user_id: str, image: str = "", env: dict | None = None
    ) -> BoundSandbox:
        eff_image = image or self._default_image
        eff_env = {**self._default_env, **(env or {})}

        def _call() -> BoundSandbox:
            with SandboxClient(addr=self._addr) as sb:
                res = sb.acquire(
                    session_id=session_id, user_id=user_id, image=eff_image, env=eff_env
                )
            box = res.sandbox
            return BoundSandbox(id=box.id, endpoint=box.endpoint, reused=res.reused)

        return await anyio.to_thread.run_sync(_call)

    async def release(self, *, session_id: str) -> None:
        def _call() -> None:
            with SandboxClient(addr=self._addr) as sb:
                sb.release(session_id=session_id)

        await anyio.to_thread.run_sync(_call)


class StaticSandboxBinder:
    """In-memory SandboxBinder for tests and dev (no sandbox-manager needed).

    Hands out a deterministic sandbox id per session and records acquire/release
    calls so tests can assert the lifecycle without a gRPC server. Pass
    `fail_with` to simulate an acquire transport failure.
    """

    def __init__(self, *, fail_with: Exception | None = None) -> None:
        self._fail = fail_with
        self.acquired: list[str] = []
        self.released: list[str] = []
        self._seen: set[str] = set()

    async def acquire(
        self, *, session_id: str, user_id: str, image: str = "", env: dict | None = None
    ) -> BoundSandbox:
        if self._fail is not None:
            raise self._fail
        self.acquired.append(session_id)
        reused = session_id in self._seen
        self._seen.add(session_id)
        return BoundSandbox(id=f"box-{session_id}", endpoint="inmem://local", reused=reused)

    async def release(self, *, session_id: str) -> None:
        self.released.append(session_id)


@dataclass(frozen=True)
class ExecOutcome:
    """Transport-neutral result of running a command in a sandbox.

    A decoded view of the proto `ExecEvent` stream (`SandboxClient.exec`): bytes
    are decoded to text here so the SDK tool layer never juggles encodings. An
    empty `error` means the command ran (even if it exited non-zero); a non-empty
    `error` means the sandbox itself could not run it.
    """

    exit_code: int = 0
    stdout: str = ""
    stderr: str = ""
    error: str = ""

    @property
    def ok(self) -> bool:
        return not self.error and self.exit_code == 0


@dataclass(frozen=True)
class ExecChunk:
    """One incremental frame of a *streaming* exec (Route A shim transport).

    `SandboxClient.exec_stream` yields proto `ExecEvent`s one at a time; this is
    their transport-neutral, already-decoded view so the provider layer never
    touches protobuf or byte encodings. Unlike `ExecOutcome` (a single buffered
    result), a stream is a sequence of these: zero or more `stdout`/`stderr`
    chunks, then exactly one terminal `exit` (or `error` if the sandbox itself
    could not run the command).
    """

    kind: str  # stdout | stderr | exit | error
    data: str = ""  # text payload for stdout / stderr
    exit_code: int = 0  # set when kind == "exit"
    error: str = ""  # set when kind == "error"


def _exec_event_to_chunk(ev: pb.ExecEvent) -> ExecChunk:
    """Decode one proto ExecEvent into an ExecChunk (bytes -> text here)."""
    if ev.kind == pb.EXEC_EVENT_KIND_STDOUT:
        return ExecChunk(kind="stdout", data=ev.stdout.decode("utf-8", "replace"))
    if ev.kind == pb.EXEC_EVENT_KIND_STDERR:
        return ExecChunk(kind="stderr", data=ev.stderr.decode("utf-8", "replace"))
    if ev.kind == pb.EXEC_EVENT_KIND_EXIT:
        return ExecChunk(kind="exit", exit_code=ev.exit_code)
    return ExecChunk(kind="error", error=ev.error)


class SandboxExecutor(Protocol):
    """The SDK tool layer depends on this Protocol only, never a concrete client.

    All methods take an explicit `sandbox_id`: the executor is stateless w.r.t.
    sessions (the binder owns the session->sandbox mapping), so one executor is
    safely shared across every concurrent session.
    """

    async def exec(
        self,
        *,
        sandbox_id: str,
        cmd: list[str],
        cwd: str = "",
        env: dict | None = None,
        stdin: str = "",
        timeout_secs: int = 0,
    ) -> ExecOutcome:
        """Run a command to completion inside the sandbox."""
        ...

    def exec_stream(
        self,
        *,
        sandbox_id: str,
        cmd: list[str],
        cwd: str = "",
        env: dict | None = None,
        stdin: str = "",
        timeout_secs: int = 0,
    ) -> AsyncIterator[ExecChunk]:
        """Run a command and yield its output incrementally as it is produced.

        This is the streaming counterpart of `exec`. Route A's shim transport
        needs it: the in-sandbox shim emits a stream of NDJSON events on stdout
        and the provider must relay them live, not wait for the whole turn to
        finish. Implementations yield `ExecChunk`s (stdout/stderr increments)
        and a terminal `exit` (or `error`).
        """
        ...

    async def read_file(self, *, sandbox_id: str, path: str) -> str:
        """Read a UTF-8 text file from the sandbox."""
        ...

    async def write_file(self, *, sandbox_id: str, path: str, content: str) -> None:
        """Write a UTF-8 text file into the sandbox."""
        ...


class SandboxManagerExecutor:
    """SandboxExecutor backed by sandbox-manager over gRPC.

    Bridges the blocking `SandboxClient` (Exec/ReadFile/WriteFile) to the async
    agent loop with `anyio.to_thread`, exactly like `SandboxManagerBinder`. A
    fresh short-lived channel per call keeps it stateless and concurrency-safe;
    bytes are decoded to text at this boundary so callers stay encoding-free.
    """

    def __init__(self, addr: str) -> None:
        self._addr = addr

    async def exec(
        self,
        *,
        sandbox_id: str,
        cmd: list[str],
        cwd: str = "",
        env: dict | None = None,
        stdin: str = "",
        timeout_secs: int = 0,
    ) -> ExecOutcome:
        def _call() -> ExecOutcome:
            with SandboxClient(addr=self._addr) as sb:
                res = sb.exec(
                    sandbox_id,
                    cmd,
                    cwd=cwd,
                    env=env or {},
                    stdin=stdin.encode("utf-8"),
                    timeout_secs=timeout_secs,
                )
            return ExecOutcome(
                exit_code=res.exit_code,
                stdout=res.stdout.decode("utf-8", "replace"),
                stderr=res.stderr.decode("utf-8", "replace"),
                error=res.error,
            )

        return await anyio.to_thread.run_sync(_call)

    async def read_file(self, *, sandbox_id: str, path: str) -> str:
        def _call() -> str:
            with SandboxClient(addr=self._addr) as sb:
                return sb.read_file(sandbox_id, path).decode("utf-8", "replace")

        return await anyio.to_thread.run_sync(_call)

    async def write_file(self, *, sandbox_id: str, path: str, content: str) -> None:
        def _call() -> None:
            with SandboxClient(addr=self._addr) as sb:
                sb.write_file(sandbox_id, path, content.encode("utf-8"))

        await anyio.to_thread.run_sync(_call)

    async def exec_stream(
        self,
        *,
        sandbox_id: str,
        cmd: list[str],
        cwd: str = "",
        env: dict | None = None,
        stdin: str = "",
        timeout_secs: int = 0,
    ) -> AsyncIterator[ExecChunk]:
        """Stream a command's output via sandbox-manager's Exec server-stream.

        `SandboxClient.exec_stream` is a *blocking* generator (one proto
        ExecEvent per `next()`). We cannot iterate it directly on the event
        loop, so we drive it on a worker thread and hand each decoded chunk
        back across the async boundary through a memory channel. The channel is
        bounded(0) so the producer thread blocks until the consumer takes the
        item -- backpressure that keeps the whole turn from buffering in RAM.
        """
        send, recv = anyio.create_memory_object_stream(0)
        stdin_bytes = stdin.encode("utf-8")

        def _pump() -> None:
            # Runs on a worker thread. A fresh short-lived channel per call keeps
            # this stateless and concurrency-safe, like the buffered exec above.
            try:
                with SandboxClient(addr=self._addr) as sb:
                    for ev in sb.exec_stream(
                        sandbox_id,
                        cmd,
                        cwd=cwd,
                        env=env or {},
                        stdin=stdin_bytes,
                        timeout_secs=timeout_secs,
                    ):
                        anyio.from_thread.run(send.send, _exec_event_to_chunk(ev))
            except Exception as exc:  # noqa: BLE001 - surface as a terminal error chunk
                anyio.from_thread.run(send.send, ExecChunk(kind="error", error=str(exc)))
            finally:
                anyio.from_thread.run(send.aclose)

        async with anyio.create_task_group() as tg:
            tg.start_soon(anyio.to_thread.run_sync, _pump)
            async with recv:
                async for chunk in recv:
                    yield chunk


class StaticSandboxExecutor:
    """In-memory SandboxExecutor for tests and dev (no sandbox-manager needed).

    Keeps a per-sandbox virtual filesystem and records every call so tests can
    assert the SDK tool layer routes to the right method with the right args.
    `exec` echoes the command by default; pass `exec_handler` to script output.
    """

    def __init__(
        self,
        *,
        exec_handler=None,
        stream_handler=None,
        fail_with: Exception | None = None,
    ) -> None:
        self._exec_handler = exec_handler
        # stream_handler(sandbox_id, cmd, stdin) -> Iterable[ExecChunk]; lets a
        # test script a streaming shim turn (NDJSON chunks + a terminal exit).
        self._stream_handler = stream_handler
        self._fail = fail_with
        self.exec_calls: list[dict] = []
        self.stream_calls: list[dict] = []
        self.reads: list[tuple[str, str]] = []
        self.writes: list[tuple[str, str, str]] = []
        self.files: dict[tuple[str, str], str] = {}

    async def exec(
        self,
        *,
        sandbox_id: str,
        cmd: list[str],
        cwd: str = "",
        env: dict | None = None,
        stdin: str = "",
        timeout_secs: int = 0,
    ) -> ExecOutcome:
        if self._fail is not None:
            raise self._fail
        self.exec_calls.append({"sandbox_id": sandbox_id, "cmd": cmd, "cwd": cwd, "env": env or {}})
        if self._exec_handler is not None:
            return self._exec_handler(sandbox_id, cmd)
        return ExecOutcome(exit_code=0, stdout="ran: " + " ".join(cmd))

    async def read_file(self, *, sandbox_id: str, path: str) -> str:
        if self._fail is not None:
            raise self._fail
        self.reads.append((sandbox_id, path))
        try:
            return self.files[(sandbox_id, path)]
        except KeyError:
            raise FileNotFoundError(path) from None

    async def write_file(self, *, sandbox_id: str, path: str, content: str) -> None:
        if self._fail is not None:
            raise self._fail
        self.writes.append((sandbox_id, path, content))
        self.files[(sandbox_id, path)] = content

    async def exec_stream(
        self,
        *,
        sandbox_id: str,
        cmd: list[str],
        cwd: str = "",
        env: dict | None = None,
        stdin: str = "",
        timeout_secs: int = 0,
    ) -> AsyncIterator[ExecChunk]:
        if self._fail is not None:
            raise self._fail
        self.stream_calls.append({"sandbox_id": sandbox_id, "cmd": cmd, "cwd": cwd, "stdin": stdin})
        if self._stream_handler is not None:
            for chunk in self._stream_handler(sandbox_id, cmd, stdin):
                yield chunk
            return
        # Default: echo the command on stdout then exit 0, mirroring `exec`.
        yield ExecChunk(kind="stdout", data="ran: " + " ".join(cmd))
        yield ExecChunk(kind="exit", exit_code=0)
