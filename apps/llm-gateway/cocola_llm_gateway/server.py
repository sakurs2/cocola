"""FastAPI app exposing Anthropic Messages and OpenAI Responses front-ends.

Endpoints:
  POST /v1/messages   Anthropic Messages API. Honors `stream` (SSE vs JSON).
                      This is the endpoint the Claude Agent SDK hits when its
                      ANTHROPIC_BASE_URL points at this gateway. The bearer token
                      it sends (as ANTHROPIC_API_KEY) is the cocola-issued JWT;
                      we verify it -> Identity (M4).
  POST /v1/responses  Transparent OpenAI Responses API for the Codex runtime.
  GET  /healthz       Liveness + which model aliases are configured.
  GET  /v1/usage      Billing read: recent records + per-user/session aggregates.
  GET  /v1/quota      Current token-quota standings for the caller (M4).

Identity (M4): resolved from the Authorization/x-api-key bearer token via the
auth.Verifier. When auth is disabled (no secret) or dev_allow_anonymous is on,
the caller resolves to a stable dev identity, preserving the old zero-config dev
boot. Quota is enforced before a stream is opened (429) and committed after.

The app is built by `create_app(service)` so tests can inject a service backed
by FakeUpstream + MemoryLedger + a Verifier/Enforcer and drive it via httpx
ASGITransport — no real network, no bound port.
"""

from __future__ import annotations

from cocola_common import (
    CocolaError,
    ErrorCode,
    Registry,
    TracingConfig,
    get_logger,
    instrument_fastapi,
    instrument_fastapi_tracing,
)
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse, StreamingResponse

from cocola_llm_gateway.anthropic_codec import (
    collect_to_anthropic_response,
    stream_to_anthropic_sse,
    to_chat_request,
)
from cocola_llm_gateway.auth import Identity, JWTError, Verifier
from cocola_llm_gateway.auth.identity import AuthConfig
from cocola_llm_gateway.auth.revocation import RevocationStore
from cocola_llm_gateway.conversation_trace import TraceContext
from cocola_llm_gateway.quota import QuotaExceeded
from cocola_llm_gateway.service import GatewayService
from cocola_llm_gateway.upstream.errors import UpstreamError

log = get_logger("cocola.llm-gateway.server")

_CODE_TO_HTTP = {
    ErrorCode.INVALID_ARGUMENT: 400,
    ErrorCode.NOT_FOUND: 404,
    ErrorCode.PERMISSION_DENIED: 403,
    ErrorCode.UNAVAILABLE: 503,
    ErrorCode.INTERNAL: 500,
    ErrorCode.UNKNOWN: 500,
}


def _bearer(request: Request) -> str | None:
    """Extract the bearer credential the SDK presents.

    The Claude Agent SDK sends ANTHROPIC_API_KEY as `x-api-key`; we also accept a
    standard `Authorization: Bearer` header for direct API callers.
    """
    return request.headers.get("authorization") or request.headers.get("x-api-key")


