"""Redis-backed ledger.

Key model (prefix `cocola:llm:`):
  detail:{request_id}      STRING  JSON UsageRecord (TTL: detail_ttl_s)
  recent                   LIST    request_ids, newest at head (LPUSH+LTRIM)
  recent:user:{user_id}    LIST    per-user recent ids (LPUSH+LTRIM)
  agg:user:{user_id}       HASH    calls/prompt/completion/cost (HINCRBY*)
  agg:session:{session_id} HASH    calls/prompt/completion/cost (HINCRBY*)

Detail records carry a TTL (raw rows are recoverable from the warehouse in M7);
aggregates are durable counters with no TTL — they are the cheap read path for
"how much has this user/session spent". A small Lua script makes the per-record
detail-write + recent-push + both aggregate bumps atomic so a crash can't leave
a half-counted call.

This mirrors the M2 go-common/redis approach: an async client wrapper, graceful
behavior on absence (the caller falls back to MemoryLedger if Redis is down).
"""
from __future__ import annotations

import json

from redis import asyncio as aioredis

from cocola_llm_gateway.billing.ledger import Aggregate, UsageRecord

_PREFIX = "cocola:llm:"
_RECENT_CAP = 1000  # keep the most recent N ids per list

# KEYS: detail, recent, recent_user, agg_user, agg_session
# ARGV: request_id, record_json, prompt_tokens, completion_tokens, cost_micro, detail_ttl, cap
_LUA_RECORD = """
redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[6])
redis.call('LPUSH', KEYS[2], ARGV[1])
redis.call('LTRIM', KEYS[2], 0, tonumber(ARGV[7]) - 1)
redis.call('LPUSH', KEYS[3], ARGV[1])
redis.call('LTRIM', KEYS[3], 0, tonumber(ARGV[7]) - 1)
for _, k in ipairs({KEYS[4], KEYS[5]}) do
  redis.call('HINCRBY', k, 'calls', 1)
  redis.call('HINCRBY', k, 'prompt_tokens', tonumber(ARGV[3]))
  redis.call('HINCRBY', k, 'completion_tokens', tonumber(ARGV[4]))
  redis.call('HINCRBY', k, 'cost_microusd', tonumber(ARGV[5]))
end
return 1
"""


def _agg_from_hash(h: dict) -> Aggregate:
    def gi(k: str) -> int:
        return int(h.get(k, 0) or 0)

    return Aggregate(
        calls=gi("calls"),
        prompt_tokens=gi("prompt_tokens"),
        completion_tokens=gi("completion_tokens"),
        # cost stored as integer micro-USD to keep HINCRBY integral; convert back.
        cost_usd=gi("cost_microusd") / 1_000_000.0,
    )


class RedisLedger:
    def __init__(self, client: "aioredis.Redis", *, detail_ttl_s: int = 7 * 24 * 3600):
        self._r = client
        self._ttl = detail_ttl_s
        self._record_sha: str | None = None

    @classmethod
    def from_url(cls, url: str, **kw) -> "RedisLedger":
        client = aioredis.from_url(url, encoding="utf-8", decode_responses=True)
        return cls(client, **kw)

    async def _eval_record(self, keys: list[str], argv: list) -> None:
        # Cache the script SHA; fall back to EVAL on NOSCRIPT.
        if self._record_sha is None:
            self._record_sha = await self._r.script_load(_LUA_RECORD)
        try:
            await self._r.evalsha(self._record_sha, len(keys), *keys, *argv)
        except aioredis.ResponseError:
            await self._r.eval(_LUA_RECORD, len(keys), *keys, *argv)

    async def record(self, rec: UsageRecord) -> None:
        keys = [
            f"{_PREFIX}detail:{rec.request_id}",
            f"{_PREFIX}recent",
            f"{_PREFIX}recent:user:{rec.user_id}",
            f"{_PREFIX}agg:user:{rec.user_id}",
            f"{_PREFIX}agg:session:{rec.session_id}",
        ]
        argv = [
            rec.request_id,
            json.dumps(rec.to_dict(), ensure_ascii=False),
            rec.prompt_tokens,
            rec.completion_tokens,
            int(round(rec.cost_usd * 1_000_000)),  # micro-USD
            self._ttl,
            _RECENT_CAP,
        ]
        await self._eval_record(keys, argv)

    async def recent(self, *, user_id: str = "", limit: int = 50) -> list[UsageRecord]:
        list_key = f"{_PREFIX}recent:user:{user_id}" if user_id else f"{_PREFIX}recent"
        ids = await self._r.lrange(list_key, 0, max(0, limit - 1))
        if not ids:
            return []
        detail_keys = [f"{_PREFIX}detail:{rid}" for rid in ids]
        raws = await self._r.mget(detail_keys)
        out: list[UsageRecord] = []
        for raw in raws:
            if not raw:
                continue  # expired detail; aggregates still hold the totals
            d = json.loads(raw)
            out.append(UsageRecord(**d))
        return out

    async def aggregate_user(self, user_id: str) -> Aggregate:
        h = await self._r.hgetall(f"{_PREFIX}agg:user:{user_id}")
        return _agg_from_hash(h)

    async def aggregate_session(self, session_id: str) -> Aggregate:
        h = await self._r.hgetall(f"{_PREFIX}agg:session:{session_id}")
        return _agg_from_hash(h)

    async def aclose(self) -> None:
        await self._r.aclose()
