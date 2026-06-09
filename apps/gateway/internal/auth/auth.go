// Package auth turns the bearer credential the web client (and the Claude Agent
// SDK) presents into a verified Identity. It is the Go twin of the Python
// gateway's auth/identity.py Verifier, and it deliberately shares ONE HS256
// codec with admin-api via packages/go-common/token: a token minted by
// admin-api verifies here byte-for-byte.
//
// Semantics (kept identical to the Python Verifier):
//   - No secret configured  => auth disabled, every caller is DevIdentity.
//   - Secret set, no token   => reject, unless AllowAnonymous (then DevIdentity).
//   - Secret set, token given => decode + verify signature/expiry; if an issuer
//     is configured the token MUST carry a matching iss; sub is required.
package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/cocola-project/cocola/packages/go-common/token"
)

// Identity is the resolved caller. It is what handlers read off the request.
type Identity struct {
	UserID   string
	TenantID string
	TokenID  string // jti; the admin-api denylist key
	IssuedAt int64
	Expires  int64
}

// DevIdentity is the stable, obviously-fake identity used when auth is disabled
// or AllowAnonymous accepts a blank token. It never leaks into production unless
// the operator explicitly runs without a secret.
var DevIdentity = Identity{UserID: "dev-user", TenantID: "dev-tenant"}

// Config controls verification. Secret == "" disables auth entirely.
type Config struct {
	Secret         string
	Issuer         string // when set, tokens must carry a matching iss
	AllowAnonymous bool   // when true, a blank token yields DevIdentity (never in prod)
}

// Enabled reports whether auth is enforced (a secret is configured).
func (c Config) Enabled() bool { return c.Secret != "" }

// Verifier resolves raw credentials to Identities. Verification is offline
// (shared HS256 secret), so there is no network hop on the hot path.
type Verifier struct {
	cfg Config
}

// NewVerifier builds a Verifier from cfg.
func NewVerifier(cfg Config) *Verifier { return &Verifier{cfg: cfg} }

// ErrUnauthorized is returned when auth is enforced and the credential is
// missing or invalid. It never embeds the token or secret.
type authError struct{ msg string }

func (e authError) Error() string { return e.msg }

// ErrUnauthorized is the sentinel callers match on to map to HTTP 401.
var ErrUnauthorized error = authError{"unauthorized"}

// Verify resolves a raw bearer credential to an Identity. now is unix seconds;
// pass 0 to use the wall clock.
func (v *Verifier) Verify(raw string, now int64) (Identity, error) {
	tok := stripBearer(raw)

	if !v.cfg.Enabled() {
		return DevIdentity, nil
	}
	if tok == "" {
		if v.cfg.AllowAnonymous {
			return DevIdentity, nil
		}
		return Identity{}, ErrUnauthorized
	}

	claims, err := token.Decode(tok, v.cfg.Secret, now)
	if err != nil {
		return Identity{}, ErrUnauthorized
	}
	if v.cfg.Issuer != "" && claims.Issuer != v.cfg.Issuer {
		return Identity{}, ErrUnauthorized
	}
	if claims.Subject == "" {
		return Identity{}, ErrUnauthorized
	}
	return Identity{
		UserID:   claims.Subject,
		TenantID: claims.Tenant,
		TokenID:  claims.ID,
		IssuedAt: claims.IssuedAt,
		Expires:  claims.Expires,
	}, nil
}

// ---- HTTP middleware ----

type ctxKey int

const identityKey ctxKey = 0

// Middleware verifies the request's bearer token and stashes the Identity in the
// context. On failure it writes a 401 JSON envelope and stops the chain. onErr
// lets the caller reuse its own error-envelope writer.
func (v *Verifier) Middleware(onErr func(w http.ResponseWriter, status int, code, msg string)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, err := v.Verify(bearer(r), 0)
			if err != nil {
				onErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "valid bearer token required")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey, id)))
		})
	}
}

// IdentityOf returns the verified Identity stashed by Middleware, or the zero
// Identity and false when none is present.
func IdentityOf(r *http.Request) (Identity, bool) {
	id, ok := r.Context().Value(identityKey).(Identity)
	return id, ok
}

// bearer extracts the credential from the Authorization or x-api-key header.
func bearer(r *http.Request) string {
	if h := r.Header.Get("authorization"); h != "" {
		return h
	}
	return r.Header.Get("x-api-key")
}

// stripBearer removes a leading "Bearer " (case-insensitive) and trims space.
func stripBearer(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) >= 7 && strings.EqualFold(s[:7], "bearer ") {
		return strings.TrimSpace(s[7:])
	}
	return s
}
