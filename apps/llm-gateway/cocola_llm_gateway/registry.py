"""Model registry + router.

Callers (and the Claude Agent SDK) ask for a model by a *caller-facing alias*
(e.g. "claude-sonnet", "cocola-fast"). The registry maps that alias to:
  - which UpstreamProvider handles it,
  - the *real* upstream model id to send on the wire,
  - the per-1K-token price used by billing.

This indirection is what lets ops re-point an alias from one vendor/model to
another by editing config — zero code change — and is the single source of truth
both routing and billing read from.

Resolution order for an incoming request:
  1. explicit request alias (if known),
  2. otherwise the configured default alias,
  3. unknown alias -> NOT_FOUND (callers must not silently get a wrong model).

The registry holds provider *instances*; the composition root (config.build_*)
constructs the concrete providers and hands them in. The registry never imports
a concrete provider class — it depends only on the UpstreamProvider Protocol.
"""
from __future__ import annotations

from dataclasses import dataclass, field

from cocola_common import CocolaError, ErrorCode
from cocola_llm_gateway.upstream.base import UpstreamProvider


@dataclass(frozen=True)
class Pricing:
    """USD price per 1,000 tokens. Cost is computed but never charged in M3."""

    input_per_1k: float = 0.0
    output_per_1k: float = 0.0

    def cost(self, prompt_tokens: int, completion_tokens: int) -> float:
        return (
            prompt_tokens / 1000.0 * self.input_per_1k
            + completion_tokens / 1000.0 * self.output_per_1k
        )


@dataclass(frozen=True)
class ModelRoute:
    """A resolved routing decision for one alias."""

    alias: str
    provider_name: str
    real_model: str
    pricing: Pricing = field(default_factory=Pricing)


class Registry:
    def __init__(
        self,
        providers: dict[str, UpstreamProvider],
        routes: dict[str, ModelRoute],
        default_alias: str,
    ):
        if default_alias and default_alias not in routes:
            raise CocolaError(
                ErrorCode.INVALID_ARGUMENT,
                f"default_alias '{default_alias}' has no route",
            )
        # Validate every route points at a registered provider.
        for r in routes.values():
            if r.provider_name not in providers:
                raise CocolaError(
                    ErrorCode.INVALID_ARGUMENT,
                    f"route '{r.alias}' references unknown provider '{r.provider_name}'",
                )
        self._providers = providers
        self._routes = routes
        self._default_alias = default_alias

    @property
    def default_alias(self) -> str:
        return self._default_alias

    def aliases(self) -> list[str]:
        return sorted(self._routes.keys())

    def resolve(self, requested_alias: str | None) -> tuple[ModelRoute, UpstreamProvider]:
        """Resolve an alias (or the default) to a route + provider instance."""
        alias = (requested_alias or "").strip() or self._default_alias
        route = self._routes.get(alias)
        if route is None:
            raise CocolaError(
                ErrorCode.NOT_FOUND,
                f"unknown model alias '{alias}'; known: {', '.join(self.aliases()) or '(none)'}",
            )
        provider = self._providers[route.provider_name]
        return route, provider

    async def aclose(self) -> None:
        for p in self._providers.values():
            await p.aclose()
