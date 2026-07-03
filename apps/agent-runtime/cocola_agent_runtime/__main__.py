"""agent-runtime entrypoint: boot the AgentRuntimeService gRPC server.

Composition root. Everything testable lives in `server.py` / providers / the
skill loader; this module only reads env, wires the concrete pieces together,
and runs the async gRPC server. It deliberately holds no business logic.

Env (all optional; sensible local defaults):
    COCOLA_AGENT_HOST / COCOLA_AGENT_PORT   where to listen (default 0.0.0.0:50061)
    COCOLA_ADMIN_BASE_URL                    admin-api root for the Skill-Market catalog
    COCOLA_ADMIN_KEY                         admin bearer key (if admin-api auth is on)
    COCOLA_SANDBOX_ADDR                      sandbox-manager gRPC addr (binds session->sandbox,
                                             routes the agent's bash/file tools into it, AND
                                             hosts the Route A brain; unset => EchoProvider)
    COCOLA_SANDBOX_IMAGE                      Route A brain image for the session sandbox
                                             (e.g. cocola/sandbox-runtime); empty => default
    COCOLA_SANDBOX_LLM_BASE_URL              gateway root injected as ANTHROPIC_BASE_URL
    COCOLA_SANDBOX_LLM_TOKEN                 cocola token injected as ANTHROPIC_AUTH_TOKEN
    COCOLA_SANDBOX_MODEL_ALIAS               alias injected as ANTHROPIC_MODEL / _SMALL_FAST_MODEL
    COCOLA_PG_DSN                            Postgres DSN; enables the durable session->claude
                                             resume index (else in-process, lost on restart)

Provider selection (ADR-0009, Route B decommissioned): Route A runs the whole
Claude Code brain inside the user's own sandbox via the in-sandbox stdio shim,
so it needs a reachable sandbox executor (COCOLA_SANDBOX_ADDR). With no executor
we fall back to EchoProvider so a zero-config boot still serves the contract end
to end (useful for wiring tests) -- no real model calls.
"""

from __future__ import annotations

import asyncio
import os

import cocola_common
import grpc
from cocola.agent.v1 import agent_pb2_grpc as pb_grpc
from cocola_common import Registry, get_logger
from cocola_common.metrics_grpc import PrometheusServerInterceptor
from cocola_common.tracing import grpc_aio_server_interceptor

from cocola_agent_runtime.agent_provider import AgentProvider
from cocola_agent_runtime.echo_provider import EchoProvider
from cocola_agent_runtime.grpc_limits import channel_options
from cocola_agent_runtime.objstore import fetcher_from_env
from cocola_agent_runtime.sandbox_binder import (
    SandboxBinder,
    SandboxExecutor,
    SandboxManagerBinder,
    SandboxManagerExecutor,
)
from cocola_agent_runtime.session_map import SessionMap
from cocola_agent_runtime.server import AgentRuntimeServicer
from cocola_agent_runtime.skill_loader import AdminSkillCatalog, SkillCatalog

log = get_logger("cocola.agent-runtime")


def _build_session_map():
    """session_id -> claude_session_id index for Route A --resume continuation.

    Postgres-backed when COCOLA_PG_DSN is set (survives an agent-runtime
    restart, so paired with the persistent ~/.claude volume a follow-up turn
    resumes the real conversation); otherwise an in-process map so a
    zero-dependency dev boot still works within one process lifetime.
    """
    dsn = os.getenv("COCOLA_PG_DSN", "").strip()
    if dsn:
        from cocola_agent_runtime.session_map import PostgresSessionMap

        log.info("session-map: Postgres (durable resume index)")
        return PostgresSessionMap(dsn)
    from cocola_agent_runtime.session_map import MemorySessionMap

    log.warning("COCOLA_PG_DSN unset; session-map is in-process (resume lost on restart)")
    return MemorySessionMap()


