# ADR-0007: Gateway BFF + agent-runtime gRPC server (backend MVP)

- Status: Accepted
- Date: 2026-06-09
- Deciders: @cocola-maintainers

## Context

M0–M5 built every piece of the backend in isolation: agent-runtime had an
`AgentProvider` (Claude Agent SDK) and a `SkillCatalog`, llm-gateway proxied
Anthropic traffic and metered tokens, sandbox-manager orchestrated containers,
and admin-api managed tokens / quotas / skills. What was missing was the seam
that joins them into a single request the frontend can call: a public entry that
authenticates a browser, forwards a prompt to the agent, and streams events back.

Two gaps had to close to reach a backend MVP:

1. **agent-runtime had no network surface.** It was a library + an M0 banner
   `__main__`. The proto already declared `AgentRuntimeService.Query` as a
   *server-streaming* RPC, but nothing served it.
2. **gateway was an M0 stub.** It needed to terminate HTTP, verify cocola
   tokens, dial agent-runtime over gRPC, and relay the event stream to the
   client.

Forces: keep the hot path fast (offline token verification, no per-request
network auth hop); do not reinvent code that already exists (the HS256 token
codec); keep the streaming transport dependency-free; keep both new surfaces
unit-testable without standing up real servers.

## Decision

**agent-runtime serves the proto `AgentRuntimeService` via `grpc.aio`.** A thin
`AgentRuntimeServicer` adapts the existing `AgentProvider` + `SkillCatalog` to
the generated stubs: it maps `QueryRequest` → `AgentOptions`, folds enabled
skills into the options, and streams each generic `AgentEvent` out as the proto
`AgentEvent` (a `kind` string + a flat `map<string,string>`; non-string values
are JSON/str-flattened). The servicer depends only on the two Protocols, so
production injects the concrete Claude SDK provider / admin skill catalog while
tests inject fakes. `__main__` is the composition root and falls back to
`EchoProvider` when no LLM is configured.

**gateway is a BFF that streams over SSE.** It exposes `POST /v1/chat`, verifies
the bearer token, dials agent-runtime, and relays each event as an SSE frame
(`event: <kind>\ndata: <json>\n\n`). The forwarded `user_id` always comes from
the verified token, never from the request body, so a caller cannot impersonate
another user. The agent client is hidden behind a narrow `Streamer` interface,
making the SSE handler testable with a fake.

**The HS256 token codec is promoted to `packages/go-common/token`.** admin-api
mints tokens and gateway verifies them with the *same* codec — one Go
implementation, byte-compatible with the Python gateway's `auth/jwt.py`.

## Alternatives Considered

- **WebSocket instead of SSE** — bidirectional and familiar, but the agent
  interaction is one-shot and unidirectional once started (POST a prompt, consume
  a stream until `done`). SSE is plain HTTP (chunked + flushed), needs no extra
  dependency, survives reverse proxies, and reconnects natively in the browser.
  WebSocket's duplex channel buys nothing here.
- **A third hand-rolled HS256 codec in gateway** — the codec lived in
  admin-api's `internal/token`, unreachable from the gateway module. Copying it a
  third time would risk drift between minting (admin-api) and verifying
  (gateway). Promoting the single file to go-common keeps exactly one codec.
- **gateway calls agent-runtime over HTTP** — would mean a second protocol on
  agent-runtime. The proto contract already specifies gRPC server-streaming, and
  internal service-to-service traffic is gRPC by ADR-0001; reusing it is free.
- **Verify tokens by calling admin-api per request** — adds a network hop to the
  hot path and couples the data plane to the control plane's availability.
  Offline HS256 verification keeps gateway stateless and horizontally scalable;
  the jti denylist remains the revocation mechanism (a follow-up wires it in).

## Consequences

