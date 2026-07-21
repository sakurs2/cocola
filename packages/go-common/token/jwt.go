// Package token mints and inspects the cocola-signed HS256 JWTs that ARE the
// Claude Agent SDK's ANTHROPIC_API_KEY. It is shared go-common code: admin-api
// mints tokens with it and the gateway BFF verifies them with the SAME codec,
// so there is exactly one HS256 implementation on the Go side (no third
// hand-rolled copy). This is the Go counterpart of the Python gateway's
// auth/jwt.py: the wire format (compact JWS, base64url without padding, header
// {"alg":"HS256","typ":"JWT"}, claims sub/ten/iat/exp/iss) is byte-compatible,
// so a token minted here verifies in the gateway and vice versa. The
// cross-language interop e2e proves it.
//
// Why hand-rolled HS256 again (instead of a JWT library)? Symmetric signing of
// compact JWS is a few lines of crypto/hmac+sha256; matching the Python side
// exactly is easier when both are deliberately tiny and we own every byte. If
// we ever move to RS256/JWKS, this is the single file to swap on the Go side.
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const alg = "HS256"

// ErrInvalid is returned for any malformed/invalid/expired token. It never
// embeds the secret or signature so it is safe to surface to clients.
var ErrInvalid = errors.New("token: invalid")

// Claims are the fields cocola puts in the signed token. The short keys match
// the Python gateway exactly (sub/ten/iat/exp/iss).
type Claims struct {
	Subject  string `json:"sub"`
	Tenant   string `json:"ten,omitempty"`
	Email    string `json:"eml,omitempty"`
	Name     string `json:"nam,omitempty"`
	Username string `json:"usr,omitempty"`
	IssuedAt int64  `json:"iat,omitempty"`
	Expires  int64  `json:"exp,omitempty"`
	Issuer   string `json:"iss,omitempty"`
	// ID is the JWT ID (jti): a per-token opaque identifier. It is the key the
	// admin-api's revocation denylist uses and the gateway reads back from a
	// verified token to check "has this specific token been revoked?". Without
	// it, a leaked-but-unexpired token could not be killed before exp.
	ID string `json:"jti,omitempty"`
}

type header struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

func b64encode(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

func b64decode(seg string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return nil, ErrInvalid
	}
	return b, nil
}

func sign(signingInput, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return b64encode(mac.Sum(nil))
}

// Encode signs a claims set into a compact JWS string.
func Encode(c Claims, secret string) (string, error) {
	if secret == "" {
		return "", ErrInvalid
	}
	hb, err := json.Marshal(header{Alg: alg, Typ: "JWT"})
	if err != nil {
		return "", err
	}
	pb, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	signingInput := b64encode(hb) + "." + b64encode(pb)
	return signingInput + "." + sign(signingInput, secret), nil
}

// Decode verifies the signature + expiry and returns the claims. now is unix
// seconds; pass 0 to use the wall clock. Signature comparison is constant time.
func Decode(tok, secret string, now int64) (Claims, error) {
	var zero Claims
	if secret == "" {
		return zero, ErrInvalid
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return zero, ErrInvalid
	}
	hb, err := b64decode(parts[0])
	if err != nil {
		return zero, ErrInvalid
	}
	var h header
	if err := json.Unmarshal(hb, &h); err != nil || h.Alg != alg {
		return zero, ErrInvalid
	}
	signingInput := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(sign(signingInput, secret)), []byte(parts[2])) {
		return zero, ErrInvalid
	}
	pb, err := b64decode(parts[1])
	if err != nil {
		return zero, ErrInvalid
	}
	var c Claims
	if err := json.Unmarshal(pb, &c); err != nil {
		return zero, ErrInvalid
	}
	if c.Expires != 0 {
		t := now
		if t == 0 {
			t = time.Now().Unix()
		}
		if t >= c.Expires {
			return zero, ErrInvalid
		}
	}
	return c, nil
}
