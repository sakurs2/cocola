"""Product conversation tracing backed by Cocola's existing Postgres.

This deliberately does not depend on an OTLP backend: every agent run remains
queryable when optional OpenTelemetry export is disabled or sampled out.
"""

from __future__ import annotations

import asyncio
import json
import re
import secrets
from dataclasses import dataclass
from datetime import UTC, datetime

from psycopg_pool import AsyncConnectionPool

_TRACEPARENT = re.compile(r"^00-([0-9a-f]{32})-([0-9a-f]{16})-[0-9a-f]{2}$", re.IGNORECASE)


@dataclass(frozen=True)
class TraceContext:
    trace_id: str
    parent_span_id: str

    @classmethod
    def parse(cls, raw: str | None) -> TraceContext | None:
        match = _TRACEPARENT.fullmatch((raw or "").strip())
        if not match or set(match.group(1)) == {"0"} or set(match.group(2)) == {"0"}:
            return None
        return cls(match.group(1).lower(), match.group(2).lower())


class ConversationTraceStore:
    def __init__(self, dsn: str):
        self._pool = AsyncConnectionPool(dsn, open=False, kwargs={"autocommit": True})
        self._lock = asyncio.Lock()
        self._opened = False

    async def _ready(self) -> None:
        if self._opened:
            return
        async with self._lock:
            if not self._opened:
                await self._pool.open()
                self._opened = True

    async def record_model_call(
        self,
        context: TraceContext,
        *,
        started_at: datetime,
        duration_us: int,
        ttft_ms: int,
        status: str,
        model_alias: str,
        real_model: str,
        provider: str,
        input_tokens: int,
        output_tokens: int,
        error_code: str,
    ) -> None:
        await self._ready()
        attributes = json.dumps(
            {
                "model_alias": model_alias,
                "real_model": real_model,
                "provider": provider,
                "ttft_ms": max(ttft_ms, 0),
                "input_tokens": max(input_tokens, 0),
                "output_tokens": max(output_tokens, 0),
                "error_code": error_code[:80],
            },
            separators=(",", ":"),
        )
        span_id = secrets.token_hex(8)
        async with self._pool.connection() as conn:
            await conn.execute(
                """
                INSERT INTO conversation_trace_spans (
                    trace_id, span_id, parent_span_id, schema_version, service,
                    name, category, started_at, duration_us, status, attributes_json
                ) VALUES (%s,%s,%s,1,'llm-gateway','model.generate','model',%s,%s,%s,%s::jsonb)
                ON CONFLICT (trace_id, span_id) DO NOTHING
                """,
                (
                    context.trace_id,
                    span_id,
                    context.parent_span_id,
                    started_at,
                    max(duration_us, 0),
                    status,
                    attributes,
                ),
            )
            await conn.execute(
                """
                UPDATE conversation_runs SET
                    llm_call_count = llm_call_count + 1,
                    input_tokens = input_tokens + %s,
                    output_tokens = output_tokens + %s,
                    last_activity_at = now(),
                    updated_at = now()
                WHERE trace_id = %s
                """,
                (max(input_tokens, 0), max(output_tokens, 0), context.trace_id),
            )

    async def aclose(self) -> None:
        if self._opened:
            await self._pool.close()


def utc_now() -> datetime:
    return datetime.now(UTC)
