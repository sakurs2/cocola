"""Composition root: build providers + registry from configuration.

This is the ONLY module that imports concrete UpstreamProvider classes. Business
logic (server, router, billing) stays provider-agnostic; here we read config and
wire concrete instances together.

Two config sources, in order of precedence:
  1. A YAML/JSON file pointed to by COCOLA_LLM_CONFIG (richest: multiple aliases,
     per-alias pricing, multiple providers).
  2. Environment variables (zero-file quick start), e.g.:
       COCOLA_LLM_PROVIDER=fake|anthropic|openai_compat
       COCOLA_LLM_DEFAULT_ALIAS=cocola-default
       COCOLA_ANTHROPIC_API_KEY / COCOLA_ANTHROPIC_BASE_URL / ..._MODEL
       COCOLA_OPENAI_API_KEY / COCOLA_OPENAI_BASE_URL / ..._MODEL

If nothing is configured, we default to a single Fake provider so the gateway
boots and is testable out of the box (and never accidentally points at a real
endpoint).

HARD CONSTRAINT (ADR-0004): no endpoint/key is hardcoded; all come from config.
"""

from __future__ import annotations

import json
import os
import pathlib
from dataclasses import dataclass
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from cocola_llm_gateway.auth.identity import AuthConfig
    from cocola_llm_gateway.quota.policy import QuotaPolicy

from cocola_common import CocolaError, ErrorCode, get_logger

from cocola_llm_gateway.registry import ModelRoute, Pricing, Registry
from cocola_llm_gateway.upstream.anthropic import AnthropicConfig, AnthropicUpstream
from cocola_llm_gateway.upstream.base import UpstreamProvider
from cocola_llm_gateway.upstream.fake import FakeUpstream
from cocola_llm_gateway.upstream.openai_compat import OpenAICompatConfig, OpenAICompatUpstream

log = get_logger("cocola.llm-gateway.config")


def read_secret_env(name: str) -> str:
    """Resolve a secret via the "_FILE" indirection convention, else plain env.

    If "<name>_FILE" is set, the secret is read from that file path (a trailing
    newline is trimmed, since templating tools commonly append one); otherwise
    we fall back to the "<name>" env var. This is the only seam the gateway needs
    to be Vault-ready WITHOUT a Vault SDK dependency (ADR-0008 §5): a Vault Agent
    Sidecar renders the secret to e.g. /vault/secrets/auth_secret and the
    operator points "<name>_FILE" at it, so the app just reads a file. With no
    "_FILE" set, behavior is identical to os.getenv, so the dev .env flow is
    unchanged. An unreadable file degrades to the env fallback rather than
    crashing on a transient mount gap.
    """
    path = os.getenv(name + "_FILE", "").strip()
    if path:
        try:
            with open(path, encoding="utf-8") as fh:
                return fh.read().rstrip("\r\n")
        except OSError:
            log.warning("secret file unreadable; falling back to env", name=name, path=path)
    return os.getenv(name, "")


@dataclass
class GatewayConfig:
    host: str = "127.0.0.1"
    port: int = 8080
    # Resilience knobs (consumed by middleware in the next task).
    request_timeout_s: float = 90.0
    max_retries: int = 2
    rate_limit_rps: float = 0.0  # 0 disables rate limiting


def _build_from_dict(spec: dict) -> Registry:
    """Build a Registry from a parsed config dict.

    Expected shape:
      {
        "default_alias": "cocola-default",
        "providers": {
          "anthropic": {"type": "anthropic", "base_url": "...",
                        "api_key_env": "COCOLA_ANTHROPIC_API_KEY"},
          "fake": {"type": "fake"}
        },
        "routes": {
          "claude-sonnet": {"provider": "anthropic", "real_model": "claude-3-5-sonnet-20241022",
                            "runtime": "claude-code", "label": "Claude Sonnet",
                            "icon": {"type": "simple-icons", "slug": "anthropic"}}
        }
      }
    """
    providers: dict[str, UpstreamProvider] = {}
    for name, pcfg in (spec.get("providers") or {}).items():
        providers[name] = _build_provider(name, pcfg)

    routes: dict[str, ModelRoute] = {}
    for alias, rcfg in (spec.get("routes") or {}).items():
        routes[alias] = ModelRoute(
            alias=alias,
            provider_name=rcfg["provider"],
            real_model=rcfg.get("real_model", alias),
            pricing=Pricing(
                input_per_1k=float(rcfg.get("input_per_1k", 0.0)),
                output_per_1k=float(rcfg.get("output_per_1k", 0.0)),
            ),
            runtime=str(rcfg.get("runtime", "claude-code")),
            label=str(rcfg.get("label", alias)),
            icon={str(k): str(v) for k, v in (rcfg.get("icon") or {}).items()}
            if isinstance(rcfg.get("icon"), dict)
            else {},
            enabled=_cfg_bool(rcfg.get("enabled", True)),
            visible=_cfg_bool(rcfg.get("visible", True)),
        )

    default_alias = spec.get("default_alias", "")
    if not default_alias and routes:
        default_alias = sorted(routes.keys())[0]
    return Registry(providers, routes, default_alias)


