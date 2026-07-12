"""Admin-managed system prompt loader."""

from __future__ import annotations

import json
import urllib.parse
import urllib.request
from collections.abc import Callable
from dataclasses import dataclass, field
from typing import Protocol

from cocola_common import get_logger

log = get_logger("cocola.agent-runtime.prompts")

Fetcher = Callable[[str, dict[str, str], float], bytes]


@dataclass(frozen=True)
class PromptMarker:
    id: str
    version: int = 0
    content_length: int = 0


@dataclass(frozen=True)
class PromptConfig:
    system_prompt: str = ""
    prompts: list[PromptMarker] = field(default_factory=list)


class PromptCatalog(Protocol):
    def effective_prompt(self, user_id: str = "") -> PromptConfig:
        """Return the system prompt policy effective for a user."""
        ...


def _urllib_fetch(url: str, headers: dict[str, str], timeout: float) -> bytes:
    req = urllib.request.Request(url, headers=headers, method="GET")
    with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310 - trusted internal URL
        return resp.read()


class AdminPromptCatalog:
    def __init__(
        self,
        base_url: str,
        *,
        admin_key: str = "",
        timeout_s: float = 3.0,
        fetcher: Fetcher | None = None,
    ) -> None:
        self._base = base_url.rstrip("/")
        self._admin_key = admin_key
        self._timeout = timeout_s
        self._fetch = fetcher or _urllib_fetch
        self._last_good: dict[str, PromptConfig] = {}

    def effective_prompt(self, user_id: str = "") -> PromptConfig:
        user_id = (user_id or "").strip()
        if not user_id:
            return PromptConfig()
        url = self._base + "/admin/agent-prompts/effective?user_id=" + urllib.parse.quote(user_id)
        headers = {"Accept": "application/json"}
        if self._admin_key:
            headers["Authorization"] = "Bearer " + self._admin_key
        try:
            raw = self._fetch(url, headers, self._timeout)
            payload = json.loads(raw)
        except Exception as exc:  # noqa: BLE001 - prompt policy should degrade safely
            cached = self._last_good.get(user_id)
            if cached is not None:
                log.warning(
                    "agent prompt fetch failed; using last-known-good policy", error=str(exc)
                )
                return PromptConfig(
                    system_prompt=cached.system_prompt,
                    prompts=list(cached.prompts),
                )
            raise RuntimeError("administrator prompt policy is unavailable") from exc
        system_prompt = str(payload.get("system_prompt") or "").strip()
        prompts = []
        for item in payload.get("prompts") or []:
            if not isinstance(item, dict):
                continue
            prompts.append(
                PromptMarker(
                    id=str(item.get("id") or ""),
                    version=int(item.get("version") or 0),
                    content_length=int(item.get("content_length") or 0),
                )
            )
        config = PromptConfig(system_prompt=system_prompt, prompts=[p for p in prompts if p.id])
        self._last_good[user_id] = config
        return PromptConfig(system_prompt=config.system_prompt, prompts=list(config.prompts))


class StaticPromptCatalog:
    def __init__(self, prompt: str = "", prompts: list[PromptMarker] | None = None) -> None:
        self._config = PromptConfig(system_prompt=prompt, prompts=list(prompts or []))

    def effective_prompt(self, user_id: str = "") -> PromptConfig:
        return PromptConfig(
            system_prompt=self._config.system_prompt,
            prompts=list(self._config.prompts),
        )
