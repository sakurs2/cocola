"""In-memory ledger for hermetic tests and single-process local runs.

Thread/task-safe via an asyncio lock. Not durable — process restart loses data,
which is fine for tests and the default zero-dependency dev boot.
"""
from __future__ import annotations

import asyncio
from collections import defaultdict

from cocola_llm_gateway.billing.ledger import Aggregate, UsageRecord


class MemoryLedger:
    def __init__(self, max_records: int = 10000):
        self._records: list[UsageRecord] = []
        self._max = max_records
        self._user_agg: dict[str, Aggregate] = defaultdict(Aggregate)
        self._session_agg: dict[str, Aggregate] = defaultdict(Aggregate)
        self._lock = asyncio.Lock()

    async def record(self, rec: UsageRecord) -> None:
        async with self._lock:
            self._records.append(rec)
            if len(self._records) > self._max:
                self._records = self._records[-self._max :]
            for agg in (self._user_agg[rec.user_id], self._session_agg[rec.session_id]):
                agg.calls += 1
                agg.prompt_tokens += rec.prompt_tokens
                agg.completion_tokens += rec.completion_tokens
                agg.cost_usd += rec.cost_usd

    async def recent(self, *, user_id: str = "", limit: int = 50) -> list[UsageRecord]:
        async with self._lock:
            items = self._records
            if user_id:
                items = [r for r in items if r.user_id == user_id]
            return list(reversed(items[-limit:]))

    async def aggregate_user(self, user_id: str) -> Aggregate:
        async with self._lock:
            a = self._user_agg.get(user_id, Aggregate())
            return Aggregate(a.calls, a.prompt_tokens, a.completion_tokens, a.cost_usd)

    async def aggregate_session(self, session_id: str) -> Aggregate:
        async with self._lock:
            a = self._session_agg.get(session_id, Aggregate())
            return Aggregate(a.calls, a.prompt_tokens, a.completion_tokens, a.cost_usd)

    async def aclose(self) -> None:  # pragma: no cover - nothing to release
        return None
