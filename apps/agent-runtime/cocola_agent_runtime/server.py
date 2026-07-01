"""gRPC server: exposes AgentProvider over the AgentRuntimeService contract.

This is the runtime\'s network edge. The gateway (BFF) is the only caller; it
opens a server-streaming `Query` RPC and forwards each `AgentEvent` to the web
client. Everything below the wire is the layering the package docstring fixes:

    grpc server (here)  ->  AgentProvider (Protocol)  ->  concrete provider
                                                   ->  SkillCatalog (enabled skills)

Design choices, all to avoid reinventing what we already have:

- The servicer depends ONLY on the `AgentProvider` Protocol and the
  `SkillCatalog` Protocol, never on a concrete provider. Production injects
  `ClaudeAgentSDKProvider` + `AdminSkillCatalog`; tests inject fakes. This is the
  same composition-root pattern the rest of the runtime uses.
- The generic `AgentEvent` dataclass the provider yields maps 1:1 onto the proto
  `AgentEvent` (a `kind` string + a flat `map<string,string>` of data). Non-string
  payloads (tool input dicts, costs) are JSON/str-encoded into the map so the
  schema stays flat and consumers can tolerate unknown kinds, exactly as the
  proto comment requires.
- Enabled Skill-Market skills are folded into the session via
  `apply_skills_to_options` before the provider runs, so toggling a skill in the
  control plane changes the agent with no redeploy.
"""

from __future__ import annotations

import json
import os
import pathlib
import posixpath
import tempfile
from typing import Any

from cocola.agent.v1 import agent_pb2 as pb
from cocola.agent.v1 import agent_pb2_grpc as pb_grpc
from cocola_common import get_logger

from cocola_agent_runtime.agent_provider import AgentEvent, AgentOptions, AgentProvider
from cocola_agent_runtime.sandbox_binder import SandboxBinder, SandboxExecutor
from cocola_agent_runtime.skill_loader import SkillCatalog, apply_skills_to_options

log = get_logger("cocola.agent-runtime.server")


def _sanitize_filename(name: str) -> str:
    """Reduce an uploaded filename to a safe basename.

    Defends the landing directory against path traversal and separator tricks:
    take the basename only, strip any residual separators / parent refs / NULs,
    and fall back to a fixed name when nothing usable remains. The file always
    lands directly under ./uploads/ -- never above it.
    """
    base = posixpath.basename((name or "").replace("\\", "/")).strip()
    base = base.replace("\x00", "").lstrip(".") or "file"
    # Collapse anything still separator-like; basename already dropped dirs.
    return base.replace("/", "_") or "file"


def _uploads_preamble(landed: list[str]) -> str:
    """Natural-language note telling the model where its uploads landed."""
    if not landed:
        return ""
    listing = "\n".join(f"- {p}" for p in landed)
    return (
        "The user uploaded the following file(s) into your working directory. "
        "Read them from these paths when relevant:\n"
        f"{listing}"
    )


def _stringify(value: Any) -> str:
    """Flatten an arbitrary event-data value to a string for the proto map.

    Strings pass through; everything else is JSON-encoded so structured payloads
    (e.g. a tool\'s input dict) survive the flat map<string,string> envelope and
    the BFF can re-parse them if it wants.
    """
    if isinstance(value, str):
        return value
    if value is None:
        return ""
    try:
        return json.dumps(value, ensure_ascii=False, default=str)
    except (TypeError, ValueError):
        return str(value)


def event_to_proto(event: AgentEvent) -> pb.AgentEvent:
    """Map the runtime\'s generic AgentEvent onto the proto AgentEvent."""
    data = {k: _stringify(v) for k, v in (event.data or {}).items()}
    return pb.AgentEvent(kind=event.kind, data=data)


