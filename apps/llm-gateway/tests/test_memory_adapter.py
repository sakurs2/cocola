"""Hermetic contract tests for the OpenViking-only internal adapters."""

from __future__ import annotations

from collections.abc import AsyncIterator

import httpx
import pytest
from cocola_llm_gateway.billing.memory import MemoryLedger
from cocola_llm_gateway.middleware import ResiliencePolicy
from cocola_llm_gateway.registry import ModelRoute, Registry
from cocola_llm_gateway.server import create_app
from cocola_llm_gateway.service import GatewayService
from cocola_llm_gateway.types import StreamEvent, StreamEventType, Usage
from cocola_llm_gateway.upstream.fake import FakeUpstream


class RecordingChatProvider(FakeUpstream):
    def __init__(self, reply: str = '{"memory":true}'):
        super().__init__(reply=reply)
        self.requests = []

    async def chat_stream(self, request):
        self.requests.append(request)
        async for event in super().chat_stream(request):
            yield event


class AnthropicPassthroughProvider(RecordingChatProvider):
    async def chat_stream(self, request):
        self.requests.append(request)
        yield StreamEvent(
            StreamEventType.MESSAGE_START,
            usage=Usage(prompt_tokens=3),
            model=request.model,
        )
        yield StreamEvent(
            StreamEventType.PASSTHROUGH,
            extra={
                "frame": {
                    "type": "content_block_delta",
                    "index": 0,
                    "delta": {"type": "thinking_delta", "thinking": "not output"},
                }
            },
        )
        yield StreamEvent(
            StreamEventType.PASSTHROUGH,
            extra={
                "frame": {
                    "type": "content_block_delta",
                    "index": 0,
                    "delta": {"type": "input_json_delta", "partial_json": "not output"},
                }
            },
        )
        for text in ('{"memory":', "true}"):
            yield StreamEvent(
                StreamEventType.PASSTHROUGH,
                extra={
                    "frame": {
                        "type": "content_block_delta",
                        "index": 1,
                        "delta": {"type": "text_delta", "text": text},
                    }
                },
            )
        yield StreamEvent(
            StreamEventType.MESSAGE_DELTA,
            usage=Usage(completion_tokens=2),
            finish_reason="end_turn",
        )
        yield StreamEvent(StreamEventType.MESSAGE_STOP)


class FakeEmbeddingsProvider:
    def __init__(self, dimension: int = 3):
        self.dimension = dimension
        self.payloads: list[dict] = []

    async def create_embeddings(self, payload: dict) -> dict:
        self.payloads.append(payload)
        values = payload["input"] if isinstance(payload["input"], list) else [payload["input"]]
        return {
            "object": "list",
            "data": [
                {"object": "embedding", "index": index, "embedding": [0.1] * self.dimension}
                for index, _ in enumerate(values)
            ],
            "model": payload["model"],
            "usage": {"prompt_tokens": len(values), "total_tokens": len(values)},
        }

    async def aclose(self) -> None:
        return None


class FakeResponsesProvider:
    def __init__(self):
        self.payloads: list[dict] = []

    async def create_response(self, payload: dict) -> dict:
        self.payloads.append(payload)
        return {
            "id": "resp_memory",
            "output_text": '{"memory":true}',
            "usage": {"input_tokens": 4, "output_tokens": 2},
        }

    async def stream_response(self, payload: dict) -> AsyncIterator[bytes]:
        if False:  # pragma: no cover - protocol shape only
            yield b""

    async def aclose(self) -> None:
        return None


def _client(service: GatewayService, token: str = "memory-secret"):
    app = create_app(service, memory_service_token=token)
    return httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://test")


def _chat_embedding_service(*, embedding_dimension: int = 3, chat=None):
    chat = chat or RecordingChatProvider()
    embedding = FakeEmbeddingsProvider(dimension=embedding_dimension)
    routes = {
        "extract": ModelRoute(
            alias="extract",
            provider_name="chat",
            real_model="claude-real",
            protocols=("anthropic-messages",),
        ),
        "embed": ModelRoute(
            alias="embed",
            provider_name="embedding",
            real_model="embedding-real",
            protocols=("openai-embeddings",),
            visible=False,
            embedding_dimension=3,
        ),
    }
    registry = Registry(
        providers={"chat": chat},
        embeddings_providers={"embedding": embedding},
        routes=routes,
        default_alias="extract",
        memory_extraction_route_id="extract",
        memory_embedding_route_id="embed",
    )
    ledger = MemoryLedger()
    return (
        GatewayService(
            registry,
            ledger,
            policy=ResiliencePolicy(timeout_s=1, max_retries=0, backoff_base_s=0),
        ),
        ledger,
        chat,
        embedding,
    )


def _chat_request(**overrides) -> dict:
    payload = {
        "model": "cocola-memory-extraction",
        "messages": [{"role": "user", "content": "Remember dark mode"}],
        "stream": False,
    }
    payload.update(overrides)
    return payload


