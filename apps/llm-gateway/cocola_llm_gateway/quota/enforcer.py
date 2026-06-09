"""Quota enforcement: pre-call gate + post-call commit.

The two-phase shape mirrors how token usage actually becomes known:

  check(identity)  -- BEFORE the model call.
      Reads the current period counters for the user (daily) and tenant
      (monthly). If either is already at/over its cap, raise QuotaExceeded so the
      HTTP layer can return 429 *before* opening an upstream stream. Because the
      exact token cost of a request isn't known until it completes, we permit a
      request to start as long as the subject is still under cap, then the next
      one is blocked. Overshoot is therefore bounded by what is in flight: a
      single serial caller overshoots by at most one request's tokens, while N
      concurrent requests can all pass `check` before any `commit` lands and
      overshoot by up to ~N requests' worth. For an internal employee budget
      this is the right trade-off (no pre-hold, no debiting, simple and
      predictable); if tight per-request enforcement is ever needed, switch to
      an atomic check-and-reserve.

  commit(identity, tokens)  -- AFTER the call, from the metering hook.
      Atomically adds the real total tokens to both counters. Best-effort: a
      counter write failure must never break the user's response (logged, not
      raised), exactly like the billing ledger.

When the policy disables a layer (limit <= 0) that layer is skipped entirely —
no reads, no writes.
"""
from __future__ import annotations

from dataclasses import dataclass

from cocola_common import get_logger

from cocola_llm_gateway.auth.jwt import Identity
from cocola_llm_gateway.quota.policy import (
    QuotaPolicy,
    QuotaStatus,
    day_window,
    month_window,
)
from cocola_llm_gateway.quota.store import QuotaStore

log = get_logger("cocola.llm-gateway.quota")


class QuotaExceeded(Exception):
    """Raised by check() when a subject is at/over a configured cap."""

    def __init__(self, status: QuotaStatus):
        self.status = status
        scope = "daily user" if status.scope == "user" else "monthly tenant"
        super().__init__(
            f"{scope} token quota exceeded for '{status.subject}': "
            f"used {status.used} >= limit {status.limit} (period {status.period})"
        )


@dataclass
class Enforcer:
    policy: QuotaPolicy
    store: QuotaStore

    async def check(self, identity: Identity, *, now: float | None = None) -> None:
        """Raise QuotaExceeded if the caller is already over any enabled cap."""
        if not self.policy.any_enabled:
            return

        if self.policy.user_enabled and identity.user_id:
            period, _ = day_window(now)
            used = await self.store.get("user", identity.user_id, period)
            st = QuotaStatus("user", identity.user_id, period, used, self.policy.user_daily_tokens)
            if st.exceeded:
                raise QuotaExceeded(st)

        if self.policy.tenant_enabled and identity.tenant_id:
            period, _ = month_window(now)
            used = await self.store.get("tenant", identity.tenant_id, period)
            st = QuotaStatus(
                "tenant", identity.tenant_id, period, used, self.policy.tenant_monthly_tokens
            )
            if st.exceeded:
                raise QuotaExceeded(st)

    async def commit(self, identity: Identity, tokens: int, *, now: float | None = None) -> None:
        """Add `tokens` to the user (daily) and tenant (monthly) counters.

        Best-effort: never raises (billing-style failure isolation).
        """
        if tokens <= 0 or not self.policy.any_enabled:
            return
        try:
            if self.policy.user_enabled and identity.user_id:
                period, ttl = day_window(now)
                await self.store.add("user", identity.user_id, period, tokens, ttl)
            if self.policy.tenant_enabled and identity.tenant_id:
                period, ttl = month_window(now)
                await self.store.add("tenant", identity.tenant_id, period, tokens, ttl)
        except Exception as e:  # noqa: BLE001 - never break the response on a quota write
            log.warning("quota commit failed", error=repr(e), user=identity.user_id)

    async def status(self, identity: Identity, *, now: float | None = None) -> list[QuotaStatus]:
        """Read current standings for the ops/debug `/v1/quota` surface."""
        out: list[QuotaStatus] = []
        if self.policy.user_enabled and identity.user_id:
            period, _ = day_window(now)
            used = await self.store.get("user", identity.user_id, period)
            out.append(
                QuotaStatus("user", identity.user_id, period, used, self.policy.user_daily_tokens)
            )
        if self.policy.tenant_enabled and identity.tenant_id:
            period, _ = month_window(now)
            used = await self.store.get("tenant", identity.tenant_id, period)
            out.append(
                QuotaStatus(
                    "tenant", identity.tenant_id, period, used, self.policy.tenant_monthly_tokens
                )
            )
        return out
