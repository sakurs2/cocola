# ADR-0005: Identity as a cocola-signed token (the SDK's API key) + period-windowed token quota

- Status: Accepted
- Date: 2026-06-09
- Deciders: @wangjiahui

## Context

M3 left the model layer working but anonymous: identity was a mock request
header (`x-cocola-user` / `x-cocola-session`) and the billing ledger _recorded_
token usage without ever _limiting_ it. M4 makes the gateway know **who** is
calling and **stop** a caller who has burned through their budget.

Two constraints shape the design:

1. **cocola is deployed internally for employees, not sold.** There is no money,
   no balance, no invoicing. The only thing we must enforce is a **token
   budget** — "this employee/team may use at most N tokens per period". So M4 is
   _quota_, deliberately **not** billing/debiting. (This narrows the original M4
   scope, which had assumed paid multi-tenancy.)

2. **The credential the gateway actually receives is dictated by the Claude Code
   CLI**, exactly as in ADR-0004. The CLI authenticates with a single value it
   reads from the environment — `ANTHROPIC_API_KEY` — and sends it on every
   `POST /v1/messages` (as `x-api-key`). cocola already injects that env var when
   it launches the SDK (`ClaudeAgentSDKProvider._build_env`). So the natural
   place to put identity is _inside that value_: make the API key a
   cocola-signed token.

## Decision

### Identity is a cocola-issued, self-describing signed token

The value cocola sets as `ANTHROPIC_API_KEY` **is** a signed JWT (compact JWS).
Its claims carry the identity:

    sub -> user_id (employee)   ten -> tenant_id (team)   iat/exp -> validity

The gateway verifies the token **offline** with a shared **HS256** secret —
no per-request network call, no session store — and resolves it to an
`Identity(user_id, tenant_id)` that replaces the mock headers as the subject for
billing _and_ quota. An invalid/expired/missing token returns an
Anthropic-compatible **401** (`authentication_error`) so the SDK surfaces it
cleanly.

**Why hand-rolled HS256 instead of PyJWT?** Signing/verifying compact JWS is ~40
lines of stdlib `hmac`+`hashlib`; it adds no third-party auth surface to audit
and keeps the hermetic, dependency-light test story intact. The whole JWT
concern lives in one module (`auth/jwt.py`); if we ever need RS256/JWKS for
multi-issuer federation, that is the single file to swap.

**Why symmetric (shared secret) for now?** The only token issuer and the only
verifier are both cocola, co-deployed. Asymmetric keys buy nothing yet and cost
key distribution. Revisit if a third party ever needs to mint tokens.

Tokens are minted by an `Issuer` exposed through a CLI
(`python -m cocola_llm_gateway.issue_token --user … [--tenant …] [--ttl-days …]`).
A future admin-api HTTP endpoint will wrap the same `Issuer` for self-service.

### Quota is a period-windowed token counter, checked before and committed after

Quota is enforced in **two phases** around the model call, mirroring how token
cost actually becomes known:

- **`check(identity)` — before the call.** Read the caller's current-period
  counters; if a counter is already at/over its cap, raise `QuotaExceeded` →
  Anthropic-compatible **429** (`rate_limit_error`) **before** any upstream
  stream is opened.
- **`commit(identity, tokens)` — after the call**, from the same metering
  `finally` hook that writes the ledger record. Atomically add the real token
  total to the counters. Best-effort: a counter-write failure is logged and
  swallowed, exactly like a billing-write failure — quota must never break the
  user's response.

Because the exact cost of a request is unknown until it finishes, a request is
allowed to _start_ as long as the subject is under cap; the _next_ request is
then blocked. Overshoot is bounded by concurrency, not fixed at one: a single
serial caller overshoots by at most one request's tokens, but N requests racing
through `check` before any `commit` lands can overshoot by up to ~N requests'
worth. For an internal employee budget this is the right trade-off — it needs
**no pre-hold and no debiting**, just an integer counter and a comparison. If
tight per-request enforcement is ever required, the seam can move to an atomic
check-and-reserve.

Two independent layers, either of which is disabled when its limit is ≤ 0:

- **per-user, daily** — the primary guardrail.
- **per-tenant, monthly** — an optional team/department ceiling.

