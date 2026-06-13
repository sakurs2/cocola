package docker

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types"
)

// fakeThawer is a minimal containerThawer that records calls and replays the
// inspect result the test wires up. It lets us exercise thawIfPaused without a
// live Docker daemon.
type fakeThawer struct {
	state       *types.ContainerState
	inspectErr  error
	unpauseErr  error
	inspectCnt  int
	unpauseCnt  int
	unpausedIDs []string
}

func (f *fakeThawer) ContainerInspect(_ context.Context, _ string) (types.ContainerJSON, error) {
	f.inspectCnt++
	if f.inspectErr != nil {
		return types.ContainerJSON{}, f.inspectErr
	}
	return types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{State: f.state},
	}, nil
}

func (f *fakeThawer) ContainerUnpause(_ context.Context, id string) error {
	f.unpauseCnt++
	f.unpausedIDs = append(f.unpausedIDs, id)
	return f.unpauseErr
}

func TestThawIfPaused_UnpausesFrozenSandbox(t *testing.T) {
	f := &fakeThawer{state: &types.ContainerState{Paused: true}}
	if err := thawIfPaused(context.Background(), f, "cid-1", "sbx-1"); err != nil {
		t.Fatalf("thawIfPaused returned error: %v", err)
	}
	if f.unpauseCnt != 1 {
		t.Fatalf("expected exactly one unpause, got %d", f.unpauseCnt)
	}
	if len(f.unpausedIDs) != 1 || f.unpausedIDs[0] != "cid-1" {
		t.Fatalf("expected unpause of cid-1, got %v", f.unpausedIDs)
	}
}

func TestThawIfPaused_RunningSandboxIsNoop(t *testing.T) {
	f := &fakeThawer{state: &types.ContainerState{Paused: false}}
	if err := thawIfPaused(context.Background(), f, "cid-2", "sbx-2"); err != nil {
		t.Fatalf("thawIfPaused returned error: %v", err)
	}
	if f.unpauseCnt != 0 {
		t.Fatalf("running sandbox should not be unpaused, got %d calls", f.unpauseCnt)
	}
}

func TestThawIfPaused_NilStateIsNoop(t *testing.T) {
	f := &fakeThawer{state: nil}
	if err := thawIfPaused(context.Background(), f, "cid-3", "sbx-3"); err != nil {
		t.Fatalf("thawIfPaused returned error: %v", err)
	}
	if f.unpauseCnt != 0 {
		t.Fatalf("nil state should be a no-op, got %d unpause calls", f.unpauseCnt)
	}
}

func TestThawIfPaused_InspectErrorIsSwallowed(t *testing.T) {
	// Inspect failures must not abort exec: the downstream exec call owns the
	// authoritative error (e.g. no-such-container).
	f := &fakeThawer{inspectErr: errors.New("boom")}
	if err := thawIfPaused(context.Background(), f, "cid-4", "sbx-4"); err != nil {
		t.Fatalf("inspect error should be swallowed, got %v", err)
	}
	if f.unpauseCnt != 0 {
		t.Fatalf("no unpause expected when inspect fails, got %d", f.unpauseCnt)
	}
}

func TestThawIfPaused_UnpauseErrorIsReturned(t *testing.T) {
	f := &fakeThawer{state: &types.ContainerState{Paused: true}, unpauseErr: errors.New("freezer stuck")}
	err := thawIfPaused(context.Background(), f, "cid-5", "sbx-5")
	if err == nil {
		t.Fatal("expected unpause error to propagate, got nil")
	}
}
