"""OpenAI Responses proxy contract for the Codex runtime."""

from __future__ import annotations

import gzip
from collections.abc import AsyncIterator

import httpx
import pytest
from cocola_common import ErrorCode
from cocola_llm_gateway.auth.jwt import Identity
from cocola_llm_gateway.billing.memory import MemoryLedger
from cocola_llm_gateway.middleware import ResiliencePolicy
from cocola_llm_gateway.registry import ModelRoute, Pricing, Registry
from cocola_llm_gateway.server import create_app
from cocola_llm_gateway.service import GatewayService, ResponsesSSEMeter
from cocola_llm_gateway.upstream.errors import UpstreamError
from cocola_llm_gateway.upstream.openai_responses import (
    OpenAIResponsesConfig,
    OpenAIResponsesUpstream,
)
from tests.conftest import auth_pair


class FakeResponsesProvider:
    def __init__(self, *, response: dict | None = None, chunks: list[bytes] | None = None):
        self.response = response or {"id": "resp_1", "output": []}
        self.chunks = chunks or []
        self.payloads: list[dict] = []
        self.stream_calls = 0
        self.closed = False

    async def create_response(self, payload: dict) -> dict:
        self.payloads.append(payload)
        return self.response

    async def stream_response(self, payload: dict) -> AsyncIterator[bytes]:
        self.payloads.append(payload)
        self.stream_calls += 1
        for chunk in self.chunks:
            yield chunk

    async def aclose(self) -> None:
        self.closed = True


class CompressedBody(httpx.AsyncByteStream):
    async def __aiter__(self):
        yield gzip.compress(b'data: {"type":"response.completed","response":{"usage":{}}}\n\n')


class FakeTraceStore:
    def __init__(self):
        self.calls: list[tuple[object, dict]] = []

    async def record_model_call(self, context, **fields):
        self.calls.append((context, fields))

    async def aclose(self):
        return None


def _service(
    provider,
    *,
    policy: ResiliencePolicy | None = None,
    trace_store: FakeTraceStore | None = None,
):
    route = ModelRoute(
        alias="codex-model",
        provider_name="responses",
        real_model="gpt-real",
        pricing=Pricing(input_per_1k=1, output_per_1k=2),
        protocols=("openai-responses",),
    )
    registry = Registry(
        providers={},
        responses_providers={"responses": provider},
        routes={route.alias: route},
        default_alias=route.alias,
    )
    ledger = MemoryLedger()
    return GatewayService(
        registry,
        ledger,
        policy=policy or ResiliencePolicy(timeout_s=1, max_retries=1, backoff_base_s=0),
        trace_store=trace_store,
    ), ledger


def _client(app):
    return httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://t")


async def test_nonstream_rewrites_alias_authenticates_and_bills_once():
    provider = FakeResponsesProvider(
        response={
            "id": "resp_123",
            "output": [{"type": "message"}],
            "usage": {
                "input_tokens": 12,
                "input_tokens_details": {"cached_tokens": 3},
                "output_tokens": 5,
                "output_tokens_details": {"reasoning_tokens": 2},
            },
        }
    )
    service, ledger = _service(provider)
    issuer, verifier = auth_pair()
    token = issuer.issue("user-1")

    async with _client(create_app(service, verifier=verifier)) as client:
        response = await client.post(
            "/v1/responses",
            json={"model": "codex-model", "input": "hello"},
            headers={
                "authorization": f"Bearer {token}",
                "x-cocola-conversation-id": "conversation-1",
            },
        )

    assert response.status_code == 200
    assert response.json()["id"] == "resp_123"
    assert provider.payloads == [{"model": "gpt-real", "input": "hello", "stream": False}]
    records = await ledger.recent(user_id="user-1")
    assert len(records) == 1
    assert records[0].request_id == "resp_123"
    assert records[0].session_id == "conversation-1"
    assert records[0].prompt_tokens == 12
    assert records[0].completion_tokens == 5


async def test_stream_preserves_sse_bytes_and_bills_completed_usage():
    chunks = [
        b"event: response.output_text.delta\n"
        b'data: {"type":"response.output_text.delta","delta":"hi"}\n\n',
        b"event: response.completed\n"
        b'data: {"type":"response.completed","response":{"id":"resp_1",'
        b'"usage":{"input_tokens":4,"output_tokens":2}}}\n\n',
    ]
    provider = FakeResponsesProvider(chunks=chunks)
    service, ledger = _service(provider)

    async with _client(create_app(service)) as client:
        response = await client.post(
            "/v1/responses",
            json={"model": "codex-model", "input": "hello", "stream": True},
            headers={"x-cocola-conversation-id": "conversation-2"},
        )

    assert response.status_code == 200
    assert response.content == b"".join(chunks)
    assert provider.payloads == [{"model": "gpt-real", "input": "hello", "stream": True}]
    records = await ledger.recent(user_id="dev-user")
    assert len(records) == 1
    assert records[0].status == "ok"
    assert records[0].prompt_tokens == 4
    assert records[0].completion_tokens == 2


