"""Shared fixtures: a gateway wired to FakeUpstream + MemoryLedger.

Everything here is hermetic — no real model, no Redis, no bound port. HTTP is
exercised via httpx.ASGITransport (in-process).
"""
import pytest

from cocola_llm_gateway.billing.memory import MemoryLedger
from cocola_llm_gateway.middleware import ResiliencePolicy
from cocola_llm_gateway.registry import ModelRoute, Pricing, Registry
from cocola_llm_gateway.service import GatewayService
from cocola_llm_gateway.upstream.fake import FakeUpstream


def build_service(reply="hello world from fake"):
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
        registry=reg, ledger=ledger, policy=ResiliencePolicy(timeout_s=10, max_retries=2)
    )
    return svc, ledger


@pytest.fixture
def service():
    svc, ledger = build_service()
    yield svc, ledger
