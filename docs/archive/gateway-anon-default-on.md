# Gateway: anonymous access defaults ON (blank token → dev-user)

Date: 2026-07-02

## Symptom

After removing the token input from the web UI (every request now goes out
with no `authorization` header), the sidebar chat list and history were STILL
empty — "还是看不到". The database was correct (7 conversations, all owned by
`dev-user`), and the read endpoint / auth logic were all correct on paper.

## Root cause (proven, not theorized)

An in-process test against the REAL Postgres, driving the actual gateway
handler + `auth.Verifier` with a blank token on `GET /v1/conversations`,
isolated it definitively:

| gateway launch | blank-token read |
|---|---|
| `make up` (run-stack sets `COCOLA_AUTH_ALLOW_ANON=1`) | 200, 7 rows ✅ |
| bare launch (GoLand / `go run`, flag unset) | **401 UNAUTHENTICATED, 0 rows** ❌ |

`.env` sets `COCOLA_AUTH_SECRET`, so auth is *enabled*. A blank token is only
accepted as `dev-user` when `AllowAnonymous` is true, and that was gated on
`COCOLA_AUTH_ALLOW_ANON == "1"` — a flag ONLY `run-stack.sh` exports. Launching
the gateway any other way (the IDE) left it false, so anonymous reads 401'd and
the whole sidebar came back empty.

## Fix

Auth identity is a later concern; today the product sends no token and every
caller should be the shared `dev-user` regardless of launcher. So flip the
default in `apps/gateway/cmd/gateway/main.go`:

```go
// before
AllowAnonymous: os.Getenv("COCOLA_AUTH_ALLOW_ANON") == "1",
// after
AllowAnonymous: os.Getenv("COCOLA_AUTH_ALLOW_ANON") != "0",
```

Anonymous access is now ON by default and only disabled by an explicit
`COCOLA_AUTH_ALLOW_ANON=0`. (It still requires a secret to matter at all; with
no secret, auth is off entirely.) A blank-token read now works no matter how
the gateway is started. `run-stack.sh` still exports `=1` (harmless, same
result).

## Validation

- In-process test vs real Postgres: env unset → 200 / 7 rows; `=0` → 401 / 0
  (both as intended).
- `go build ./apps/gateway/...`, `go vet`, `go test ./apps/gateway/...` — all green.
