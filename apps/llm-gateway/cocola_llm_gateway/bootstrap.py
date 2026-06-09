"""Production composition root: build a fully-wired GatewayService from env.

Separated from server.py so tests can build their own service with fakes and
call create_app() directly, while production goes through here.

Ledger selection:
  - COCOLA_LLM_REDIS_URL set  -> RedisLedger (durable aggregates)
  - otherwise                 -> MemoryLedger (graceful default; warns)
If Redis is configured but unreachable at first use, the gateway still serves
requests; only billing persistence is affected (logged).
"""
from __future__ import annotations

import os

from cocola_common import get_logger
from cocola_llm_gateway.billing import MemoryLedger, RedisLedger
from cocola_llm_gateway.billing.ledger import Ledger
from cocola_llm_gateway.config import gateway_config_from_env, load_registry
from cocola_llm_gateway.middleware import ResiliencePolicy
from cocola_llm_gateway.service import GatewayService

log = get_logger("cocola.llm-gateway.bootstrap")


def build_ledger() -> Ledger:
    url = os.getenv("COCOLA_LLM_REDIS_URL", "").strip()
    if url:
        log.info("billing ledger: redis", url=url)
        return RedisLedger.from_url(url)
    log.info("billing ledger: in-memory (set COCOLA_LLM_REDIS_URL for durable billing)")
    return MemoryLedger()


def build_service() -> GatewayService:
    registry = load_registry()
    gcfg = gateway_config_from_env()
    policy = ResiliencePolicy(
        timeout_s=gcfg.request_timeout_s,
        max_retries=gcfg.max_retries,
        rate_limit_rps=gcfg.rate_limit_rps,
    )
    ledger = build_ledger()
    return GatewayService(registry, ledger, policy)