def create_app(
    service: GatewayService,
    *,
    verifier: Verifier | None = None,
    revocation: RevocationStore | None = None,
    metrics: Registry | None = None,
    tracing: TracingConfig | None = None,
) -> FastAPI:
    app = FastAPI(title="cocola-llm-gateway", version="0.0.1")
    # Distributed tracing (ADR-0011): server spans for every request, OFF unless
    # COCOLA_OTEL_ENABLED. The instrumentor is SSE-safe; with tracing disabled
    # this is a no-op so tests stay dependency-light.
    if tracing is not None:
        instrument_fastapi_tracing(app, tracing)
    # Observability: when a registry is supplied, wire the pure-ASGI RED
    # middleware (SSE-safe — it never buffers the body) and mount /metrics on
    # this same app, so no extra port is opened (see <network_security>). nil
    # leaves the app uninstrumented, keeping unit tests dependency-light.
    if metrics is not None:
        instrument_fastapi(app, metrics)
    # Default to a disabled verifier (no secret) so existing zero-config callers
    # and tests that don't care about auth keep working as the dev identity.
    vrf = verifier or Verifier(AuthConfig())
    # Optional revocation denylist. When present, a verified token whose `jti`
    # has been revoked is rejected even before its `exp` — this closes the gap
    # left by stateless offline verification (see ADR-0006).
    deny = revocation

    async def _authenticate(request: Request) -> Identity:
        """Resolve identity and enforce the revocation denylist.

        Raises JWTError if the token is missing/invalid (from the verifier) or
        if its `jti` is on the denylist. Callers map JWTError -> 401.
        """
        ident = vrf.verify(_bearer(request))
        if deny is not None and ident.token_id and await deny.is_revoked(ident.token_id):
            raise JWTError("token revoked")
        return ident

    @app.get("/healthz")
    async def healthz() -> dict:
        default_alias, aliases = await service.registry_status()
        return {
            "status": "ok",
            "default_alias": default_alias,
            "aliases": aliases,
            "auth_enabled": vrf.config.enabled,
        }

    @app.post("/v1/messages")
    async def messages(request: Request):
        try:
            body = await request.json()
        except Exception:
            return _err(ErrorCode.INVALID_ARGUMENT, "request body must be JSON")

        # 1) Identity — reject bad/missing/revoked tokens before doing any work.
        try:
            identity = await _authenticate(request)
        except JWTError as e:
            return _auth_err(str(e))

        requested_alias = body.get("model")

        # 2) Resolve alias up-front so an unknown model fails fast with 404.
        try:
            resolved_model = await service.resolve_model(requested_alias)
        except CocolaError as e:
            return _err(e.code, e.message)

        # 3) Quota gate — reject over-budget callers before opening a stream.
        try:
            await service.check_quota(identity)
        except QuotaExceeded as e:
            return _quota_err(e)

        chat_req = to_chat_request(
            body,
            resolved_model=resolved_model,
            user_id=identity.user_id,
            session_id=request.headers.get("x-cocola-session", "").strip() or identity.user_id,
        )
        wants_stream = bool(body.get("stream", False))

        if wants_stream:
            event_stream = service.chat_stream(
                chat_req,
                requested_alias=requested_alias,
                identity=identity,
                trace_context=TraceContext.parse(request.headers.get("traceparent")),
            )
            sse = stream_to_anthropic_sse(event_stream, fallback_model=resolved_model)
            return StreamingResponse(
                sse,
                media_type="text/event-stream",
                headers={"cache-control": "no-cache", "x-accel-buffering": "no"},
            )

        # Non-streaming: drain to a single JSON response.
        event_stream = service.chat_stream(
            chat_req,
            requested_alias=requested_alias,
            identity=identity,
            trace_context=TraceContext.parse(request.headers.get("traceparent")),
        )
        try:
            payload = await collect_to_anthropic_response(
                event_stream, fallback_model=resolved_model
            )
        except Exception as e:
            log.warning("upstream drain failed", error=repr(e))
            return _err(ErrorCode.UNAVAILABLE, f"upstream error: {e}")
        return JSONResponse(payload)

    @app.post("/v1/responses")
    async def responses(request: Request):
        try:
            body = await request.json()
        except Exception:
            return _responses_err(400, "request body must be JSON")
        if not isinstance(body, dict):
            return _responses_err(400, "request body must be a JSON object")
        requested_alias = str(body.get("model") or "").strip()
        if not requested_alias:
            return _responses_err(400, "model is required")
        try:
            identity = await _authenticate(request)
        except JWTError as exc:
            return _responses_err(401, str(exc), error_type="authentication_error")
        try:
            await service.resolve_responses_model(requested_alias)
        except CocolaError as exc:
            return _responses_err(_CODE_TO_HTTP.get(exc.code, 500), exc.message)
        try:
            await service.check_quota(identity)
        except QuotaExceeded as exc:
            return _responses_err(429, str(exc), error_type="rate_limit_error")

        session_id = (
            request.headers.get("x-cocola-conversation-id", "").strip()
            or request.headers.get("x-cocola-session", "").strip()
            or identity.user_id
        )
        if not bool(body.get("stream", False)):
            try:
                payload = await service.responses_create(
                    body,
                    requested_alias=requested_alias,
                    identity=identity,
                    session_id=session_id,
                    trace_context=TraceContext.parse(request.headers.get("traceparent")),
                )
            except UpstreamError as exc:
                log.warning(
                    "responses upstream failed",
                    error_type=type(exc).__name__,
                    upstream_status=exc.status,
                )
                return _responses_upstream_err(exc)
            except Exception as exc:  # noqa: BLE001 - sanitized provider errors only
                log.warning("responses upstream failed", error_type=type(exc).__name__)
                return _responses_err(503, "upstream Responses request failed")
            return JSONResponse(payload)

        event_stream = service.responses_stream(
            body,
            requested_alias=requested_alias,
            identity=identity,
            session_id=session_id,
            trace_context=TraceContext.parse(request.headers.get("traceparent")),
        )
        try:
            first = await anext(event_stream)
        except UpstreamError as exc:
            log.warning(
                "responses stream start failed",
                error_type=type(exc).__name__,
                upstream_status=exc.status,
            )
            await event_stream.aclose()
            return _responses_upstream_err(exc)
        except Exception as exc:  # noqa: BLE001 - no response headers sent yet
            log.warning("responses stream start failed", error_type=type(exc).__name__)
            await event_stream.aclose()
            return _responses_err(503, "upstream Responses stream failed")

        async def stream_with_first():
            try:
                yield first
                async for chunk in event_stream:
                    yield chunk
            finally:
                await event_stream.aclose()

        return StreamingResponse(
            stream_with_first(),
            media_type="text/event-stream",
            headers={"cache-control": "no-cache", "x-accel-buffering": "no"},
        )

    @app.get("/v1/usage")
    async def usage(request: Request):
        # Billing reads expose token usage, so they require identity. A caller
        # may only read their OWN usage. Cross-user/admin reads belong to the
        # admin API; query parameters never override the authenticated subject.
        try:
            identity = await _authenticate(request)
        except JWTError as e:
            return _auth_err(str(e))
        user_id = identity.user_id
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

    @app.get("/v1/quota")
    async def quota(request: Request):
        try:
            identity = await _authenticate(request)
        except JWTError as e:
            return _auth_err(str(e))
        statuses = await service.quota_status(identity)
        return JSONResponse(
            {
                "user_id": identity.user_id,
                "tenant_id": identity.tenant_id,
                "scopes": [
                    {
                        "scope": s.scope,
                        "subject": s.subject,
                        "period": s.period,
                        "used": s.used,
                        "limit": s.limit,
                        "remaining": s.remaining,
                        "exceeded": s.exceeded,
                    }
                    for s in statuses
                ],
            }
        )

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