async def test_upstream_decodes_compressed_sse_before_forwarding():
    async def handler(request):
        return httpx.Response(
            200,
            headers={"content-encoding": "gzip", "content-type": "text/event-stream"},
            stream=CompressedBody(),
        )

    upstream = OpenAIResponsesUpstream(
        OpenAIResponsesConfig(base_url="https://upstream.test/v1", api_key="test-key")
    )
    await upstream._client.aclose()
    upstream._client = httpx.AsyncClient(
        base_url="https://upstream.test/v1/",
        transport=httpx.MockTransport(handler),
    )
    try:
        chunks = [chunk async for chunk in upstream.stream_response({"stream": True})]
    finally:
        await upstream.aclose()

    assert b"".join(chunks).startswith(b"data: ")


async def test_responses_share_the_configured_rate_limiter():
    provider = FakeResponsesProvider()
    service, _ = _service(
        provider,
        policy=ResiliencePolicy(
            timeout_s=1,
            max_retries=0,
            backoff_base_s=0,
            rate_limit_rps=0.001,
            rate_burst=1,
        ),
    )
    identity = Identity(user_id="user-1")

    await service.responses_create(
        {"model": "codex-model"},
        requested_alias="codex-model",
        identity=identity,
        session_id="conversation-1",
    )
    with pytest.raises(UpstreamError) as exc_info:
        await service.responses_create(
            {"model": "codex-model"},
            requested_alias="codex-model",
            identity=identity,
            session_id="conversation-1",
        )

    assert exc_info.value.status == 429


async def test_stream_retries_only_before_first_sse_chunk():
    class RetryProvider(FakeResponsesProvider):
        async def stream_response(self, payload: dict) -> AsyncIterator[bytes]:
            self.stream_calls += 1
            if self.stream_calls == 1:
                raise UpstreamError(ErrorCode.UNAVAILABLE, "temporary", retryable=True)
            yield b'data: {"type":"response.completed","response":{"usage":{}}}\n\n'

    provider = RetryProvider()
    service, _ = _service(provider)
    chunks = [
        chunk
        async for chunk in service.responses_stream(
            {"model": "codex-model", "stream": True},
            requested_alias="codex-model",
            identity=Identity(user_id="user-1"),
            session_id="conversation-1",
        )
    ]

    assert provider.stream_calls == 2
    assert len(chunks) == 1


async def test_stream_retries_an_empty_stream_before_first_sse_chunk():
    class EmptyThenSuccessProvider(FakeResponsesProvider):
        async def stream_response(self, payload: dict) -> AsyncIterator[bytes]:
            self.stream_calls += 1
            if self.stream_calls == 1:
                return
            yield b'data: {"type":"response.completed","response":{"usage":{}}}\n\n'

    provider = EmptyThenSuccessProvider()
    service, _ = _service(provider)
    chunks = [
        chunk
        async for chunk in service.responses_stream(
            {"model": "codex-model", "stream": True},
            requested_alias="codex-model",
            identity=Identity(user_id="user-1"),
            session_id="conversation-1",
        )
    ]

    assert provider.stream_calls == 2
    assert len(chunks) == 1


async def test_stream_never_retries_after_first_sse_chunk():
    class BrokenAfterFirstProvider(FakeResponsesProvider):
        async def stream_response(self, payload: dict) -> AsyncIterator[bytes]:
            self.stream_calls += 1
            yield b'data: {"type":"response.output_text.delta","delta":"partial"}\n\n'
            raise UpstreamError(ErrorCode.UNAVAILABLE, "connection lost", retryable=True)

    provider = BrokenAfterFirstProvider()
    service, ledger = _service(provider)

    with pytest.raises(UpstreamError, match="connection lost"):
        _ = [
            chunk
            async for chunk in service.responses_stream(
                {"model": "codex-model", "stream": True},
                requested_alias="codex-model",
                identity=Identity(user_id="user-1"),
                session_id="conversation-1",
            )
        ]

    assert provider.stream_calls == 1
    records = await ledger.recent(user_id="user-1")
    assert len(records) == 1
    assert records[0].status == "error"


async def test_stream_bills_completed_usage_even_if_connection_fails_after_terminal_event():
    class BrokenAfterCompletedProvider(FakeResponsesProvider):
        async def stream_response(self, payload: dict) -> AsyncIterator[bytes]:
            yield (
                b'data: {"type":"response.completed","response":{"usage":'
                b'{"input_tokens":9,"output_tokens":4}}}\n\n'
            )
            raise UpstreamError(ErrorCode.UNAVAILABLE, "connection lost", retryable=True)

    service, ledger = _service(BrokenAfterCompletedProvider())

    with pytest.raises(UpstreamError, match="connection lost"):
        _ = [
            chunk
            async for chunk in service.responses_stream(
                {"model": "codex-model", "stream": True},
                requested_alias="codex-model",
                identity=Identity(user_id="user-1"),
                session_id="conversation-1",
            )
        ]

    records = await ledger.recent(user_id="user-1")
    assert len(records) == 1
    assert records[0].status == "error"
    assert records[0].prompt_tokens == 9
    assert records[0].completion_tokens == 4


