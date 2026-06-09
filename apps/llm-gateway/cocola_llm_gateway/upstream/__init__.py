"""Upstream model provider adapters.

The service layer imports only `UpstreamProvider` (the Protocol) and
`UpstreamError`. Concrete adapters are constructed by the composition root
(main / config), never imported directly by business logic.
"""
from cocola_llm_gateway.upstream.base import UpstreamProvider
from cocola_llm_gateway.upstream.errors import UpstreamError

__all__ = ["UpstreamProvider", "UpstreamError"]
