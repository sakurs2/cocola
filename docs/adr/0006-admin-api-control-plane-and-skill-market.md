# ADR-0006: Admin-api control plane (Go) + Skill-Market catalog, with token revocation and dynamic quota

- Status: Accepted
- Date: 2026-06-09
- Deciders: @wangjiahui

## Context

ADR-0005 (M4) gave the gateway identity (a cocola-signed token) and a token
quota, and explicitly deferred four follow-ups to "a future admin-api":

- a **token-issuance endpoint** wrapping the same `Issuer` for self-service
  minting + rotation,
- **revocation** (HS256 tokens are otherwise valid until `exp`),
- **dynamic, per-subject quota** overriding the static env caps, and
- a place to **curate capabilities** employees can turn on.

M5 builds that control plane. Where the **llm-gateway is the employee-facing
data plane** (one hot path: `POST /v1/messages`), the **admin-api is the
operator-facing control plane**: low-traffic, write-heavy, CRUD-shaped. It is a
**Go** service (per ADR-0001's Go-for-services split) and the home for the
Skill-Market catalog M5 introduces.

Three forces shape the design:

1. **The identity language is already fixed (ADR-0005).** Tokens are compact
   HS256 JWS with `sub/ten/iat/exp/iss`, verified offline by the Python gateway.
   Anything that mints tokens must produce **byte-identical** output the gateway
   already accepts — across a _language boundary_ (Go issuer → Python verifier).

2. **No persistence decision is due yet.** ADR (M7) owns the PostgreSQL/Redis
   tiering. M5 must not pre-empt it, but must not hard-code a backend either.

3. **This is an internal tool operated by a few people.** The admin surface does
   not need the same identity machinery as the employee surface; it needs to be
   correct and auditable, not richly multi-tenant.

## Decision

### A Go admin-api, layered store → service → http

Three internal layers, each with one job, mirroring the sandbox-manager
conventions:

- **`internal/store`** — the persistence seam. A single `Store` interface covers
  every admin resource (issued-token metadata + revocation, quota overrides,
  skills, audit), with an in-memory implementation (`NewMemory`) backing tests
  and zero-dependency dev boots. This funnels all access through one interface
  exactly like `go-common/redis.KV`, so the **PostgreSQL backend lands in M7
  behind the same interface with no service/handler change**.
- **`internal/service`** — the business layer. It composes the token `Issuer`
  and the store into operations, and is the **one place that writes the audit
  log**. Errors are typed sentinels (`ErrInvalidArg`/`ErrNotFound`/
  `ErrConflict`) the handler maps to HTTP status codes.
- **`internal/httpapi`** — a thin chi router: decode → call service → encode,
  plus three cross-cutting concerns (auth middleware, JSON error envelope,
  request decoding). No business logic.

### Token issuance/revocation, minted by a Go twin of the Python issuer

`internal/token` is a **hand-rolled HS256 codec in Go**, deliberately
byte-compatible with the gateway's `auth/jwt.py`: same header
`{"alg":"HS256","typ":"JWT"}`, same compact `b64url(header).b64url(payload).
b64url(sig)`, base64url **without padding**, constant-time compare on verify.
The same rationale as ADR-0005 applies (no third-party JWT surface; one module
to swap for RS256/JWKS later) — now it must also hold _across languages_. The
cross-language e2e (`scripts/admin-m5-e2e.py`) is the guard: a token **minted in
Go** is **verified in Python**, and tokens with a wrong secret or foreign issuer
are rejected. This is the M5 acceptance proof.

`POST /admin/tokens` mints and **persists only metadata** — the token string is
a bearer credential returned **once** and never stored. `DELETE /admin/tokens/
{id}` marks it revoked; `GET /admin/tokens/revoked` exposes the **denylist** the
gateway consults to reject revoked-but-unexpired tokens. This is the chosen
answer to ADR-0005's revocation follow-up: a **denylist keyed by an opaque token
id**, not key rotation. Rotation is just "issue new + revoke old" with the same
two endpoints.

> Update (denylist closed loop, 2026-06-09): the gateway now _consults_ the
> denylist on the hot path — see "Addendum" below. The remaining-half note that
> stood here (deferring consumption to M6) is superseded; the consumption side
> shipped together with the `jti` claim that keys it.

### Dynamic, per-subject quota overrides

`PUT /admin/quotas {scope,subject,limit}` (scope ∈ `user|tenant`) upserts an
override; `GET`/`DELETE` round it out. This is the data the gateway's quota
`Enforcer` (ADR-0005) reads to **supersede its static env caps per
user/tenant** — the dynamic-quota follow-up. M5 owns the authoritative override
store + API; the gateway now _reads_ those overrides on the quota path (with a
small TTL cache) — see the quota-override addendum below. The deferral note that
stood here is superseded; the consumption side shipped with the override seam.

### Skill-Market as an admin-owned catalog

A `Skill` is a named, versioned capability (`id,name,description,version,
entrypoint,enabled`). The admin-api **owns the catalog** (full CRUD + enable/
disable); the agent-runtime will **consume only `Enabled` entries**. Keeping the
catalog in the control plane (not scattered in runtime config) makes "what can
employees use" a single audited, toggleable surface. The runtime-side loader
that reads enabled skills is a deliberate follow-up — M5 establishes the catalog
and its lifecycle.

### Admin auth: a single static bearer key (for now)

The admin surface is gated by a static shared admin key
(`COCOLA_ADMIN_KEY`, constant-time compared). **When unset, auth is disabled**
and all callers are a `dev-admin` — the exact convention ADR-0005 set for the
gateway's `COCOLA_AUTH_SECRET`, so dev/CI boots stay zero-config. Operators are
a small set; a shared key kept in the deployment secret is the simplest correct
thing. **Per-operator admin identities + RBAC are deferred.** Every write is
attributed (an optional `x-cocola-admin` header names the operator) and recorded
in the **audit log** (`GET /admin/audit`), so even with a shared key the trail
is meaningful.

### Token minting is optional, decoupled from the rest

The service takes a nil-able `Issuer`: **without `COCOLA_AUTH_SECRET`, token
endpoints return 400** but quota/skill/audit management still works. This keeps
the admin-api useful in deployments that haven't turned on signed-token auth yet.

## Alternatives considered

- **Extend the Python gateway with admin endpoints** instead of a new Go
  service. Rejected: it would put write-heavy CRUD and operator auth on the same
  process as the latency-sensitive model hot path, and ADR-0001 already assigns
  services to Go. A separate control plane scales and fails independently.
- **Reuse employee JWTs for admin auth.** Rejected: operators are not employees;
  conflating the two would require an RBAC/claims scheme heavier than a small
  internal tool needs today. A static key + audit trail is sufficient and
  upgradeable.
- **Key rotation instead of a denylist for revocation.** Rotating the signing
  secret invalidates _every_ token at once — too blunt for "revoke one
  employee's token". A per-id denylist revokes precisely; rotation remains
  available as "issue new + revoke old".
- **A shared Go/Python JWT library via codegen or cgo.** Over-engineered for ~40
  lines per side. Two tiny, independently-tested codecs guarded by a
  cross-language e2e is simpler and keeps each language idiomatic.
- **Pick a database now.** Rejected: M7 owns persistence tiering. The `Store`
  interface + in-memory impl lets M5 be complete and tested without pre-empting
  that decision.

## Consequences

- **Positive:** cocola has a control plane that closes ADR-0005's four
  follow-ups' _source of truth_ — self-service minting, precise revocation
  (denylist), dynamic per-subject quota, and a curated Skill-Market — all
  audited, all behind one swappable `Store`. Cross-language identity interop is
  proven by an e2e (Go mint → Python verify). Token minting is optional, so the
  service is useful before signed-token auth is enabled.
- **Negative:** admin auth is still a single shared key with no per-operator
  RBAC; the stores remain in-memory for _durability_ (a restart loses records)
  until M7 wires a persistent backend (PG). The fleet-wide propagation gaps that
  stood here are closed — see the addenda: the gateway reads quota overrides and
  the denylist on the hot path, the admin-api publishes both to a shared Redis so
  revokes/overrides take effect across processes, and the agent-runtime has a
  skill loader consuming `Enabled` entries.
- **Follow-ups:**
  - **M6 (done):** gateway-side quota-override read + denylist consumption on the
    hot path; admin-api publishing both to a shared Redis (fleet-wide propagation);
    agent-runtime skill loader consuming `Enabled` entries. See the addenda below.
  - **M7:** PostgreSQL `Store` implementation (Redis is already the propagation
    backend) behind the existing interface for _durability_; durable audit log;
    a live cross-process Redis e2e once a real backend is available.
  - **Per-operator admin identities + RBAC** replacing the shared admin key.
  - Optional **RS256/JWKS** if a non-cocola token issuer is ever introduced
    (the Go and Python codecs swap in lockstep, guarded by the same e2e).

## Addendum: denylist closed loop (`jti` + gateway gate)

The original M5 shipped the denylist's _source of truth_ (the admin-api endpoints
above) but left the gateway-side _consumption_ for later. That half is now done,
closing ADR-0005's revocation follow-up end to end.