def _resolve_secret(cfg: dict, inline_key: str, env_key_field: str) -> str:
    """Prefer an env var named by `<env_key_field>`; fall back to inline value.

    We strongly prefer the env-indirection form (api_key_env) so secrets never
    live in the config file.
    """
    env_name = cfg.get(env_key_field)
    if env_name:
        return read_secret_env(env_name)
    return cfg.get(inline_key, "")


def _build_provider(name: str, cfg: dict) -> UpstreamProvider:
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
    if ptype == "openai_compat":
        return OpenAICompatUpstream(
            OpenAICompatConfig(
                base_url=cfg.get("base_url", OpenAICompatConfig.base_url),
                api_key=_resolve_secret(cfg, "api_key", "api_key_env"),
                timeout_s=float(cfg.get("timeout_s", OpenAICompatConfig.timeout_s)),
            )
        )
    raise CocolaError(ErrorCode.INVALID_ARGUMENT, f"unknown provider type '{ptype}'")


def load_registry() -> Registry:
    """Load a Registry from COCOLA_LLM_CONFIG file, else from env, else Fake."""
    path = _llm_config_path()
    if path:
        with open(path, encoding="utf-8") as fh:
            spec = json.load(fh) if path.endswith(".json") else _load_yaml(fh.read())
        log.info("llm registry loaded from file", path=path)
        return _build_from_dict(spec)

    return _build_from_env()


def _llm_config_path() -> str:
    explicit = os.getenv("COCOLA_LLM_CONFIG", "").strip()
    if explicit:
        explicit_path = pathlib.Path(explicit)
        if explicit_path.is_absolute():
            return explicit
        repo_root = pathlib.Path(__file__).resolve().parents[3]
        for candidate in (pathlib.Path.cwd() / explicit_path, repo_root / explicit_path):
            if candidate.exists():
                return str(candidate)
        return explicit
    repo_root = pathlib.Path(__file__).resolve().parents[3]
    for candidate in (
        pathlib.Path.cwd() / "deploy" / "llm-config.json",
        repo_root / "deploy" / "llm-config.json",
    ):
        if candidate.exists():
            return str(candidate)
    return ""


