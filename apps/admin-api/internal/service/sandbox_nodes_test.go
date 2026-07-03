package service

import "testing"

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
