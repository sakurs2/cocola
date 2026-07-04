package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
	rds "github.com/cocola-project/cocola/packages/go-common/redis"
	"github.com/cocola-project/cocola/packages/go-common/token"
)

type fakeSandboxPodReader struct {
	pods []kubePod
}

func (f fakeSandboxPodReader) ListSandboxPods(context.Context) ([]kubePod, error) {
	return f.pods, nil
}

func TestSandboxRuntimeManagerMapsStateUserAndPod(t *testing.T) {
	ctx := context.Background()
	kv := rds.NewFake()
	putRuntimeMeta(t, kv, sandboxRuntimeMeta{
		SandboxID:   "sb-running",
		SessionID:   "conv-1",
		UserID:      "alice@example.com",
		Image:       "cocola/sandbox-runtime:dev",
		State:       "active",
		CreatedUnix: 100,
	})
	if err := kv.Set(ctx, sandboxLeaseKey("sb-running"), "1", time.Minute); err != nil {
		t.Fatalf("set lease: %v", err)
	}
	putRuntimeMeta(t, kv, sandboxRuntimeMeta{
		SandboxID:   "sb-idle",
		SessionID:   "conv-2",
		UserID:      "unknown",
		State:       "active",
		CreatedUnix: 200,
	})
	putRuntimeMeta(t, kv, sandboxRuntimeMeta{
		SandboxID:   "sb-paused",
		SessionID:   "conv-3",
		UserID:      "alice@example.com",
		State:       "paused",
		CreatedUnix: 300,
		PausedUnix:  360,
	})

	pods := fakeSandboxPodReader{pods: []kubePod{
		runtimePod("pod-running", "sb-running", "Running", "node-a"),
		runtimePod("pod-starting", "sb-idle", "Pending", "node-b"),
		runtimePod("pod-paused", "sb-paused", "Running", "node-c"),
	}}
	mgr := NewRedisSandboxRuntimeManager(kv, WithSandboxPodReader(pods))

	mem := store.NewMemory()
	svc := New(mem, token.NewIssuer("s", "cocola", time.Hour), time.Now)
	if err := mem.CreateAuthUser(ctx, store.AuthUser{
		ID:        "user-1",
		Username:  "alice",
		Email:     "alice@example.com",
		Name:      "alice",
		Role:      RoleUser,
		Enabled:   true,
		CreatedAt: time.Unix(1, 0),
		UpdatedAt: time.Unix(1, 0),
	}); err != nil {
		t.Fatalf("create auth user: %v", err)
	}
	svc.WithSandboxRuntimeManager(svc.AttachSandboxRuntimeUsernames(mgr))

	list, err := svc.ListSandboxes(ctx)
	if err != nil {
		t.Fatalf("ListSandboxes: %v", err)
	}
	if len(list.Sandboxes) != 3 {
		t.Fatalf("sandboxes: want 3, got %+v", list.Sandboxes)
	}
	byID := map[string]SandboxRuntime{}
	for _, sb := range list.Sandboxes {
		byID[sb.SandboxID] = sb
	}
	running := byID["sb-running"]
	if running.Status != "running" || running.Username != "alice" || running.PodName != "pod-running" || running.NodeName != "node-a" {
		t.Fatalf("running sandbox wrong: %+v", running)
	}
	starting := byID["sb-idle"]
	if starting.Status != "starting" || starting.PodPhase != "Pending" {
		t.Fatalf("starting sandbox wrong: %+v", starting)
	}
	paused := byID["sb-paused"]
	if paused.Status != "reclaiming" || paused.PausedAt.IsZero() {
		t.Fatalf("paused sandbox wrong: %+v", paused)
	}
}

func TestSandboxRuntimeManagerPendingReclaimWithoutLease(t *testing.T) {
	ctx := context.Background()
	kv := rds.NewFake()
	putRuntimeMeta(t, kv, sandboxRuntimeMeta{
		SandboxID: "sb-no-lease",
		SessionID: "conv",
		UserID:    "u",
		State:     "active",
	})
	mgr := NewRedisSandboxRuntimeManager(kv)

	list, err := mgr.ListSandboxes(ctx)
	if err != nil {
		t.Fatalf("ListSandboxes: %v", err)
	}
	if len(list.Sandboxes) != 1 || list.Sandboxes[0].Status != "pending_reclaim" {
		t.Fatalf("status = %+v, want pending_reclaim", list.Sandboxes)
	}
}

func TestSandboxRuntimeManagerDropsStaleMetaWithoutPod(t *testing.T) {
	ctx := context.Background()
	kv := rds.NewFake()
	putRuntimeMeta(t, kv, sandboxRuntimeMeta{
		SandboxID: "sb-stale",
		SessionID: "conv-stale",
		UserID:    "u",
		State:     "active",
	})
	mgr := NewRedisSandboxRuntimeManager(kv, WithSandboxPodReader(fakeSandboxPodReader{}))

	list, err := mgr.ListSandboxes(ctx)
	if err != nil {
		t.Fatalf("ListSandboxes: %v", err)
	}
	if len(list.Sandboxes) != 0 {
		t.Fatalf("sandboxes = %+v, want stale metadata removed from response", list.Sandboxes)
	}
	if _, err := kv.Get(ctx, sandboxMetaKey("sb-stale")); err != rds.ErrNil {
		t.Fatalf("meta after cleanup err = %v, want ErrNil", err)
	}
	if _, err := kv.Get(ctx, sandboxConvKey("conv-stale")); err != rds.ErrNil {
		t.Fatalf("conv after cleanup err = %v, want ErrNil", err)
	}
}

func TestSandboxRuntimeManagerKeepsPendingReclaimWithPod(t *testing.T) {
	ctx := context.Background()
	kv := rds.NewFake()
	putRuntimeMeta(t, kv, sandboxRuntimeMeta{
		SandboxID: "sb-pending",
		SessionID: "conv-pending",
		UserID:    "u",
		State:     "active",
	})
	mgr := NewRedisSandboxRuntimeManager(kv, WithSandboxPodReader(fakeSandboxPodReader{
		pods: []kubePod{runtimePod("pod-pending", "sb-pending", "Running", "node-a")},
	}))

	list, err := mgr.ListSandboxes(ctx)
	if err != nil {
		t.Fatalf("ListSandboxes: %v", err)
	}
	if len(list.Sandboxes) != 1 || list.Sandboxes[0].Status != "pending_reclaim" {
		t.Fatalf("sandboxes = %+v, want pending_reclaim retained while pod exists", list.Sandboxes)
	}
	if _, err := kv.Get(ctx, sandboxMetaKey("sb-pending")); err != nil {
		t.Fatalf("meta should remain while pod exists: %v", err)
	}
}

func putRuntimeMeta(t *testing.T, kv rds.KV, meta sandboxRuntimeMeta) {
	t.Helper()
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	if err := kv.Set(context.Background(), sandboxMetaKey(meta.SandboxID), string(raw), 0); err != nil {
		t.Fatalf("set meta: %v", err)
	}
}

func runtimePod(name, sandboxID, phase, node string) kubePod {
	p := kubePod{}
	p.Metadata.Name = name
	p.Metadata.Namespace = "opensandbox"
	p.Metadata.Labels = map[string]string{"opensandbox.io/id": sandboxID}
	p.Spec.NodeName = node
	p.Status.Phase = phase
	return p
}