async def test_stream_bills_incomplete_terminal_usage_as_error():
    chunks = [
        b'data: {"type":"response.incomplete","response":{"id":"resp_partial",'
        b'"usage":{"input_tokens":8,"output_tokens":3}}}\n\n'
    ]
    service, ledger = _service(FakeResponsesProvider(chunks=chunks))

    async with _client(create_app(service)) as client:
        response = await client.post(
            "/v1/responses",
            json={"model": "codex-model", "input": "hello", "stream": True},
        )

    assert response.status_code == 200
    records = await ledger.recent(user_id="dev-user")
    assert len(records) == 1
    assert records[0].request_id == "resp_partial"
    assert records[0].status == "error"
    assert records[0].prompt_tokens == 8
    assert records[0].completion_tokens == 3


async def test_responses_record_nonstream_and_stream_model_traces():
    provider = FakeResponsesProvider(
        response={
            "id": "resp_sync",
            "usage": {"input_tokens": 2, "output_tokens": 1},
        },
        chunks=[
            b'data: {"type":"response.completed","response":{"id":"resp_stream",'
            b'"usage":{"input_tokens":4,"output_tokens":3}}}\n\n'
        ],
    )
    trace_store = FakeTraceStore()
    service, _ = _service(provider, trace_store=trace_store)
    traceparent = f"00-{'a' * 32}-{'b' * 16}-01"

    async with _client(create_app(service)) as client:
        nonstream = await client.post(
            "/v1/responses",
            json={"model": "codex-model", "input": "hello"},
            headers={"traceparent": traceparent},
        )
        stream = await client.post(
            "/v1/responses",
            json={"model": "codex-model", "input": "hello", "stream": True},
            headers={"traceparent": traceparent},
        )

    assert nonstream.status_code == 200
    assert stream.status_code == 200
    assert len(trace_store.calls) == 2
    assert [fields["status"] for _, fields in trace_store.calls] == ["success", "success"]
    assert [fields["input_tokens"] for _, fields in trace_store.calls] == [2, 4]
    assert [fields["output_tokens"] for _, fields in trace_store.calls] == [1, 3]


async def test_responses_errors_do_not_expose_upstream_body():
    class SecretFailureProvider(FakeResponsesProvider):
        async def create_response(self, payload: dict) -> dict:
            raise UpstreamError(
                ErrorCode.UNAVAILABLE,
                "upstream body contained sk-secret-value",
                retryable=False,
            )

    service, ledger = _service(SecretFailureProvider())

    async with _client(create_app(service)) as client:
        response = await client.post(
            "/v1/responses",
            json={"model": "codex-model", "input": "hello"},
        )

    assert response.status_code == 503
    assert "sk-secret-value" not in response.text
    records = await ledger.recent(user_id="dev-user")
    assert len(records) == 1
    assert records[0].status == "error"
    assert "sk-secret-value" not in records[0].error


async def test_responses_preserve_safe_upstream_rate_limit_status():
    class RateLimitedProvider(FakeResponsesProvider):
        async def create_response(self, payload: dict) -> dict:
            raise UpstreamError(
                ErrorCode.UNAVAILABLE,
                "upstream body contained private details",
                status=429,
                retryable=False,
            )

    service, _ = _service(RateLimitedProvider())

    async with _client(create_app(service)) as client:
        response = await client.post(
            "/v1/responses",
            json={"model": "codex-model", "input": "hello"},
        )

    assert response.status_code == 429
    assert response.json()["error"]["type"] == "rate_limit_error"
    assert "private details" not in response.text


@pytest.mark.parametrize(
    ("upstream_status", "error_type"),
    [(400, "invalid_request_error"), (401, "authentication_error"), (403, "permission_error")],
)
async def test_responses_preserve_safe_upstream_client_error_class(
    upstream_status: int,
    error_type: str,
):
    class ClientFailureProvider(FakeResponsesProvider):
        async def create_response(self, payload: dict) -> dict:
            raise UpstreamError(
                ErrorCode.UNAVAILABLE,
                "upstream body contained private details",
                status=upstream_status,
                retryable=False,
            )

    service, _ = _service(ClientFailureProvider())

    async with _client(create_app(service)) as client:
        response = await client.post(
            "/v1/responses",
            json={"model": "codex-model", "input": "hello"},
        )

    assert response.status_code == upstream_status
    assert response.json()["error"]["type"] == error_type
    assert "private details" not in response.text


def test_responses_meter_handles_crlf_split_across_chunks():
    meter = ResponsesSSEMeter()
    meter.feed(
        b'data: {"type":"response.completed","response":{"usage":'
        b'{"input_tokens":7,"output_tokens":3}}}\r'
    )
    meter.feed(b"\n\r\n")

    assert meter.completed
    assert meter.usage.input_tokens == 7
    assert meter.usage.output_tokens == 3
