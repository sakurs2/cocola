package httpapi

import (
	"context"
	"errors"
	"testing"

	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
)

type modelCapabilityStore struct {
	chatrun.Store
	enabled bool
	err     error
	calls   int
}

func (s *modelCapabilityStore) AnthropicServerWebToolsEnabled(
	_ context.Context,
	_ string,
) (bool, error) {
	s.calls++
	return s.enabled, s.err
}

func TestReadAnthropicServerWebToolsEnabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := &modelCapabilityStore{enabled: true}
	enabled, err := readAnthropicServerWebToolsEnabled(ctx, store, "route-1")
	if err != nil || !enabled || store.calls != 1 {
		t.Fatalf("enabled=%v calls=%d err=%v", enabled, store.calls, err)
	}

	store.err = errors.New("database unavailable")
	enabled, err = readAnthropicServerWebToolsEnabled(ctx, store, "route-1")
	if err == nil || enabled {
		t.Fatalf("read failure must fail closed: enabled=%v err=%v", enabled, err)
	}

	store.calls = 0
	enabled, err = readAnthropicServerWebToolsEnabled(ctx, store, "")
	if err != nil || enabled || store.calls != 0 {
		t.Fatalf("empty route should not query: enabled=%v calls=%d err=%v", enabled, store.calls, err)
	}
}

func TestReadAnthropicServerWebToolsEnabledWithoutReaderFailsClosed(t *testing.T) {
	t.Parallel()
	enabled, err := readAnthropicServerWebToolsEnabled(
		context.Background(),
		chatrun.NewMemory(nil),
		"route-1",
	)
	if err != nil || enabled {
		t.Fatalf("store without capability reader must fail closed: enabled=%v err=%v", enabled, err)
	}
}
