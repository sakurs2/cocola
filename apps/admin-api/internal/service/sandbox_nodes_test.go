package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

func TestNewSandboxNodeManagerFromEnvRequiresK3SMode(t *testing.T) {
	t.Setenv("COCOLA_K8S_API_SERVER", "https://example.invalid")
	t.Setenv("COCOLA_K8S_BEARER_TOKEN", "token")

	mgr, err := NewSandboxNodeManagerFromEnv()
	if err != nil {
		t.Fatalf("without mode: %v", err)
	}
	if mgr != nil {
		t.Fatalf("without COCOLA_CLUSTER_MANAGER_MODE=k3s manager should be disabled")
	}

	t.Setenv("COCOLA_CLUSTER_MANAGER_MODE", "k3s")
	mgr, err = NewSandboxNodeManagerFromEnv()
	if err != nil {
		t.Fatalf("with k3s mode: %v", err)
	}
	if mgr == nil {
		t.Fatalf("with k3s mode and kube API server manager should be enabled")
	}
}

func TestNewSandboxNodeManagerFromEnvComposeMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/node" || r.Header.Get(hostAgentKeyHeader) != "key" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(hostAgentNode{
			Name: "local", Ready: true, CPUCapacity: "8", CPUAllocatable: "8",
			MemoryCapacity: "1024Ki", MemoryAllocatable: "1024Ki",
			Labels: map[string]string{"cocola.dev/runtime-mode": "compose"},
		})
	}))
	defer server.Close()
	t.Setenv("COCOLA_CLUSTER_MANAGER_MODE", "compose")
	t.Setenv("COCOLA_HOST_AGENT_URL", server.URL)
	t.Setenv("COCOLA_HOST_AGENT_KEY", "key")

	mgr, err := NewSandboxNodeManagerFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := mgr.ListNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes.Nodes) != 1 || nodes.Nodes[0].Name != "local" || !nodes.Nodes[0].Ready {
		t.Fatalf("compose nodes = %+v", nodes.Nodes)
	}
	if _, err := mgr.DisableNode(context.Background(), "local"); !errors.Is(err, ErrNodeOperationUnsupported) {
		t.Fatalf("compose node operation error = %v", err)
	}
}

