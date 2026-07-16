package orchestrator

import (
	"context"
	"errors"
	"strconv"
	"testing"
)

type fakeKubeReader struct {
	nodes []kubeNode
	pods  []kubePod
}

func (f fakeKubeReader) listNodes(context.Context) ([]kubeNode, error) {
	return f.nodes, nil
}

func (f fakeKubeReader) listSandboxPods(context.Context) ([]kubePod, error) {
	return f.pods, nil
}

func TestKubeCapacityGuardRejectsWhenAllNodesFull(t *testing.T) {
	guard := &KubeCapacityGuard{
		client: fakeKubeReader{
			nodes: []kubeNode{readyNode("node-a", 1)},
			pods:  []kubePod{sandboxPod("node-a", "Running")},
		},
	}
	if node, err := guard.SelectNode(context.Background(), "", "", nil); !errors.Is(err, ErrCapacityBusy) {
		t.Fatalf("SelectNode = %q, %v, want ErrCapacityBusy", node, err)
	}
}

func TestKubeCapacityGuardSelectsNodeWithRoom(t *testing.T) {
	guard := &KubeCapacityGuard{
		client: fakeKubeReader{
			nodes: []kubeNode{readyNode("node-a", 1), readyNode("node-b", 2)},
			pods:  []kubePod{sandboxPod("node-a", "Running"), sandboxPod("node-b", "Running")},
		},
	}
	node, err := guard.SelectNode(context.Background(), "", "", nil)
	if err != nil {
		t.Fatalf("SelectNode error = %v, want nil", err)
	}
	if node != "node-b" {
		t.Fatalf("SelectNode = %q, want node-b", node)
	}
}

func TestKubeCapacityGuardChecksExistingNodeWithoutColdCapacity(t *testing.T) {
	guard := &KubeCapacityGuard{
		client: fakeKubeReader{
			nodes: []kubeNode{readyNode("node-a", 1)},
			pods:  []kubePod{sandboxPod("node-a", "Running")},
		},
	}
	available, err := guard.NodeAvailable(context.Background(), "node-a")
	if err != nil || !available {
		t.Fatalf("NodeAvailable = %v, %v, want true", available, err)
	}
	if available, err := guard.NodeAvailable(context.Background(), "missing"); err != nil || available {
		t.Fatalf("missing NodeAvailable = %v, %v, want false", available, err)
	}
}

func TestKubeCapacityGuardCountsPendingPodsAgainstTotalCapacity(t *testing.T) {
	guard := &KubeCapacityGuard{
		client: fakeKubeReader{
			nodes: []kubeNode{readyNode("node-a", 2)},
			pods:  []kubePod{sandboxPod("node-a", "Running"), sandboxPod("", "Pending")},
		},
	}
	if node, err := guard.SelectNode(context.Background(), "", "", nil); !errors.Is(err, ErrCapacityBusy) {
		t.Fatalf("SelectNode = %q, %v, want ErrCapacityBusy", node, err)
	}
}

func TestKubeCapacityGuardCountsNodeSelectedPendingPodOnTargetNode(t *testing.T) {
	pending := sandboxPod("", "Pending")
	pending.Spec.NodeSelector = map[string]string{"kubernetes.io/hostname": "node-a"}
	guard := &KubeCapacityGuard{
		client: fakeKubeReader{
			nodes: []kubeNode{readyNode("node-a", 1), readyNode("node-b", 1)},
			pods:  []kubePod{pending},
		},
	}
	node, err := guard.SelectNode(context.Background(), "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if node != "node-b" {
		t.Fatalf("SelectNode = %q, want node-b", node)
	}
}

