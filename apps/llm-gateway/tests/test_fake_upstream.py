from cocola_llm_gateway.types import ChatMessage, ChatParams, ChatRequest, StreamEventType
from cocola_llm_gateway.upstream.fake import FakeUpstream


def _req(text="hello world foo"):
    return ChatRequest(
        model="fake-1",
        messages=[ChatMessage(role="user", content=text)],
        params=ChatParams(),
    )


async def test_fake_emits_full_event_sequence():
    up = FakeUpstream(reply="echo: hello world foo", chunk_size=4)
    events = [ev async for ev in up.chat_stream(_req())]
    kinds = [e.type for e in events]
    assert kinds[0] is StreamEventType.MESSAGE_START
    assert kinds[-1] is StreamEventType.MESSAGE_STOP
    assert StreamEventType.CONTENT_DELTA in kinds
    assert StreamEventType.MESSAGE_DELTA in kinds


async def test_fake_default_reply_echoes_last_user():
    up = FakeUpstream()  # no reply -> derives "echo: <last user>"
    events = [ev async for ev in up.chat_stream(_req("ping"))]
    text = "".join(e.text for e in events if e.type is StreamEventType.CONTENT_DELTA)
    assert text == "echo: ping"


async def test_fake_usage_tokens_are_reported():
    up = FakeUpstream(reply="abcdef", chunk_size=2)
    start = None
    delta = None
    async for ev in up.chat_stream(_req("one two three")):
        if ev.type is StreamEventType.MESSAGE_START:
            start = ev
        elif ev.type is StreamEventType.MESSAGE_DELTA:
            delta = ev
    assert start.usage.prompt_tokens == 3  # word count
    assert delta.usage.completion_tokens == 3  # 6 chars / chunk 2
