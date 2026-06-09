"""agent-runtime entrypoint: boot the AgentRuntimeService gRPC server.

Composition root. Everything testable lives in `server.py` / providers / the
skill loader; this module only reads env, wires the concrete pieces together,
and runs the async gRPC server. It deliberately holds no business logic.

Env (all optional; sensible local defaults):
    COCOLA_AGENT_HOST / COCOLA_AGENT_PORT   where to listen (default 0.0.0.0:50061)
    COCOLA_LLM_BASE_URL                      cocola llm-gateway root the SDK targets
    COCOLA_ANTHROPIC_MODEL                   caller-facing model alias (default "default")
    COCOLA_AGENT_API_KEY                     cocola-issued token the SDK presents (dev default)
    COCOLA_ADMIN_BASE_URL                    admin-api root for the Skill-Market catalog
    COCOLA_ADMIN_KEY                         admin bearer key (if admin-api auth is on)

Provider selection: with COCOLA_LLM_BASE_URL set we drive the real Claude Agent
SDK routed through the gateway; without it we fall back to EchoProvider so a
zero-config boot still serves the contract end to end (useful for wiring tests).
"""

from __future__ import annotations

import asyncio
import os

import grpc
from cocola.agent.v1 import agent_pb2_grpc as pb_grpc
from cocola_common import get_logger

from cocola_agent_runtime.agent_provider import AgentProvider
from cocola_agent_runtime.echo_provider import EchoProvider
from cocola_agent_runtime.server import AgentRuntimeServicer
from cocola_agent_runtime.skill_loader import AdminSkillCatalog, SkillCatalog

log = get_logger("cocola.agent-runtime")


def _build_provider() -> AgentProvider:
    base_url = os.getenv("COCOLA_LLM_BASE_URL", "").strip()
    if not base_url:
        log.warning(
            "COCOLA_LLM_BASE_URL unset; using EchoProvider (no real model calls)"
        )
        return EchoProvider()
    # Imported lazily so an Echo boot needs neither the SDK nor the CLI present.
    from cocola_agent_runtime.claude_sdk_provider import (
        ClaudeAgentSDKProvider,
        ClaudeSDKConfig,
    )

    cfg = ClaudeSDKConfig(
        base_url=base_url,
        model=os.getenv("COCOLA_ANTHROPIC_MODEL", "default"),
        api_key=os.getenv("COCOLA_AGENT_API_KEY", "cocola-local"),
    )
    log.info("using ClaudeAgentSDKProvider", base_url=base_url, model=cfg.model)
    return ClaudeAgentSDKProvider(cfg)


def _build_skill_catalog() -> SkillCatalog | None:
    admin_base = os.getenv("COCOLA_ADMIN_BASE_URL", "").strip()
    if not admin_base:
        log.warning("COCOLA_ADMIN_BASE_URL unset; sessions run with no market skills")
        return None
    log.info("Skill-Market enabled", admin_base=admin_base)
    return AdminSkillCatalog(admin_base, admin_key=os.getenv("COCOLA_ADMIN_KEY", ""))


async def serve() -> None:
    host = os.getenv("COCOLA_AGENT_HOST", "0.0.0.0")
    port = int(os.getenv("COCOLA_AGENT_PORT", "50061"))
    addr = f"{host}:{port}"

    servicer = AgentRuntimeServicer(_build_provider(), skills=_build_skill_catalog())
    server = grpc.aio.server()
    pb_grpc.add_AgentRuntimeServiceServicer_to_server(servicer, server)
    server.add_insecure_port(addr)

    await server.start()
    log.info("cocola-agent-runtime serving", addr=addr)
    await server.wait_for_termination()


def main() -> None:
    asyncio.run(serve())


if __name__ == "__main__":
    main()
