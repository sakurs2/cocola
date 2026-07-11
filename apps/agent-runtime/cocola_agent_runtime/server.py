"""gRPC server: exposes AgentProvider over the AgentRuntimeService contract.

This is the runtime\'s network edge. The gateway (BFF) is the only caller; it
opens a server-streaming `Query` RPC and forwards each `AgentEvent` to the web
client. Everything below the wire is the layering the package docstring fixes:

    grpc server (here)  ->  AgentProvider (Protocol)  ->  concrete provider
                                                   ->  SkillCatalog / MCPCatalog

Design choices, all to avoid reinventing what we already have:

- The servicer depends ONLY on the `AgentProvider` Protocol and the
  `SkillCatalog` Protocol, never on a concrete provider. Production injects
  `InSandboxShimProvider` (Route A) + admin-api catalogs; tests inject fakes.
  This is the same composition-root pattern the rest of the runtime uses.
- The generic `AgentEvent` dataclass the provider yields maps 1:1 onto the proto
  `AgentEvent` (a `kind` string + a flat `map<string,string>` of data). Non-string
  payloads (tool input dicts, costs) are JSON/str-encoded into the map so the
  schema stays flat and consumers can tolerate unknown kinds, exactly as the
  proto comment requires.
- Enabled Skill-Market skills are folded into the session via
  `apply_skills_to_options` before the provider runs, so toggling a skill in the
  control plane changes the agent with no redeploy.
- Enabled MCP servers are passed through to the in-sandbox Claude SDK via
  `mcp_servers`; the old host-side MCP forwarding seam remains deleted.
"""

from __future__ import annotations

import asyncio
import dataclasses
import io
import json
import mimetypes
import os
import pathlib
import posixpath
import re
import tempfile
import time
import uuid
import zipfile
from typing import Any, NamedTuple

from cocola.agent.v1 import agent_pb2 as pb
from cocola.agent.v1 import agent_pb2_grpc as pb_grpc
from cocola_common import get_logger

from cocola_agent_runtime.agent_provider import AgentEvent, AgentOptions, AgentProvider
from cocola_agent_runtime.checkpoint import CheckpointManager
from cocola_agent_runtime.mcp_loader import MCPCatalog
from cocola_agent_runtime.objstore import Fetcher
from cocola_agent_runtime.prompt_loader import PromptCatalog, PromptConfig
from cocola_agent_runtime.sandbox_binder import SandboxBinder, SandboxExecutor
from cocola_agent_runtime.session_map import SessionMap
from cocola_agent_runtime.skill_loader import Skill, SkillCatalog, skills_system_preamble

log = get_logger("cocola.agent-runtime.server")

