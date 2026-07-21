"""Thin async-friendly gRPC client wrapper around SandboxService.

agent-runtime never talks to a concrete sandbox backend (Docker / K8s+gVisor)
directly. It only knows this gRPC contract, exposed by sandbox-manager. That
keeps the Python runtime fully decoupled from the orchestration layer: swapping
Docker for K8s is invisible here.

The generated stubs live in packages/proto/gen/python; that directory must be
on PYTHONPATH (the Makefile / demo wire it up).
"""

from __future__ import annotations

from collections.abc import Iterator
from dataclasses import dataclass, field

import grpc
from cocola.sandbox.v1 import sandbox_pb2 as pb
from cocola.sandbox.v1 import sandbox_pb2_grpc as pb_grpc

from cocola_agent_runtime.grpc_limits import channel_options


@dataclass
class ExecResult:
    """Aggregated result of a (streamed) Exec call."""

    exit_code: int = 0
    stdout: bytes = b""
    stderr: bytes = b""
    error: str = ""


@dataclass
class AcquireResult:
    """Result of an Acquire call: the bound sandbox plus a reuse flag."""

    sandbox: pb.Sandbox
    reused: bool = False
    workspace_state: int = pb.WORKSPACE_STATE_UNSPECIFIED
    workspace_node: str = ""
    previous_workspace_node: str = ""


@dataclass
class SandboxClient:
    """Blocking gRPC client. Suitable for scripts and the e2e demo.

    For the async Agent loop we will wrap these calls with anyio.to_thread,
    but the wire surface stays identical, so the contract is defined once here.
    """

    addr: str = "localhost:50051"
    _channel: grpc.Channel | None = field(default=None, init=False, repr=False)
    _stub: pb_grpc.SandboxServiceStub | None = field(default=None, init=False, repr=False)

    def __enter__(self) -> SandboxClient:
        # Raise the message ceiling above gRPC's 4 MiB default: WriteFile
        # carries the full attachment bytes into the sandbox, which can
        # exceed 4 MiB (COCOLA_GRPC_MAX_MESSAGE_BYTES, default 64 MiB).
        self._channel = grpc.insecure_channel(self.addr, options=channel_options())
        self._stub = pb_grpc.SandboxServiceStub(self._channel)
        return self

    def __exit__(self, *_exc) -> None:
        if self._channel is not None:
            self._channel.close()

    @property
    def stub(self) -> pb_grpc.SandboxServiceStub:
        if self._stub is None:
            raise RuntimeError("SandboxClient must be used as a context manager")
        return self._stub

    def create(
        self,
        user_id: str,
        session_id: str,
        image: str = "",
        env: dict[str, str] | None = None,
    ) -> pb.Sandbox:
        spec = pb.SandboxSpec(
            user_id=user_id,
            session_id=session_id,
            image=image,
            env=env or {},
        )
        resp = self.stub.Create(pb.CreateRequest(spec=spec))
        return resp.sandbox

    def exec_stream(
        self,
        sandbox_id: str,
        cmd: list[str],
        cwd: str = "",
        env: dict[str, str] | None = None,
        stdin: bytes = b"",
        timeout_secs: int = 0,
    ) -> Iterator[pb.ExecEvent]:

        yield from self.open_exec_stream(
            sandbox_id,
            cmd,
            cwd=cwd,
            env=env,
            stdin=stdin,
            timeout_secs=timeout_secs,
        )

    def open_exec_stream(
        self,
        sandbox_id: str,
        cmd: list[str],
        cwd: str = "",
        env: dict[str, str] | None = None,
        stdin: bytes = b"",
        timeout_secs: int = 0,
    ):
        """Start Exec and return its cancellable gRPC stream call."""
        req = pb.ExecRequest(
            sandbox_id=sandbox_id,
            cmd=cmd,
            cwd=cwd,
            env=env or {},
            stdin=stdin,
            timeout_secs=timeout_secs,
        )
        return self.stub.Exec(req)

    def exec(self, sandbox_id: str, cmd: list[str], **kw) -> ExecResult:
        """Drain the Exec stream into a single ExecResult."""
        result = ExecResult()
        out, err = bytearray(), bytearray()
        for ev in self.exec_stream(sandbox_id, cmd, **kw):
            if ev.kind == pb.EXEC_EVENT_KIND_STDOUT:
                out += ev.stdout
            elif ev.kind == pb.EXEC_EVENT_KIND_STDERR:
                err += ev.stderr
            elif ev.kind == pb.EXEC_EVENT_KIND_EXIT:
                result.exit_code = ev.exit_code
            elif ev.kind == pb.EXEC_EVENT_KIND_ERROR:
                result.error = ev.error
        result.stdout, result.stderr = bytes(out), bytes(err)
        return result

    def write_file(self, sandbox_id: str, path: str, data: bytes) -> None:
        self.stub.WriteFile(pb.WriteFileRequest(sandbox_id=sandbox_id, path=path, data=data))

    def read_file(self, sandbox_id: str, path: str) -> bytes:
        return self.stub.ReadFile(pb.ReadFileRequest(sandbox_id=sandbox_id, path=path)).data

    def acquire(
        self,
        session_id: str,
        user_id: str = "",
        image: str = "",
        env: dict[str, str] | None = None,
        allow_workspace_reset: bool = False,
        additional_egress_allowlist: list[str] | None = None,
    ) -> AcquireResult:
        """Bind a session to a sandbox (create-or-reuse).

        This is the session-aware entrypoint the Agent loop should use instead
        of create(): the same session_id always converges on the same sandbox,
        and the call renews the lease so the reaper keeps it alive.
        """
        resp = self.stub.Acquire(
            pb.AcquireRequest(
                session_id=session_id,
                user_id=user_id,
                image=image,
                env=env or {},
                allow_workspace_reset=allow_workspace_reset,
                additional_egress_allowlist=additional_egress_allowlist or [],
            )
        )
        return AcquireResult(
            sandbox=resp.sandbox,
            reused=resp.reused,
            workspace_state=resp.workspace_state,
            workspace_node=resp.workspace_node,
            previous_workspace_node=resp.previous_workspace_node,
        )

    def heartbeat(self, sandbox_id: str) -> None:
        """Renew the lease for a bound sandbox so it is not reclaimed."""
        self.stub.Heartbeat(pb.HeartbeatRequest(sandbox_id=sandbox_id))

    def release(self, session_id: str, user_id: str, *, timeout_s: float | None = None) -> None:
        """Explicitly release a session's sandbox (unbind + destroy)."""
        request = pb.ReleaseRequest(session_id=session_id, user_id=user_id)
        if timeout_s is None:
            self.stub.Release(request)
        else:
            self.stub.Release(request, timeout=timeout_s)

    def destroy(self, sandbox_id: str) -> None:
        self.stub.Destroy(pb.DestroyRequest(sandbox_id=sandbox_id))

    def health(self, sandbox_id: str) -> pb.HealthResponse:
        return self.stub.Health(pb.HealthRequest(sandbox_id=sandbox_id))
