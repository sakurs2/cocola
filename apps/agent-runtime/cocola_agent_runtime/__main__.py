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
    COCOLA_SANDBOX_ADDR                      sandbox-manager gRPC addr (binds session->sandbox
                                             AND routes the agent's bash/file tools into it)
    COCOLA_AGENT_ROUTE                       "A" => run agent inside the sandbox via the shim
                                             (Route A, ADR-0009); default/unset => Route B
    COCOLA_SANDBOX_IMAGE                      Route A brain image for the session sandbox
                                             (e.g. cocola/sandbox-runtime); empty => provider default
    COCOLA_SANDBOX_LLM_BASE_URL              gateway root injected into the sandbox as ANTHROPIC_BASE_URL
    COCOLA_SANDBOX_LLM_TOKEN                 cocola token injected as ANTHROPIC_AUTH_TOKEN
    COCOLA_SANDBOX_MODEL_ALIAS              gateway model alias injected as ANTHROPIC_MODEL / _SMALL_FAST_MODEL

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
from cocola_agent_runtime.sandbox_binder import (
    SandboxBinder,
    SandboxExecutor,
    SandboxManagerBinder,
    SandboxManagerExecutor,
)
from cocola_agent_runtime.server import AgentRuntimeServicer
from cocola_agent_runtime.skill_loader import AdminSkillCatalog, SkillCatalog

log = get_logger("cocola.agent-runtime")


def _build_provider(executor: SandboxExecutor | None) -> AgentProvider:
    # Route A (ADR-0009): run the WHOLE Claude Code brain inside the user's own
    # sandbox via the in-sandbox stdio shim. agent-runtime is then a pure
    # control-plane router. Gated behind an explicit env switch so the default
    # stays Route B -- this makes the cut-over a grey-launch with a one-line
    # rollback (unset COCOLA_AGENT_ROUTE). Route A needs a real sandbox to run
    # in; without an executor there is nowhere to put the brain, so we fall back.
    route = os.getenv("COCOLA_AGENT_ROUTE", "").strip().upper()
    if route == "A":
        if executor is None:
            log.warning(
                "COCOLA_AGENT_ROUTE=A but no sandbox executor "
                "(COCOLA_SANDBOX_ADDR unset); falling back to Route B"
            )
        else:
            from cocola_agent_runtime.shim_provider import InSandboxShimProvider

            log.info("using InSandboxShimProvider (Route A: brain in sandbox)")
            return InSandboxShimProvider(executor)

    base_url = os.getenv("COCOLA_LLM_BASE_URL", "").strip()
    if not base_url:
        log.warning("COCOLA_LLM_BASE_URL unset; using EchoProvider (no real model calls)")
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
    log.info(
        "using ClaudeAgentSDKProvider",
        base_url=base_url,
        model=cfg.model,
        sandbox_tools=executor is not None,
    )
    # The same executor handles every session; the bound sandbox_id (per-session)
    # is threaded through AgentOptions, so one executor is safe to share.
    return ClaudeAgentSDKProvider(cfg, executor=executor)


def _build_skill_catalog() -> SkillCatalog | None:
    admin_base = os.getenv("COCOLA_ADMIN_BASE_URL", "").strip()
    if not admin_base:
        log.warning("COCOLA_ADMIN_BASE_URL unset; sessions run with no market skills")
        return None
    log.info("Skill-Market enabled", admin_base=admin_base)
    return AdminSkillCatalog(admin_base, admin_key=os.getenv("COCOLA_ADMIN_KEY", ""))


def _sandbox_provisioning() -> tuple[str, dict[str, str]]:
    """Route A (ADR-0009) sandbox image + the ENV injected at creation time.

    The session sandbox runs the WHOLE Claude Code brain, so it must be created
    from the brain image (COCOLA_SANDBOX_IMAGE, e.g. cocola/sandbox-runtime)
    rather than the provider's default alpine, and it must carry the model
    credentials so the in-sandbox `claude` CLI can reach the llm-gateway:

      ANTHROPIC_BASE_URL   <- COCOLA_SANDBOX_LLM_BASE_URL (the gateway root)
      ANTHROPIC_AUTH_TOKEN <- COCOLA_SANDBOX_LLM_TOKEN (the cocola-issued token)
      ANTHROPIC_MODEL / ANTHROPIC_SMALL_FAST_MODEL <- COCOLA_SANDBOX_MODEL_ALIAS
          (both the main and the fast model resolve to a known gateway alias;
           the registry 404s unknown aliases, so an unset fast model would fail)

    Credentials live in the sandbox ENV, never in the prompt channel. Empty
    values are dropped so a partially-configured boot doesn't inject blanks.
    """
    image = os.getenv("COCOLA_SANDBOX_IMAGE", "").strip()
    env: dict[str, str] = {}
    base_url = os.getenv("COCOLA_SANDBOX_LLM_BASE_URL", "").strip()
    if base_url:
        env["ANTHROPIC_BASE_URL"] = base_url
    token = os.getenv("COCOLA_SANDBOX_LLM_TOKEN", "").strip()
    if token:
        env["ANTHROPIC_AUTH_TOKEN"] = token
    alias = os.getenv("COCOLA_SANDBOX_MODEL_ALIAS", "").strip()
    if alias:
        env["ANTHROPIC_MODEL"] = alias
        env["ANTHROPIC_SMALL_FAST_MODEL"] = alias
    return image, env


def _build_binder() -> SandboxBinder | None:
    addr = os.getenv("COCOLA_SANDBOX_ADDR", "").strip()
    if not addr:
        log.warning("COCOLA_SANDBOX_ADDR unset; sessions run without a bound sandbox")
        return None
    image, env = _sandbox_provisioning()
    log.info(
        "sandbox binding enabled",
        sandbox_addr=addr,
        sandbox_image=image or "(provider default)",
        creds_injected=bool(env.get("ANTHROPIC_BASE_URL")),
    )
    return SandboxManagerBinder(addr, default_image=image, default_env=env)


def _build_executor() -> SandboxExecutor | None:
    # Same switch as the binder: with a sandbox-manager addr the agent's bash /
    # file tools execute inside the bound sandbox; without it the agent has no
    # sandbox IO at all (and binding is off too), so there is nothing to route.
    addr = os.getenv("COCOLA_SANDBOX_ADDR", "").strip()
    if not addr:
        return None
    log.info("sandbox tool execution enabled", sandbox_addr=addr)
    return SandboxManagerExecutor(addr)


async def serve() -> None:
    host = os.getenv("COCOLA_AGENT_HOST", "0.0.0.0")
    port = int(os.getenv("COCOLA_AGENT_PORT", "50061"))
    addr = f"{host}:{port}"

    executor = _build_executor()
    servicer = AgentRuntimeServicer(
        _build_provider(executor),
        skills=_build_skill_catalog(),
        binder=_build_binder(),
    )
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
