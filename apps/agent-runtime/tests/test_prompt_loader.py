"""Hermetic tests for admin-api backed agent prompt loading."""

from __future__ import annotations

import json

from cocola_agent_runtime.prompt_loader import AdminPromptCatalog, PromptMarker, StaticPromptCatalog


def _fetcher(payload, calls=None):
    def fetch(url, headers, timeout):
        if calls is not None:
            calls.append((url, headers, timeout))
        return json.dumps(payload).encode()

    return fetch


def test_admin_prompt_catalog_fetches_effective_prompt_for_user():
    calls = []
    catalog = AdminPromptCatalog(
        "http://admin/",
        admin_key="k",
        fetcher=_fetcher(
            {
                "system_prompt": "Prefer short answers.",
                "prompts": [{"id": "global", "version": 3, "content_length": 21}],
            },
            calls=calls,
        ),
    )

    config = catalog.effective_prompt("alice@example.com")

    assert config.system_prompt == "Prefer short answers."
    assert config.prompts == [PromptMarker(id="global", version=3, content_length=21)]
    assert calls[0][0] == "http://admin/admin/agent-prompts/effective?user_id=alice%40example.com"
    assert calls[0][1]["Authorization"] == "Bearer k"


def test_admin_prompt_catalog_degrades_to_empty_on_failure():
    def boom(url, headers, timeout):
        raise RuntimeError("offline")

    assert (
        AdminPromptCatalog("http://admin", fetcher=boom).effective_prompt("u").system_prompt == ""
    )


def test_static_prompt_catalog_returns_copy():
    marker = PromptMarker(id="global", version=1, content_length=4)
    catalog = StaticPromptCatalog("test", [marker])

    assert catalog.effective_prompt("u").system_prompt == "test"
    assert catalog.effective_prompt("u").prompts == [marker]
    assert catalog.effective_prompt("u").prompts is not catalog.effective_prompt("u").prompts
