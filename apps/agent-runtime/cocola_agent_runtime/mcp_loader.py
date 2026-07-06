"""MCP catalog loader: fetch user-effective MCP server config from admin-api."""

from __future__ import annotations

import json
import urllib.parse
import urllib.request
from collections.abc import Callable
from typing import Protocol

from cocola_common import get_logger

log = get_logger("cocola.agent-runtime.mcps")

MCPServers = dict[str, dict]
Fetcher = Callable[[str, dict[str, str], float], bytes]


class MCPCatalog(Protocol):
    def effective_mcp_servers(self, user_id: str = "") -> MCPServers:
        """Return Claude SDK mcp_servers config for a user."""
        ...


def _urllib_fetch(url: str, headers: dict[str, str], timeout: float) -> bytes:
    req = urllib.request.Request(url, headers=headers, method="GET")
    with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310 - trusted internal URL
        return resp.read()


class AdminMCPCatalog:
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

    def effective_mcp_servers(self, user_id: str = "") -> MCPServers:
        user_id = (user_id or "").strip()
        if not user_id:
            return {}
        url = self._base + "/admin/mcps/effective?user_id=" + urllib.parse.quote(user_id)
        headers = {"Accept": "application/json"}
        if self._admin_key:
            headers["Authorization"] = "Bearer " + self._admin_key
        try:
            raw = self._fetch(url, headers, self._timeout)
            payload = json.loads(raw)
        except Exception as exc:  # noqa: BLE001 - MCP config is optional per turn
            log.warning("mcp catalog fetch failed; running with no MCP servers", error=str(exc))
            return {}
        servers = payload.get("mcp_servers") or {}
        if not isinstance(servers, dict):
            return {}
        effective = {
            str(name): cfg
            for name, cfg in servers.items()
            if isinstance(name, str) and isinstance(cfg, dict)
        }
        log.info(
            "mcp catalog loaded",
            user_id=user_id,
            mcp_count=len(effective),
            mcp_names=sorted(effective),
        )
        return effective


class StaticMCPCatalog:
    def __init__(self, servers: MCPServers | None = None) -> None:
        self._servers = dict(servers or {})

    def effective_mcp_servers(self, user_id: str = "") -> MCPServers:
        return dict(self._servers)
