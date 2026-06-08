"""LLM Gateway.

Responsibilities (final shape; M0 ships only the package skeleton):
- Accept OpenAI-compatible /v1/chat/completions from agent-runtime
- Resolve tenant + user from inbound JWT (issued by gateway)
- Enforce per-tenant / per-user token quotas (Redis)
- Route to the cheapest healthy upstream that supports the requested model
- Record usage to ClickHouse for billing reconciliation
"""