The gap was structural: the admin-api keys revocation by `TokenRecord.ID`, but
the issued tokens carried no token id, so a verifier had nothing to match against
the denylist. The fix threads a standard **`jti` (JWT ID) claim** through both
codecs:

- **Both issuers stamp a random `jti`** (12 bytes → 24 hex chars) on every token,
  in lockstep across the language boundary (Go `newJTI`, Python `_new_jti`). The
  admin-api persists `TokenRecord.ID == claims.jti`, so **the denylist key and
  the token's `jti` are the same value** — no mapping table.
- **The Python verifier extracts `jti` into `Identity.token_id`.** A legacy token
  without `jti` verifies as before but exposes an empty `token_id`, so the gate
  simply skips it (no false rejections).

On the gateway hot path, a new `RevocationStore` seam mirrors the `QuotaStore`
pattern (Protocol + `MemoryRevocationStore` + `RedisRevocationStore`), wrapped by
a `TTLCachedRevocation` so the per-request check is an in-process set-membership
test most of the time, with a few-seconds staleness bound (never past `exp`).
`create_app(..., revocation=...)` gates **all three identity-bearing surfaces**
(`/v1/messages`, `/v1/usage`, `/v1/quota`): a verified-but-revoked token is
rejected with `401 authentication_error "token revoked"` before any work happens.
The gate is only active when auth is enabled (tokens carry a `jti`) and a store
is wired; with neither, behavior is unchanged.

