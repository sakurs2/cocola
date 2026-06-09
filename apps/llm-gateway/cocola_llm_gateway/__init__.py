"""LLM Gateway.

An Anthropic-Messages-API-compatible HTTP proxy that sits between the Claude
Agent SDK and the actual model upstream. The SDK is redirected here purely via
`ANTHROPIC_BASE_URL`; every model call then flows through one choke point.

Responsibilities:
- Expose `POST /v1/messages` (Anthropic schema, SSE or JSON) + `GET /healthz`
  + a debug `GET /v1/usage`.
- Normalize the vendor schema at the edge (anthropic_codec) so routing,
  resilience, and billing only ever see a small internal StreamEvent vocabulary.
- Route a caller-facing alias to (provider, real model, pricing) via a
  config-driven registry — vendors are swappable without code changes.
- Stream resiliently (rate limit + timeout + pre-first-byte retry).
- Meter every call and record usage to a ledger (Memory or Redis). Cost is
  computed but never charged in M3 (enforcement lands in M4+).

See docs/adr/0004 for the full rationale.
"""
