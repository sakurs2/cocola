"""gRPC server: exposes AgentProvider over the AgentRuntimeService contract.

This is the runtime\'s network edge. The gateway (BFF) is the only caller; it
opens a server-streaming `Query` RPC and forwards each `AgentEvent` to the web
client. Everything below the wire is the layering the package docstring fixes:

    grpc server (here)  ->  AgentProvider (Protocol)  ->  concrete provider
                                                   ->  SkillCatalog (enabled skills)

Design choices, all to avoid reinventing what we already have:

- The servicer depends ONLY on the `AgentProvider` Protocol and the
  `SkillCatalog` Protocol, never on a concrete provider. Production injects
  `InSandboxShimProvider` (Route A) + `AdminSkillCatalog`; tests inject fakes.
  This is the same composition-root pattern the rest of the runtime uses.
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

import asyncio
import dataclasses
import json
import mimetypes
import os
import pathlib
import posixpath
import tempfile
import time
import uuid
from typing import Any, NamedTuple

from cocola.agent.v1 import agent_pb2 as pb
from cocola.agent.v1 import agent_pb2_grpc as pb_grpc
from cocola_common import get_logger

from cocola_agent_runtime.agent_provider import AgentEvent, AgentOptions, AgentProvider
from cocola_agent_runtime.objstore import Fetcher
from cocola_agent_runtime.sandbox_binder import SandboxBinder, SandboxExecutor
from cocola_agent_runtime.session_map import SessionMap
from cocola_agent_runtime.skill_loader import SkillCatalog, apply_skills_to_options

log = get_logger("cocola.agent-runtime.server")

ARTIFACT_SYSTEM_PROMPT = (
    "When you create files that the user should download or preview, save them "
    "under ./outputs/. Only files in ./outputs/ are published to the user."
)
CURRENT_RUNTIME = os.getenv("COCOLA_AGENT_RUNTIME", "claude-code")
MODEL_ALIAS_METADATA_KEY = "x-cocola-model-alias"

_OUTPUTS_SNAPSHOT_SCRIPT = r"""
import json
import os

root = "outputs"
os.makedirs(root, exist_ok=True)
out = {}
for dirpath, _, files in os.walk(root):
    for name in files:
        path = os.path.join(dirpath, name)
        try:
            st = os.stat(path)
        except OSError:
            continue
        rel = os.path.relpath(path, ".").replace(os.sep, "/")
        out[rel] = {"size": st.st_size, "mtime_ns": st.st_mtime_ns}
print(json.dumps(out, sort_keys=True))
"""

_CHECKPOINT_UPLOAD_SCRIPT = r"""
set -eu
: "${COCOLA_CHECKPOINT_PUT_URL:?missing checkpoint upload URL}"
if ! command -v zstd >/dev/null 2>&1; then
  echo "zstd is required for checkpoint upload" >&2
  exit 127
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required for checkpoint upload" >&2
  exit 127
fi
mkdir -p /workspace /home/cocola/.claude
tar -C / -cf - workspace home/cocola/.claude \
  | zstd -T0 -3 -q \
  | curl -fsS -X PUT --upload-file - "${COCOLA_CHECKPOINT_PUT_URL}"
"""

_CHECKPOINT_RESTORE_SCRIPT = r"""
set -eu
: "${COCOLA_CHECKPOINT_GET_URL:?missing checkpoint download URL}"
if ! command -v zstd >/dev/null 2>&1; then
  echo "zstd is required for checkpoint restore" >&2
  exit 127
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required for checkpoint restore" >&2
  exit 127
fi
mkdir -p /workspace /home/cocola/.claude
curl -fsS "${COCOLA_CHECKPOINT_GET_URL}" \
  | zstd -d -q \
  | tar -C / -xf -