def _auth_err(message: str) -> JSONResponse:
    # Anthropic-compatible error shape so the SDK surfaces it cleanly.
    return JSONResponse(
        {
            "type": "error",
            "error": {"type": "authentication_error", "message": message},
        },
        status_code=401,
    )


def _quota_err(exc: QuotaExceeded) -> JSONResponse:
    st = exc.status
    return JSONResponse(
        {
            "type": "error",
            "error": {
                "type": "rate_limit_error",
                "message": str(exc),
                "scope": st.scope,
                "period": st.period,
                "used": st.used,
                "limit": st.limit,
            },
        },
        status_code=429,
    )


def _responses_err(status: int, message: str, *, error_type: str = "invalid_request_error"):
    return JSONResponse(
        {"error": {"type": error_type, "message": message}},
        status_code=status,
    )


def _responses_upstream_err(exc: UpstreamError) -> JSONResponse:
    """Preserve actionable status classes without exposing upstream bodies."""
    status = exc.status or 503
    if status == 429:
        return _responses_err(429, "upstream rate limit exceeded", error_type="rate_limit_error")
    if status == 401:
        return _responses_err(
            401,
            "upstream authentication failed",
            error_type="authentication_error",
        )
    if status == 403:
        return _responses_err(403, "upstream request was forbidden", error_type="permission_error")
    if status in {400, 404, 409, 422}:
        return _responses_err(status, "upstream rejected the Responses request")
    return _responses_err(503, "upstream Responses request failed", error_type="server_error")
