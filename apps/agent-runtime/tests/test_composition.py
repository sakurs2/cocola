"""Production composition-root invariants."""

import pytest
from cocola_agent_runtime.__main__ import (
    _build_mcp_catalog,
    _build_prompt_catalog,
    _build_provider,
    _build_session_map,
    _build_skill_catalog,
    _sandbox_provisioning,
)


def test_production_provider_requires_sandbox_executor():
    with pytest.raises(RuntimeError, match="COCOLA_SANDBOX_ADDR"):
        _build_provider(None, None)


def test_production_session_map_requires_postgres(monkeypatch):
    monkeypatch.delenv("COCOLA_PG_DSN", raising=False)
    with pytest.raises(RuntimeError, match="COCOLA_PG_DSN"):
        _build_session_map()


def test_sandbox_provisioning_never_bakes_a_static_auth_token(monkeypatch):
    monkeypatch.setenv("COCOLA_SANDBOX_IMAGE", "registry/runtime:v1")
    monkeypatch.setenv("COCOLA_SANDBOX_LLM_BASE_URL", "http://llm-gateway:8080")
    monkeypatch.setenv("COCOLA_SANDBOX_MODEL_ALIAS", "cocola-default")
    monkeypatch.setenv("COCOLA_SANDBOX_LLM_TOKEN", "legacy-shared-token")

    image, env = _sandbox_provisioning()

    assert image == "registry/runtime:v1"
    assert env == {
        "ANTHROPIC_BASE_URL": "http://llm-gateway:8080",
        "ANTHROPIC_MODEL": "cocola-default",
        "ANTHROPIC_SMALL_FAST_MODEL": "cocola-default",
        "COCOLA_LLM_BASE_URL": "http://llm-gateway:8080",
    }


@pytest.mark.parametrize(
    "missing",
    ["COCOLA_SANDBOX_IMAGE", "COCOLA_SANDBOX_LLM_BASE_URL", "COCOLA_SANDBOX_MODEL_ALIAS"],
)
def test_sandbox_provisioning_requires_complete_route(monkeypatch, missing):
    monkeypatch.setenv("COCOLA_SANDBOX_IMAGE", "registry/runtime:v1")
    monkeypatch.setenv("COCOLA_SANDBOX_LLM_BASE_URL", "http://llm-gateway:8080")
    monkeypatch.setenv("COCOLA_SANDBOX_MODEL_ALIAS", "cocola-default")
    monkeypatch.delenv(missing, raising=False)

    with pytest.raises(RuntimeError, match=missing):
        _sandbox_provisioning()


@pytest.mark.parametrize(
    "builder", [_build_skill_catalog, _build_mcp_catalog, _build_prompt_catalog]
)
def test_admin_catalogs_require_admin_url_and_key(monkeypatch, builder):
    monkeypatch.delenv("COCOLA_ADMIN_URL", raising=False)
    monkeypatch.delenv("COCOLA_ADMIN_KEY", raising=False)
    with pytest.raises(RuntimeError, match="COCOLA_ADMIN_URL"):
        builder()

    monkeypatch.setenv("COCOLA_ADMIN_URL", "http://admin-api:8090")
    with pytest.raises(RuntimeError, match="COCOLA_ADMIN_KEY"):
        builder()
