"""Billing ledger contract + records.

The ledger is the accounting source of truth: one record per completed model
call, plus rolled-up aggregates per (user, session, model). M3 *records* cost
but never *charges* — there is no balance, hold, or rejection. Real debiting
arrives with the user/quota system in M4+.

The interface is storage-agnostic on purpose: tests use the in-memory impl, prod
uses Redis, and a future SQL/warehouse sink (M7) can implement the same
Protocol. Business logic (the metering hook) depends only on `Ledger`.
"""
from __future__ import annotations

import time
from dataclasses import asdict, dataclass, field
from typing import Protocol, runtime_checkable


@dataclass(slots=True)
class UsageRecord:
    """One billable model call."""

    request_id: str
    user_id: str
    session_id: str
    alias: str            # caller-facing model alias
    real_model: str       # resolved upstream model id
    provider: str         # upstream provider name
    prompt_tokens: int
    completion_tokens: int
    cost_usd: float
    ts_unix: float = field(default_factory=lambda: time.time())
    status: str = "ok"    # ok | error
    error: str = ""

    @property
    def total_tokens(self) -> int:
        return self.prompt_tokens + self.completion_tokens

    def to_dict(self) -> dict:
        return asdict(self)


@dataclass(slots=True)
class Aggregate:
    """Rolled-up totals for a grouping key (e.g. a user, or a session)."""

    calls: int = 0
    prompt_tokens: int = 0
    completion_tokens: int = 0
    cost_usd: float = 0.0

    @property
    def total_tokens(self) -> int:
        return self.prompt_tokens + self.completion_tokens


@runtime_checkable
class Ledger(Protocol):
    """Append-only usage sink with cheap aggregate reads."""

    async def record(self, rec: UsageRecord) -> None:
        """Persist one usage record and update aggregates atomically."""
        ...

    async def recent(self, *, user_id: str = "", limit: int = 50) -> list[UsageRecord]:
        """Return the most recent records, optionally filtered by user."""
        ...

    async def aggregate_user(self, user_id: str) -> Aggregate:
        """Return rolled-up totals for a user."""
        ...

    async def aggregate_session(self, session_id: str) -> Aggregate:
        """Return rolled-up totals for a session."""
        ...

    async def aclose(self) -> None:
        ...