**Windows are self-rolling.** The period id (`YYYYMMDD` / `YYYYMM`, UTC) is
embedded in the counter key and the key is written with a TTL that expires
shortly after the window ends. A new day/month therefore reads a fresh zero
counter — **no cron, no reset job**.

### Storage mirrors the M3 ledger seam

A `QuotaStore` Protocol has a `MemoryQuotaStore` (hermetic default) and a
`RedisQuotaStore` (`INCRBY` + `EXPIRE` in one `MULTI/EXEC`, key
`cocola:quota:{scope}:{subject}:{period}`). The `Enforcer` (policy + store) is
the single object the service depends on; it is attached only when a quota layer
is enabled, so a no-cap deployment does zero quota work.

### Composition + back-compat

`bootstrap.py` builds a `Verifier` from `COCOLA_AUTH_*` and an `Enforcer` from
`COCOLA_QUOTA_*`. **Auth is enforced only when `COCOLA_AUTH_SECRET` is set**;
with no secret, auth is disabled and every caller resolves to a stable
`dev-user` identity — preserving the zero-config dev/CI boot. In that disabled
mode only, the legacy `x-cocola-user` / `x-cocola-tenant` headers are still
honored so existing dev flows attribute usage to a real subject. With auth
enabled the verified token is authoritative and those headers are ignored.

### Hard testability constraint (unchanged from ADR-0004)

Auth and quota are exercised with `MemoryLedger` + `MemoryQuotaStore` + a
`Verifier` over `httpx.ASGITransport` — no real model, no Redis, no bound port.
The cross-package e2e (`scripts/llm-m4-e2e.py`) mints a token, drives the SDK
provider through the gateway with a tiny cap, and asserts: authorized call is
billed to the **token subject**, the next call is blocked with **429**, and an
invalid token is rejected with **401**.

## Alternatives considered

- **A separate API-key store (opaque keys → DB lookup per request).** Requires a
  network/DB round-trip on the hot path and a key table to manage. Rejected: a
  self-describing signed token verifies offline and carries tenancy + expiry
  with no lookup.
- **Put identity in a custom header instead of the API key.** The Claude Code
  CLI only forwards `ANTHROPIC_API_KEY`; arbitrary custom auth headers are not a
  contract we control. Encoding identity into the key is the seam the SDK gives
  us.
- **PyJWT / authlib dependency.** More features (RS256, full claim validation)
  than we need for a symmetric, single-issuer setup; more surface to audit.
  Rejected for ~40 lines of stdlib, isolated to one swappable module.
- **Real debiting / pre-hold / balance.** This is an internal token budget, not
  billing. A pre-hold + settlement protocol guards against overspend that does
  not matter here. Rejected as unnecessary complexity (explicit user decision).
- **A scheduled reset job for quotas.** Fragile (missed runs, clock skew).
  Rejected for period-in-key + TTL, which rolls over with zero moving parts.
- **Asymmetric (RS256/JWKS) keys now.** No third-party issuer exists yet.
  Deferred; the one-module JWT design keeps it a cheap future swap.

## Consequences

- **Positive:** the gateway now authenticates every call offline via the token
  the SDK already carries (one env var, no SDK change); usage and quota are
  attributed to a real employee/team; over-budget callers are stopped with a
  spec-compliant 429 the SDK understands; quotas roll over automatically; the
  whole path stays hermetically testable; zero-config dev boots still work.
- **Negative:** a caller may overshoot its cap by up to the tokens of whatever
  requests are in flight (one for a serial caller, ~N under N-way concurrency;
  accepted by design); HS256 means the signing secret must be shared
  by issuer and gateway (fine while both are cocola); quota is configured
  statically via env today (no runtime/per-user override yet).
- **Follow-ups:**
  - **Dynamic, per-subject quota** (override the static env caps per user/tenant)
    backed by admin-api + PostgreSQL, with a Redis cache.
  - **Go admin-api token-issuance endpoint** wrapping the same `Issuer` for
    self-service minting + rotation/revocation.
  - **Revocation / rotation** (a denylist or short TTLs + refresh) — HS256 tokens
    are otherwise valid until `exp`.
  - **Per-tenant two-layer enforcement surfaced in the admin UI** (the monthly
    tenant ceiling exists in code; it needs configuration + reporting UX).
  - Optional **RS256/JWKS** if a non-cocola issuer is ever introduced.
