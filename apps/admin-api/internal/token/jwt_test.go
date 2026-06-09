package token

import (
	"strings"
	"testing"
	"time"
)

func TestEncodeDecodeRoundtrip(t *testing.T) {
	c := Claims{Subject: "emp-1", Tenant: "team-a", IssuedAt: 1000, Issuer: "cocola"}
	tok, err := Encode(c, "s")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Count(tok, ".") != 2 {
		t.Fatalf("want 3 segments, got %q", tok)
	}
	back, err := Decode(tok, "s", 0)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back.Subject != "emp-1" || back.Tenant != "team-a" || back.Issuer != "cocola" {
		t.Fatalf("claims roundtrip mismatch: %+v", back)
	}
}

func TestWrongSecretRejected(t *testing.T) {
	tok, _ := Encode(Claims{Subject: "x"}, "right")
	if _, err := Decode(tok, "wrong", 0); err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestTamperRejected(t *testing.T) {
	tok, _ := Encode(Claims{Subject: "x"}, "s")
	parts := strings.Split(tok, ".")
	bad := parts[0] + "." + parts[1][:len(parts[1])-1] + "A" + "." + parts[2]
	if _, err := Decode(bad, "s", 0); err == nil {
		t.Fatal("expected error for tampered payload")
	}
}

func TestExpiryEnforced(t *testing.T) {
	tok, _ := Encode(Claims{Subject: "x", Expires: 100}, "s")
	if _, err := Decode(tok, "s", 101); err == nil {
		t.Fatal("expected expired error")
	}
	if _, err := Decode(tok, "s", 99); err != nil {
		t.Fatalf("should be valid before exp: %v", err)
	}
}

func TestBadShapeRejected(t *testing.T) {
	if _, err := Decode("not-a-jwt", "s", 0); err == nil {
		t.Fatal("expected error for malformed token")
	}
}

func TestIssuerIssue(t *testing.T) {
	iss := NewIssuer("s", "", time.Hour)
	tok, c, err := iss.Issue("emp-7", "team-x", 0, 1000)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if c.Issuer != "cocola" {
		t.Fatalf("default issuer should be cocola, got %q", c.Issuer)
	}
	if c.Expires != 1000+3600 {
		t.Fatalf("exp = iat + ttl expected, got %d", c.Expires)
	}
	back, err := Decode(tok, "s", 1001)
	if err != nil {
		t.Fatalf("decode minted token: %v", err)
	}
	if back.Subject != "emp-7" || back.Tenant != "team-x" {
		t.Fatalf("minted claims mismatch: %+v", back)
	}
}

func TestIssuerNonExpiring(t *testing.T) {
	iss := NewIssuer("s", "cocola", time.Hour)
	_, c, err := iss.Issue("emp-1", "", -1, 1000) // negative ttl => non-expiring
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if c.Expires != 0 {
		t.Fatalf("negative ttl should yield non-expiring token, got exp=%d", c.Expires)
	}
}

func TestIssuerRequiresSecretAndUser(t *testing.T) {
	if _, _, err := NewIssuer("", "cocola", 0).Issue("emp-1", "", 0, 0); err == nil {
		t.Fatal("expected error without secret")
	}
	if _, _, err := NewIssuer("s", "cocola", 0).Issue("", "", 0, 0); err == nil {
		t.Fatal("expected error without user")
	}
}
