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
from cocola_llm_gateway.quota.overrides import OverrideStore
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
    # Optional per-subject overrides (admin-api source of truth). When present,
    # an override supersedes the static policy cap for that user_id/tenant_id;
    # absent (None) -> the policy default applies. See ADR-0006 addendum.
    overrides: OverrideStore | None = None

    async def _limit_for(self, scope: str, subject: str, default: int) -> int:
        """Resolve the effective cap: a per-subject override, else the default.

        None from the override store means "no override, use the default". An
        override of 0 means "explicitly unlimited for this subject" (mirrors the
        admin-api QuotaOverride and the policy's limit <= 0 == unlimited).
        """
        if self.overrides is None or not subject:
            return default
        ov = await self.overrides.get(scope, subject)
        return default if ov is None else ov

    @property
    def _maybe_active(self) -> bool:
        """Fast skip: with no static caps AND no override source, do nothing.

        When overrides are wired we can't skip on the policy alone — an override
        may enable a cap a subject the static policy leaves unlimited — so the
        per-subject limit is resolved in check/commit/status instead.
        """
        return self.policy.any_enabled or self.overrides is not None

    async def check(self, identity: Identity, *, now: float | None = None) -> None:
        """Raise QuotaExceeded if the caller is already over any enabled cap.

        The cap is the per-subject override when one exists, else the static
        policy default. A layer is enforced only when its *effective* limit is
        positive, so an override can both enable a cap where the default is
        unlimited and lift a cap (override 0) where the default is set.
        """
        if not self._maybe_active:
            return

        if identity.user_id:
            limit = await self._limit_for("user", identity.user_id, self.policy.user_daily_tokens)
            if limit > 0:
                period, _ = day_window(now)
                used = await self.store.get("user", identity.user_id, period)
                st = QuotaStatus("user", identity.user_id, period, used, limit)
                if st.exceeded:
                    raise QuotaExceeded(st)

        if identity.tenant_id:
            limit = await self._limit_for(
                "tenant", identity.tenant_id, self.policy.tenant_monthly_tokens
            )
            if limit > 0:
                period, _ = month_window(now)
                used = await self.store.get("tenant", identity.tenant_id, period)
                st = QuotaStatus("tenant", identity.tenant_id, period, used, limit)
                if st.exceeded:
                    raise QuotaExceeded(st)

    async def commit(self, identity: Identity, tokens: int, *, now: float | None = None) -> None:
        """Add `tokens` to the user (daily) and tenant (monthly) counters.

        Best-effort: never raises (billing-style failure isolation).
        """
        if tokens <= 0 or not self._maybe_active:
            return
        try:
            if identity.user_id:
                limit = await self._limit_for(
                    "user", identity.user_id, self.policy.user_daily_tokens
                )
                if limit > 0:
                    period, ttl = day_window(now)
                    await self.store.add("user", identity.user_id, period, tokens, ttl)
            if identity.tenant_id:
                limit = await self._limit_for(
                    "tenant", identity.tenant_id, self.policy.tenant_monthly_tokens
                )
                if limit > 0:
                    period, ttl = month_window(now)
                    await self.store.add("tenant", identity.tenant_id, period, tokens, ttl)
        except Exception as e:  # noqa: BLE001 - never break the response on a quota write
            log.warning("quota commit failed", error=repr(e), user=identity.user_id)

    async def status(self, identity: Identity, *, now: float | None = None) -> list[QuotaStatus]:
        """Read current standings for the ops/debug `/v1/quota` surface."""
        out: list[QuotaStatus] = []
        if identity.user_id:
            limit = await self._limit_for("user", identity.user_id, self.policy.user_daily_tokens)
            if limit > 0:
                period, _ = day_window(now)
                used = await self.store.get("user", identity.user_id, period)
                out.append(QuotaStatus("user", identity.user_id, period, used, limit))
        if identity.tenant_id:
            limit = await self._limit_for(
                "tenant", identity.tenant_id, self.policy.tenant_monthly_tokens
            )
            if limit > 0:
                period, _ = month_window(now)
                used = await self.store.get("tenant", identity.tenant_id, period)
                out.append(QuotaStatus("tenant", identity.tenant_id, period, used, limit))
        return out
