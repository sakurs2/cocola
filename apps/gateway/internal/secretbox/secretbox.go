// Package secretbox provides the Gateway's shared encrypted-secret primitive.
// Ciphertexts are authenticated with caller-supplied AAD so a value cannot be
// moved across users, connectors, or fields.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

// Box encrypts secrets and signs small internal payloads with one 32-byte key.
type Box struct{ key [32]byte }

// New decodes a base64-encoded 32-byte key.
func New(encoded string) (*Box, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(raw) != 32 {
		return nil, errors.New("COCOLA_SCM_SECRET_KEY must be base64-encoded 32 bytes")
	}
	box := &Box{}
	copy(box.key[:], raw)
	return box, nil
}

// Encrypt seals plain with AES-GCM and binds it to aad.
func (b *Box) Encrypt(plain string, aad []byte) (string, error) {
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

// Decrypt opens a value produced by Encrypt with the same aad.
func (b *Box) Decrypt(value string, aad []byte) (string, error) {
	if !strings.HasPrefix(value, "v1:") {
		return "", errors.New("unsupported secret ciphertext")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "v1:"))
	if err != nil {
		return "", errors.New("invalid secret ciphertext")
	}
	block, err := aes.NewCipher(b.key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize() {
		return "", errors.New("invalid secret ciphertext")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], aad)
	if err != nil {
		return "", errors.New("invalid secret ciphertext")
	}
	return string(plain), nil
}

// SignMAC authenticates payload under a domain-separated purpose.
func (b *Box) SignMAC(purpose string, payload []byte) []byte {
	mac := hmac.New(sha256.New, b.key[:])
	_, _ = mac.Write([]byte(purpose))
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

// VerifyMAC performs constant-time verification of SignMAC output.
func (b *Box) VerifyMAC(purpose string, payload, signature []byte) bool {
	want := b.SignMAC(purpose, payload)
	return len(signature) == len(want) && subtle.ConstantTimeCompare(signature, want) == 1
}
