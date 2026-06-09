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
   already accepts — across a *language boundary* (Go issuer → Python verifier).

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
to swap for RS256/JWKS later) — now it must also hold *across languages*. The
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

> Note: wiring the gateway to *poll/consult* the denylist on the hot path is the
> remaining half (a small verifier change + a cache); M5 ships the authoritative
> source of truth and the e2e proves minting interop. The gateway-side denylist
> check is tracked as an M6 follow-up so the hot path keeps a single, cached
> read rather than a per-request control-plane call.

### Dynamic, per-subject quota overrides

`PUT /admin/quotas {scope,subject,limit}` (scope ∈ `user|tenant`) upserts an
override; `GET`/`DELETE` round it out. This is the data the gateway's quota
`Enforcer` (ADR-0005) reads to **supersede its static env caps per
user/tenant** — the dynamic-quota follow-up. M5 owns the authoritative override
store + API; the gateway reading overrides (with a Redis cache) joins the
denylist consumption in M6, after M7 makes the store durable.

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
  secret invalidates *every* token at once — too blunt for "revoke one
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
  follow-ups' *source of truth* — self-service minting, precise revocation
  (denylist), dynamic per-subject quota, and a curated Skill-Market — all
  audited, all behind one swappable `Store`. Cross-language identity interop is
  proven by an e2e (Go mint → Python verify). Token minting is optional, so the
  service is useful before signed-token auth is enabled.
- **Negative:** the gateway does not *yet* consult the denylist or quota
  overrides on the hot path (M6); the store is in-memory, so revocations/
  overrides/skills are process-local until M7 makes them durable; admin auth is
  a single shared key with no per-operator RBAC; the Skill-Market has a catalog
  but no runtime loader yet.
- **Follow-ups:**
  - **M6:** gateway-side denylist check + quota-override read (single cached
    control-plane read, off the per-request path); agent-runtime skill loader
    consuming `Enabled` entries.
  - **M7:** PostgreSQL `Store` implementation (+ Redis cache) behind the
    existing interface; durable audit log.
  - **Per-operator admin identities + RBAC** replacing the shared admin key.
  - Optional **RS256/JWKS** if a non-cocola token issuer is ever introduced
    (the Go and Python codecs swap in lockstep, guarded by the same e2e).
