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

Failure policy: acquiring a sandbox is best-effort at this layer's CALLER. The
binder itself raises on transport failure; the server decides whether a missing
sandbox is fatal (it emits a terminal `error` event rather than crashing). This
keeps the binder a thin, honest wrapper.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

import anyio

from cocola_agent_runtime.sandbox_client import SandboxClient
from cocola_common import get_logger

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

    def __init__(self, addr: str) -> None:
        self._addr = addr

    async def acquire(
        self, *, session_id: str, user_id: str, image: str = "", env: dict | None = None
    ) -> BoundSandbox:
        def _call() -> BoundSandbox:
            with SandboxClient(addr=self._addr) as sb:
                res = sb.acquire(
                    session_id=session_id, user_id=user_id, image=image, env=env or {}
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
        return BoundSandbox(
            id=f"box-{session_id}", endpoint="inmem://local", reused=reused
        )

    async def release(self, *, session_id: str) -> None:
        self.released.append(session_id)