ARTIFACT_SYSTEM_PROMPT = (
    "When you create files that the user should download or preview, save them "
    "under ./outputs/. Only files in ./outputs/ are published to the user."
)
ADMIN_SYSTEM_PROMPT_HEADER = "Administrator-configured system instructions:"
CURRENT_RUNTIME = os.getenv("COCOLA_AGENT_RUNTIME", "claude-code")
MODEL_ALIAS_METADATA_KEY = "x-cocola-model-alias"
# Per-user sandbox token forwarded by the gateway (gRPC metadata seam, no
# proto change). Injected into the sandbox as ANTHROPIC_AUTH_TOKEN per turn so
# the in-sandbox brain calls the llm-gateway as the real user. Never logged.
SANDBOX_TOKEN_METADATA_KEY = "x-cocola-sandbox-token"
TRACEPARENT_METADATA_KEY = "traceparent"
PRODUCT_TRACEPARENT_METADATA_KEY = "x-cocola-product-traceparent"
ENVIRONMENT_PREPARATION_SCHEMA_VERSION = 1
ENVIRONMENT_PREPARATION_PART_ID = "environment"

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
_SKILLS_BATCH_INSTALL_SCRIPT = r"""
import io
import json
import os
import shutil
import sys
import tempfile
import zipfile

archive_path, base, shared_root = sys.argv[1:4]
skills_root = os.path.join(base, "skills")
state_root = os.path.join(base, ".cocola")
os.makedirs(skills_root, exist_ok=True)
os.makedirs(state_root, exist_ok=True)
stage_root = tempfile.mkdtemp(prefix="skill-sync-", dir=state_root)


def remove_path(path):
    if not os.path.lexists(path):
        return
    if os.path.islink(path) or os.path.isfile(path):
        os.unlink(path)
    else:
        shutil.rmtree(path)


def extract_bundle(data, target):
    os.makedirs(target, exist_ok=True)
    with zipfile.ZipFile(io.BytesIO(data)) as bundle:
        for info in bundle.infolist():
            name = info.filename.replace("\\", "/")
            if not name or name.startswith("/") or ".." in name.split("/"):
                raise SystemExit(f"unsafe skill archive path: {name}")
            if info.is_dir():
                continue
            dest = os.path.join(target, name)
            os.makedirs(os.path.dirname(dest), exist_ok=True)
            with bundle.open(info) as src, open(dest, "wb") as out:
                shutil.copyfileobj(src, out)


try:
    with zipfile.ZipFile(archive_path) as batch:
        manifest = json.loads(batch.read("manifest.json"))
        if not isinstance(manifest, list):
            raise SystemExit("invalid skill batch manifest")

        # Validate and stage every local package before changing live targets.
        # A malformed bundle therefore cannot leave a partially-updated set.
        for item in manifest:
            if not isinstance(item, dict):
                raise SystemExit("invalid skill batch entry")
            skill_id = item.get("id", "")
            allowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-"
            if not skill_id or any(ch not in allowed for ch in skill_id):
                raise SystemExit(f"invalid skill id: {skill_id}")
            shared = os.path.join(shared_root, skill_id)
            if os.path.isfile(os.path.join(shared, "SKILL.md")):
                continue

            staged = os.path.join(stage_root, skill_id)
            kind = item.get("kind")
            if kind == "bundle":
                extract_bundle(batch.read(item["member"]), staged)
            elif kind == "markdown":
                os.makedirs(staged, exist_ok=True)
                with open(os.path.join(staged, "SKILL.md"), "wb") as out:
                    out.write(batch.read(item["member"]))
            elif kind == "empty":
                os.makedirs(staged, exist_ok=True)
            else:
                raise SystemExit(f"invalid skill payload kind: {kind}")
            if kind != "empty" and not os.path.isfile(os.path.join(staged, "SKILL.md")):
                raise SystemExit(f"skill archive missing SKILL.md: {skill_id}")

        for item in manifest:
            skill_id = item["id"]
            target = os.path.join(skills_root, skill_id)
            shared = os.path.join(shared_root, skill_id)
            remove_path(target)
            if os.path.isfile(os.path.join(shared, "SKILL.md")):
                os.symlink(shared, target)
            else:
                os.replace(os.path.join(stage_root, skill_id), target)
finally:
    shutil.rmtree(stage_root, ignore_errors=True)
    try:
        os.unlink(archive_path)
    except OSError:
        pass
"""
_SAFE_SKILL_ID_RE = re.compile(r"[^a-zA-Z0-9_.-]+")


async def _build_skill_batch_archive(skills: list[Skill], objstore: Fetcher | None) -> bytes:
    """Build one transport archive for every enabled non-shared skill payload.

    Shared-image skills are still represented in the manifest; the in-sandbox
    installer chooses the shared copy before reading their payload. Object-store
    reads run concurrently, while sandbox I/O remains one write plus one exec.
    """

    async def load_payload(skill: Skill) -> tuple[str, bytes]:
        if skill.bundle_object_key and objstore is not None:
            data = await asyncio.to_thread(objstore.get, skill.bundle_object_key)
            return "bundle", data
        if skill.skill_md:
            return "markdown", skill.skill_md.encode("utf-8")
        return "empty", b""

    skill_ids: list[str] = []
    seen_ids: set[str] = set()
    for skill in skills:
        skill_id = _SAFE_SKILL_ID_RE.sub("-", skill.id).strip(".-") or "skill"
        if skill_id in seen_ids:
            raise RuntimeError(f"duplicate normalized skill id: {skill_id}")
        seen_ids.add(skill_id)
        skill_ids.append(skill_id)

    payloads = await asyncio.gather(*(load_payload(skill) for skill in skills))
    manifest: list[dict[str, str]] = []
    out = io.BytesIO()
    with zipfile.ZipFile(out, "w", compression=zipfile.ZIP_DEFLATED) as batch:
        for index, (skill_id, payload) in enumerate(zip(skill_ids, payloads, strict=True)):
            kind, data = payload
            entry: dict[str, str] = {"id": skill_id, "kind": kind}
            if kind != "empty":
                suffix = "zip" if kind == "bundle" else "md"
                member = f"payloads/{index:04d}.{suffix}"
                batch.writestr(
                    member,
                    data,
                    compress_type=(
                        zipfile.ZIP_STORED if kind == "bundle" else zipfile.ZIP_DEFLATED
                    ),
                )
                entry["member"] = member
            manifest.append(entry)
        batch.writestr(
            "manifest.json",
            json.dumps(manifest, ensure_ascii=False, separators=(",", ":")),
        )
    return out.getvalue()


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


