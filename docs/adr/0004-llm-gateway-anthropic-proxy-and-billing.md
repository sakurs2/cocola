# ADR-0004: LLM gateway as an Anthropic-compatible proxy in front of the Claude Agent SDK, with a metering ledger

- Status: Accepted
- Date: 2026-06-09
- Deciders: @wangjiahui

## Context

M3 adds the model layer. Two decisions made earlier in the project frame every
choice here:

1. **cocola does not build its own agent loop.** The ReAct loop, tool calling,
   context/turn management, and sub-agent orchestration are delegated wholesale
   to the **Claude Code Agent SDK** (`claude_agent_sdk`). The SDK spawns the
   Claude Code CLI as a child process; that CLI is what actually opens HTTP
   connections to a model endpoint. cocola's job is _not_ to re-implement any of
   that — it is to sit between the SDK and the model so we can route, meter, and
   (later) bill every call.
2. **The platform must be self-hostable and vendor-swappable.** An operator must
   be able to point cocola at Anthropic, an OpenAI-compatible endpoint, or an
   internal inference service by editing config — never by changing code — and
   must be able to run the entire test suite with no model, no API key, and no
   network.

The hard part is the seam. The Claude Code CLI speaks exactly one wire format
and discovers its endpoint from exactly one place:

- it talks the **Anthropic Messages API** (`POST /v1/messages`, SSE streaming);
- it reads the endpoint and credential from the environment —
  `ANTHROPIC_BASE_URL` and `ANTHROPIC_API_KEY`.

So the gateway's public contract is dictated by the SDK, not chosen freely.

## Decision

### The gateway is an Anthropic-Messages-compatible HTTP proxy

cocola-llm-gateway exposes `POST /v1/messages` (Anthropic schema, SSE when
`stream:true`, JSON otherwise) plus `GET /healthz` and a debug `GET /v1/usage`.
The Claude Agent SDK is redirected to it purely by injecting
`ANTHROPIC_BASE_URL=http://<gateway>` and a cocola-issued `ANTHROPIC_API_KEY`
into the SDK's `env`. **Zero SDK code changes, zero monkeypatching** — the
redirection is one environment variable.

gRPC was considered for the gateway's front door (it is the transport for
sandbox-manager) and **deferred**: the SDK can only speak Anthropic HTTP, so a
gRPC front-end would serve no client today. The internal architecture is kept
gRPC-ready (a normalized event stream, see below) so a gRPC sibling front-end
can be added later without touching routing/billing.

### A normalized internal event vocabulary isolates the vendor schema

The Anthropic wire schema is confined to a single codec module
(`anthropic_codec`). Everything inside the gateway speaks a small normalized
vocabulary (`StreamEvent`: `MESSAGE_START / CONTENT_DELTA / MESSAGE_DELTA /
MESSAGE_STOP / ERROR`, carrying a `Usage`). The codec does three translations:

    Anthropic request JSON  --parse-->  ChatRequest        (normalized in)
    StreamEvent stream      --encode-->  Anthropic SSE      (normalized out)
    StreamEvent stream      --collect->  Anthropic JSON     (non-stream out)

Because routing, resilience, and billing only ever see `StreamEvent`, a second
front-end (e.g. an OpenAI-style `/v1/chat/completions`) is an additive sibling
codec, and a new upstream vendor never leaks its schema past its adapter.

### Pluggable upstreams behind an `UpstreamProvider` Protocol

Upstreams implement a single Protocol (`chat_stream(req) -> AsyncIterator[
StreamEvent]`, `aclose()`), mirroring M2's `SandboxProvider` seam. Three
adapters ship:

- **`FakeUpstream`** — deterministic, in-process, no network; the _only_ provider
  unit tests are allowed to use.
- **`AnthropicUpstream`** — passthrough to a real Anthropic-compatible endpoint;
  base URL + key are config-injected.
- **`OpenAICompatUpstream`** — reserved adapter proving the seam generalizes
  beyond Anthropic.

Providers stay **dumb**: one provider == one vendor call. All cross-cutting
behavior lives outside them (a standing project rule), so it applies uniformly
across vendors and is testable with the Fake.

### Model route registry / router

