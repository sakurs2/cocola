package project

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/secretbox"
)

type secretBox struct{ core *secretbox.Box }

func newSecretBox(encoded string) (*secretBox, error) {
	box, err := secretbox.New(encoded)
	if err != nil {
		return nil, err
	}
	return &secretBox{core: box}, nil
}

func (b *secretBox) encrypt(plain string, aad []byte) (string, error) {
	return b.core.Encrypt(plain, aad)
}

func (b *secretBox) decrypt(value string, aad []byte) (string, error) {
	return b.core.Decrypt(value, aad)
}

type oauthState struct {
	UserID         string `json:"u"`
	TenantID       string `json:"t"`
	ReturnTo       string `json:"r"`
	PublicOrigin   string `json:"o,omitempty"`
	RegistrationID string `json:"g,omitempty"`
	FlowType       string `json:"f,omitempty"`
	Nonce          string `json:"n"`
	Expires        int64  `json:"e"`
}

func (b *secretBox) signState(identity Identity, returnTo string, now time.Time) (string, error) {
	return b.signFlowState(identity, "oauth", returnTo, "", "", 10*time.Minute, now)
}

func (b *secretBox) signFlowState(
	identity Identity,
	flowType string,
	returnTo string,
	publicOrigin string,
	registrationID string,
	ttl time.Duration,
	now time.Time,
) (string, error) {
	nonce := make([]byte, 18)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	payload, err := json.Marshal(oauthState{
		UserID: identity.UserID, TenantID: identity.TenantID, ReturnTo: safeReturnTo(returnTo),
		PublicOrigin: publicOrigin, RegistrationID: registrationID, FlowType: flowType,
		Nonce: base64.RawURLEncoding.EncodeToString(nonce), Expires: now.Add(ttl).Unix(),
	})
	if err != nil {
		return "", err
	}
	signature := b.core.SignMAC("cocola.github.oauth-state.v1\x00", payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (b *secretBox) verifyState(value string, identity Identity, now time.Time) (oauthState, error) {
	return b.verifyFlowState(value, identity, "", now)
}

func (b *secretBox) verifyFlowState(value string, identity Identity, flowType string, now time.Time) (oauthState, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return oauthState{}, ErrInvalidArgument
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return oauthState{}, ErrInvalidArgument
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return oauthState{}, ErrInvalidArgument
	}
	if !b.core.VerifyMAC("cocola.github.oauth-state.v1\x00", payload, signature) {
		return oauthState{}, ErrInvalidArgument
	}
	var state oauthState
	if json.Unmarshal(payload, &state) != nil || state.UserID != identity.UserID ||
		state.TenantID != identity.TenantID || state.Expires < now.Unix() || state.Nonce == "" ||
		(flowType != "" && state.FlowType != flowType) {
		return oauthState{}, ErrInvalidArgument
	}
	state.ReturnTo = safeReturnTo(state.ReturnTo)
	return state, nil
}

func tokenAAD(identity Identity, field string) []byte {
	return []byte(fmt.Sprintf("cocola:scm:github:%s:%s:%s", identity.TenantID, identity.UserID, field))
}

func registrationAAD(identity Identity, registrationID, field string) []byte {
	return []byte(fmt.Sprintf("cocola:scm:github:%s:%s:registration:%s:%s",
		identity.TenantID, identity.UserID, registrationID, field))
}

func projectTokenAAD(identity Identity, projectID string) []byte {
	return []byte(fmt.Sprintf("cocola:scm:forgejo:%s:%s:project:%s:token",
		identity.TenantID, identity.UserID, projectID))
}

func (b *secretBox) signBrokerCredential(claims BrokerCredentialClaims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(
			b.core.SignMAC("cocola.scm.broker.v1\x00", payload),
		), nil
}

func (b *secretBox) verifyBrokerCredential(value string, now time.Time) (BrokerCredentialClaims, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return BrokerCredentialClaims{}, ErrInvalidArgument
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return BrokerCredentialClaims{}, ErrInvalidArgument
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return BrokerCredentialClaims{}, ErrInvalidArgument
	}
	if !b.core.VerifyMAC("cocola.scm.broker.v1\x00", payload, signature) {
		return BrokerCredentialClaims{}, ErrInvalidArgument
	}
	var claims BrokerCredentialClaims
	if json.Unmarshal(payload, &claims) != nil || claims.UserID == "" ||
		claims.ConversationID == "" || claims.RunID == "" || claims.ProjectID == "" ||
		claims.RepositoryID <= 0 || claims.InstallationID <= 0 || claims.RegistrationID == "" ||
		claims.ExpiresAt < now.Unix() {
		return BrokerCredentialClaims{}, ErrInvalidArgument
	}
	return claims, nil
}

func tokenLeaseAAD(value TokenLease) []byte {
	return []byte(fmt.Sprintf("cocola:scm:github:%s:%s:run:%s:lease:%s",
		value.TenantID, value.UserID, value.RunID, value.ID))
}

func safeReturnTo(value string) string {
	value = strings.TrimSpace(value)
	if value == "/connectors" || value == "/projects/new" || strings.HasPrefix(value, "/projects/") {
		if !strings.Contains(value, "://") && !strings.HasPrefix(value, "//") && !strings.ContainsAny(value, "\r\n") {
			return value
		}
	}
	return "/projects/new"
}