"""


class _ResolvedAttachment(NamedTuple):
    """An attachment with its bytes in hand, whatever the delivery path.

    filename is the raw (unsanitized) upload name; content is the full bytes,
    either from the inline push (small files) or pulled from the object store
    (large files, ADR-0017 P1a). Provisioning sanitizes the name at write time.
    """

    filename: str
    content: bytes


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


def _merge_system_prompt(base: str | None, extra: str) -> str:
    if not base:
        return extra
    return extra + "\n\n" + base


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


def _artifact_max_bytes() -> int:
    raw = os.getenv("COCOLA_ARTIFACT_MAX_BYTES", "").strip()
    if raw:
        try:
            n = int(raw)
            if n > 0:
                return n
        except ValueError:
            pass
    return 32 * 1024 * 1024


def _session_checkpoint_enabled() -> bool:
    raw = os.getenv("COCOLA_SESSION_CHECKPOINT_ENABLED", "").strip().lower()
    return raw in {"1", "true", "yes", "on"}


def _checkpoint_url_ttl_seconds() -> int:
    raw = os.getenv("COCOLA_SESSION_CHECKPOINT_URL_TTL_SECONDS", "").strip()
    if raw:
        try:
            n = int(raw)
            if n > 0:
                return n
        except ValueError:
            pass
    return 3600


def _metadata_value(context, key: str) -> str:
    for item in context.invocation_metadata() or ():
        if item.key.lower() == key.lower():
            return str(item.value).strip()
    return ""


def _enabled(value: Any) -> bool:
    if value is None:
        return True
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        return value.strip().lower() not in {"0", "false", "no", "off"}
    return bool(value)


def _validated_model_alias(requested_alias: str) -> str | None:
    """Resolve and validate a user-facing model alias for this runtime.

    If COCOLA_LLM_CONFIG is mounted here, agent-runtime validates that the alias
    exists, is enabled, and targets this runtime. Without that file, we keep the
    dev path permissive and simply pass through the alias to Claude Code.
    """
    alias = requested_alias.strip()
    path = _llm_config_path()
    if not path:
        return alias or None
    with open(path, encoding="utf-8") as fh:
        spec = json.load(fh)
    routes = spec.get("routes") or {}
    if not alias:
        alias = str(spec.get("default_alias") or "").strip()
    if not alias:
        return None
    route = routes.get(alias)
    if not isinstance(route, dict):
        raise ValueError(f"unknown model alias {alias!r}")
    if not _enabled(route.get("enabled", True)):
        raise ValueError(f"model alias {alias!r} is disabled")
    runtime = str(route.get("runtime") or "claude-code")
    if runtime != CURRENT_RUNTIME:
        raise ValueError(
            f"model alias {alias!r} targets runtime {runtime!r}, "
            f"current runtime is {CURRENT_RUNTIME!r}"
        )
    return alias


def _llm_config_path() -> str:
    explicit = os.getenv("COCOLA_LLM_CONFIG", "").strip()
    if explicit:
        explicit_path = pathlib.Path(explicit)
        if explicit_path.is_absolute():
            return explicit
        repo_root = pathlib.Path(__file__).resolve().parents[3]
        for candidate in (pathlib.Path.cwd() / explicit_path, repo_root / explicit_path):
            if candidate.exists():
                return str(candidate)
        return explicit
    repo_root = pathlib.Path(__file__).resolve().parents[3]
    for candidate in (
        pathlib.Path.cwd() / "deploy" / "llm-config.json",
        repo_root / "deploy" / "llm-config.json",
    ):
        if candidate.exists():
            return str(candidate)
    return ""


def _model_env(model_alias: str | None) -> dict[str, str]:
    if not model_alias:
        return {}
    return {
        "ANTHROPIC_MODEL": model_alias,
        "ANTHROPIC_SMALL_FAST_MODEL": model_alias,
    }


class AgentRuntimeServicer(pb_grpc.AgentRuntimeServiceServicer):
    """Serves AgentRuntimeService.Query by driving an injected AgentProvider."""

    def __init__(
        self,
        provider: AgentProvider,
        *,
        skills: SkillCatalog | None = None,
        binder: SandboxBinder | None = None,
        executor: SandboxExecutor | None = None,
        objstore: Fetcher | None = None,
        session_map: SessionMap | None = None,
    ) -> None:
        self._provider = provider
        self._skills = skills
        self._binder = binder
        self._session_map = session_map
        # The executor writes user-uploaded attachments into the bound sandbox
        # before the agent runs (push model, ADR-0017). Optional: when it is not
        # wired, attachments are dropped with a warning rather than failing the
        # turn -- the same posture as running without a binder.
        self._executor = executor
        # Object store to pull large (key-only) attachments from on the model's
        # behalf (ADR-0017 P1a). The gateway uploads every file and ships large
        # ones as an oss_key with no inline bytes; here we materialize those
        # before provisioning. Optional: unset => a key-only attachment surfaces
        # as a clean provisioning error rather than a silent empty file.
        self._objstore = objstore

    async def ReleaseSession(self, request, context):  # noqa: N802 - gRPC-generated name
        """Best-effort release of runtime state bound to a conversation."""
        session_id = request.session_id
        if not session_id:
            return pb.ReleaseSessionResponse()
        if self._binder is not None:
            try:
                await self._binder.release(session_id=session_id)
            except Exception as exc:  # noqa: BLE001 - delete should not fail on cleanup
                log.warning("sandbox release failed", session_id=session_id, error=str(exc))
        if self._session_map is not None:
            try:
                await self._session_map.delete(session_id)
            except Exception as exc:  # noqa: BLE001 - stale resume cleanup is best-effort
                log.warning("session map delete failed", session_id=session_id, error=str(exc))
        return pb.ReleaseSessionResponse()

    async def Query(self, request, context):  # noqa: N802 - gRPC-generated name
        """Server-streaming RPC: run one agent turn, stream events back.

        A provider error is surfaced as a terminal `error` event (not a dropped
        stream) so the BFF/client always sees a clean end. We do NOT also append
        a `done` here: the provider is responsible for its own terminal event
        (the shim provider yields `done`); on error we substitute one.
        """
        sandbox_id = request.sandbox_id or None
        try:
            model_alias = _validated_model_alias(_metadata_value(context, MODEL_ALIAS_METADATA_KEY))
        except Exception as exc:  # noqa: BLE001 - config/alias error -> clean terminal event
            log.warning(
                "model alias validation failed",
                session_id=request.session_id,
                error=str(exc),
            )
            await context.write(
                pb.AgentEvent(
                    kind="error",
                    data={"error": f"model selection failed: {exc}"},
                )
            )
            return
        model_env = _model_env(model_alias)

        # Bind the session to a real sandbox when a binder is wired and the
        # caller did not pin one. Acquire is create-or-reuse (M2): the same
        # session converges on one sandbox and the call renews its lease. A bind
        # failure is surfaced as a terminal `error` event rather than crashing
        # the stream, and the agent does not run without its execution sandbox.
        if self._binder is not None and sandbox_id is None:
            try:
                box = await self._binder.acquire(
                    session_id=request.session_id, user_id=request.user_id, env=model_env
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
            if not box.reused:
                await self._restore_session_checkpoint(
                    sandbox_id=sandbox_id,
                    session_id=request.session_id,
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
            model_alias=model_alias,
        )
        artifacts_enabled = bool(self._objstore is not None and hasattr(self._objstore, "put"))
        if artifacts_enabled:
            opts = dataclasses.replace(
                opts,
                system_prompt=_merge_system_prompt(opts.system_prompt, ARTIFACT_SYSTEM_PROMPT),
            )
        if self._skills is not None:
            opts = apply_skills_to_options(opts, self._skills)

        log.info(
            "agent query",
            user_id=request.user_id,
            session_id=request.session_id,
            has_sandbox=bool(sandbox_id),
            attachments=len(request.attachments),
            model_alias=model_alias or "",
        )
        outputs_before = await self._snapshot_outputs(sandbox_id) if artifacts_enabled else {}
        try:
            terminal_done: AgentEvent | None = None
            async for event in self._provider.query(prompt, opts):
                if event.kind == "done":
                    terminal_done = event
                    continue
                await context.write(event_to_proto(event))
            if artifacts_enabled:
                async for event in self._publish_output_artifacts(
                    sandbox_id=sandbox_id,
                    user_id=request.user_id,
                    session_id=request.session_id,
                    before=outputs_before,
                ):
                    await context.write(event_to_proto(event))
            await self._checkpoint_session_storage(
                sandbox_id=sandbox_id,
                user_id=request.user_id,
                session_id=request.session_id,
            )
            if terminal_done is not None:
                await context.write(event_to_proto(terminal_done))
        except Exception as exc:  # noqa: BLE001 - turn any provider fault into a clean terminal event
            log.warning("agent query failed", session_id=request.session_id, error=str(exc))
            await context.write(pb.AgentEvent(kind="error", data={"error": str(exc)}))

    async def _checkpoint_session_storage(
        self,
        *,
        sandbox_id: str | None,
        user_id: str,
        session_id: str,
    ) -> str | None:
        """Best-effort upload of session storage from inside the sandbox.

        The object store signs a short-lived PUT URL; the sandbox streams
        ``tar | zstd | curl`` directly to that URL. Agent-runtime never reads the
        archive bytes into memory, and MinIO credentials never enter the sandbox.
        """
        if not _session_checkpoint_enabled():
            return None
        if self._executor is None or self._objstore is None or not sandbox_id:
            return None
        signer = getattr(self._objstore, "presigned_put_url", None)
        if signer is None:
            log.warning(
                "session checkpoint skipped: object store cannot sign put urls",
                session_id=session_id,
            )
            return None

        checkpoint_id = str(uuid.uuid4())
        key = "checkpoints/{}/{}/{}-{}.tar.zst".format(
            _sanitize_filename(user_id or "user"),
            _sanitize_filename(session_id or "session"),
            int(time.time()),
            checkpoint_id,
        )
        try:
            put_url = await asyncio.to_thread(
                signer,
                key,
                expires_seconds=_checkpoint_url_ttl_seconds(),
            )
            res = await self._executor.exec(
                sandbox_id=sandbox_id,
                cmd=["sh", "-c", _CHECKPOINT_UPLOAD_SCRIPT],
                env={"COCOLA_CHECKPOINT_PUT_URL": put_url},
                timeout_secs=0,
            )
        except Exception as exc:  # noqa: BLE001 - checkpoint is best-effort
            log.warning("session checkpoint failed", session_id=session_id, error=str(exc))
            return None
        if not res.ok:
            log.warning(
                "session checkpoint command failed",
                session_id=session_id,
                exit_code=res.exit_code,
                error=(res.error or res.stderr or ""),
            )
            return None
        log.info("session checkpoint uploaded", session_id=session_id, key=key)
        if self._session_map is not None:
            try:
                await self._session_map.put_checkpoint(
                    session_id,
                    key,
                    user_id=user_id,
                )
            except Exception as exc:  # noqa: BLE001 - checkpoint index is best-effort
                log.warning(
                    "session checkpoint index update failed",
                    session_id=session_id,
                    error=str(exc),
                )
        return key

    async def _restore_session_checkpoint(
        self,
        *,
        sandbox_id: str | None,
        session_id: str,
    ) -> bool:
        """Best-effort restore of the latest checkpoint into a fresh sandbox."""
        if self._executor is None or self._objstore is None or self._session_map is None:
            return False
        if not sandbox_id:
            return False
        try:
            key = await self._session_map.get_checkpoint(session_id)
        except Exception as exc:  # noqa: BLE001 - restore is best-effort
            log.warning("session checkpoint lookup failed", session_id=session_id, error=str(exc))
            return False
        if not key:
            return False
        signer = getattr(self._objstore, "presigned_get_url", None)
        if signer is None:
            log.warning(
                "session checkpoint restore skipped: object store cannot sign get urls",
                session_id=session_id,
            )
            return False
        try:
            get_url = await asyncio.to_thread(
                signer,
                key,
                expires_seconds=_checkpoint_url_ttl_seconds(),
            )
            res = await self._executor.exec(
                sandbox_id=sandbox_id,
                cmd=["sh", "-c", _CHECKPOINT_RESTORE_SCRIPT],
                env={"COCOLA_CHECKPOINT_GET_URL": get_url},
                timeout_secs=0,
            )
        except Exception as exc:  # noqa: BLE001 - restore is best-effort
            log.warning("session checkpoint restore failed", session_id=session_id, error=str(exc))
            return False
        if not res.ok:
            log.warning(
                "session checkpoint restore command failed",
                session_id=session_id,
                exit_code=res.exit_code,
                error=(res.error or res.stderr or ""),
            )
            return False
        log.info("session checkpoint restored", session_id=session_id, key=key)
        return True

    async def _snapshot_outputs(self, sandbox_id: str | None) -> dict[str, dict[str, int]]:
        """Return metadata for files under ./outputs/ in the bound sandbox."""
        if self._executor is None or not sandbox_id:
            return {}
        try:
            res = await self._executor.exec(
                sandbox_id=sandbox_id,
                cmd=["python3", "-c", _OUTPUTS_SNAPSHOT_SCRIPT],
            )
        except Exception as exc:  # noqa: BLE001 - artifact scan is best-effort
            log.warning("outputs snapshot failed", sandbox_id=sandbox_id, error=str(exc))
            return {}
        if not res.ok:
            log.warning(
                "outputs snapshot command failed",
                sandbox_id=sandbox_id,
                error=(res.error or res.stderr or str(res.exit_code)),
            )
            return {}
        try:
            raw = json.loads(res.stdout or "{}")
        except json.JSONDecodeError:
            log.warning("outputs snapshot emitted invalid JSON", sandbox_id=sandbox_id)
            return {}
        if not isinstance(raw, dict):
            return {}
        out: dict[str, dict[str, int]] = {}
        for path, meta in raw.items():
            if not isinstance(path, str) or not path.startswith("outputs/"):
                continue
            if not isinstance(meta, dict):
                continue
            try:
                out[path] = {"size": int(meta["size"]), "mtime_ns": int(meta["mtime_ns"])}
            except (KeyError, TypeError, ValueError):
                continue
        return out

    async def _publish_output_artifacts(
        self,
        *,
        sandbox_id: str | None,
        user_id: str,
        session_id: str,
        before: dict[str, dict[str, int]],
    ):
        """Upload changed ./outputs files and emit one file event per artifact."""
        if self._executor is None or self._objstore is None or not sandbox_id:
            return
        after = await self._snapshot_outputs(sandbox_id)
        max_bytes = _artifact_max_bytes()
        changed = sorted(path for path, meta in after.items() if before.get(path) != meta)
        for path in changed:
            size = int(after[path].get("size", 0))
            filename = _sanitize_filename(posixpath.basename(path))
            if size > max_bytes:
                yield AgentEvent(
                    kind="error",
                    data={
                        "error": (
                            f"artifact {filename!r} is {size} bytes, "
                            f"larger than COCOLA_ARTIFACT_MAX_BYTES={max_bytes}"
                        )
                    },
                )
                continue
            try:
                data = await self._executor.read_bytes(sandbox_id=sandbox_id, path=path)
            except Exception as exc:  # noqa: BLE001 - per-file failure should not fail the turn
                yield AgentEvent(
                    kind="error",
                    data={"error": f"artifact {filename!r} could not be read: {exc}"},
                )
                continue
            artifact_id = str(uuid.uuid4())
            mime = mimetypes.guess_type(filename)[0] or "application/octet-stream"
            key = "artifacts/{}/{}/{}-{}".format(
                _sanitize_filename(user_id or "user"),
                _sanitize_filename(session_id or "session"),
                artifact_id,
                filename,
            )
            try:
                await asyncio.to_thread(self._objstore.put, key, data, mime)
            except Exception as exc:  # noqa: BLE001 - per-file failure should not fail the turn
                yield AgentEvent(
                    kind="error",
                    data={"error": f"artifact {filename!r} upload failed: {exc}"},
                )
                continue
            yield AgentEvent(
                kind="file",
                data={
                    "id": artifact_id,
                    "filename": filename,
                    "mime": mime,
                    "size": str(len(data)),
                    "object_key": key,
                },
            )

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
          ./uploads/ in the session cwd (/workspace, ADR-0008 T1b),
          resolved via `pwd` (provider-agnostic) with `mkdir -p uploads` in the
          same shell -- WriteFile (docker CopyToContainer) makes no parent dirs.
        - Local dev (no executor/sandbox): there is no bound sandbox to write
          into, so we write into a per-session HOST dir and hand its path back as
          the workspace cwd; an in-process provider's native Read/Bash would then
          resolve ./uploads/ against it. (With Route B decommissioned the default
          no-sandbox provider is EchoProvider, which makes no model calls.)

        Content is written binary-safe so images survive intact.
        """
        if not attachments:
            return "", None

        # Materialize any key-only (large) attachments by pulling their bytes
        # from the object store on the model's behalf (ADR-0017 P1a), so both
        # delivery routes below see a uniform "filename + bytes in hand" shape.
        resolved = await self._materialize_attachments(attachments)

        if self._executor is not None and sandbox_id:
            preamble = await self._provision_into_sandbox(sandbox_id, resolved)
            return preamble, None

        preamble, workspace = self._provision_onto_host(session_id, resolved)
        return preamble, workspace

    async def _materialize_attachments(self, attachments) -> list[_ResolvedAttachment]:
        """Resolve every attachment to (filename, bytes).

        Small files arrive with inline ``content``; large files arrive as an
        ``oss_key`` with empty content and are pulled via the object-store
        fetcher. get_object is a blocking SDK call, so it runs in a worker thread
        to avoid stalling the event loop. A missing fetcher or a fetch failure
        for a key-only file raises -- the caller turns it into a clean terminal
        `error` event rather than provisioning an empty file.
        """
        resolved: list[_ResolvedAttachment] = []
        for att in attachments:
            content = bytes(att.content)
            oss_key = getattr(att, "oss_key", "")
            if not content and oss_key:
                if self._objstore is None:
                    raise RuntimeError(
                        f"attachment {att.filename!r} is object-store only "
                        f"(key={oss_key}) but no object store is configured"
                    )
                content = await asyncio.to_thread(self._objstore.get, oss_key)
            resolved.append(_ResolvedAttachment(filename=att.filename, content=content))
        return resolved

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
