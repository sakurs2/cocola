"""Model registry + router.

Callers ask for a model by an immutable route id. The registry maps that id to:
  - the provider-scoped display alias,
  - which UpstreamProvider handles it,
  - the *real* upstream model id to send on the wire,
  - the per-1K-token price used by billing.

This indirection is what lets ops re-point an alias from one vendor/model to
another by editing config — zero code change — and is the single source of truth
both routing and billing read from.

Legacy aliases remain resolvable only when exactly one compatible route uses
that alias. This keeps pre-migration sessions working without guessing between
two providers that intentionally expose the same alias.

The registry holds provider *instances*; the composition root (config.build_*)
constructs the concrete providers and hands them in. The registry never imports
a concrete provider class — it depends only on the UpstreamProvider Protocol.
"""

from __future__ import annotations

from dataclasses import dataclass, field

from cocola_common import CocolaError, ErrorCode

from cocola_llm_gateway.upstream.base import UpstreamProvider
from cocola_llm_gateway.upstream.responses_base import ResponsesProvider


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
    """A resolved provider decision; the containing registry key is its route id."""

    alias: str
    provider_name: str
    real_model: str
    pricing: Pricing = field(default_factory=Pricing)
    protocols: tuple[str, ...] = ("anthropic-messages",)
    label: str = ""
    icon: dict[str, str] = field(default_factory=dict)
    enabled: bool = True
    visible: bool = True
    is_default: bool = False


class Registry:
    def __init__(
        self,
        providers: dict[str, UpstreamProvider],
        routes: dict[str, ModelRoute],
        default_alias: str,
        responses_providers: dict[str, ResponsesProvider] | None = None,
    ):
        if (
            default_alias
            and default_alias not in routes
            and not any(route.alias == default_alias for route in routes.values())
        ):
            raise CocolaError(
                ErrorCode.INVALID_ARGUMENT,
                f"default_alias '{default_alias}' has no route",
            )
        responses_providers = responses_providers or {}
        # Validate every route points at a registered provider of its protocol.
        for r in routes.values():
            known = r.provider_name in providers or r.provider_name in responses_providers
            if not known:
                raise CocolaError(
                    ErrorCode.INVALID_ARGUMENT,
                    f"route '{r.alias}' references unknown provider '{r.provider_name}'",
                )
        self._providers = providers
        self._responses_providers = responses_providers
        self._routes = routes
        self._default_alias = default_alias
        self._default_route_ids: dict[str, str] = {}
        for route_id, route in routes.items():
            if route.is_default:
                for protocol in route.protocols:
                    self._default_route_ids[protocol] = route_id
        if default_alias:
            exact = routes.get(default_alias)
            if exact is not None:
                for protocol in exact.protocols:
                    self._default_route_ids.setdefault(protocol, default_alias)
            else:
                protocols = {protocol for route in routes.values() for protocol in route.protocols}
                for protocol in protocols:
                    matches = [
                        route_id
                        for route_id, route in routes.items()
                        if route.alias == default_alias and protocol in route.protocols
                    ]
                    if len(matches) == 1:
                        self._default_route_ids.setdefault(protocol, matches[0])

    @property
    def default_alias(self) -> str:
        return self._default_alias

    def aliases(self) -> list[str]:
        return sorted(alias for alias, route in self._routes.items() if route.enabled)

    def _route(self, requested_route_id: str | None, protocol: str) -> ModelRoute:
        route_id = (requested_route_id or "").strip() or self._default_route_ids.get(protocol, "")
        route = self._routes.get(route_id)
        if route is None and route_id:
            matches = [
                candidate
                for candidate in self._routes.values()
                if candidate.enabled
                and candidate.alias == route_id
                and protocol in candidate.protocols
            ]
            if len(matches) == 1:
                route = matches[0]
        if route is None or not route.enabled:
            raise CocolaError(
                ErrorCode.NOT_FOUND,
                f"unknown model route '{route_id}'; known: {', '.join(self.aliases()) or '(none)'}",
            )
        return route

    def resolve_chat(self, requested_route_id: str | None) -> tuple[ModelRoute, UpstreamProvider]:
        route = self._route(requested_route_id, "anthropic-messages")
        provider = self._providers.get(route.provider_name)
        if provider is None or "anthropic-messages" not in route.protocols:
            raise CocolaError(
                ErrorCode.NOT_FOUND, f"model alias '{route.alias}' is not chat compatible"
            )
        return route, provider

    def resolve_responses(
        self, requested_route_id: str | None
    ) -> tuple[ModelRoute, ResponsesProvider]:
        route = self._route(requested_route_id, "openai-responses")
        provider = self._responses_providers.get(route.provider_name)
        if provider is None or "openai-responses" not in route.protocols:
            raise CocolaError(
                ErrorCode.NOT_FOUND, f"model alias '{route.alias}' is not Responses compatible"
            )
        return route, provider

    # Existing normalized-chat callers keep the concise name.
    resolve = resolve_chat

    async def aclose(self) -> None:
        for p in self._providers.values():
            await p.aclose()
        for p in self._responses_providers.values():
            await p.aclose()
