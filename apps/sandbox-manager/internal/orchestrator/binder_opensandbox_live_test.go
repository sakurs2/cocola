package orchestrator

// Live binder->real-OpenSandbox integration check for the ON-DEMAND allocation
// path (ADR-0015/0016). Skipped unless COCOLA_OPENSANDBOX_URL is set, so the
// normal hermetic suite is unaffected. This exercises the exact slow path a
// /v1/chat turn drives: AcquireWithOutcome -> per-session lock -> provider.Create
// (with mapVolumes session workspace mount) -> bind; then the fast path (reuse) and a
// real Exec inside the bound box. On-demand cold-start is the only allocation
// path (warm pool removed in ADR-0016); no listeners started.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider/opensandbox"
	rds "github.com/cocola-project/cocola/packages/go-common/redis"
)

func TestLive_OnDemandAllocation_OpenSandbox(t *testing.T) {
	if os.Getenv("COCOLA_OPENSANDBOX_URL") == "" {
		t.Skip("COCOLA_OPENSANDBOX_URL not set; skipping live on-demand allocation check")
	}
	p, err := opensandbox.New()
	if err != nil {
		t.Fatalf("opensandbox.New: %v", err)
	}
	b := NewBinder(rds.NewFake(), p, Config{
		LeaseTTL:       30 * time.Second,
		HeartbeatEvery: 10 * time.Second,
		DestroyGrace:   5 * time.Second,
		LockTTL:        15 * time.Second,
		ReaperEvery:    time.Second,
		LockRetry:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	img := os.Getenv("COCOLA_SANDBOX_IMAGE")
	if img == "" {
		img = "python:3.12-slim"
	}
	spec := AcquireSpec{SessionID: "live-sess-1", UserID: "live-user-1", Image: img}

	// --- slow path: fresh on-demand create + mount ---
	out1, err := b.AcquireWithOutcome(ctx, spec)
	if err != nil {
		t.Fatalf("first acquire (create): %v", err)
	}
	if out1.Reused {
		t.Fatalf("first acquire should be a fresh create, got Reused=true")
	}
	sid := out1.Sandbox.ID
	t.Logf("slow path: created+bound sandbox %s", sid)
	defer func() { _ = p.Destroy(context.Background(), sid) }()

	// --- fast path: same session reuses the same box ---
	out2, err := b.AcquireWithOutcome(ctx, spec)
	if err != nil {
		t.Fatalf("second acquire (reuse): %v", err)
	}
	if !out2.Reused {
		t.Fatalf("second acquire should reuse, got Reused=false")
	}
	if out2.Sandbox.ID != sid {
		t.Fatalf("reuse returned a different sandbox: %s != %s", out2.Sandbox.ID, sid)
	}
	t.Logf("fast path: reused sandbox %s", out2.Sandbox.ID)

	// Wait until the box reports healthy before exec (a real /v1/chat turn
	// likewise runs after the sandbox is up). Mirrors the verify harness.
	deadline := time.Now().Add(60 * time.Second)
	for {
		h, herr := p.Health(ctx, sid)
		if herr == nil && h != nil && h.Healthy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sandbox %s never became healthy: err=%v health=%+v", sid, herr, h)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// --- the dialogue's actual work: run a command inside the bound box and
	//     prove the session workspace mounts are present (mapVolumes wired). ---
	ev, err := p.Exec(ctx, sid, provider.ExecRequest{
		Cmd: []string{"sh", "-c", "echo dialogue-ok && ls -d /workspace /home/cocola/.claude /data/plugins"},
	})
	if err != nil {
		t.Fatalf("exec in bound box: %v", err)
	}
	var sb strings.Builder
	var exit int32 = -1
	for e := range ev {
		switch e.Kind {
		case provider.ExecEventStdout:
			sb.Write(e.Stdout)
		case provider.ExecEventStderr:
			sb.Write(e.Stderr)
		case provider.ExecEventExit:
			exit = e.Exit
		case provider.ExecEventError:
			t.Fatalf("exec error event: %v", e.Err)
		}
	}
	gotOut := sb.String()
	t.Logf("exec exit=%d output=%q", exit, gotOut)
	if exit != 0 {
		t.Fatalf("exec exit=%d (want 0); output=%q", exit, gotOut)
	}
	for _, want := range []string{"dialogue-ok", "/workspace", "/home/cocola/.claude", "/data/plugins"} {
		if !strings.Contains(gotOut, want) {
			t.Fatalf("exec output missing %q; got %q", want, gotOut)
		}
	}
	t.Log("on-demand allocation path OK: slow-create+mount -> fast-reuse -> exec sees session workspace mounts")
}