async def test_memory_adapter_requires_its_own_service_token():
    service, _, _, _ = _chat_embedding_service()
    async with _client(service) as client:
        missing = await client.post("/internal/memory/v1/chat/completions", json=_chat_request())
        wrong = await client.post(
            "/internal/memory/v1/chat/completions",
            json=_chat_request(),
            headers={"authorization": "Bearer wrong"},
        )

    assert missing.status_code == 401
    assert wrong.status_code == 401


async def test_anthropic_memory_adapter_supports_json_and_platform_usage():
    service, ledger, chat, _ = _chat_embedding_service()
    async with _client(service) as client:
        response = await client.post(
            "/internal/memory/v1/chat/completions",
            json=_chat_request(
                response_format={"type": "json_object"}, temperature=0, max_tokens=128
            ),
            headers={"authorization": "Bearer memory-secret"},
        )

    assert response.status_code == 200
    assert response.json()["choices"][0]["message"]["content"] == '{"memory":true}'
    request = chat.requests[0]
    assert request.model == "claude-real"
    assert "Return only one valid JSON object" in request.messages[0].content
    records = await ledger.recent(user_id="memory-service")
    assert len(records) == 1
    assert records[0].session_id == "memory-service"


async def test_anthropic_memory_adapter_extracts_passthrough_text_only():
    chat = AnthropicPassthroughProvider()
    service, ledger, _, _ = _chat_embedding_service(chat=chat)
    async with _client(service) as client:
        response = await client.post(
            "/internal/memory/v1/chat/completions",
            json=_chat_request(response_format={"type": "json_object"}),
            headers={"authorization": "Bearer memory-secret"},
        )

    assert response.status_code == 200
    assert response.json()["choices"][0]["message"]["content"] == '{"memory":true}'
    assert response.json()["usage"] == {
        "prompt_tokens": 3,
        "completion_tokens": 2,
        "total_tokens": 5,
    }
    records = await ledger.recent(user_id="memory-service")
    assert len(records) == 1
    assert records[0].completion_tokens == 2


@pytest.mark.parametrize(
    "overrides",
    [
        {"stream": True},
        {"tools": [{"type": "function"}]},
        {"tool_choice": "auto"},
        {"messages": [{"role": "user", "content": [{"type": "text", "text": "hi"}]}]},
    ],
)
async def test_memory_chat_rejects_stream_tools_and_multimodal(overrides):
    service, _, _, _ = _chat_embedding_service()
    async with _client(service) as client:
        response = await client.post(
            "/internal/memory/v1/chat/completions",
            json=_chat_request(**overrides),
            headers={"authorization": "Bearer memory-secret"},
        )
    assert response.status_code == 400


async def test_embedding_adapter_rewrites_model_and_enforces_dimension():
    service, ledger, _, embedding = _chat_embedding_service()
    async with _client(service) as client:
        response = await client.post(
            "/internal/memory/v1/embeddings",
            json={"model": "cocola-memory-embedding", "input": ["one", "two"]},
            headers={"authorization": "Bearer memory-secret"},
        )

    assert response.status_code == 200
    assert response.json()["model"] == "cocola-memory-embedding"
    assert embedding.payloads == [
        {
            "model": "embedding-real",
            "input": ["one", "two"],
            "encoding_format": "float",
        }
    ]
    records = await ledger.recent(user_id="memory-service")
    assert len(records) == 1
    assert records[0].prompt_tokens == 2


async def test_embedding_dimension_mismatch_is_sanitized():
    service, _, _, _ = _chat_embedding_service(embedding_dimension=2)
    async with _client(service) as client:
        response = await client.post(
            "/internal/memory/v1/embeddings",
            json={"model": "cocola-memory-embedding", "input": "hello"},
            headers={"authorization": "Bearer memory-secret"},
        )

    assert response.status_code == 503
    assert "dimension" not in response.text.lower()


async def test_responses_route_is_adapted_without_becoming_public_chat_completions():
    provider = FakeResponsesProvider()
    route = ModelRoute(
        alias="responses-extract",
        provider_name="responses",
        real_model="gpt-real",
        protocols=("openai-responses",),
    )
    registry = Registry(
        providers={},
        responses_providers={"responses": provider},
        routes={"responses-extract": route},
        default_alias="responses-extract",
        memory_extraction_route_id="responses-extract",
    )
    service = GatewayService(registry, MemoryLedger())
    async with _client(service) as client:
        response = await client.post(
            "/internal/memory/v1/chat/completions",
            json=_chat_request(
                response_format={
                    "type": "json_schema",
                    "json_schema": {"name": "memory", "schema": {"type": "object"}},
                }
            ),
            headers={"authorization": "Bearer memory-secret"},
        )

    assert response.status_code == 200
    assert provider.payloads[0]["model"] == "gpt-real"
    assert provider.payloads[0]["stream"] is False
    assert provider.payloads[0]["text"]["format"]["type"] == "json_schema"
