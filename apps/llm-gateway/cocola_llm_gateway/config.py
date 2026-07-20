"""Build providers and immutable process configuration.

This is the ONLY module that imports concrete UpstreamProvider classes. Business
logic (server, router, billing) stays provider-agnostic; here we read config and
wire concrete instances together.

Production model providers and routes are loaded from the admin-managed
Postgres tables by db_registry.py. ``_build_from_dict`` remains the composition
seam used by that source and by hermetic tests; this module deliberately has no
file or provider-env fallback, so all processes observe one model catalog.

HARD CONSTRAINT (ADR-0004): no endpoint/key is hardcoded; all come from config.
"""

from __future__ import annotations

import os
from dataclasses import dataclass
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from cocola_llm_gateway.auth.identity import AuthConfig
    from cocola_llm_gateway.quota.policy import QuotaPolicy

from cocola_common import CocolaError, ErrorCode, get_logger

from cocola_llm_gateway.registry import ModelRoute, Pricing, Registry
from cocola_llm_gateway.upstream.anthropic import AnthropicConfig, AnthropicUpstream
from cocola_llm_gateway.upstream.base import UpstreamProvider
from cocola_llm_gateway.upstream.embeddings_base import EmbeddingsProvider
from cocola_llm_gateway.upstream.fake import FakeUpstream
from cocola_llm_gateway.upstream.openai_embeddings import (
    OpenAIEmbeddingsConfig,
    OpenAIEmbeddingsUpstream,
)
from cocola_llm_gateway.upstream.openai_responses import (
    OpenAIResponsesConfig,
    OpenAIResponsesUpstream,
)
from cocola_llm_gateway.upstream.responses_base import ResponsesProvider

log = get_logger("cocola.llm-gateway.config")


def read_secret_env(name: str) -> str:
    """Resolve a secret via the "_FILE" indirection convention, else plain env.

    If "<name>_FILE" is set, the secret is read from that file path (a trailing
    newline is trimmed, since templating tools commonly append one); otherwise
    the "<name>" env var is used. This is the only seam the gateway needs
    to be Vault-ready WITHOUT a Vault SDK dependency (ADR-0008 §5): a Vault Agent
    Sidecar renders the secret to e.g. /vault/secrets/auth_secret and the
    operator points "<name>_FILE" at it, so the app just reads a file. With no
    "_FILE" set, behavior is identical to os.getenv, so the dev .env flow is
    unchanged. An explicitly configured but unreadable file is a startup error;
    silently switching to an environment value could change identity or
    encryption keys.
    """
    path = os.getenv(name + "_FILE", "").strip()
    if path:
        try:
            with open(path, encoding="utf-8") as fh:
                return fh.read().rstrip("\r\n")
        except OSError as exc:
            raise RuntimeError(f"{name}_FILE is unreadable: {path}") from exc
    return os.getenv(name, "")


@dataclass
class GatewayConfig:
    host: str = "127.0.0.1"
    port: int = 8080
    # Resilience knobs (consumed by middleware in the next task).
    request_timeout_s: float = 600.0
    max_retries: int = 2
    rate_limit_rps: float = 0.0  # 0 disables rate limiting


def _build_from_dict(spec: dict) -> Registry:
    """Build a Registry from a parsed config dict.

    Expected shape:
      {
        "default_alias": "cocola-default",
        "providers": {
          "anthropic": {"type": "anthropic", "base_url": "...",
                        "api_key_env": "TEST_UPSTREAM_API_KEY"},
          "fake": {"type": "fake"}
        },
        "routes": {
          "claude-sonnet": {"provider": "anthropic", "real_model": "claude-3-5-sonnet-20241022",
                            "label": "Claude Sonnet",
                            "icon": {"type": "simple-icons", "slug": "anthropic"}}
        }
      }
    """
    providers: dict[str, UpstreamProvider] = {}
    responses_providers: dict[str, ResponsesProvider] = {}
    embeddings_providers: dict[str, EmbeddingsProvider] = {}
    provider_protocols: dict[str, tuple[str, ...]] = {}
    for name, pcfg in (spec.get("providers") or {}).items():
        provider = _build_provider(name, pcfg)
        if isinstance(provider, EmbeddingsProvider):
            embeddings_providers[name] = provider
            provider_protocols[name] = ("openai-embeddings",)
        elif isinstance(provider, ResponsesProvider):
            responses_providers[name] = provider
            provider_protocols[name] = ("openai-responses",)
        else:
            providers[name] = provider
            provider_protocols[name] = ("anthropic-messages",)

    routes: dict[str, ModelRoute] = {}
    for route_id, rcfg in (spec.get("routes") or {}).items():
        route_alias = str(rcfg.get("alias", route_id))
        routes[route_id] = ModelRoute(
            alias=route_alias,
            provider_name=rcfg["provider"],
            real_model=rcfg.get("real_model", route_alias),
            pricing=Pricing(
                input_per_1k=float(rcfg.get("input_per_1k", 0.0)),
                output_per_1k=float(rcfg.get("output_per_1k", 0.0)),
            ),
            protocols=provider_protocols.get(rcfg["provider"], ()),
            label=str(rcfg.get("label", route_alias)),
            icon={str(k): str(v) for k, v in (rcfg.get("icon") or {}).items()}
            if isinstance(rcfg.get("icon"), dict)
            else {},
            enabled=_cfg_bool(rcfg.get("enabled", True)),
            visible=_cfg_bool(rcfg.get("visible", True)),
            is_default=_cfg_bool(rcfg.get("is_default", False)),
            embedding_dimension=int(rcfg.get("embedding_dimension", 0)),
        )

    default_alias = spec.get("default_alias", "")
    if not default_alias and routes:
        default_alias = sorted(routes.keys())[0]
    return Registry(
        providers,
        routes,
        default_alias,
        responses_providers,
        embeddings_providers,
        memory_enabled=_cfg_bool(spec.get("memory_enabled", False)),
        memory_extraction_route_id=str(spec.get("memory_extraction_route_id") or ""),
        memory_embedding_route_id=str(spec.get("memory_embedding_route_id") or ""),
    )


