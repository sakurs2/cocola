"""Restore session checkpoints created by sandbox-manager.

Sandbox Manager is the sole checkpoint writer because it owns reclaim, graceful
shutdown, and sandbox lifecycle. Agent Runtime only restores the latest object
before a fresh sandbox starts executing the next turn.
"""

from __future__ import annotations

import asyncio
import contextlib
import os
import uuid
from dataclasses import dataclass
from typing import Literal

from cocola_common import get_logger

from cocola_agent_runtime.objstore import Fetcher
from cocola_agent_runtime.sandbox_binder import SandboxExecutor
from cocola_agent_runtime.session_map import SessionMap

log = get_logger("cocola.agent-runtime.checkpoint")

DEFAULT_TIMEOUT_SECS = 60


@dataclass(frozen=True)
class CheckpointConfig:
    timeout_secs: int = DEFAULT_TIMEOUT_SECS

    @classmethod
    def from_env(cls) -> CheckpointConfig:
        return cls(
            timeout_secs=_env_int("COCOLA_SESSION_CHECKPOINT_TIMEOUT_SECS", DEFAULT_TIMEOUT_SECS),
        )


@dataclass(frozen=True)
class RestoreOutcome:
    status: Literal["skipped", "restored", "missing", "failed"]
    error: str = ""

    @property
    def restored(self) -> bool:
        return self.status == "restored"

    @property
    def degraded(self) -> bool:
        return self.status in {"missing", "failed"}


def _env_int(name: str, default: int) -> int:
    raw = os.getenv(name, "").strip()
    if not raw:
        return default
    try:
        value = int(raw)
    except ValueError:
        return default
    return value if value > 0 else default


def _sh_quote(value: str) -> str:
    return "'" + value.replace("'", "'\\''") + "'"


def _restore_command(archive_path: str) -> list[str]:
    """Unpack a checkpoint archive already written to ``archive_path`` on disk."""
    path_q = _sh_quote(archive_path)
    script = f"set -eu; zstd -d -c {path_q} | tar -C / -xf -; rm -f {path_q}"
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

    async def restore_if_fresh(
        self, *, sandbox_id: str, user_id: str, session_id: str, reused: bool
    ) -> RestoreOutcome:
        """Restore the latest checkpoint before running the agent in a fresh sandbox."""
        if reused or not self.enabled or not sandbox_id:
            return RestoreOutcome("skipped")
        assert self._executor is not None
        assert self._objstore is not None
        assert self._session_map is not None
        archive_path = _sandbox_tmp_path()
        try:
            binding = await self._session_map.get_binding(session_id, user_id=user_id)
            key = await self._session_map.get_checkpoint(session_id, user_id=user_id)
            if not key:
                if binding is None:
                    return RestoreOutcome("skipped")
                log.warning(
                    "checkpoint unavailable for existing session",
                    session_id=session_id,
                    sandbox_id=sandbox_id,
                )
                return RestoreOutcome("missing", "checkpoint object key is missing")
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
                return RestoreOutcome(
                    "failed", res.error or res.stderr or f"restore exited {res.exit_code}"
                )
            log.info("checkpoint restored", session_id=session_id, object_key=key)
            return RestoreOutcome("restored")
        except Exception as exc:  # noqa: BLE001 - restore is best-effort
            log.warning("checkpoint restore failed", session_id=session_id, error=str(exc))
            await self._best_effort_cleanup(sandbox_id, archive_path)
            return RestoreOutcome("failed", str(exc))

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