class AgentRuntimeServicer(pb_grpc.AgentRuntimeServiceServicer):
    """Serves AgentRuntimeService.Query by driving an injected AgentProvider."""

    def __init__(
        self,
        provider: AgentProvider,
        *,
        skills: SkillCatalog | None = None,
        binder: SandboxBinder | None = None,
        executor: SandboxExecutor | None = None,
    ) -> None:
        self._provider = provider
        self._skills = skills
        self._binder = binder
        # The executor writes user-uploaded attachments into the bound sandbox
        # before the agent runs (push model, ADR-0017). Optional: when it is not
        # wired, attachments are dropped with a warning rather than failing the
        # turn -- the same posture as running without a binder.
        self._executor = executor

    async def Query(self, request, context):  # noqa: N802 - gRPC-generated name
        """Server-streaming RPC: run one agent turn, stream events back.

        A provider error is surfaced as a terminal `error` event (not a dropped
        stream) so the BFF/client always sees a clean end. We do NOT also append
        a `done` here: the provider is responsible for its own terminal event
        (ClaudeAgentSDKProvider yields `done`); on error we substitute one.
        """
        sandbox_id = request.sandbox_id or None

        # Bind the session to a real sandbox when a binder is wired and the
        # caller did not pin one. Acquire is create-or-reuse (M2): the same
        # session converges on one sandbox and the call renews its lease. A bind
        # failure is surfaced as a terminal `error` event rather than crashing
        # the stream, and the agent does not run without its execution sandbox.
        if self._binder is not None and sandbox_id is None:
            try:
                box = await self._binder.acquire(
                    session_id=request.session_id, user_id=request.user_id
                )
            except Exception as exc:  # noqa: BLE001 - bind failure -> clean terminal event
                log.warning(
                    "sandbox acquire failed",
                    session_id=request.session_id,
                    error=str(exc),
                )
                await context.write(
                    pb.AgentEvent(
                        kind="error",
                        data={"error": f"sandbox acquire failed: {exc}"},
                    )
                )
                return
            sandbox_id = box.id
            # Make the binding observable to the BFF/client.
            await context.write(
                event_to_proto(
                    AgentEvent(
                        kind="sandbox",
                        data={
                            "sandbox_id": box.id,
                            "endpoint": box.endpoint,
                            "reused": box.reused,
                        },
                    )
                )
            )

        # Pre-provision user-uploaded attachments into the bound sandbox before
        # the agent runs (push model, ADR-0017). We land them under ./uploads/
        # in the session workspace and prepend a short preamble so the model
        # knows the files exist and where to find them. A provisioning failure is
        # surfaced as a terminal `error` event (like an acquire failure) rather
        # than silently running the agent against files that never arrived.
        prompt = request.prompt
        workspace: str | None = None
        if request.attachments:
            try:
                preamble, workspace = await self._provision_attachments(
                    sandbox_id, request.session_id, list(request.attachments)
                )
            except Exception as exc:  # noqa: BLE001 - clean terminal event, no bare crash
                log.warning(
                    "attachment provisioning failed",
                    session_id=request.session_id,
                    error=str(exc),
                )
                await context.write(
                    pb.AgentEvent(
                        kind="error",
                        data={"error": f"attachment provisioning failed: {exc}"},
                    )
                )
                return
            if preamble:
                prompt = f"{preamble}\n\n{request.prompt}"

        opts = AgentOptions(
            user_id=request.user_id,
            session_id=request.session_id,
            sandbox_id=sandbox_id,
            workspace=workspace,
            max_turns=request.max_turns or 30,
        )
        if self._skills is not None:
            opts = apply_skills_to_options(opts, self._skills)

        log.info(
            "agent query",
            user_id=request.user_id,
            session_id=request.session_id,
            has_sandbox=bool(sandbox_id),
            attachments=len(request.attachments),
        )
        try:
            async for event in self._provider.query(prompt, opts):
                await context.write(event_to_proto(event))
        except Exception as exc:  # noqa: BLE001 - turn any provider fault into a clean terminal event
            log.warning("agent query failed", session_id=request.session_id, error=str(exc))
            await context.write(pb.AgentEvent(kind="error", data={"error": str(exc)}))

    async def _provision_attachments(
        self, sandbox_id, session_id, attachments
    ) -> tuple[str, str | None]:
        """Land uploaded files where the agent brain can read them.

        Returns ``(preamble, workspace)``:

        - ``preamble`` is a short natural-language note listing the landed
          ``./uploads/<name>`` paths, or "" when there is nothing to provision.
        - ``workspace`` is a HOST directory the in-process provider must adopt as
          its cwd, or ``None`` when landing happened inside a sandbox (Route A,
          the brain already runs with that cwd) or nothing was provisioned.

        The delivery target follows WHERE the brain runs (ADR-0017):

        - Route A (executor + sandbox bound): the brain runs INSIDE the sandbox,
          so we write into its workspace over the executor. Files land under
          ./uploads/ in the session cwd (/workspace/<session_id>/, ADR-0008 T1b),
          resolved via `pwd` (provider-agnostic) with `mkdir -p uploads` in the
          same shell -- WriteFile (docker CopyToContainer) makes no parent dirs.
        - Local dev (no executor/sandbox): the brain runs in THIS process
          (ClaudeAgentSDKProvider). A sandbox write would be unreachable, so we
          write into a per-session HOST dir and hand its path back as the cwd for
          the SDK, whose native Read/Bash then resolve ./uploads/ against it.

        Content is written binary-safe so images survive intact.
        """
        if not attachments:
            return "", None

        if self._executor is not None and sandbox_id:
            preamble = await self._provision_into_sandbox(sandbox_id, attachments)
            return preamble, None

        preamble, workspace = self._provision_onto_host(session_id, attachments)
        return preamble, workspace

    async def _provision_into_sandbox(self, sandbox_id, attachments) -> str:
        """Route A: write attachments into the bound sandbox's ./uploads/."""
        # One shell: create ./uploads and print the absolute workspace cwd.
        res = await self._executor.exec(
            sandbox_id=sandbox_id,
            cmd=["sh", "-c", "mkdir -p uploads && pwd"],
        )
        if not res.ok:
            raise RuntimeError(
                f"prepare uploads dir failed (exit={res.exit_code}): "
                f"{res.error or res.stderr}".strip()
            )
        cwd = res.stdout.strip() or "/workspace"
        uploads_dir = posixpath.join(cwd, "uploads")

        landed: list[str] = []
        for att in attachments:
            name = _sanitize_filename(att.filename)
            abs_path = posixpath.join(uploads_dir, name)
            await self._executor.write_bytes(
                sandbox_id=sandbox_id, path=abs_path, data=bytes(att.content)
            )
            landed.append(f"./uploads/{name}")
        return _uploads_preamble(landed)

    def _provision_onto_host(self, session_id, attachments) -> tuple[str, str]:
        """Local dev: write attachments into a per-session HOST workspace.

        The workspace root is COCOLA_LOCAL_WORKSPACE_ROOT (default: a stable
        `cocola-workspaces/` under the OS temp dir). Each session gets its own
        subdir so concurrent sessions never collide, and ./uploads/ lives under
        it -- the same relative layout the sandbox path uses, so the preamble is
        identical regardless of where the brain runs.
        """
        root = os.getenv("COCOLA_LOCAL_WORKSPACE_ROOT", "").strip() or posixpath.join(
            tempfile.gettempdir(), "cocola-workspaces"
        )
        safe_session = _sanitize_filename(session_id or "session")
        workspace = pathlib.Path(root) / safe_session
        uploads_dir = workspace / "uploads"
        uploads_dir.mkdir(parents=True, exist_ok=True)

        landed: list[str] = []
        for att in attachments:
            name = _sanitize_filename(att.filename)
            (uploads_dir / name).write_bytes(bytes(att.content))
            landed.append(f"./uploads/{name}")

        log.info(
            "attachments landed on host workspace (no sandbox)",
            session_id=session_id,
            workspace=str(workspace),
            count=len(landed),
        )
        return _uploads_preamble(landed), str(workspace)
