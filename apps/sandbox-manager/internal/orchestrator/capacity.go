package orchestrator

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
)

const (
	sandboxNodeModeAnnotation = "cocola.dev/sandbox-node-mode"
	sandboxNodeMaxAnnotation  = "cocola.dev/max-sandbox-pods"
)

var ErrCapacityBusy = errors.New("resource busy: no sandbox capacity available")

// ErrWorkspaceNodeUnavailable means a persisted node-local workspace cannot
// currently be mounted. Callers must not silently replace it on another node.
var ErrWorkspaceNodeUnavailable = errors.New("workspace node unavailable")

// CapacityGuard is consulted only before cold-creating a sandbox.
type CapacityGuard interface {
	SelectNode(ctx context.Context, preferredNode, excludedNode string, storageBytes map[string]int64) (string, error)
}

// NodeAvailabilityChecker is implemented by guards that can validate an
// already-running sandbox's node without counting that sandbox against cold
// creation capacity.
type NodeAvailabilityChecker interface {
	NodeAvailable(ctx context.Context, nodeName string) (bool, error)
}

// NewCapacityGuardFromEnv enables Kubernetes node capacity gating only for the
// k3s runtime. Other runtimes keep existing behavior.
func NewCapacityGuardFromEnv() (CapacityGuard, error) {
	if strings.ToLower(strings.TrimSpace(os.Getenv("COCOLA_CLUSTER_MANAGER_MODE"))) != "k3s" {
		return nil, nil
	}
	cfg, ok, err := kubeConfigFromEnv()
	if err != nil || !ok {
		return nil, err
	}
	return &KubeCapacityGuard{
		client:     newKubeClient(cfg),
		defaultMax: envOptionalInt("COCOLA_SANDBOX_NODE_DEFAULT_MAX"),
	}, nil
}

type KubeCapacityGuard struct {
	client     kubeReader
	defaultMax *int
}

type kubeReader interface {
	listNodes(ctx context.Context) ([]kubeNode, error)
	listSandboxPods(ctx context.Context) ([]kubePod, error)
}

func (g *KubeCapacityGuard) NodeAvailable(ctx context.Context, nodeName string) (bool, error) {
	nodes, err := g.client.listNodes(ctx)
	if err != nil {
		return false, err
	}
	for _, node := range nodes {
		if node.Metadata.Name == nodeName {
			return nodeUsable(node), nil
		}
	}
	return false, nil
}

func (g *KubeCapacityGuard) SelectNode(ctx context.Context, preferredNode, excludedNode string, storageBytes map[string]int64) (string, error) {
	nodes, err := g.client.listNodes(ctx)
	if err != nil {
		return "", err
	}
	pods, err := g.client.listSandboxPods(ctx)
	if err != nil {
		return "", err
	}

	usedByNode := map[string]int{}
	pending := 0
	for _, pod := range pods {
		if isFinishedPod(pod.Status.Phase) {
			continue
		}
		if pod.Spec.NodeName == "" {
			if target := pod.Spec.NodeSelector["kubernetes.io/hostname"]; target != "" {
				usedByNode[target]++
				continue
			}
			pending++
			continue
		}
		usedByNode[pod.Spec.NodeName]++
	}
	if preferredNode != "" {
		for _, node := range nodes {
			if node.Metadata.Name != preferredNode {
				continue
			}
			if nodeUsable(node) && nodeHasCapacity(node, usedByNode[preferredNode], g.defaultMax) {
				return preferredNode, nil
			}
			return "", ErrWorkspaceNodeUnavailable
		}
		return "", ErrWorkspaceNodeUnavailable
	}

	free := 0
	bestNode := ""
	bestRemaining := 0
	bestUsed := 0
	bestStorageBytes := int64(0)
	hasUnlimited := false
	for _, node := range nodes {
		if node.Metadata.Name == excludedNode || !nodeUsable(node) {
			continue
		}
		name := node.Metadata.Name
		if name == "" {
			continue
		}
		used := usedByNode[name]
		requested := storageBytes[name]
		max := nodeMaxSandboxPods(node, g.defaultMax)
		if max == nil {
			if !hasUnlimited || requested < bestStorageBytes ||
				(requested == bestStorageBytes && (used < bestUsed || (used == bestUsed && name < bestNode))) {
				hasUnlimited = true
				bestNode = name
				bestUsed = used
				bestStorageBytes = requested
			}
			continue
		}
		if remaining := *max - used; remaining > 0 {
			free += remaining
			if !hasUnlimited && (bestNode == "" || requested < bestStorageBytes ||
				(requested == bestStorageBytes && (remaining > bestRemaining ||
					(remaining == bestRemaining && name < bestNode)))) {
				bestNode = name
				bestRemaining = remaining
				bestStorageBytes = requested
			}
		}
	}
	if hasUnlimited {
		return bestNode, nil
	}
	if free-pending > 0 {
		return bestNode, nil
	}
	return "", ErrCapacityBusy
}

func nodeUsable(node kubeNode) bool {
	return nodeReady(node) && !nodeDiskPressure(node) && !node.Spec.Unschedulable && node.Metadata.Annotations[sandboxNodeModeAnnotation] != "offline"
}

func nodeHasCapacity(node kubeNode, used int, fallback *int) bool {
	max := nodeMaxSandboxPods(node, fallback)
	return max == nil || used < *max
}

func nodeDiskPressure(node kubeNode) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == "DiskPressure" {
			return condition.Status == "True"
		}
	}
	return false
}

func nodeMaxSandboxPods(node kubeNode, fallback *int) *int {
	if raw := strings.TrimSpace(node.Metadata.Annotations[sandboxNodeMaxAnnotation]); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return &n
		}
	}
	return fallback
}

func envOptionalInt(key string) *int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return nil
	}
	return &n
}