def _build_from_env() -> Registry:
    provider = os.getenv("COCOLA_LLM_PROVIDER", "fake").strip()
    default_alias = os.getenv("COCOLA_LLM_DEFAULT_ALIAS", "cocola-default").strip()

    if provider == "fake":
        spec = {
            "default_alias": default_alias,
            "providers": {"fake": {"type": "fake"}},
            "routes": {default_alias: {"provider": "fake", "real_model": "fake-1"}},
        }
        log.info("llm registry: fake provider (no real endpoint)", default_alias=default_alias)
        return _build_from_dict(spec)

    if provider == "anthropic":
        spec = {
            "default_alias": default_alias,
            "providers": {
                "anthropic": {
                    "type": "anthropic",
                    "base_url": os.getenv("COCOLA_ANTHROPIC_BASE_URL", AnthropicConfig.base_url),
                    "api_key_env": "COCOLA_ANTHROPIC_API_KEY",
                    # Default ON: upstream SSE streaming is the primary path
                    # (verified healthy). Set COCOLA_ANTHROPIC_STREAM=0 to fall
                    # back to non-stream + locally synthesized events if a relay's
                    # SSE endpoint breaks again (see anthropic.py).
                    "stream": _envflag("COCOLA_ANTHROPIC_STREAM", default=True),
                }
            },
            "routes": {
                default_alias: {
                    "provider": "anthropic",
                    "real_model": os.getenv("COCOLA_ANTHROPIC_MODEL", "claude-3-5-sonnet-20241022"),
                    "input_per_1k": float(os.getenv("COCOLA_ANTHROPIC_IN_PER_1K", "0.003")),
                    "output_per_1k": float(os.getenv("COCOLA_ANTHROPIC_OUT_PER_1K", "0.015")),
                }
            },
        }
        return _build_from_dict(spec)

    if provider == "openai_compat":
        spec = {
            "default_alias": default_alias,
            "providers": {
                "openai_compat": {
                    "type": "openai_compat",
                    "base_url": os.getenv("COCOLA_OPENAI_BASE_URL", OpenAICompatConfig.base_url),
                    "api_key_env": "COCOLA_OPENAI_API_KEY",
                }
            },
            "routes": {
                default_alias: {
                    "provider": "openai_compat",
                    "real_model": os.getenv("COCOLA_OPENAI_MODEL", "gpt-4o-mini"),
                    "input_per_1k": float(os.getenv("COCOLA_OPENAI_IN_PER_1K", "0.0")),
                    "output_per_1k": float(os.getenv("COCOLA_OPENAI_OUT_PER_1K", "0.0")),
                }
            },
        }
        return _build_from_dict(spec)

    raise CocolaError(ErrorCode.INVALID_ARGUMENT, f"unknown COCOLA_LLM_PROVIDER '{provider}'")


def _load_yaml(text: str) -> dict:
    try:
        import yaml  # type: ignore
    except ModuleNotFoundError as e:  # pragma: no cover - optional dep
        raise CocolaError(
            ErrorCode.INVALID_ARGUMENT,
            "YAML config requires pyyaml; use a .json config or install pyyaml",
            cause=e,
        ) from e
    return yaml.safe_load(text)


def gateway_config_from_env() -> GatewayConfig:
    return GatewayConfig(
        host=os.getenv("COCOLA_LLM_HOST", "127.0.0.1"),
        port=int(os.getenv("COCOLA_LLM_PORT", "8080")),
        request_timeout_s=float(os.getenv("COCOLA_LLM_TIMEOUT_SECS", "90")),
        max_retries=int(os.getenv("COCOLA_LLM_MAX_RETRIES", "2")),
        rate_limit_rps=float(os.getenv("COCOLA_LLM_RATE_LIMIT_RPS", "0")),
    )


# --------------------------------------------------------------------------- #
# M4: auth + quota configuration
# --------------------------------------------------------------------------- #


def auth_config_from_env() -> AuthConfig:
    """Build AuthConfig from env.

    COCOLA_AUTH_SECRET           HS256 signing secret. Empty => auth disabled
                                 (everyone resolves to the dev identity).
    COCOLA_AUTH_ISSUER           expected `iss` claim (default "cocola").
    COCOLA_AUTH_TOKEN_TTL_SECS   default lifetime when issuing (default 30d).
    COCOLA_AUTH_DEV_ANON         "1"/"true" => blank token -> dev identity even
                                 when a secret is set (local convenience only).
    """
    from cocola_llm_gateway.auth.identity import AuthConfig

    return AuthConfig(
        secret=read_secret_env("COCOLA_AUTH_SECRET").strip(),
        issuer=os.getenv("COCOLA_AUTH_ISSUER", "cocola").strip() or "cocola",
        default_ttl_s=int(os.getenv("COCOLA_AUTH_TOKEN_TTL_SECS", str(30 * 24 * 3600))),
        dev_allow_anonymous=_envflag("COCOLA_AUTH_DEV_ANON"),
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


def _envflag(name: str, *, default: bool = False) -> bool:
    raw = os.getenv(name, "").strip().lower()
    if not raw:
        return default
    return raw in ("1", "true", "yes", "on")


def _cfg_bool(value: object) -> bool:
    if value is None:
        return True
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        raw = value.strip().lower()
        return raw not in ("0", "false", "no", "off")
    return bool(value)
