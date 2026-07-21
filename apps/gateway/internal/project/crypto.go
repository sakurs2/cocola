package project

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

type secretBox struct{ key [32]byte }

func newSecretBox(encoded string) (*secretBox, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(raw) != 32 {
		return nil, errors.New("COCOLA_SCM_SECRET_KEY must be base64-encoded 32 bytes")
	}
	box := &secretBox{}
	copy(box.key[:], raw)
	return box, nil
}

func (b *secretBox) encrypt(plain string, aad []byte) (string, error) {
	block, err := aes.NewCipher(b.key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, []byte(plain), aad)
	return "v1:" + base64.RawURLEncoding.EncodeToString(append(nonce, sealed...)), nil
}

func (b *secretBox) decrypt(value string, aad []byte) (string, error) {
	if !strings.HasPrefix(value, "v1:") {
		return "", errors.New("unsupported scm ciphertext")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "v1:"))
	if err != nil {
		return "", errors.New("invalid scm ciphertext")
	}
	block, err := aes.NewCipher(b.key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize() {
		return "", errors.New("invalid scm ciphertext")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], aad)
	if err != nil {
		return "", errors.New("invalid scm ciphertext")
	}
	return string(plain), nil
}

type oauthState struct {
	UserID   string `json:"u"`
	TenantID string `json:"t"`
	ReturnTo string `json:"r"`
	Nonce    string `json:"n"`
	Expires  int64  `json:"e"`
}

func (b *secretBox) signState(identity Identity, returnTo string, now time.Time) (string, error) {
	nonce := make([]byte, 18)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	payload, err := json.Marshal(oauthState{
		UserID: identity.UserID, TenantID: identity.TenantID, ReturnTo: safeReturnTo(returnTo),
		Nonce: base64.RawURLEncoding.EncodeToString(nonce), Expires: now.Add(10 * time.Minute).Unix(),
	})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, b.key[:])
	_, _ = mac.Write([]byte("cocola.github.oauth-state.v1\x00"))
	_, _ = mac.Write(payload)
	signature := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (b *secretBox) verifyState(value string, identity Identity, now time.Time) (oauthState, error) {
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
	mac := hmac.New(sha256.New, b.key[:])
	_, _ = mac.Write([]byte("cocola.github.oauth-state.v1\x00"))
	_, _ = mac.Write(payload)
	want := mac.Sum(nil)
	if len(signature) != len(want) || subtle.ConstantTimeCompare(signature, want) != 1 {
		return oauthState{}, ErrInvalidArgument
	}
	var state oauthState
	if json.Unmarshal(payload, &state) != nil || state.UserID != identity.UserID ||
		state.TenantID != identity.TenantID || state.Expires < now.Unix() || state.Nonce == "" {
		return oauthState{}, ErrInvalidArgument
	}
	state.ReturnTo = safeReturnTo(state.ReturnTo)
	return state, nil
}

func tokenAAD(identity Identity, field string) []byte {
	return []byte(fmt.Sprintf("cocola:scm:github:%s:%s:%s", identity.TenantID, identity.UserID, field))
}

func safeReturnTo(value string) string {
	value = strings.TrimSpace(value)
	if value == "/projects/new" || strings.HasPrefix(value, "/projects/") {
		if !strings.Contains(value, "://") && !strings.HasPrefix(value, "//") && !strings.ContainsAny(value, "\r\n") {
			return value
		}
	}
	return "/projects/new"
}