def trace_event(
    name: str,
    category: str,
    started_at_ns: int,
    *,
    status: str = "ok",
    **metadata: Any,
) -> AgentEvent:
    """Build an internal timing event for the gateway trace sink.

    The gateway records these events and intentionally does not forward them to
    the browser chat stream. Keep metadata operational and non-secret.
    """
    duration_ms = max((time.time_ns() - started_at_ns) // 1_000_000, 0)
    data: dict[str, Any] = {
        "name": name,
        "category": category,
        "service": "agent-runtime",
        "started_at_unix_ms": started_at_ns // 1_000_000,
        "duration_ms": duration_ms,
        "status": status or "ok",
    }
    data.update(metadata)
    return AgentEvent(kind="trace", data=data)


def environment_preparation_event(
    state: str,
    components: list[dict[str, Any]],
) -> AgentEvent:
    """Build one versioned, secret-free, full preparation snapshot."""
    snapshot = {
        "schema_version": ENVIRONMENT_PREPARATION_SCHEMA_VERSION,
        "part_id": ENVIRONMENT_PREPARATION_PART_ID,
        "state": state,
        "components": components,
    }
    return AgentEvent(
        kind="environment_prepare",
        data={"snapshot": json.dumps(snapshot, ensure_ascii=False, separators=(",", ":"))},
    )


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


def _metadata_value(context, key: str) -> str:
    for item in context.invocation_metadata() or ():
        if item.key.lower() == key.lower():
            return str(item.value).strip()
    return ""


def _product_traceparent(context) -> str:
    """Prefer Cocola's persisted parent over otelgrpc's transport span."""
    for key in (PRODUCT_TRACEPARENT_METADATA_KEY, TRACEPARENT_METADATA_KEY):
        value = _metadata_value(context, key).lower()
        if re.fullmatch(r"00-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}", value):
            return value
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


def _acquire_env(model_env: dict[str, str], sandbox_token: str) -> dict[str, str]:
    """Env passed to sandbox acquire: model alias plus, when present, the
    per-user token as ANTHROPIC_AUTH_TOKEN.

    On a COLD create this seeds the sandbox with the caller's token; on a WARM
    reuse the sandbox already has the baked static token, but the provider's
    per-turn exec env (shim_provider._model_env) re-injects this same token and
    is the authoritative override, so identity is correct either way.
    """
    env = dict(model_env)
    if sandbox_token:
        env["ANTHROPIC_AUTH_TOKEN"] = sandbox_token
    return env


class AgentRuntimeServicer(pb_grpc.AgentRuntimeServiceServicer):
    """Serves AgentRuntimeService.Query by driving an injected AgentProvider."""

    def __init__(
        self,
        provider: AgentProvider,
        *,
        skills: SkillCatalog | None = None,
        mcps: MCPCatalog | None = None,
        prompts: PromptCatalog | None = None,
        binder: SandboxBinder | None = None,
        executor: SandboxExecutor | None = None,
        objstore: Fetcher | None = None,
        session_map: SessionMap | None = None,
        checkpoint: CheckpointManager | None = None,
    ) -> None:
        self._provider = provider
        self._skills = skills
        self._mcps = mcps
        self._prompts = prompts
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
        self._checkpoint = checkpoint

    async def ReleaseSession(self, request, context):  # noqa: N802 - gRPC-generated name
        """Best-effort release of runtime state bound to a conversation."""
        session_id = request.session_id
        if not session_id:
            return pb.ReleaseSessionResponse()
        if self._checkpoint is not None and self._session_map is not None:
            try:
                binding = await self._session_map.get_binding(session_id)
                if binding and binding.sandbox_id:
                    await self._checkpoint.checkpoint_on_reclaim(
                        sandbox_id=binding.sandbox_id,
                        user_id=request.user_id,
                        session_id=session_id,
                    )
            except Exception as exc:  # noqa: BLE001 - release must continue
                log.warning(
                    "checkpoint before release failed",
                    session_id=session_id,
                    error=str(exc),
                )
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
        # Per-user sandbox token (P0 identity fix): forwarded by the gateway as
        # gRPC metadata. When present it is injected into the sandbox as
        # ANTHROPIC_AUTH_TOKEN per turn (creation-time exec env override), so the
        # in-sandbox brain authenticates to the llm-gateway AS THE USER instead
        # of via the static cluster-wide token baked at sandbox creation.
        sandbox_token = _metadata_value(context, SANDBOX_TOKEN_METADATA_KEY)
        traceparent = _product_traceparent(context)
        preparing_environment = False
        environment_components: list[dict[str, Any]] = []
        environment_degraded = False

        # Bind the session to a real sandbox when a binder is wired and the
        # caller did not pin one. Acquire is create-or-reuse (M2): the same
        # session converges on one sandbox and the call renews its lease. A bind
        # failure is surfaced as a terminal `error` event rather than crashing
        # the stream, and the agent does not run without its execution sandbox.
        if self._binder is not None and sandbox_id is None:
            acquire_start_ns = time.time_ns()
            try:
                box = await self._binder.acquire(
                    session_id=request.session_id,
                    user_id=request.user_id,
                    env=_acquire_env(model_env, sandbox_token),
                )
            except Exception as exc:  # noqa: BLE001 - bind failure -> clean terminal event
                log.warning(
                    "sandbox acquire failed",
                    session_id=request.session_id,
                    error=str(exc),
                )
                await context.write(
                    event_to_proto(
                        trace_event(
                            "sandbox.acquire",
                            "sandbox",
                            acquire_start_ns,
                            status="error",
                            session_id=request.session_id,
                            error_type=type(exc).__name__,
                        )
                    )
                )
                await context.write(
                    pb.AgentEvent(
                        kind="error",
                        data={"error": f"sandbox acquire failed: {exc}"},
                    )
                )
                return
            sandbox_id = box.id
            acquire_trace_name = "sandbox.reuse" if box.reused else "sandbox.create"
            await context.write(
                event_to_proto(
                    trace_event(
                        acquire_trace_name,
                        "sandbox",
                        acquire_start_ns,
                        sandbox_id=box.id,
                        endpoint=box.endpoint,
                        reused=box.reused,
                        session_id=request.session_id,
                    )
                )
            )
            if not box.reused:
                preparing_environment = True
                environment_components = [
                    {
                        "kind": "sandbox",
                        "status": "ready",
                        "label": "Workspace",
                        "summary": "Ready",
                    }
                ]
                await context.write(
                    event_to_proto(
                        environment_preparation_event("preparing", environment_components)
                    )
                )
            if self._checkpoint is not None:
                restore_start_ns = time.time_ns()
                restore = await self._checkpoint.restore_if_fresh(
                    sandbox_id=box.id,
                    session_id=request.session_id,
                    reused=box.reused,
                )
                await context.write(
                    event_to_proto(
                        trace_event(
                            "sandbox.checkpoint_restore",
                            "sandbox",
                            restore_start_ns,
                            status="error" if restore.degraded else "success",
                            sandbox_id=box.id,
                            reused=box.reused,
                            restored=restore.restored,
                            restore_status=restore.status,
                        )
                    )
                )
                if preparing_environment:
                    if restore.restored:
                        environment_components.append(
                            {
                                "kind": "checkpoint",
                                "status": "ready",
                                "label": "Session restore",
                                "summary": "Saved state restored",
                            }
                        )
                    elif restore.degraded:
                        environment_degraded = True
                        environment_components.append(
                            {
                                "kind": "checkpoint",
                                "status": "failed",
                                "label": "Session restore",
                                "summary": (
                                    "Saved session data is unavailable"
                                    if restore.status == "missing"
                                    else "Could not restore saved session data"
                                ),
                            }
                        )
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
            provision_start_ns = time.time_ns()
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
                if preparing_environment:
                    await context.write(
                        event_to_proto(
                            environment_preparation_event(
                                "degraded",
                                [
                                    *environment_components,
                                    {
                                        "kind": "attachments",
                                        "status": "failed",
                                        "label": "Attachments",
                                        "summary": "Could not prepare uploaded files",
                                    },
                                ],
                            )
                        )
                    )
                await context.write(
                    event_to_proto(
                        trace_event(
                            "sandbox.attachments_provision",
                            "sandbox",
                            provision_start_ns,
                            status="error",
                            sandbox_id=sandbox_id or "",
                            attachment_count=len(request.attachments),
                            error_type=type(exc).__name__,
                        )
                    )
                )
                await context.write(
                    pb.AgentEvent(
                        kind="error",
                        data={"error": f"attachment provisioning failed: {exc}"},
                    )
                )
                return
            await context.write(
                event_to_proto(
                    trace_event(
                        "sandbox.attachments_provision",
                        "sandbox",
                        provision_start_ns,
                        sandbox_id=sandbox_id or "",
                        attachment_count=len(request.attachments),
                        target="host" if workspace else "sandbox",
                    )
                )
            )
            if preamble:
                prompt = f"{preamble}\n\n{request.prompt}"
            if preparing_environment:
                environment_components.append(
                    {
                        "kind": "attachments",
                        "status": "ready",
                        "label": "Attachments",
                        "summary": f"{len(request.attachments)} prepared",
                        "count": len(request.attachments),
                    }
                )

        active_skills: list[Skill] = []
        loaded_skills: list[Skill] = []
        if self._skills is not None:
            skills_start_ns = time.time_ns()
            try:
                active_skills = self._skills.enabled_skills(request.user_id)
                if sandbox_id and active_skills and self._executor is not None:
                    await self._sync_skills_into_sandbox(sandbox_id, active_skills)
                    loaded_skills = list(active_skills)
                await context.write(
                    event_to_proto(
                        trace_event(
                            "sandbox.skills_sync",
                            "sandbox",
                            skills_start_ns,
                            sandbox_id=sandbox_id or "",
                            skill_count=len(active_skills),
                        )
                    )
                )
            except Exception as exc:  # noqa: BLE001 - skills degrade to prompt-less session
                active_skills = []
                if preparing_environment:
                    environment_degraded = True
                    environment_components.append(
                        {
                            "kind": "skills",
                            "status": "failed",
                            "label": "Skills",
                            "summary": "Could not load configured skills",
                        }
                    )
                log.warning("skill sync failed; running without skills", error=str(exc))
                await context.write(
                    event_to_proto(
                        trace_event(
                            "sandbox.skills_sync",
                            "sandbox",
                            skills_start_ns,
                            status="error",
                            sandbox_id=sandbox_id or "",
                            error_type=type(exc).__name__,
                        )
                    )
                )

        if preparing_environment:
            if loaded_skills:
                environment_components.append(
                    {
                        "kind": "skills",
                        "status": "ready",
                        "label": "Skills",
                        "summary": f"{len(loaded_skills)} loaded",
                        "count": len(loaded_skills),
                        "metadata": {
                            "items": [
                                {
                                    "id": skill.id,
                                    "label": skill.name or skill.id,
                                    "version": skill.version,
                                }
                                for skill in loaded_skills
                            ]
                        },
                    }
                )
            await context.write(
                event_to_proto(
                    environment_preparation_event(
                        "degraded" if environment_degraded else "ready",
                        environment_components,
                    )
                )
            )

        active_mcp_servers: dict[str, dict] = {}
        if self._mcps is not None:
            mcp_start_ns = time.time_ns()
            try:
                active_mcp_servers = self._mcps.effective_mcp_servers(request.user_id)
                active_mcp_names = sorted(active_mcp_servers)
                await context.write(
                    event_to_proto(
                        trace_event(
                            "sandbox.mcp_config_load",
                            "agent_init",
                            mcp_start_ns,
                            sandbox_id=sandbox_id or "",
                            mcp_count=len(active_mcp_servers),
                            mcp_names=active_mcp_names,
                        )
                    )
                )
            except Exception as exc:  # noqa: BLE001 - MCP config degrades to none
                active_mcp_servers = {}
                log.warning("mcp config load failed; running without MCP servers", error=str(exc))
                await context.write(
                    event_to_proto(
                        trace_event(
                            "sandbox.mcp_config_load",
                            "agent_init",
                            mcp_start_ns,
                            status="error",
                            sandbox_id=sandbox_id or "",
                            error_type=type(exc).__name__,
                        )
                    )
                )

        active_prompt = PromptConfig()
        if self._prompts is not None:
            prompt_start_ns = time.time_ns()
            try:
                active_prompt = self._prompts.effective_prompt(request.user_id)
                await context.write(
                    event_to_proto(
                        trace_event(
                            "agent.prompt_config_load",
                            "agent_init",
                            prompt_start_ns,
                            prompt_count=len(active_prompt.prompts),
                            prompt_ids=[p.id for p in active_prompt.prompts],
                            prompt_versions=[p.version for p in active_prompt.prompts],
                            content_length=len(active_prompt.system_prompt),
                        )
                    )
                )
            except Exception as exc:  # noqa: BLE001 - prompt policy degrades to default
                active_prompt = PromptConfig()
                log.warning(
                    "agent prompt load failed; running without admin prompt", error=str(exc)
                )
                await context.write(
                    event_to_proto(
                        trace_event(
                            "agent.prompt_config_load",
                            "agent_init",
                            prompt_start_ns,
                            status="error",
                            error_type=type(exc).__name__,
                        )
                    )
                )

        opts = AgentOptions(
            user_id=request.user_id,
            session_id=request.session_id,
            sandbox_id=sandbox_id,
            workspace=workspace,
            max_turns=request.max_turns or 30,
            model_alias=model_alias,
            mcp_servers=active_mcp_servers,
            environment_skills=[
                {
                    "id": skill.id,
                    "name": skill.name or skill.id,
                    "version": skill.version,
                }
                for skill in loaded_skills
            ],
            auth_token=sandbox_token or None,
            traceparent=traceparent or None,
        )
        skills_preamble = skills_system_preamble(active_skills)
        if skills_preamble:
            opts = dataclasses.replace(
                opts,
                system_prompt=_merge_system_prompt(opts.system_prompt, skills_preamble),
            )
        admin_prompt = active_prompt.system_prompt.strip()
        if admin_prompt:
            opts = dataclasses.replace(
                opts,
                system_prompt=_merge_system_prompt(
                    opts.system_prompt,
                    f"{ADMIN_SYSTEM_PROMPT_HEADER}\n{admin_prompt}",
                ),
            )
        artifacts_enabled = bool(self._objstore is not None and hasattr(self._objstore, "put"))
        if artifacts_enabled:
            opts = dataclasses.replace(
                opts,
                system_prompt=_merge_system_prompt(opts.system_prompt, ARTIFACT_SYSTEM_PROMPT),
            )

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
            if terminal_done is not None:
                await context.write(event_to_proto(terminal_done))
        except Exception as exc:  # noqa: BLE001 - turn any provider fault into a clean terminal event
            log.warning("agent query failed", session_id=request.session_id, error=str(exc))
            await context.write(pb.AgentEvent(kind="error", data={"error": str(exc)}))

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

    async def _sync_skills_into_sandbox(self, sandbox_id: str, skills: list[Skill]) -> None:
        if self._executor is None or not skills:
            return
        data = await _build_skill_batch_archive(skills, self._objstore)
        archive = f"/tmp/cocola-skills-{uuid.uuid4().hex}.zip"
        await self._executor.write_bytes(sandbox_id=sandbox_id, path=archive, data=data)
        res = await self._executor.exec(
            sandbox_id=sandbox_id,
            cmd=[
                "python3",
                "-c",
                _SKILLS_BATCH_INSTALL_SCRIPT,
                archive,
                "/home/cocola/.claude",
                "/data/plugins/skills",
            ],
            timeout_secs=max(30, min(300, len(skills) * 10)),
        )
        if not res.ok:
            raise RuntimeError(res.error or res.stderr or "batch skill sync failed")

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
