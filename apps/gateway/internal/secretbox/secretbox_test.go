package secretbox

import (
	"encoding/base64"
	"testing"
)

func testBox(t *testing.T) *Box {
	t.Helper()
	box, err := New(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return box
}

func TestCiphertextIsBoundToAAD(t *testing.T) {
	box := testBox(t)
	ciphertext, err := box.Encrypt("secret", []byte("user-a:field-a"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	plain, err := box.Decrypt(ciphertext, []byte("user-a:field-a"))
	if err != nil || plain != "secret" {
		t.Fatalf("Decrypt = %q, %v", plain, err)
	}
	if _, err := box.Decrypt(ciphertext, []byte("user-b:field-a")); err == nil {
		t.Fatal("cross-user decrypt unexpectedly succeeded")
	}
	if _, err := box.Decrypt(ciphertext, []byte("user-a:field-b")); err == nil {
		t.Fatal("cross-field decrypt unexpectedly succeeded")
	}
}

func TestMACIsPurposeSeparated(t *testing.T) {
	box := testBox(t)
	payload := []byte("payload")
	signature := box.SignMAC("purpose-a\x00", payload)
	if !box.VerifyMAC("purpose-a\x00", payload, signature) {
		t.Fatal("valid MAC rejected")
	}
	if box.VerifyMAC("purpose-b\x00", payload, signature) {
		t.Fatal("cross-purpose MAC unexpectedly succeeded")
	}
}
