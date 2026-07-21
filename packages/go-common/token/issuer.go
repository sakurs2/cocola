package token

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Issuer mints tokens for employees using a fixed secret + issuer + default TTL.
// It is the Go equivalent of the Python gateway's auth.Issuer, exposed over the
// admin-api so ops can mint/rotate tokens for employees via HTTP instead of the
// gateway's local CLI.
type Issuer struct {
	secret     string
	issuer     string
	defaultTTL time.Duration
}

// NewIssuer builds an Issuer. issuer defaults to "cocola" when empty. A zero
// defaultTTL means tokens are non-expiring unless an explicit ttl is given.
func NewIssuer(secret, issuer string, defaultTTL time.Duration) *Issuer {
	if issuer == "" {
		issuer = "cocola"
	}
	return &Issuer{secret: secret, issuer: issuer, defaultTTL: defaultTTL}
}

// Issue mints a token for userID. tenant is optional. ttl overrides the default;
// pass a negative ttl to force a non-expiring token, or 0 to use the default.
// now is unix seconds (0 = wall clock) to keep issuance testable.
func (i *Issuer) Issue(userID, tenant string, ttl time.Duration, now int64) (string, Claims, error) {
	return i.issue(Claims{Subject: userID, Tenant: tenant}, ttl, now)
}

// IssueUser mints a runtime token for a persisted Cocola account. Subject is
// the immutable auth_users.id; profile claims are signed presentation data for
// downstream services and may change independently of resource ownership.
func (i *Issuer) IssueUser(userID, tenant, email, name, username string, ttl time.Duration, now int64) (string, Claims, error) {
	return i.issue(Claims{
		Subject:  userID,
		Tenant:   tenant,
		Email:    email,
		Name:     name,
		Username: username,
	}, ttl, now)
}

func (i *Issuer) issue(claims Claims, ttl time.Duration, now int64) (string, Claims, error) {
	if i.secret == "" {
		return "", Claims{}, ErrInvalid
	}
	if claims.Subject == "" {
		return "", Claims{}, ErrInvalid
	}
	iat := now
	if iat == 0 {
		iat = time.Now().Unix()
	}
	effective := ttl
	if ttl == 0 {
		effective = i.defaultTTL
	}
	c := claims
	c.IssuedAt = iat
	c.Issuer = i.issuer
	c.ID = newJTI()
	if effective > 0 {
		c.Expires = iat + int64(effective.Seconds())
	}
	tok, err := Encode(c, i.secret)
	if err != nil {
		return "", Claims{}, err
	}
	return tok, c, nil
}

// Issuer name accessor (used when persisting token metadata).
func (i *Issuer) Name() string { return i.issuer }

// newJTI mints a random token id embedded as the jti claim. It is also the key
// the revocation denylist uses, so it must be unique per issued token.
func newJTI() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
