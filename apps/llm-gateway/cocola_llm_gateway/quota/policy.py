"""Quota policy + period windows.

This is an *internal-employee* token-budget system, not billing. There is no
money, no balance, no debiting — only "how many tokens has this subject used in
the current period, and is that under the configured cap". When over cap, the
next request is rejected (HTTP 429) until the window rolls over.

Two independent layers, either of which can be unlimited (limit <= 0):
  - per-user, daily      (the primary guardrail)
  - per-tenant, monthly  (optional team/department ceiling)

Windows are computed in UTC so a counter key is stable regardless of where the
gateway runs. The key embeds the period so rollover is automatic: a new day/
month simply reads a fresh, zero counter — no cron, no reset job.
"""
from __future__ import annotations

import time
from dataclasses import dataclass
from datetime import datetime, timezone


@dataclass(frozen=True, slots=True)
class QuotaPolicy:
    """Configured token caps. A limit of 0 (or negative) means unlimited."""

    user_daily_tokens: int = 0
    tenant_monthly_tokens: int = 0

    @property
    def user_enabled(self) -> bool:
        return self.user_daily_tokens > 0

    @property
    def tenant_enabled(self) -> bool:
        return self.tenant_monthly_tokens > 0

    @property
    def any_enabled(self) -> bool:
        return self.user_enabled or self.tenant_enabled


def day_window(now: float | None = None) -> tuple[str, int]:
    """Return (period_id, ttl_seconds) for the current UTC day.

    period_id like '20260609'. ttl is seconds remaining until the next UTC day
    (plus a small slack) so the counter key self-expires shortly after rollover.
    """
    t = time.time() if now is None else now
    dt = datetime.fromtimestamp(t, tz=timezone.utc)
    period = dt.strftime("%Y%m%d")
    end = dt.replace(hour=23, minute=59, second=59, microsecond=0)
    ttl = int(end.timestamp() - t) + 60
    return period, max(ttl, 60)


def month_window(now: float | None = None) -> tuple[str, int]:
    """Return (period_id, ttl_seconds) for the current UTC month.

    period_id like '202606'. ttl covers the rest of the month (approximated by
    counting to the first of next month) plus slack.
    """
    t = time.time() if now is None else now
    dt = datetime.fromtimestamp(t, tz=timezone.utc)
    period = dt.strftime("%Y%m")
    if dt.month == 12:
        nxt = dt.replace(year=dt.year + 1, month=1, day=1, hour=0, minute=0, second=0, microsecond=0)
    else:
        nxt = dt.replace(month=dt.month + 1, day=1, hour=0, minute=0, second=0, microsecond=0)
    ttl = int(nxt.timestamp() - t) + 60
    return period, max(ttl, 60)


@dataclass(frozen=True, slots=True)
class QuotaStatus:
    """Snapshot of one scope's quota standing (for reads / 429 responses)."""

    scope: str          # "user" | "tenant"
    subject: str        # user_id or tenant_id
    period: str         # window id, e.g. "20260609"
    used: int           # tokens used in this window
    limit: int          # configured cap (0 => unlimited)

    @property
    def unlimited(self) -> bool:
        return self.limit <= 0

    @property
    def remaining(self) -> int:
        if self.unlimited:
            return -1
        return max(0, self.limit - self.used)

    @property
    def exceeded(self) -> bool:
        return not self.unlimited and self.used >= self.limit