`scripts/admin-revocation-e2e.py` is the acceptance proof: a token **minted in
Go** is verified in Python, passes while absent from the denylist, then — after a
revoke adds its `jti` — is rejected on all three surfaces while still within its
`exp`.

What remains for M6/M7 is the **shared backend**: today the admin-api's denylist
and the gateway's `RevocationStore` are separate in-memory stores, so they agree
only within a process. Pointing both at the same Redis (admin-api writes on
`DELETE /admin/tokens/{id}`, gateways read via `RedisRevocationStore`) makes a
revoke take effect fleet-wide — the wiring is in place (`COCOLA_LLM_REDIS_URL`),
the durability decision is M7's.

## Addendum: dynamic quota-override consumption (gateway reads the source of truth)

The original M5 shipped the override's _source of truth_ (`PUT/GET/DELETE
/admin/quotas` above) but left the gateway-side _consumption_ for M6. That half
is now done, closing ADR-0005's dynamic-quota follow-up end to end: an admin
override now actually changes what the gateway enforces, per subject.

The contract that bridges the two sides is the override's tri-state answer,
matched on both sides:

- **no override -> `None`** -> fall back to the static env cap (QuotaPolicy
  default for that scope),
- **override `N>0`** -> the per-subject cap,
- **override `0`** -> _explicitly unlimited_ for that subject.

This mirrors the Go `QuotaOverride` exactly (a `Limit` of 0 means "explicitly
unlimited") and the policy's existing `limit <= 0 == unlimited`. The Python
return type is `int | None`: `None` and `0` are different answers — `None` says
"I have nothing to say, use the default"; `0` says "this subject is uncapped".

On the gateway side a new `OverrideStore` seam mirrors the `QuotaStore` /
`RevocationStore` pattern (Protocol + `MemoryOverrideStore` +
`RedisOverrideStore`, the latter a single HASH `cocola:quota:override` keyed
`scope/subject`), wrapped by `TTLCachedOverrides` so the per-request lookup is an
in-process dict hit most of the time, with a few-seconds staleness bound. The
cache stores both "has an override" and "no override", so an uncapped subject
does not hit the backend on every call.

The `Enforcer` (ADR-0005) gained an optional `overrides` field. Before checking
or committing a layer it resolves the _effective_ limit via `_limit_for(scope,
subject, default)`: the override if present, else the static default. A subtle
consequence is that overrides can **enable a cap the static policy leaves
unlimited**, so the enforcer can no longer short-circuit on the policy alone —
`_maybe_active` is `policy.any_enabled or overrides is not None`. Each layer is
still enforced only when its effective `limit > 0`, so an override of `0`
correctly disables that layer for the subject.

`scripts/admin-quota-override-e2e.py` is the acceptance proof: a token **minted
in Go** is attributed to a subject; with no static cap and no override the
subject is unlimited; an admin override of `5` then caps it (first call `200`,
next `429`); raising it to `100000` lets calls pass and `/v1/quota` reports the
override limit; an override of `0` marks the subject explicitly unlimited again.

What remains for M6/M7 is the **shared backend** (the same gap as the denylist):
today the admin-api's override store and the gateway's `OverrideStore` are
separate in-memory stores. Pointing both at the same Redis (admin-api writes on
`PUT /admin/quotas`, gateways read via `RedisOverrideStore`) makes an override
take effect fleet-wide — the wiring is in place (`COCOLA_LLM_REDIS_URL` plus
`COCOLA_QUOTA_OVERRIDE_CACHE_TTL_SECS`), the durability decision is M7's.

## Addendum: shared-Redis fleet-wide propagation (admin-api publishes what gateways read)

Both addenda above ended on the same open gap: the admin-api owns the
authoritative records, but its denylist and override stores were separate
in-memory stores from the gateway's `RevocationStore` / `OverrideStore`, so a
revoke or override only took effect within a process. The gateway already
_reads_ the shared keys (`RedisRevocationStore` -> SET `cocola:revoked`,
`RedisOverrideStore` -> HASH `cocola:quota:override`); the missing half was the
admin-api _writing_ them. That half is now done, so a revoke/override takes
effect **fleet-wide**.

The bridge is a **`store.Mirror` decorator**, not a change to the service or
handlers. `NewMirror(inner Store, pub Publisher)` wraps the authoritative store;
the three fleet-visible mutations (`RevokeToken`, `SetQuota`, `DeleteQuota`)
call the inner store first and, **only if that write succeeds**, best-effort
publish to Redis via a small `Publisher` interface. A publish failure never
fails the admin operation — the authoritative write already landed — it is
routed to an injectable `OnPublishError` hook and logged, mirroring the
audit-log convention. If no publisher is configured, `NewMirror` returns the
inner store unchanged, so dev/CI boots stay zero-dependency exactly as before.

The publisher itself is an isolated `internal/redispub` package over raw
go-redis (the project's `go-common/redis.KV` deliberately exposes only
Get/Set/Del-style ops, not the SADD/HSET this needs). It writes the **same keys
and field encoding the gateway reads**: SET `cocola:revoked` (SADD on revoke),
HASH `cocola:quota:override` field `scope/subject` (HSET on set, HDEL on
delete). That cross-language key contract is the load-bearing invariant, so it
is pinned by a Go unit test (`redispub_test.go`) against the gateway's literals;
the gateway side has the matching constants. `main.go` builds the publisher only
when `COCOLA_REDIS_ADDR` is set (with `COCOLA_REDIS_PASSWORD/DB/POOL_SIZE`),
Pings on boot, and logs loudly when publishing is **disabled** so a
misconfigured deployment is obvious rather than silently process-local.

Because a live cross-process Redis is not available in this build sandbox, the
publish path is validated by the `mirror_test.go` fake-publisher unit tests
(publish happens only after a successful store write; a store error suppresses
the publish; a publish error is best-effort and surfaces via `OnPublishError`)
plus the pinned key-contract test; the gateway's existing Redis-store tests
cover the read side. A live end-to-end across two processes is left to a
deployment with a real Redis. Durability/tiering remains M7's call — this
addendum only connects the two existing seams onto one backend.

## Addendum: agent-runtime skill loader (the runtime consumes `Enabled` entries)

The Skill-Market decision above established the admin-owned catalog and noted the
runtime-side loader as a deliberate follow-up. That loader now exists, so
toggling a skill in the control plane changes what the agent can do with **no
runtime redeploy**.

The loader mirrors the runtime's existing seams (`agent_provider`,
`claude_sdk_provider`): a small `SkillCatalog` Protocol the runtime depends on, a
production `AdminSkillCatalog` that GETs `/admin/skills?enabled=true` (sending the
admin key as a bearer token when configured), and a `StaticSkillCatalog` for
tests. The HTTP transport is an **injectable `Fetcher` callable** defaulting to
stdlib `urllib`, so the package imports without httpx (which the runtime venv
does not carry) and unit tests never open a socket. A fetch or parse failure
**degrades to an empty list** (logged): a control-plane blip must not break
session startup — the agent simply runs with no market skills until the next
refresh. The loader re-filters to enabled-with-id entries defensively, so a
catalog change can never leak a disabled skill into a session.

Consumption is transport-neutral by design. Rather than coupling to a specific
SDK tool-registration API (which varies by SDK version), `skills_system_preamble`
renders the enabled skills into the system prompt, and `apply_skills_to_options`
folds that preamble into a copy of the base `AgentOptions` (preserving any base
system prompt). This is the seam the M2 gRPC server calls when it builds options
for a session; mapping a skill's `entrypoint` to a concrete SDK tool / MCP server
is a later refinement, but listing enabled skills is what makes them observable
to the agent today. The loader is covered by hermetic unit tests
(`tests/test_skill_loader.py`): JSON->Skill mapping, the defensive filter, the
bearer header, graceful degrade on transport/parse error, and the
options-merge behavior.
