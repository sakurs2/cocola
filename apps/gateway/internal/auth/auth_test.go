package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cocola-project/cocola/packages/go-common/token"
)

func mint(t *testing.T, secret, sub, iss string, exp int64) string {
	t.Helper()
	tok, err := token.Encode(token.Claims{Subject: sub, Issuer: iss, Expires: exp}, secret)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return tok
}

func TestVerifyDisabledWhenNoSecret(t *testing.T) {
	v := NewVerifier(Config{})
	id, err := v.Verify("", 0)
	if err != nil {
		t.Fatalf("disabled auth should never error: %v", err)
	}
	if id != DevIdentity {
		t.Fatalf("want DevIdentity, got %+v", id)
	}
}

func TestVerifyMissingTokenRejected(t *testing.T) {
	v := NewVerifier(Config{Secret: "s", Issuer: "cocola"})
	if _, err := v.Verify("", 0); err != ErrUnauthorized {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}

func TestVerifyAllowAnonymous(t *testing.T) {
	v := NewVerifier(Config{Secret: "s", AllowAnonymous: true})
	id, err := v.Verify("", 0)
	if err != nil || id != DevIdentity {
		t.Fatalf("allow-anon blank token should yield DevIdentity, got %+v err=%v", id, err)
	}
}

func TestVerifyGoodToken(t *testing.T) {
	v := NewVerifier(Config{Secret: "s", Issuer: "cocola"})
	tok := mint(t, "s", "emp-1", "cocola", 0)
	id, err := v.Verify("Bearer "+tok, 0)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if id.UserID != "emp-1" {
		t.Fatalf("want emp-1, got %q", id.UserID)
	}
}

func TestVerifyIssuerMismatchRejected(t *testing.T) {
	v := NewVerifier(Config{Secret: "s", Issuer: "cocola"})
	tok := mint(t, "s", "emp-1", "evil", 0)
	if _, err := v.Verify(tok, 0); err != ErrUnauthorized {
		t.Fatalf("issuer mismatch must reject, got %v", err)
	}
}

func TestVerifyMissingSubjectRejected(t *testing.T) {
	v := NewVerifier(Config{Secret: "s", Issuer: "cocola"})
	tok := mint(t, "s", "", "cocola", 0)
	if _, err := v.Verify(tok, 0); err != ErrUnauthorized {
		t.Fatalf("missing sub must reject, got %v", err)
	}
}

func TestVerifyExpiredRejected(t *testing.T) {
	v := NewVerifier(Config{Secret: "s", Issuer: "cocola"})
	tok := mint(t, "s", "emp-1", "cocola", 100)
	if _, err := v.Verify(tok, 101); err != ErrUnauthorized {
		t.Fatalf("expired token must reject, got %v", err)
	}
}

func TestVerifyWrongSecretRejected(t *testing.T) {
	v := NewVerifier(Config{Secret: "right", Issuer: "cocola"})
	tok := mint(t, "wrong", "emp-1", "cocola", 0)
	if _, err := v.Verify(tok, 0); err != ErrUnauthorized {
		t.Fatalf("wrong secret must reject, got %v", err)
	}
}

func TestMiddlewareInjectsIdentity(t *testing.T) {
	v := NewVerifier(Config{Secret: "s", Issuer: "cocola"})
	tok := mint(t, "s", "emp-9", "cocola", 0)

	var seen Identity
	var ok bool
	h := v.Middleware(func(w http.ResponseWriter, status int, code, msg string) {
		w.WriteHeader(status)
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, ok = IdentityOf(r)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/v1/chat", nil)
	req.Header.Set("authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !ok || seen.UserID != "emp-9" {
		t.Fatalf("middleware should inject identity, got ok=%v id=%+v", ok, seen)
	}
}

func TestMiddlewareRejectsBadToken(t *testing.T) {
	v := NewVerifier(Config{Secret: "s", Issuer: "cocola"})
	called := false
	h := v.Middleware(func(w http.ResponseWriter, status int, code, msg string) {
		w.WriteHeader(status)
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest("POST", "/v1/chat", nil)
	req.Header.Set("authorization", "Bearer not-a-jwt")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if called {
		t.Fatal("next handler must not run on auth failure")
	}
}
