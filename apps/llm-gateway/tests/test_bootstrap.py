import pytest
from cocola_llm_gateway import bootstrap
from cocola_llm_gateway.billing.postgres_ledger import PostgresLedger
from cocola_llm_gateway.quota.postgres_store import MirroredQuotaStore


def test_production_stores_require_postgres_and_redis(monkeypatch):
    monkeypatch.delenv("COCOLA_PG_DSN", raising=False)
    monkeypatch.delenv("COCOLA_LLM_REDIS_URL", raising=False)

    with pytest.raises(RuntimeError, match="COCOLA_PG_DSN is required"):
        bootstrap.build_ledger()
    with pytest.raises(RuntimeError, match="COCOLA_PG_DSN is required"):
        bootstrap.build_quota_store()
    with pytest.raises(RuntimeError, match="COCOLA_LLM_REDIS_URL is required"):
        bootstrap.build_override_store()
    with pytest.raises(RuntimeError, match="COCOLA_PG_DSN is required"):
        bootstrap.build_service()


def test_production_stores_use_durable_backends(monkeypatch):
    monkeypatch.setenv("COCOLA_PG_DSN", "postgresql://user:pass@localhost/db")
    monkeypatch.setenv("COCOLA_LLM_REDIS_URL", "redis://localhost:6379/0")

    assert isinstance(bootstrap.build_ledger(), PostgresLedger)
    assert isinstance(bootstrap.build_quota_store(), MirroredQuotaStore)


def test_authenticated_revocation_requires_redis(monkeypatch):
    monkeypatch.setenv("COCOLA_AUTH_SECRET", "secret")
    monkeypatch.delenv("COCOLA_LLM_REDIS_URL", raising=False)

    with pytest.raises(RuntimeError, match="COCOLA_LLM_REDIS_URL is required"):
        bootstrap.build_revocation()
