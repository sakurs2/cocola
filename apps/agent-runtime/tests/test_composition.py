"""Production composition-root invariants."""

import pytest
from cocola_agent_runtime.__main__ import _build_provider, _build_session_map


def test_production_provider_requires_sandbox_executor():
    with pytest.raises(RuntimeError, match="COCOLA_SANDBOX_ADDR"):
        _build_provider(None, None)


def test_production_session_map_requires_postgres(monkeypatch):
    monkeypatch.delenv("COCOLA_PG_DSN", raising=False)
    with pytest.raises(RuntimeError, match="COCOLA_PG_DSN"):
        _build_session_map()
