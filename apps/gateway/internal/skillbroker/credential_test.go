package skillbroker

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCredentialBindsRunCapabilityAndExpiry(t *testing.T) {
	broker, err := New("test-secret")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	broker.now = func() time.Time { return now }
	credential, err := broker.Issue("tenant-a", "user-a", "conversation-a", "run-a")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := broker.Verify(credential)
	if err != nil || claims.Capability != Capability || claims.RunID != "run-a" {
		t.Fatalf("Verify() = %#v, %v", claims, err)
	}
	broker.now = func() time.Time { return now.Add(CredentialTTL + time.Second) }
	if _, err := broker.Verify(credential); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("expired credential error = %v", err)
	}
}

func TestCredentialRejectsTampering(t *testing.T) {
	broker, _ := New("test-secret")
	credential, _ := broker.Issue("tenant-a", "user-a", "conversation-a", "run-a")
	parts := strings.Split(credential, ".")
	parts[0] = strings.Repeat("A", len(parts[0]))
	if _, err := broker.Verify(strings.Join(parts, ".")); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("tampered credential error = %v", err)
	}
}