func TestNodeSummaryDistinguishesDisabledAndOffline(t *testing.T) {
	base := kubeNode{}
	base.Metadata.Name = "node-a"
	base.Status.Conditions = append(base.Status.Conditions, struct {
		Type    string `json:"type"`
		Status  string `json:"status"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}{Type: "Ready", Status: "True", Reason: "KubeletReady"})
	base.Spec.Unschedulable = true

	disabled := nodeSummary(base, 0)
	if disabled.Status != "disabled" {
		t.Fatalf("plain cordoned node status = %q, want disabled", disabled.Status)
	}

	offlineNode := base
	offlineNode.Metadata.Annotations = map[string]string{sandboxNodeModeAnnotation: "offline"}
	offline := nodeSummary(offlineNode, 0)
	if offline.Status != "offline" {
		t.Fatalf("offline node status = %q, want offline", offline.Status)
	}

	pending := nodeSummary(offlineNode, 2)
	if pending.Status != "offline_pending" {
		t.Fatalf("offline node with sandbox pods status = %q, want offline_pending", pending.Status)
	}
}

type fakeSandboxNodeManager struct {
	nodes        SandboxNodeList
	offlineCalls int
}

func (f *fakeSandboxNodeManager) ListNodes(context.Context) (SandboxNodeList, error) {
	return f.nodes, nil
}
func (f *fakeSandboxNodeManager) DisableNode(context.Context, string) (SandboxNode, error) {
	return SandboxNode{}, nil
}
func (f *fakeSandboxNodeManager) RestoreNode(context.Context, string) (SandboxNode, error) {
	return SandboxNode{}, nil
}
func (f *fakeSandboxNodeManager) SetMaxSandboxPods(context.Context, string, *int) (SandboxNode, error) {
	return SandboxNode{}, nil
}
func (f *fakeSandboxNodeManager) OfflineNode(context.Context, string, bool) (OfflineNodeResult, error) {
	f.offlineCalls++
	return OfflineNodeResult{}, nil
}
func (f *fakeSandboxNodeManager) JoinCommand(context.Context) (JoinCommand, error) {
	return JoinCommand{}, nil
}

type fakeSessionStorageMonitor struct {
	usage map[string]NodeStorageUsage
	err   error
}

func (*fakeSessionStorageMonitor) List(context.Context) ([]SessionStorageView, error) {
	return nil, nil
}
func (f *fakeSessionStorageMonitor) NodeUsage(context.Context) (map[string]NodeStorageUsage, error) {
	return f.usage, f.err
}
func (*fakeSessionStorageMonitor) NodeFilesystems(context.Context) ([]NodeStorageFilesystem, error) {
	return nil, nil
}
func (*fakeSessionStorageMonitor) Measure(context.Context, string, string) (SessionStorageMeasurement, error) {
	return SessionStorageMeasurement{}, nil
}
func (*fakeSessionStorageMonitor) DeleteOrphan(context.Context, string, string) error {
	return nil
}
func (*fakeSessionStorageMonitor) Close() {}

func TestSandboxNodesIncludeLocalStorageUsageAndRequireOfflineConfirmation(t *testing.T) {
	nodes := &fakeSandboxNodeManager{nodes: SandboxNodeList{Nodes: []SandboxNode{{
		Name: "node-a", Ready: true, Schedulable: true,
	}}}}
	monitor := &fakeSessionStorageMonitor{usage: map[string]NodeStorageUsage{
		"node-a": {SessionCount: 2, RequestedBytes: 4 * 1024 * 1024 * 1024, ResetCount: 1},
	}}
	svc := New(store.NewMemory(), nil, time.Now).
		WithSandboxNodeManager(nodes).
		WithSessionStorageMonitor(monitor)

	listed, err := svc.ListSandboxNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !listed.UsageAvailable {
		t.Fatal("node storage usage should be available")
	}
	got := listed.Nodes[0]
	if got.SessionCount != 2 || got.SessionRequestedBytes != 4*1024*1024*1024 || got.WorkspaceResetCount != 1 {
		t.Fatalf("node storage usage = %+v", got)
	}
	result, err := svc.OfflineSandboxNode(context.Background(), "node-a", false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if result.AffectedSessions != 2 || nodes.offlineCalls != 0 {
		t.Fatalf("offline result = %+v, calls = %d", result, nodes.offlineCalls)
	}
}

func TestSandboxNodeListDegradesWithoutStorageMonitorAndOperationsFailClosed(t *testing.T) {
	nodes := &fakeSandboxNodeManager{nodes: SandboxNodeList{Nodes: []SandboxNode{{Name: "node-a"}}}}
	svc := New(store.NewMemory(), nil, time.Now).WithSandboxNodeManager(nodes)

	listed, err := svc.ListSandboxNodes(context.Background())
	if err != nil {
		t.Fatalf("ListSandboxNodes error = %v", err)
	}
	if listed.UsageAvailable || len(listed.Nodes) != 1 || listed.Nodes[0].Name != "node-a" {
		t.Fatalf("ListSandboxNodes result = %+v", listed)
	}
	if _, err := svc.OfflineSandboxNode(context.Background(), "node-a", true, "admin"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("OfflineSandboxNode error = %v, want ErrNotConfigured", err)
	}
	if nodes.offlineCalls != 0 {
		t.Fatalf("offline calls = %d, want 0", nodes.offlineCalls)
	}
}

func TestSandboxNodeListDegradesWhenStorageUsageFails(t *testing.T) {
	nodes := &fakeSandboxNodeManager{nodes: SandboxNodeList{Nodes: []SandboxNode{{Name: "node-a"}}}}
	monitor := &fakeSessionStorageMonitor{err: errors.New("storage probe failed")}
	svc := New(store.NewMemory(), nil, time.Now).
		WithSandboxNodeManager(nodes).
		WithSessionStorageMonitor(monitor)

	listed, err := svc.ListSandboxNodes(context.Background())
	if err != nil {
		t.Fatalf("ListSandboxNodes error = %v", err)
	}
	if listed.UsageAvailable || len(listed.Nodes) != 1 {
		t.Fatalf("ListSandboxNodes result = %+v", listed)
	}
}
