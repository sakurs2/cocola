package service

import "testing"

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
