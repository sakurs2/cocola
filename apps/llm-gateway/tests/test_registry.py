import pytest
from cocola_common import CocolaError, ErrorCode
from cocola_llm_gateway.config import _build_from_dict
from cocola_llm_gateway.registry import ModelRoute, Pricing, Registry
from cocola_llm_gateway.upstream.fake import FakeUpstream


def _reg():
    fake = FakeUpstream()
    routes = {
        "fast": ModelRoute("fast", "fake", "fake-fast", Pricing(0.001, 0.002)),
        "smart": ModelRoute("smart", "fake", "fake-smart", Pricing(0.003, 0.015)),
    }
    return Registry({"fake": fake}, routes, default_alias="fast")


def test_pricing_cost():
    p = Pricing(input_per_1k=0.003, output_per_1k=0.015)
    assert p.cost(1000, 1000) == pytest.approx(0.018)
    assert p.cost(3, 6) == pytest.approx(3 / 1000 * 0.003 + 6 / 1000 * 0.015)


def test_resolve_explicit_alias():
    reg = _reg()
    route, provider = reg.resolve("smart")
    assert route.real_model == "fake-smart"
    assert provider.name == "fake"


def test_resolve_falls_back_to_default():
    reg = _reg()
    route, _ = reg.resolve(None)
    assert route.alias == "fast"


def test_resolve_unknown_raises_not_found():
    reg = _reg()
    with pytest.raises(CocolaError) as ei:
        reg.resolve("nope")
    assert ei.value.code is ErrorCode.NOT_FOUND


def test_disabled_alias_is_hidden_and_not_resolvable():
    fake = FakeUpstream()
    routes = {
        "fast": ModelRoute("fast", "fake", "fake-fast"),
        "off": ModelRoute("off", "fake", "fake-off", enabled=False),
    }
    reg = Registry({"fake": fake}, routes, default_alias="fast")

    assert reg.aliases() == ["fast"]
    with pytest.raises(CocolaError) as ei:
        reg.resolve("off")
    assert ei.value.code is ErrorCode.NOT_FOUND


def test_bad_default_alias_rejected():
    with pytest.raises(CocolaError):
        Registry({"fake": FakeUpstream()}, {}, default_alias="missing")


def test_route_with_unknown_provider_rejected():
    routes = {"x": ModelRoute("x", "ghost", "m")}
    with pytest.raises(CocolaError):
        Registry({"fake": FakeUpstream()}, routes, default_alias="x")


async def test_openai_responses_provider_is_isolated_from_chat_routes():
    registry = _build_from_dict(
        {
            "default_alias": "codex",
            "providers": {
                "responses": {
                    "type": "openai_responses",
                    "base_url": "https://example.invalid/v1",
                    "api_key": "test-only-key",
                }
            },
            "routes": {
                "codex": {
                    "provider": "responses",
                    "real_model": "gpt-real",
                }
            },
        }
    )
    try:
        route, _ = registry.resolve_responses("codex")
        assert route.real_model == "gpt-real"
        assert route.protocols == ("openai-responses",)
        with pytest.raises(CocolaError) as error:
            registry.resolve_chat("codex")
        assert error.value.code is ErrorCode.NOT_FOUND
    finally:
        await registry.aclose()
