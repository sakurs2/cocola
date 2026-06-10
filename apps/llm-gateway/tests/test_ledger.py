import pytest
from cocola_llm_gateway.billing.ledger import UsageRecord
from cocola_llm_gateway.billing.memory import MemoryLedger


def _rec(user="U1", session="S1", p=3, c=6, cost=0.0045):
    return UsageRecord(
        request_id="r1",
        user_id=user,
        session_id=session,
        alias="default",
        real_model="fake-1",
        provider="fake",
        prompt_tokens=p,
        completion_tokens=c,
        cost_usd=cost,
    )


async def test_memory_ledger_records_and_aggregates():
    led = MemoryLedger()
    await led.record(_rec())
    await led.record(_rec(p=1, c=2, cost=0.001))
    recent = await led.recent(user_id="U1", limit=10)
    assert len(recent) == 2
    agg = await led.aggregate_user("U1")
    assert agg.calls == 2
    assert agg.prompt_tokens == 4
    assert agg.completion_tokens == 8
    assert agg.cost_usd == pytest.approx(0.0055)


async def test_memory_ledger_filters_by_user():
    led = MemoryLedger()
    await led.record(_rec(user="U1"))
    await led.record(_rec(user="U2"))
    assert len(await led.recent(user_id="U1")) == 1
    assert len(await led.recent(user_id="U2")) == 1
    assert len(await led.recent()) == 2


async def test_session_aggregate():
    led = MemoryLedger()
    await led.record(_rec(session="SX", p=5, c=5, cost=0.01))
    a = await led.aggregate_session("SX")
    assert a.calls == 1
    assert a.total_tokens == 10
