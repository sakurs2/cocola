"""FastAPI app exposing the Anthropic-compatible front-end + billing reads.

Endpoints:
  POST /v1/messages   Anthropic Messages API. Honors `stream` (SSE vs JSON).
                      This is the endpoint the Claude Agent SDK hits when its
                      ANTHROPIC_BASE_URL points at this gateway.
  GET  /healthz       Liveness + which model aliases are configured.
  GET  /v1/usage      Billing read: recent records + per-user/session aggregates.
                      (Debug/ops surface; auth lands in M4.)

The app is built by `create_app(service)` so tests can inject a service backed
by FakeUpstream + MemoryLedger and drive it via httpx ASGITransport — no real
network, no bound port.

Identity is mocked until M4: user_id/session_id are read from headers
(x-cocola-user / x-cocola-session) with sensible fallbacks.
"""
from __future__ import annotations

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse, StreamingResponse

from cocola_common import CocolaError, ErrorCode, get_logger
from cocola_llm_gateway.anthropic_codec import (
    collect_to_anthropic_response,
    stream_to_anthropic_sse,
    to_chat_request,
)
from cocola_llm_gateway.service import GatewayService

log = get_logger("cocola.llm-gateway.server")

_CODE_TO_HTTP = {
    ErrorCode.INVALID_ARGUMENT: 400,
    ErrorCode.NOT_FOUND: 404,
    ErrorCode.PERMISSION_DENIED: 403,
    ErrorCode.UNAVAILABLE: 503,
    ErrorCode.INTERNAL: 500,
    ErrorCode.UNKNOWN: 500,
}


def _identity(request: Request) -> tuple[str, str]:
    user = request.headers.get("x-cocola-user", "") or "mock-user"
    session = request.headers.get("x-cocola-session", "") or "mock-session"
    return user, session


def create_app(service: GatewayService) -> FastAPI:
    app = FastAPI(title="cocola-llm-gateway", version="0.0.1")

    @app.get("/healthz")
    async def healthz() -> dict:
        return {
            "status": "ok",
            "default_alias": service.registry.default_alias,
            "aliases": service.registry.aliases(),
        }

    @app.post("/v1/messages")
    async def messages(request: Request):
        try:
            body = await request.json()
        except Exception:
            return _err(ErrorCode.INVALID_ARGUMENT, "request body must be JSON")

        user_id, session_id = _identity(request)
        requested_alias = body.get("model")

        # Resolve alias up-front so an unknown model fails fast with 404 (before
        # we open a stream).
        try:
            resolved_model = service.resolve_model(requested_alias)
        except CocolaError as e:
            return _err(e.code, e.message)

        chat_req = to_chat_request(
            body,
            resolved_model=resolved_model,
            user_id=user_id,
            session_id=session_id,
        )
        wants_stream = bool(body.get("stream", False))

        if wants_stream:
            event_stream = service.chat_stream(chat_req, requested_alias=requested_alias)
            sse = stream_to_anthropic_sse(event_stream, fallback_model=resolved_model)
            return StreamingResponse(
                sse,
                media_type="text/event-stream",
                headers={"cache-control": "no-cache", "x-accel-buffering": "no"},
            )

        # Non-streaming: drain to a single JSON response.
        event_stream = service.chat_stream(chat_req, requested_alias=requested_alias)
        try:
            payload = await collect_to_anthropic_response(event_stream, fallback_model=resolved_model)
        except Exception as e:
            return _err(ErrorCode.UNAVAILABLE, f"upstream error: {e}")
        return JSONResponse(payload)

    @app.get("/v1/usage")
    async def usage(request: Request):
        user_id = request.query_params.get("user_id", "")
        session_id = request.query_params.get("session_id", "")
        limit = int(request.query_params.get("limit", "20"))
        recent = await service.ledger.recent(user_id=user_id, limit=limit)
        out: dict = {"recent": [r.to_dict() for r in recent]}
        if user_id:
            a = await service.ledger.aggregate_user(user_id)
            out["user_aggregate"] = _agg_dict(a)
        if session_id:
            a = await service.ledger.aggregate_session(session_id)
            out["session_aggregate"] = _agg_dict(a)
        return JSONResponse(out)

    return app


def _agg_dict(a) -> dict:
    return {
        "calls": a.calls,
        "prompt_tokens": a.prompt_tokens,
        "completion_tokens": a.completion_tokens,
        "total_tokens": a.total_tokens,
        "cost_usd": a.cost_usd,
    }


def _err(code: ErrorCode, message: str) -> JSONResponse:
    http = _CODE_TO_HTTP.get(code, 500)
    return JSONResponse(
        {"type": "error", "error": {"type": code.value, "message": message}},
        status_code=http,
    )
