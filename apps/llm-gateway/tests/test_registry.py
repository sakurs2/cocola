import pytest
from cocola_common import CocolaError, ErrorCode
from cocola_llm_gateway.config import _build_from_dict
from cocola_llm_gateway.registry import ModelRoute, Pricing, Registry
from cocola_llm_gateway.upstream.fake import FakeUpstream


class FakeResponses:
    async def create_response(self, payload):
        return payload

    async def stream_response(self, payload):
        if False:
            yield b""

    async def aclose(self):
        return None


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


def test_duplicate_aliases_route_by_id_and_never_guess_provider():
    routes = {
        "route-a": ModelRoute("shared", "a", "real-a"),
        "route-b": ModelRoute("shared", "b", "real-b"),
    }
    reg = Registry({"a": FakeUpstream(), "b": FakeUpstream()}, routes, default_alias="")

    assert reg.resolve_chat("route-a")[0].real_model == "real-a"
    assert reg.resolve_chat("route-b")[0].real_model == "real-b"
    with pytest.raises(CocolaError) as error:
        reg.resolve_chat("shared")
    assert error.value.code is ErrorCode.NOT_FOUND


def test_defaults_and_legacy_alias_resolution_are_scoped_to_protocol():
    routes = {
        "chat-route": ModelRoute("shared", "chat", "chat-real", is_default=True),
        "responses-route": ModelRoute(
            "shared",
            "responses",
            "responses-real",
            protocols=("openai-responses",),
            is_default=True,
        ),
    }
    reg = Registry(
        {"chat": FakeUpstream()},
        routes,
        default_alias="",
        responses_providers={"responses": FakeResponses()},
    )

    assert reg.resolve_chat(None)[0].real_model == "chat-real"
    assert reg.resolve_responses(None)[0].real_model == "responses-real"
    assert reg.resolve_chat("shared")[0].real_model == "chat-real"
    assert reg.resolve_responses("shared")[0].real_model == "responses-real"


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
