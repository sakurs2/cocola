// Package skillbroker owns the short-lived capability that lets one active
// Agent run validate and publish Personal Skills through the Gateway.
package skillbroker

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	Capability    = "skill.publish"
	CredentialTTL = 12 * time.Hour
	macPurpose    = "cocola.skill.broker.v1\x00"
)

var ErrInvalidCredential = errors.New("skillbroker: invalid credential")

type Claims struct {
	TenantID       string `json:"tenant_id"`
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	RunID          string `json:"run_id"`
	Capability     string `json:"capability"`
	ExpiresAt      int64  `json:"expires_at"`
}

type Broker struct {
	key []byte
	now func() time.Time
}

func New(secret string) (*Broker, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, ErrInvalidCredential
	}
	return &Broker{key: []byte(secret), now: time.Now}, nil
}

func (b *Broker) Issue(tenantID, userID, conversationID, runID string) (string, error) {
	if b == nil || strings.TrimSpace(userID) == "" ||
		strings.TrimSpace(conversationID) == "" || strings.TrimSpace(runID) == "" {
		return "", ErrInvalidCredential
	}
	claims := Claims{
		TenantID: strings.TrimSpace(tenantID), UserID: strings.TrimSpace(userID),
		ConversationID: strings.TrimSpace(conversationID), RunID: strings.TrimSpace(runID),
		Capability: Capability, ExpiresAt: b.now().Add(CredentialTTL).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(b.sign(payload)), nil
}

func (b *Broker) Verify(value string) (Claims, error) {
	if b == nil {
		return Claims{}, ErrInvalidCredential
	}
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) != 2 {
		return Claims{}, ErrInvalidCredential
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrInvalidCredential
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, b.sign(payload)) {
		return Claims{}, ErrInvalidCredential
	}
	var claims Claims
	if json.Unmarshal(payload, &claims) != nil ||
		claims.UserID == "" || claims.ConversationID == "" || claims.RunID == "" ||
		claims.Capability != Capability || claims.ExpiresAt <= b.now().Unix() {
		return Claims{}, ErrInvalidCredential
	}
	return claims, nil
}

func (b *Broker) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, b.key)
	_, _ = mac.Write([]byte(macPurpose))
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}