func TestKubeCapacityGuardTreatsUnsetAsUnlimitedAndZeroAsZero(t *testing.T) {
	unlimited := &KubeCapacityGuard{
		client: fakeKubeReader{
			nodes: []kubeNode{readyNodeWithoutMax("node-a")},
			pods:  []kubePod{sandboxPod("node-a", "Running")},
		},
	}
	node, err := unlimited.SelectNode(context.Background(), "", "", nil)
	if err != nil {
		t.Fatalf("unset max error = %v, want nil", err)
	}
	if node != "node-a" {
		t.Fatalf("unset max node = %q, want node-a", node)
	}

	zero := &KubeCapacityGuard{
		client: fakeKubeReader{
			nodes: []kubeNode{readyNode("node-a", 0)},
		},
	}
	if node, err := zero.SelectNode(context.Background(), "", "", nil); !errors.Is(err, ErrCapacityBusy) {
		t.Fatalf("zero max SelectNode = %q, %v, want ErrCapacityBusy", node, err)
	}
}

func TestKubeCapacityGuardPrefersLeastUsedUnlimitedNode(t *testing.T) {
	guard := &KubeCapacityGuard{
		client: fakeKubeReader{
			nodes: []kubeNode{readyNodeWithoutMax("node-a"), readyNodeWithoutMax("node-b")},
			pods: []kubePod{
				sandboxPod("node-a", "Running"),
				sandboxPod("node-a", "Running"),
				sandboxPod("node-b", "Running"),
			},
		},
	}
	node, err := guard.SelectNode(context.Background(), "", "", nil)
	if err != nil {
		t.Fatalf("SelectNode error = %v, want nil", err)
	}
	if node != "node-b" {
		t.Fatalf("SelectNode = %q, want least-used node-b", node)
	}
}

func TestBinderCapacityGuardOnlyRunsOnCreateMiss(t *testing.T) {
	b, fp := newTestBinder(t)
	ctx := context.Background()
	if _, err := b.Acquire(ctx, AcquireSpec{SessionID: "s1", UserID: "u"}); err != nil {
		t.Fatal(err)
	}
	b.WithCapacityGuard(alwaysBusyGuard{})
	if _, err := b.Acquire(ctx, AcquireSpec{SessionID: "s1", UserID: "u"}); err != nil {
		t.Fatalf("reuse should not check capacity: %v", err)
	}
	if got := fp.creates.Load(); got != 1 {
		t.Fatalf("creates = %d, want 1", got)
	}
	if _, err := b.Acquire(ctx, AcquireSpec{SessionID: "s2", UserID: "u"}); !errors.Is(err, ErrCapacityBusy) {
		t.Fatalf("new session error = %v, want ErrCapacityBusy", err)
	}
}

func TestBinderPassesSelectedNodeToProvider(t *testing.T) {
	b, fp := newTestBinder(t)
	b.WithCapacityGuard(staticNodeGuard("node-b"))

	if _, err := b.Acquire(context.Background(), AcquireSpec{SessionID: "s1", UserID: "u"}); err != nil {
		t.Fatal(err)
	}

	fp.mu.Lock()
	got := fp.lastSpec.TargetNodeName
	fp.mu.Unlock()
	if got != "node-b" {
		t.Fatalf("TargetNodeName = %q, want node-b", got)
	}
}

type alwaysBusyGuard struct{}

func (alwaysBusyGuard) SelectNode(context.Context, string, string, map[string]int64) (string, error) {
	return "", ErrCapacityBusy
}

type staticNodeGuard string

func (g staticNodeGuard) SelectNode(context.Context, string, string, map[string]int64) (string, error) {
	return string(g), nil
}

func readyNode(name string, max int) kubeNode {
	n := kubeNode{}
	n.Metadata.Name = name
	n.Metadata.Annotations = map[string]string{sandboxNodeMaxAnnotation: strconv.Itoa(max)}
	n.Status.Conditions = append(n.Status.Conditions, struct {
		Type    string `json:"type"`
		Status  string `json:"status"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}{Type: "Ready", Status: "True"})
	return n
}

func readyNodeWithoutMax(name string) kubeNode {
	n := readyNode(name, 1)
	n.Metadata.Annotations = nil
	return n
}

func sandboxPod(node, phase string) kubePod {
	p := kubePod{}
	p.Spec.NodeName = node
	p.Status.Phase = phase
	return p
}
