from cocola_llm_gateway.config import GatewayConfig
from cocola_llm_gateway.middleware import ResiliencePolicy
from cocola_llm_gateway.upstream.anthropic import AnthropicConfig


def test_model_call_timeout_defaults_cover_long_agent_runs():
    assert GatewayConfig().request_timeout_s == 600
    assert ResiliencePolicy().timeout_s == 600
    assert AnthropicConfig().timeout_s == 600
