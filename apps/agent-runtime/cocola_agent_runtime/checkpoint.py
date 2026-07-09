"""Best-effort session checkpointing around sandbox reclamation.

The runtime workspace stays on the sandbox backend's local storage. To make the
important conversational state portable across a later fresh sandbox, we archive
only a small allowlist of directories immediately before a controlled reclaim,
then restore the latest archive when a session cold-starts again.

Transport note: the archive bytes move between the sandbox and this process over
execd's binary-safe file channel (``write_bytes`` / ``read_bytes``), never the
command line. Piping a base64 blob through argv/stdin was fragile (length limits,
runuser stdin forwarding, 1.33x bloat) and was the root cause of restore failing
with an abnormal-termination exit code. Writing the archive to a temp file inside
the sandbox and unpacking it from disk mirrors how skills are synced in and how
output artifacts are read back -- one proven, binary-clean pattern everywhere.
"""

from __future__ import annotations

import asyncio
import contextlib
import os
import re
import uuid
from dataclasses import dataclass, field
from datetime import UTC, datetime

from cocola_common import get_logger

from cocola_agent_runtime.objstore import Fetcher
from cocola_agent_runtime.sandbox_binder import SandboxExecutor
from cocola_agent_runtime.session_map import SessionMap

log = get_logger("cocola.agent-runtime.checkpoint")

DEFAULT_CHECKPOINT_DIRS = (
    "/home/cocola/.claude",
    "/workspace/uploads",
    "/workspace/outputs",
    "/workspace/persist",
)
DEFAULT_TIMEOUT_SECS = 60
DEFAULT_MAX_BYTES = 256 * 1024 * 1024

# Marker the archive script prints so we can tell "nothing to archive" apart from
# "archived a file", without inspecting the (now file-resident) payload on stdout.
_ARCHIVED_MARKER = "archived"
_EMPTY_MARKER = "empty"


@dataclass(frozen=True)
class CheckpointConfig:
    dirs: tuple[str, ...] = field(default_factory=lambda: DEFAULT_CHECKPOINT_DIRS)
    timeout_secs: int = DEFAULT_TIMEOUT_SECS
    max_bytes: int = DEFAULT_MAX_BYTES

    @classmethod
    def from_env(cls) -> CheckpointConfig:
        return cls(
            dirs=_dirs_from_env(),
            timeout_secs=_env_int("COCOLA_SESSION_CHECKPOINT_TIMEOUT_SECS", DEFAULT_TIMEOUT_SECS),
            max_bytes=_env_int("COCOLA_SESSION_CHECKPOINT_MAX_BYTES", DEFAULT_MAX_BYTES),
        )


def _env_int(name: str, default: int) -> int:
    raw = os.getenv(name, "").strip()
    if not raw:
        return default
    try:
        value = int(raw)
    except ValueError:
        return default
    return value if value > 0 else default


def _dirs_from_env() -> tuple[str, ...]:
    raw = os.getenv("COCOLA_SESSION_CHECKPOINT_DIRS", "").strip()
    if not raw:
        return DEFAULT_CHECKPOINT_DIRS
    dirs = tuple(p.strip() for p in raw.split(",") if p.strip())
    return dirs or DEFAULT_CHECKPOINT_DIRS


def _safe_key_part(value: str, fallback: str) -> str:
    cleaned = re.sub(r"[^A-Za-z0-9_.-]+", "-", (value or "").strip()).strip("-")
    return cleaned or fallback


def _sh_quote(value: str) -> str:
    return "'" + value.replace("'", "'\\''") + "'"


def _archive_command(dirs: tuple[str, ...], out_path: str) -> list[str]:
    """Archive the allowlisted dirs into ``out_path`` *inside the sandbox*.

    Writes a zstd-compressed tar to a temp file on the sandbox filesystem and
    prints a short marker on stdout so the caller can distinguish "no dirs
    existed" from "archive written" -- the payload itself never touches stdout,
    so there is no base64 encode/bloat and no argv/stdin size ceiling.
    """
    rel_paths = [p.strip().lstrip("/") for p in dirs if p.strip()]
    quoted = " ".join(_sh_quote(p) for p in rel_paths)
    out_q = _sh_quote(out_path)
    script = (
        "set -eu; "
        "paths=''; "
        f"for p in {quoted}; do "
        '[ -e "/$p" ] && paths="$paths $p"; '
        "done; "
        f'if [ -z "$paths" ]; then printf %s {_sh_quote(_EMPTY_MARKER)}; exit 0; fi; '
        f"tar -C / -cf - $paths | zstd -q -c > {out_q}; "
        f"printf %s {_sh_quote(_ARCHIVED_MARKER)}"
    )
    return ["sh", "-lc", script]


def _restore_command(archive_path: str) -> list[str]:
    """Unpack a checkpoint archive already written to ``archive_path`` on disk."""
    path_q = _sh_quote(archive_path)
    script = (
        "set -eu; "
        f'zstd -d -c {path_q} | tar -C / -xf -; '
        f"rm -f {path_q}"
    )
    return ["sh", "-lc", script]


def _cleanup_command(archive_path: str) -> list[str]:
    return ["sh", "-lc", f"rm -f {_sh_quote(archive_path)}"]


def _sandbox_tmp_path() -> str:
    return f"/tmp/cocola-checkpoint-{uuid.uuid4().hex}.tar.zst"


