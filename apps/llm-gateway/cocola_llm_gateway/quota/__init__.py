"""Token quota: period-windowed usage caps for internal employees.

This is a *budget guardrail*, not billing — no money, no debiting. Counters roll
over automatically (the period is embedded in the key + a self-expiring TTL).

Public surface:
- QuotaPolicy   — configured caps (user daily, tenant monthly).
- QuotaStatus   — a scope's current standing (used/limit/remaining/exceeded).
- QuotaStore    — counter storage Protocol (Memory + Redis impls).
- Enforcer      — check() pre-call gate + commit() post-call counter add.
- QuotaExceeded — raised by check() -> HTTP 429.

See ADR-0005.
"""
from cocola_llm_gateway.quota.enforcer import Enforcer, QuotaExceeded
from cocola_llm_gateway.quota.policy import QuotaPolicy, QuotaStatus, day_window, month_window
from cocola_llm_gateway.quota.store import MemoryQuotaStore, QuotaStore, RedisQuotaStore

__all__ = [
    "QuotaPolicy",
    "QuotaStatus",
    "QuotaStore",
    "MemoryQuotaStore",
    "RedisQuotaStore",
    "Enforcer",
    "QuotaExceeded",
    "day_window",
    "month_window",
]
