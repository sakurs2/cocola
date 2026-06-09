"""Production composition root: build a fully-wired app from env.

Separated from server.py so tests can build their own service/verifier with
fakes and call create_app() directly, while production goes through here.

Ledger selection:
  - COCOLA_LLM_REDIS_URL set  -> RedisLedger (durable aggregates)
  - otherwise                 -> MemoryLedger (graceful default; warns)

Quota store selection mirrors the ledger:
  - COCOLA_LLM_REDIS_URL set  -> RedisQuotaStore (shared, period-windowed)
  - otherwise                 -> MemoryQuotaStore
The enforcer is only attached when a quota layer is enabled (limit > 0); with no
caps configured the service skips quota work entirely.

Auth (M4): a Verifier is built from COCOLA_AUTH_* env. With no secret, auth is
disabled and every caller resolves to the dev identity (preserving zero-config
dev boots); set COCOLA_AUTH_SECRET to enforce signed tokens.
"""
from __future__ import annotations

import os

from cocola_common import get_logger
from cocola_llm_gateway.auth import Verifier
from cocola_llm_gateway.billing import MemoryLedger, RedisLedger
from cocola_llm_gateway.billing.ledger import Ledger
from cocola_llm_gateway.config import (
    auth_config_from_env,
    gateway_config_from_env,
    load_registry,
    quota_policy_from_env,
)
from cocola_llm_gateway.middleware import ResiliencePolicy
from cocola_llm_gateway.quota import Enforcer, MemoryQuotaStore, QuotaStore, RedisQuotaStore
from cocola_llm_gateway.quota.policy import QuotaPolicy
from cocola_llm_gateway.service import GatewayService

log = get_logger("cocola.llm-gateway.bootstrap")


def build_ledger() -> Ledger:
    url = os.getenv("COCOLA_LLM_REDIS_URL", "").strip()
    if url:
        log.info("billing ledger: redis", url=url)
        return RedisLedger.from_url(url)
    log.info("billing ledger: in-memory (set COCOLA_LLM_REDIS_URL for durable billing)")
    return MemoryLedger()


def build_quota_store() -> QuotaStore:
    url = os.getenv("COCOLA_LLM_REDIS_URL", "").strip()
    if url:
        log.info("quota store: redis", url=url)
        return RedisQuotaStore.from_url(url)
    log.info("quota store: in-memory")
    return MemoryQuotaStore()


def build_enforcer(policy: QuotaPolicy | None = None) -> Enforcer | None:
    policy = policy or quota_policy_from_env()
    if not policy.any_enabled:
        log.info("quota: disabled (no token caps configured)")
        return None
    log.info(
        "quota: enabled",
        user_daily=policy.user_daily_tokens,
        tenant_monthly=policy.tenant_monthly_tokens,
    )
    return Enforcer(policy, build_quota_store())


def build_service() -> GatewayService:
    registry = load_registry()
    gcfg = gateway_config_from_env()
    policy = ResiliencePolicy(
        timeout_s=gcfg.request_timeout_s,
        max_retries=gcfg.max_retries,
        rate_limit_rps=gcfg.rate_limit_rps,
    )
    ledger = build_ledger()
    enforcer = build_enforcer()
    return GatewayService(registry, ledger, policy, enforcer)


def build_verifier() -> Verifier:
    cfg = auth_config_from_env()
    if cfg.enabled:
        log.info("auth: enabled", issuer=cfg.issuer, dev_anon=cfg.dev_allow_anonymous)
    else:
        log.warning("auth: DISABLED (no COCOLA_AUTH_SECRET) — all callers are the dev identity")
    return Verifier(cfg)


def build_app():
    """Build the production ASGI app (service + verifier wired from env)."""
    from cocola_llm_gateway.server import create_app

    return create_app(build_service(), verifier=build_verifier())