class CheckpointManager:
    def __init__(
        self,
        *,
        objstore: Fetcher | None,
        executor: SandboxExecutor | None,
        session_map: SessionMap | None,
        config: CheckpointConfig | None = None,
    ) -> None:
        self._objstore = objstore
        self._executor = executor
        self._session_map = session_map
        self._config = config or CheckpointConfig.from_env()

    @property
    def enabled(self) -> bool:
        return (
            self._objstore is not None
            and self._executor is not None
            and self._session_map is not None
        )

    async def checkpoint_on_reclaim(
        self, *, sandbox_id: str, user_id: str, session_id: str
    ) -> str | None:
        """Archive configured dirs and record the resulting latest object key.

        Failures are logged and swallowed so resource reclamation never gets
        stuck behind persistence trouble.
        """
        if not self.enabled or not sandbox_id:
            return None
        assert self._executor is not None
        assert self._objstore is not None
        assert self._session_map is not None
        archive_path = _sandbox_tmp_path()
        try:
            res = await self._executor.exec(
                sandbox_id=sandbox_id,
                cmd=_archive_command(self._config.dirs, archive_path),
                timeout_secs=self._config.timeout_secs,
            )
            if not res.ok:
                await self._record_failure(
                    session_id, res.error or res.stderr or str(res.exit_code)
                )
                log.warning(
                    "checkpoint archive failed",
                    sandbox_id=sandbox_id,
                    session_id=session_id,
                    error=res.error or res.stderr or str(res.exit_code),
                )
                return None
            if res.stdout.strip() != _ARCHIVED_MARKER:
                log.info("checkpoint skipped: no configured dirs exist", session_id=session_id)
                return None
            data = await self._executor.read_bytes(sandbox_id=sandbox_id, path=archive_path)
            if self._config.max_bytes and len(data) > self._config.max_bytes:
                await self._record_failure(
                    session_id,
                    f"archive size {len(data)} exceeds max {self._config.max_bytes}",
                )
                log.warning(
                    "checkpoint archive exceeds max bytes",
                    session_id=session_id,
                    size=len(data),
                    max_bytes=self._config.max_bytes,
                )
                return None
            key = self._object_key(user_id=user_id, session_id=session_id)
            await asyncio.to_thread(self._objstore.put, key, data, "application/zstd")
            await self._session_map.put_checkpoint(session_id, key, size_bytes=len(data))
            log.info("checkpoint uploaded", session_id=session_id, object_key=key, size=len(data))
            return key
        except Exception as exc:  # noqa: BLE001 - reclaim must continue
            await self._record_failure(session_id, str(exc))
            log.warning("checkpoint failed", session_id=session_id, error=str(exc))
            return None
        finally:
            await self._best_effort_cleanup(sandbox_id, archive_path)

    async def restore_if_fresh(self, *, sandbox_id: str, session_id: str, reused: bool) -> bool:
        """Restore the latest checkpoint before running the agent in a fresh sandbox."""
        if reused or not self.enabled or not sandbox_id:
            return False
        assert self._executor is not None
        assert self._objstore is not None
        assert self._session_map is not None
        archive_path = _sandbox_tmp_path()
        try:
            key = await self._session_map.get_checkpoint(session_id)
            if not key:
                return False
            data = await asyncio.to_thread(self._objstore.get, key)
            await self._executor.write_bytes(sandbox_id=sandbox_id, path=archive_path, data=data)
            res = await self._executor.exec(
                sandbox_id=sandbox_id,
                cmd=_restore_command(archive_path),
                timeout_secs=self._config.timeout_secs,
            )
            if not res.ok:
                log.warning(
                    "checkpoint restore failed",
                    sandbox_id=sandbox_id,
                    session_id=session_id,
                    object_key=key,
                    error=res.error or res.stderr or str(res.exit_code),
                )
                await self._best_effort_cleanup(sandbox_id, archive_path)
                return False
            log.info("checkpoint restored", session_id=session_id, object_key=key)
            return True
        except Exception as exc:  # noqa: BLE001 - restore is best-effort
            log.warning("checkpoint restore failed", session_id=session_id, error=str(exc))
            await self._best_effort_cleanup(sandbox_id, archive_path)
            return False

    async def _best_effort_cleanup(self, sandbox_id: str, archive_path: str) -> None:
        if self._executor is None:
            return
        # cleanup is advisory; the sandbox is transient, so any failure is moot
        with contextlib.suppress(Exception):
            await self._executor.exec(
                sandbox_id=sandbox_id,
                cmd=_cleanup_command(archive_path),
                timeout_secs=10,
            )

    def _object_key(self, *, user_id: str, session_id: str) -> str:
        ts = datetime.now(UTC).strftime("%Y%m%dT%H%M%SZ")
        return "checkpoints/{}/{}/{}-{}.tar.zst".format(
            _safe_key_part(user_id, "user"),
            _safe_key_part(session_id, "session"),
            ts,
            uuid.uuid4(),
        )

    async def _record_failure(self, session_id: str, error: str) -> None:
        if self._session_map is None:
            return
        try:
            await self._session_map.put_checkpoint_failure(session_id, error)
        except Exception as exc:  # noqa: BLE001 - avoid masking the original failure
            log.warning(
                "checkpoint failure metadata update failed",
                session_id=session_id,
                error=str(exc),
            )
