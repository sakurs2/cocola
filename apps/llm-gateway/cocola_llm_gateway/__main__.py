"""M4 entrypoint: boot the Anthropic-compatible LLM gateway over HTTP.

Run:
    python -m cocola_llm_gateway
Env knobs (see config.py / bootstrap.py): COCOLA_LLM_REDIS_URL,
COCOLA_LLM_HOST/PORT, COCOLA_PG_DSN, COCOLA_AUTH_SECRET,
COCOLA_QUOTA_USER_DAILY_TOKENS ...

Providers and model routes are managed in Admin and loaded from Postgres.

Point the Claude Agent SDK at it with:
    ANTHROPIC_BASE_URL=http://<host>:<port>
    ANTHROPIC_API_KEY=<cocola-issued token>   # see: python -m cocola_llm_gateway.issue_token
"""

from __future__ import annotations

import cocola_common
import uvicorn
from cocola_common import Registry, get_logger

from cocola_llm_gateway.bootstrap import build_service, build_verifier
from cocola_llm_gateway.config import gateway_config_from_env
from cocola_llm_gateway.server import create_app


def main() -> None:
    log = get_logger("cocola.llm-gateway")
    gcfg = gateway_config_from_env()
    service = build_service()
    verifier = build_verifier()
    # /metrics is mounted on this same app (no extra port). Always on; scraping
    # is cheap and the metric set is bounded.
    metrics = Registry("llm-gateway")
    # Tracing (ADR-0011): OFF unless COCOLA_OTEL_ENABLED. init() always installs
    # the propagator (so inbound traceparent correlates logs) and returns a
    # stopper; the FastAPI instrumentor is wired inside create_app.
    tracing_cfg = cocola_common.config_from_env("llm-gateway")
    cocola_common.init(tracing_cfg)
    app = create_app(service, verifier=verifier, metrics=metrics, tracing=tracing_cfg)
    log.info(
        "cocola-llm-gateway starting",
        milestone="M4",
        host=gcfg.host,
        port=gcfg.port,
        default_alias=service.registry.default_alias,
        aliases=service.registry.aliases(),
        auth_enabled=verifier.config.enabled,
    )
    uvicorn.run(app, host=gcfg.host, port=gcfg.port, log_level="info")


if __name__ == "__main__":
    main()
