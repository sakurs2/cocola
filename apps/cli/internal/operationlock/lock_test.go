package operationlock

import (
	"strings"
	"testing"
)

func TestAcquireRejectsConcurrentOperationAndRecoversAfterRelease(t *testing.T) {
	home := t.TempDir()
	first, err := Acquire(home, "cocola start")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Acquire(home, "cocola stop")
	if err == nil || !strings.Contains(err.Error(), "another Cocola operation") ||
		!strings.Contains(err.Error(), "cocola start") {
		t.Fatalf("concurrent acquire error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(home, "cocola stop")
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}