def _resolve_secret(cfg: dict, inline_key: str, env_key_field: str) -> str:
    """Prefer an env var named by `<env_key_field>`; fall back to inline value.

    We strongly prefer the env-indirection form (api_key_env) so secrets never
    live in the config file.
    """
    env_name = cfg.get(env_key_field)
    if env_name:
        return read_secret_env(env_name)
    return cfg.get(inline_key, "")


def _build_provider(
    name: str, cfg: dict
) -> UpstreamProvider | ResponsesProvider | EmbeddingsProvider:
    ptype = cfg.get("type", name)
    if ptype == "fake":
        return FakeUpstream(reply=cfg.get("reply", ""), chunk_size=int(cfg.get("chunk_size", 4)))
    if ptype == "anthropic":
        return AnthropicUpstream(
            AnthropicConfig(
                base_url=cfg.get("base_url", AnthropicConfig.base_url),
                api_key=_resolve_secret(cfg, "api_key", "api_key_env"),
                anthropic_version=cfg.get("anthropic_version", AnthropicConfig.anthropic_version),
                timeout_s=float(cfg.get("timeout_s", AnthropicConfig.timeout_s)),
                stream=bool(cfg.get("stream", AnthropicConfig.stream)),
            )
        )
    if ptype == "openai_responses":
        return OpenAIResponsesUpstream(
            OpenAIResponsesConfig(
                base_url=cfg.get("base_url", OpenAIResponsesConfig.base_url),
                api_key=_resolve_secret(cfg, "api_key", "api_key_env"),
                timeout_s=float(cfg.get("timeout_s", OpenAIResponsesConfig.timeout_s)),
            )
        )
    if ptype == "openai_embeddings":
        return OpenAIEmbeddingsUpstream(
            OpenAIEmbeddingsConfig(
                base_url=cfg.get("base_url", OpenAIEmbeddingsConfig.base_url),
                api_key=_resolve_secret(cfg, "api_key", "api_key_env"),
                timeout_s=float(cfg.get("timeout_s", OpenAIEmbeddingsConfig.timeout_s)),
            )
        )
    raise CocolaError(ErrorCode.INVALID_ARGUMENT, f"unknown provider type '{ptype}'")


def gateway_config_from_env() -> GatewayConfig:
    return GatewayConfig(
        host=os.getenv("COCOLA_LLM_HOST", "127.0.0.1"),
        port=int(os.getenv("COCOLA_LLM_PORT", "8080")),
        request_timeout_s=float(os.getenv("COCOLA_LLM_TIMEOUT_SECS", "600")),
        max_retries=int(os.getenv("COCOLA_LLM_MAX_RETRIES", "2")),
        rate_limit_rps=float(os.getenv("COCOLA_LLM_RATE_LIMIT_RPS", "0")),
    )


# --------------------------------------------------------------------------- #
# M4: auth + quota configuration
# --------------------------------------------------------------------------- #


def auth_config_from_env() -> AuthConfig:
    """Build AuthConfig from env.

    COCOLA_AUTH_SECRET           required HS256 signing secret.
    COCOLA_AUTH_ISSUER           expected `iss` claim (default "cocola").
    COCOLA_AUTH_TOKEN_TTL_SECS   default lifetime when issuing (default 30d).
    """
    from cocola_llm_gateway.auth.identity import AuthConfig

    return AuthConfig(
        secret=read_secret_env("COCOLA_AUTH_SECRET").strip(),
        issuer=os.getenv("COCOLA_AUTH_ISSUER", "cocola").strip() or "cocola",
        default_ttl_s=int(os.getenv("COCOLA_AUTH_TOKEN_TTL_SECS", str(30 * 24 * 3600))),
        dev_allow_anonymous=False,
    )


def quota_policy_from_env() -> QuotaPolicy:
    """Build QuotaPolicy from env. A limit of 0 disables that layer.

    COCOLA_QUOTA_USER_DAILY_TOKENS     per-user daily token cap (0 => unlimited).
    COCOLA_QUOTA_TENANT_MONTHLY_TOKENS per-tenant monthly token cap (0 => unlimited).
    """
    from cocola_llm_gateway.quota.policy import QuotaPolicy

    return QuotaPolicy(
        user_daily_tokens=int(os.getenv("COCOLA_QUOTA_USER_DAILY_TOKENS", "0")),
        tenant_monthly_tokens=int(os.getenv("COCOLA_QUOTA_TENANT_MONTHLY_TOKENS", "0")),
    )


def _cfg_bool(value: object) -> bool:
    if value is None:
        return True
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        raw = value.strip().lower()
        return raw not in ("0", "false", "no", "off")
    return bool(value)
