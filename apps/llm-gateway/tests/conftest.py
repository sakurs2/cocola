"""Shared fixtures: a gateway wired to FakeUpstream + MemoryLedger.

Everything here is hermetic — no real model, no Redis, no bound port. HTTP is
exercised via httpx.ASGITransport (in-process).
"""
import pytest

from cocola_llm_gateway.auth import AuthConfig, Issuer, Verifier
from cocola_llm_gateway.billing.memory import MemoryLedger
from cocola_llm_gateway.middleware import ResiliencePolicy
from cocola_llm_gateway.quota import Enforcer, MemoryQuotaStore, QuotaPolicy
from cocola_llm_gateway.registry import ModelRoute, Pricing, Registry
from cocola_llm_gateway.service import GatewayService
from cocola_llm_gateway.upstream.fake import FakeUpstream


def build_service(reply="hello world from fake", *, enforcer=None):
    fake = FakeUpstream(reply=reply)
    routes = {
        "default": ModelRoute(
            alias="default",
            provider_name="fake",
            real_model="fake-1",
            pricing=Pricing(input_per_1k=0.003, output_per_1k=0.015),
        )
    }
    reg = Registry(providers={"fake": fake}, routes=routes, default_alias="default")
    ledger = MemoryLedger()
    svc = GatewayService(
        registry=reg,
        ledger=ledger,
        policy=ResiliencePolicy(timeout_s=10, max_retries=2),
        enforcer=enforcer,
    )
    return svc, ledger


def build_enforcer(*, user_daily=0, tenant_monthly=0):
    """A MemoryQuotaStore-backed enforcer. Returns (enforcer, store)."""
    pol = QuotaPolicy(user_daily_tokens=user_daily, tenant_monthly_tokens=tenant_monthly)
    store = MemoryQuotaStore()
    return Enforcer(pol, store), store


def auth_pair(secret="test-secret", **kw):
    """Return (Issuer, Verifier) sharing one AuthConfig."""
    cfg = AuthConfig(secret=secret, **kw)
    return Issuer(cfg), Verifier(cfg)


@pytest.fixture
def service():
    svc, ledger = build_service()
    yield svc, ledger
