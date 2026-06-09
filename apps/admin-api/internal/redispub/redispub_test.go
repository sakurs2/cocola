package redispub

import "testing"

// These constants and the field encoding are a cross-language contract with the
// gateway's Python stores (auth/revocation.py, quota/overrides.py). If either
// side changes a key name or the "scope/subject" field shape, fleet-wide
// revocation/override silently breaks — so pin them here.
func TestKeyContractMatchesGateway(t *testing.T) {
	if RevokedKey != "cocola:revoked" {
		t.Fatalf("revoked key drifted from gateway: %q", RevokedKey)
	}
	if OverrideKey != "cocola:quota:override" {
		t.Fatalf("override key drifted from gateway: %q", OverrideKey)
	}
}

func TestOverrideFieldEncoding(t *testing.T) {
	cases := []struct {
		scope, subject, want string
	}{
		{"user", "emp-vip", "user/emp-vip"},
		{"tenant", "team-r", "tenant/team-r"},
	}
	for _, c := range cases {
		if got := overrideField(c.scope, c.subject); got != c.want {
			t.Fatalf("overrideField(%q,%q)=%q want %q", c.scope, c.subject, got, c.want)
		}
	}
}
