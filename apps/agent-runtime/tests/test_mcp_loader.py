"""Hermetic tests for admin-api backed MCP catalog loading."""

from __future__ import annotations

import json

from cocola_agent_runtime.mcp_loader import AdminMCPCatalog, StaticMCPCatalog


def _fetcher(payload, calls=None):
    def fetch(url, headers, timeout):
        if calls is not None:
            calls.append((url, headers, timeout))
        return json.dumps(payload).encode()

    return fetch


def test_admin_mcp_catalog_fetches_effective_config_for_user():
    calls = []
    catalog = AdminMCPCatalog(
        "http://admin/",
        admin_key="k",
        fetcher=_fetcher(
            {
                "mcp_servers": {
                    "github": {
                        "type": "stdio",
                        "command": "npx",
                        "args": ["-y", "server"],
                        "env": {"GITHUB_TOKEN": "secret"},
                    }
                }
            },
            calls=calls,
        ),
    )

    servers = catalog.effective_mcp_servers("alice@example.com")

    assert servers["github"]["command"] == "npx"
    assert servers["github"]["env"]["GITHUB_TOKEN"] == "secret"
    assert calls[0][0] == "http://admin/admin/mcps/effective?user_id=alice%40example.com"
    assert calls[0][1]["Authorization"] == "Bearer k"


def test_admin_mcp_catalog_degrades_to_empty_on_failure():
    def boom(url, headers, timeout):
        raise RuntimeError("offline")

    assert AdminMCPCatalog("http://admin", fetcher=boom).effective_mcp_servers("u") == {}


def test_static_mcp_catalog_returns_copy():
    servers = {"x": {"type": "http", "url": "https://example.com"}}
    catalog = StaticMCPCatalog(servers)

    assert catalog.effective_mcp_servers("u") == servers
    assert catalog.effective_mcp_servers("u") is not catalog.effective_mcp_servers("u")