def _build_provider(
    executor: SandboxExecutor | None, session_map: SessionMap | None
) -> AgentProvider:
    # Route A (ADR-0009) is the only real path: run the WHOLE Claude Code brain
    # inside the user's own sandbox via the in-sandbox stdio shim, so agent-runtime
    # is a pure control-plane router. Route A needs a reachable sandbox to run the
    # brain in; without an executor (COCOLA_SANDBOX_ADDR unset) there is nowhere to
    # put it, so we degrade to EchoProvider -- a zero-config, no-model boot that
    # still serves the gRPC contract end to end (useful for wiring tests).
    #
    # The legacy central-SDK path (Route B, ClaudeAgentSDKProvider spawning the
    # claude CLI on the agent-runtime host) was decommissioned; see ADR-0009 and
    # docs/archive/refactor-decommission-route-b.md.
    if executor is None:
        log.warning(
            "no sandbox executor (COCOLA_SANDBOX_ADDR unset); "
            "using EchoProvider (no real model calls)"
        )
        return EchoProvider()

    from cocola_agent_runtime.shim_provider import InSandboxShimProvider

    if session_map is None:
        session_map = _build_session_map()
    log.info("using InSandboxShimProvider (Route A: brain in sandbox)")
    return InSandboxShimProvider(executor, session_map=session_map)


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
    # Same switch as the binder. Route A's InSandboxShimProvider drives the
    # in-sandbox brain over this executor's streaming exec; without a
    # sandbox-manager addr there is no sandbox to run the brain in, so Route A
    # falls back (and binding is off too).
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
    session_map = _build_session_map() if executor is not None else None
    servicer = AgentRuntimeServicer(
        _build_provider(executor, session_map),
        skills=_build_skill_catalog(),
        binder=_build_binder(),
        executor=executor,
        objstore=fetcher_from_env(),
        session_map=session_map,
    )
    # Observability: RED metrics for every RPC. agent-runtime has no HTTP server
    # of its own, so unlike the llm-gateway it exposes /metrics on a dedicated
    # port via the prometheus_client WSGI server. COCOLA_METRICS_PORT="" (or 0)
    # disables it; per <network_security> the listener only ever runs here, in
    # the real composition root, never in tests.
    metrics = Registry("agent-runtime")
    # Tracing (ADR-0011): OFF unless COCOLA_OTEL_ENABLED. init() installs the
    # propagator (inbound traceparent -> log correlation) always; when enabled it
    # also stands up the OTLP exporter. The aio server interceptor produces a
    # server span per RPC and is None (skipped) when tracing is off.
    tracing_cfg = cocola_common.config_from_env("agent-runtime")
    stop_tracing = cocola_common.init(tracing_cfg)
    interceptors = [PrometheusServerInterceptor(metrics)]
    otel_interceptor = grpc_aio_server_interceptor(tracing_cfg)
    if otel_interceptor is not None:
        interceptors.append(otel_interceptor)
    # Raise the single-message ceiling above gRPC's 4 MiB default so
    # inline attachment bytes (up to the ADR-0017 split threshold) and
    # the sandbox WriteFile hop are not rejected as ResourceExhausted
    # (COCOLA_GRPC_MAX_MESSAGE_BYTES, default 64 MiB).
    server = grpc.aio.server(interceptors=interceptors, options=channel_options())
    pb_grpc.add_AgentRuntimeServiceServicer_to_server(servicer, server)
    server.add_insecure_port(addr)

    metrics_port = int(os.getenv("COCOLA_METRICS_PORT", "9094"))
    if metrics_port:
        from prometheus_client import start_http_server

        start_http_server(metrics_port, registry=metrics.registry)
        log.info("cocola-agent-runtime metrics", port=metrics_port)

    await server.start()
    log.info("cocola-agent-runtime serving", addr=addr)
    try:
        await server.wait_for_termination()
    finally:
        await stop_tracing()


def main() -> None:
    asyncio.run(serve())


if __name__ == "__main__":
    main()
