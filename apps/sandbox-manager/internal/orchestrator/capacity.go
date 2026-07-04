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

// CapacityGuard is consulted only before cold-creating a sandbox.
type CapacityGuard interface {
	SelectNode(ctx context.Context) (string, error)
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

func (g *KubeCapacityGuard) SelectNode(ctx context.Context) (string, error) {
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
			pending++
			continue
		}
		usedByNode[pod.Spec.NodeName]++
	}

	free := 0
	bestNode := ""
	bestRemaining := 0
	bestUsed := 0
	hasUnlimited := false
	for _, node := range nodes {
		if !nodeReady(node) || node.Spec.Unschedulable || node.Metadata.Annotations[sandboxNodeModeAnnotation] == "offline" {
			continue
		}
		name := node.Metadata.Name
		if name == "" {
			continue
		}
		used := usedByNode[name]
		max := nodeMaxSandboxPods(node, g.defaultMax)
		if max == nil {
			if !hasUnlimited || used < bestUsed || (used == bestUsed && name < bestNode) {
				hasUnlimited = true
				bestNode = name
				bestUsed = used
			}
			continue
		}
		if remaining := *max - used; remaining > 0 {
			free += remaining
			if !hasUnlimited && (remaining > bestRemaining || (remaining == bestRemaining && (bestNode == "" || name < bestNode))) {
				bestNode = name
				bestRemaining = remaining
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
