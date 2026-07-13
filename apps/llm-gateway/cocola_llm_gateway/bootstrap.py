"""Production composition root.

Tests inject explicit in-memory fakes into ``create_app``. The executable uses
this module and requires the durable Postgres + Redis topology prepared by
``make dev`` or the standalone ``cocola`` CLI; missing connection configuration
is a startup error rather than an implicit, restart-unsafe runtime mode.
"""

from __future__ import annotations

import os

from cocola_common import get_logger

from cocola_llm_gateway.auth import Verifier
from cocola_llm_gateway.auth.revocation import (
    RedisRevocationStore,
    RevocationStore,
    TTLCachedRevocation,
)
from cocola_llm_gateway.billing import PostgresLedger
from cocola_llm_gateway.billing.ledger import Ledger
from cocola_llm_gateway.config import (
    auth_config_from_env,
    gateway_config_from_env,
    quota_policy_from_env,
    read_secret_env,
)
from cocola_llm_gateway.conversation_trace import ConversationTraceStore
from cocola_llm_gateway.db_registry import PostgresRegistrySource
from cocola_llm_gateway.middleware import ResiliencePolicy
from cocola_llm_gateway.quota import (
    Enforcer,
    OverrideStore,
    QuotaStore,
    RedisOverrideStore,
    RedisQuotaStore,
    TTLCachedOverrides,
)
from cocola_llm_gateway.quota.policy import QuotaPolicy
from cocola_llm_gateway.quota.postgres_store import MirroredQuotaStore, PostgresQuotaStore
from cocola_llm_gateway.registry import Registry
from cocola_llm_gateway.service import GatewayService

log = get_logger("cocola.llm-gateway.bootstrap")


def _required_env(name: str) -> str:
    value = os.getenv(name, "").strip()
    if not value:
        raise RuntimeError(f"{name} is required")
    return value


def _required_secret(name: str) -> str:
    value = read_secret_env(name).strip()
    if not value:
        raise RuntimeError(f"{name} is required")
    return value


def build_ledger() -> Ledger:
    log.info("billing ledger: postgres")
    return PostgresLedger(_required_env("COCOLA_PG_DSN"))


def build_quota_store() -> QuotaStore:
    dsn = _required_env("COCOLA_PG_DSN")
    url = _required_env("COCOLA_LLM_REDIS_URL")
    log.info("quota store: postgres + redis mirror", url=url)
    return MirroredQuotaStore(RedisQuotaStore.from_url(url), PostgresQuotaStore(dsn))


def build_override_store() -> OverrideStore:
    """Build the per-subject quota override store.

    Admin API writes the shared Redis hash and the gateway reads it through a
    small TTL cache.
    """
    url = _required_env("COCOLA_LLM_REDIS_URL")
    log.info("quota overrides: redis", url=url)
    inner: OverrideStore = RedisOverrideStore.from_url(url)
    ttl = float(os.getenv("COCOLA_QUOTA_OVERRIDE_CACHE_TTL_SECS", "5"))
    return TTLCachedOverrides(inner, ttl_s=ttl)


def build_enforcer(policy: QuotaPolicy | None = None) -> Enforcer:
    policy = policy or quota_policy_from_env()
    overrides = build_override_store()
    # Overrides can cap a subject even when the environment default is unlimited,
    # so the production enforcer is always present.
    log.info(
        "quota: enabled",
        user_daily=policy.user_daily_tokens,
        tenant_monthly=policy.tenant_monthly_tokens,
        overrides=overrides is not None,
    )
    return Enforcer(policy, build_quota_store(), overrides=overrides)


def build_service() -> GatewayService:
    dsn = _required_env("COCOLA_PG_DSN")
    _required_env("COCOLA_LLM_REDIS_URL")
    registry = Registry({}, {}, "")
    registry_source = PostgresRegistrySource(
        dsn,
        registry,
        secret=_required_secret("COCOLA_MODEL_SECRET_KEY"),
        ttl_s=float(os.getenv("COCOLA_LLM_REGISTRY_CACHE_TTL_SECS", "2")),
    )
    gcfg = gateway_config_from_env()
    policy = ResiliencePolicy(
        timeout_s=gcfg.request_timeout_s,
        max_retries=gcfg.max_retries,
        rate_limit_rps=gcfg.rate_limit_rps,
    )
    ledger = build_ledger()
    enforcer = build_enforcer()
    trace_store = ConversationTraceStore(dsn)
    return GatewayService(
        registry,
        ledger,
        policy,
        enforcer,
        registry_source=registry_source,
        trace_store=trace_store,
    )


def build_verifier() -> Verifier:
    cfg = auth_config_from_env()
    if not cfg.enabled:
        raise RuntimeError("COCOLA_AUTH_SECRET is required")
    log.info("auth: enabled", issuer=cfg.issuer)
    return Verifier(cfg)


def build_revocation() -> RevocationStore:
    """Build the required shared revocation denylist."""
    _required_secret("COCOLA_AUTH_SECRET")
    url = _required_env("COCOLA_LLM_REDIS_URL")
    log.info("revocation: redis denylist", url=url)
    inner: RevocationStore = RedisRevocationStore.from_url(url)
    ttl = float(os.getenv("COCOLA_REVOCATION_CACHE_TTL_SECS", "5"))
    return TTLCachedRevocation(inner, ttl_s=ttl)
