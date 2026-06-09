package token

import "time"

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
	if i.secret == "" {
		return "", Claims{}, ErrInvalid
	}
	if userID == "" {
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
	c := Claims{Subject: userID, Tenant: tenant, IssuedAt: iat, Issuer: i.issuer}
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