Each model route has an immutable route ID and a provider-scoped display alias
(for example, two providers may both expose `gpt-5`). The registry maps the
route ID to `(provider, alias, real upstream model id, per-1K-token pricing)`.
Callers send the route ID, so duplicate aliases never make routing ambiguous.
Legacy alias lookup is accepted only when exactly one compatible route matches;
otherwise resolution fails with `NOT_FOUND` instead of guessing.

Defaults are independent per wire protocol: `anthropic-messages` for Claude Code
and `openai-responses` for Codex. The registry never imports a concrete provider
class. The composition root (`config.py` / `bootstrap.py`) is the _only_ place
that constructs concrete providers and reads secrets.

### Resilience is middleware; metering is a hook — neither is in a provider

- **`ResilientStreamer`** wraps any provider stream with per-key rate limiting,
  a wall-clock timeout, and retry. **Streaming-retry correctness rule:** retry
  only _before the first byte_ is emitted. Once any content has streamed to the
  client we cannot replay it, so a mid-stream failure surfaces as a terminal
  `ERROR` event rather than a retry.
- **Metering** is a hook wrapped _around_ the stream in the service layer: it
  passes events through unchanged while accumulating `Usage`, then writes exactly
  one `UsageRecord` in a `finally`. A billing failure is logged and swallowed —
  it must never break the user's stream.

### Billing ledger: record-only in M3

Cost is computed per call (`tokens × configured price`) and recorded, but
**never charged** — quota enforcement is a separate concern. The current
production composition uses `PostgresLedger`; `MemoryLedger` remains only as a
hermetic test implementation. The earlier Redis-only ledger was removed after
Postgres became the durable accounting source.

### Hard testability constraint

- All upstream endpoints and API keys are **config/env-injected only** — no
  internal inference endpoint is ever hardcoded.
- Unit tests use `FakeUpstream` exclusively: no real model, no API key.
- HTTP is exercised in-process via `httpx.ASGITransport` — **no bound port**
  (the project forbids listening sockets in the sandbox). `test_server_http.py`
  and `test_server_auth_quota.py` drive `POST /v1/messages` through the gateway
  to `FakeUpstream` and assert the ledger recorded the billed call. (The old
  cross-package `scripts/llm-m3-e2e.py`, which drove the path through the
  now-decommissioned Route B `ClaudeAgentSDKProvider`, was removed with Route B;
  see ADR-0009「实现进展（2026-07-02）下线 Route B」.)

## Alternatives considered

- **Embed model calls directly in agent-runtime (no gateway).** Simplest, but
  there would be no single choke point for routing/metering/billing, and every
  call would bypass cost accounting. Rejected: a unified, swappable model layer
  is a day-one platform requirement.
- **A custom (non-Anthropic) gateway protocol.** Cleaner in the abstract, but the
  Claude Code CLI cannot speak it — it would require forking the SDK/CLI.
  Rejected: the SDK's wire format is a fixed external constraint.
- **gRPC front-end now.** No client can use it today (the SDK is HTTP-only). Kept
  as a future sibling instead, enabled by the normalized event stream.
- **Charge/enforce quota in M3.** Premature: identity and tenancy land in M4.
  Record-only now de-risks the metering plumbing without billing-correctness
  pressure.
- **Mid-stream retry / replay.** Would corrupt an already-partially-sent stream.
  Rejected in favor of the pre-first-byte-only rule.
- **Float cost counters in Redis.** Lose precision under `HINCRBYFLOAT`/rounding
  (a real bug we hit at milli-USD). Rejected for integer micro-USD.

## Consequences

- **Positive:** the Claude Agent SDK runs unmodified against cocola via one env
  var; every model call flows through one routing + metering choke point;
  vendors are swappable by config; the whole suite runs hermetically (no model,
  no key, no port); the normalized event stream leaves room for gRPC and an
  OpenAI front-end as additive work.
- **Negative:** the gateway is bound to the Anthropic Messages schema on its
  public edge for now; M3 flattens non-text content blocks (tool_use/image) to
  text in the codec — a documented limitation to be lifted with richer
  passthrough; cost is recorded but not enforced until M4.
- **Follow-ups:** real identity/tenancy + quota enforcement and debiting (M4);
  richer content-block passthrough; export gateway metrics via Prometheus/OTel;
  optional gRPC and OpenAI-compatible front-ends.
