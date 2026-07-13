"""LLM Gateway.

An Anthropic-Messages-API-compatible HTTP proxy that sits between the Claude
Agent SDK and the actual model upstream. The SDK is redirected here purely via
`ANTHROPIC_BASE_URL`; every model call then flows through one choke point.

Responsibilities:
- Expose `POST /v1/messages` (Anthropic schema, SSE or JSON) + `GET /healthz`
  + debug `GET /v1/usage` + `GET /v1/quota`.
- Authenticate each call (M4): the cocola-signed token the SDK presents as
  `ANTHROPIC_API_KEY` is verified offline (HS256) into an Identity; bad/expired
  tokens get a 401. Auth is enforced only when a signing secret is configured.
- Enforce a per-period token quota (M4): a pre-call check rejects over-budget
  callers with 429; the real token total is committed after the call. This is a
  usage *budget* for internal employees — no money, no debiting.
- Normalize the vendor schema at the edge (anthropic_codec) so routing,
  resilience, billing, and quota only ever see a small internal StreamEvent
  vocabulary.
- Route an immutable route ID to (provider, alias, real model, pricing) via a
  config-driven registry — vendors are swappable without code changes.
- Stream resiliently (rate limit + timeout + pre-first-byte retry).
- Meter every call and record usage to a ledger (Memory or Redis).

See docs/adr/0004 (proxy + metering) and docs/adr/0005 (identity + quota).
"""
