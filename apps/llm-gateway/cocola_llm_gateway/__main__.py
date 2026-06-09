"""M3 entrypoint: boot the Anthropic-compatible LLM gateway over HTTP.

Run:
    python -m cocola_llm_gateway
Env knobs (see config.py / bootstrap.py): COCOLA_LLM_PROVIDER,
COCOLA_LLM_DEFAULT_ALIAS, COCOLA_LLM_CONFIG, COCOLA_LLM_REDIS_URL,
COCOLA_LLM_HOST/PORT, COCOLA_*_API_KEY ...

Point the Claude Agent SDK at it with:
    ANTHROPIC_BASE_URL=http://<host>:<port>
"""
from __future__ import annotations

import uvicorn

from cocola_common import get_logger
from cocola_llm_gateway.bootstrap import build_service
from cocola_llm_gateway.config import gateway_config_from_env
from cocola_llm_gateway.server import create_app


def main() -> None:
    log = get_logger("cocola.llm-gateway")
    gcfg = gateway_config_from_env()
    service = build_service()
    app = create_app(service)
    log.info(
        "cocola-llm-gateway starting",
        milestone="M3",
        host=gcfg.host,
        port=gcfg.port,
        default_alias=service.registry.default_alias,
        aliases=service.registry.aliases(),
    )
    uvicorn.run(app, host=gcfg.host, port=gcfg.port, log_level="info")


if __name__ == "__main__":
    main()
