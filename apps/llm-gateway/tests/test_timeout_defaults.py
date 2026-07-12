from cocola_llm_gateway.config import GatewayConfig
from cocola_llm_gateway.middleware import ResiliencePolicy
from cocola_llm_gateway.upstream.anthropic import AnthropicConfig
from cocola_llm_gateway.upstream.openai_compat import OpenAICompatConfig


def test_model_call_timeout_defaults_cover_long_agent_runs():
    assert GatewayConfig().request_timeout_s == 300
    assert ResiliencePolicy().timeout_s == 300
    assert AnthropicConfig().timeout_s == 300
    assert OpenAICompatConfig().timeout_s == 300