- **Positive** — there is now a real end-to-end backend path
  (frontend → gateway → agent-runtime → llm-gateway/sandbox-manager → events).
  Both new surfaces have hermetic unit tests. One token codec is shared by every
  Go service, eliminating drift. SSE adds zero new dependencies.
- **Negative / accepted risk** — gateway↔agent-runtime is plaintext gRPC (internal
  network trust); TLS/mTLS is deferred to M6. Token *revocation* (jti denylist
  lookup) is not yet enforced on the gateway hot path — only signature + expiry +
  issuer are checked. SSE is one-directional, so mid-stream client→agent
  interrupts beyond a connection close are out of scope.
- **Followups** — wire the admin-api jti denylist into gateway verification;
  add gateway↔agent-runtime mTLS (M6); add an integration test that runs both
  servers over a loopback socket; surface per-request quota checks at the BFF.

## Addendum — real LLM链路 token passthrough (M4 收口 verified)

The real-LLM seam is now wired and proven hermetically. agent-runtime's
composition root flips from EchoProvider to `ClaudeAgentSDKProvider` when
`COCOLA_LLM_BASE_URL` is set; the provider injects `ANTHROPIC_BASE_URL` (→ the
cocola llm-gateway) and `ANTHROPIC_API_KEY` (→ the cocola-issued token) into the
SDK subprocess via `_build_env()` — pure env injection, zero SDK code changes.

The contract that closes M4: **the token the gateway verifies IS the token the
SDK presents.** `apps/llm-gateway/tests/test_token_passthrough_e2e.py` proves it
without a real SDK subprocess or a bound port (ADR-0004: FakeUpstream only). A
fake `query_fn` stands in for `claude_agent_sdk.query`; instead of spawning the
CLI it reads exactly `provider._build_env()` and drives the gateway's ASGI app
in-process, sending the token as `x-api-key`. The gateway verifies it, routes
through FakeUpstream, returns an Anthropic response, and the real provider maps
it back to generic `AgentEvent`s. The test asserts billing is attributed to the
token subject (the SDK key was the verified token) and that an unsigned token
surfaces as an error with no model call and no billing.

## Addendum — session↔sandbox binding in the Query path (step 3)

agent-runtime now binds a session to a real sandbox inside `Query`, instead of
merely passing through whatever `sandbox_id` the caller supplied. The sandbox
lifecycle itself was already closed in M2 on the sandbox-manager side (Acquire =
create-or-reuse + lease renew, Heartbeat, Release); step 3 reuses that contract
from the runtime.

- `SandboxBinder` (Protocol) is the only thing the servicer depends on — same
  composition-root pattern as `AgentProvider` / `SkillCatalog`.
  `SandboxManagerBinder` wraps the existing blocking `SandboxClient` and bridges
  it to the async server via `anyio.to_thread` (exactly as the client docstring
  foretold), opening a short-lived channel per call since Acquire is idempotent.
- In `Query`: if a binder is wired and the caller did not pin a sandbox, the
  servicer acquires one for the session, injects the real id into `AgentOptions`,
  and emits an observable `sandbox` event before any agent output. A caller-pinned
  `sandbox_id` is respected verbatim (no acquire). A bind failure becomes a
  terminal `error` event and the provider never runs — the agent does not execute
  without its sandbox.
- Composition root: `COCOLA_SANDBOX_ADDR` selects the binder; unset keeps the
  zero-config boot (sessions run with no bound sandbox), mirroring how
  `COCOLA_LLM_BASE_URL` / `COCOLA_ADMIN_BASE_URL` gate their features.
- **Deliberately deferred**: the runtime does not yet route the SDK's tool
  execution (bash/file IO) through the bound sandbox's `Exec`/`WriteFile`/
  `ReadFile`. That requires registering sandbox-backed tools with the Claude
  Agent SDK and is the next slice; binding the session is the prerequisite that
  makes those calls have a target. Release is also left to the M2 reaper/lease
  rather than forced at stream end, so a sandbox survives across a session's
  multiple `Query` turns.
