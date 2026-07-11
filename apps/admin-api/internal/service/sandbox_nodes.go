package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

var ErrNotConfigured = errors.New("service: sandbox node manager not configured")

// SandboxNodeManager is the small operations surface cocola needs for k3s node
// management. It intentionally does not expose scheduling primitives.
type SandboxNodeManager interface {
	ListNodes(ctx context.Context) (SandboxNodeList, error)
	DisableNode(ctx context.Context, name string) (SandboxNode, error)
	RestoreNode(ctx context.Context, name string) (SandboxNode, error)
	SetMaxSandboxPods(ctx context.Context, name string, max *int) (SandboxNode, error)
	OfflineNode(ctx context.Context, name string, force bool) (OfflineNodeResult, error)
	JoinCommand(ctx context.Context) (JoinCommand, error)
}

type SandboxNodeList struct {
	Nodes []SandboxNode `json:"nodes"`
}

type SandboxNode struct {
	Name              string            `json:"name"`
	Status            string            `json:"status"`
	Ready             bool              `json:"ready"`
	Schedulable       bool              `json:"schedulable"`
	CPUCapacity       string            `json:"cpu_capacity"`
	MemoryCapacity    string            `json:"memory_capacity"`
	CPUAllocatable    string            `json:"cpu_allocatable"`
	MemoryAllocatable string            `json:"memory_allocatable"`
	SandboxPods       int               `json:"sandbox_pods"`
	MaxSandboxPods    *int              `json:"max_sandbox_pods,omitempty"`
	Reason            string            `json:"reason,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
}

type OfflineNodeResult struct {
	Node        SandboxNode `json:"node"`
	EvictedPods []string    `json:"evicted_pods,omitempty"`
	PendingPods []string    `json:"pending_pods,omitempty"`
	Message     string      `json:"message"`
}

type JoinCommand struct {
	Command string `json:"command"`
	Note    string `json:"note"`
}

func (a *Admin) ListSandboxNodes(ctx context.Context) (SandboxNodeList, error) {
	if a.sandboxNodes == nil {
		return SandboxNodeList{}, ErrNotConfigured
	}
	return a.sandboxNodes.ListNodes(ctx)
}

func (a *Admin) DisableSandboxNode(ctx context.Context, name, actor string) (SandboxNode, error) {
	if a.sandboxNodes == nil {
		return SandboxNode{}, ErrNotConfigured
	}
	if strings.TrimSpace(name) == "" {
		return SandboxNode{}, ErrInvalidArg
	}
	out, err := a.sandboxNodes.DisableNode(ctx, name)
	if err != nil {
		return SandboxNode{}, err
	}
	return out, nil
}

func (a *Admin) RestoreSandboxNode(ctx context.Context, name, actor string) (SandboxNode, error) {
	if a.sandboxNodes == nil {
		return SandboxNode{}, ErrNotConfigured
	}
	if strings.TrimSpace(name) == "" {
		return SandboxNode{}, ErrInvalidArg
	}
	out, err := a.sandboxNodes.RestoreNode(ctx, name)
	if err != nil {
		return SandboxNode{}, err
	}
	return out, nil
}

func (a *Admin) SetSandboxNodeMaxPods(ctx context.Context, name string, max *int, actor string) (SandboxNode, error) {
	if a.sandboxNodes == nil {
		return SandboxNode{}, ErrNotConfigured
	}
	if strings.TrimSpace(name) == "" {
		return SandboxNode{}, ErrInvalidArg
	}
	if max != nil && *max < 0 {
		return SandboxNode{}, ErrInvalidArg
	}
	out, err := a.sandboxNodes.SetMaxSandboxPods(ctx, name, max)
	if err != nil {
		return SandboxNode{}, err
	}
	return out, nil
}

func (a *Admin) OfflineSandboxNode(ctx context.Context, name string, force bool, actor string) (OfflineNodeResult, error) {
	if a.sandboxNodes == nil {
		return OfflineNodeResult{}, ErrNotConfigured
	}
	if strings.TrimSpace(name) == "" {
		return OfflineNodeResult{}, ErrInvalidArg
	}
	out, err := a.sandboxNodes.OfflineNode(ctx, name, force)
	if err != nil {
		return OfflineNodeResult{}, err
	}
	return out, nil
}

func (a *Admin) SandboxNodeJoinCommand(ctx context.Context) (JoinCommand, error) {
	if a.sandboxNodes == nil {
		return JoinCommand{
			Command: "curl -sfL https://get.k3s.io | K3S_URL=https://<server>:6443 K3S_TOKEN=<token> sh -",
			Note:    "Set COCOLA_K3S_JOIN_COMMAND on admin-api to show an environment-specific command.",
		}, nil
	}
	return a.sandboxNodes.JoinCommand(ctx)
}

// NewSandboxNodeManagerFromEnv returns nil when Kubernetes configuration is not
// present. This keeps admin-api's existing zero-dependency dev mode intact.
func NewSandboxNodeManagerFromEnv() (SandboxNodeManager, error) {
	if strings.ToLower(strings.TrimSpace(os.Getenv("COCOLA_CLUSTER_MANAGER_MODE"))) != "k3s" {
		return nil, nil
	}
	cfg, ok, err := kubeConfigFromEnv()
	if err != nil || !ok {
		return nil, err
	}
	return NewKubeSandboxNodeManager(cfg), nil
}

type KubeSandboxNodeManager struct {
	client *kubeClient
}

const sandboxNodeModeAnnotation = "cocola.dev/sandbox-node-mode"
const sandboxNodeMaxAnnotation = "cocola.dev/max-sandbox-pods"

func NewKubeSandboxNodeManager(cfg kubeConfig) *KubeSandboxNodeManager {
	return &KubeSandboxNodeManager{client: newKubeClient(cfg)}
}

func (m *KubeSandboxNodeManager) ListNodes(ctx context.Context) (SandboxNodeList, error) {
	nodes, pods, err := m.client.snapshot(ctx)
	if err != nil {
		return SandboxNodeList{}, err
	}
	podsByNode := map[string][]kubePod{}
	for _, p := range pods {
		if p.Spec.NodeName == "" || isFinishedPod(p.Status.Phase) {
			continue
		}
		podsByNode[p.Spec.NodeName] = append(podsByNode[p.Spec.NodeName], p)
	}
	out := make([]SandboxNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, nodeSummary(n, len(podsByNode[n.Metadata.Name])))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return SandboxNodeList{Nodes: out}, nil
}

func (m *KubeSandboxNodeManager) DisableNode(ctx context.Context, name string) (SandboxNode, error) {
	if err := m.client.patchNodeState(ctx, name, true, "disabled"); err != nil {
		return SandboxNode{}, err
	}
	return m.getNode(ctx, name)
}

func (m *KubeSandboxNodeManager) RestoreNode(ctx context.Context, name string) (SandboxNode, error) {
	if err := m.client.patchNodeState(ctx, name, false, ""); err != nil {
		return SandboxNode{}, err
	}
	return m.getNode(ctx, name)
}

func (m *KubeSandboxNodeManager) SetMaxSandboxPods(ctx context.Context, name string, max *int) (SandboxNode, error) {
	var value any
	if max != nil {
		value = fmt.Sprintf("%d", *max)
	}
	if err := m.client.patchNodeAnnotation(ctx, name, sandboxNodeMaxAnnotation, value); err != nil {
		return SandboxNode{}, err
	}
	return m.getNode(ctx, name)
}

func (m *KubeSandboxNodeManager) OfflineNode(ctx context.Context, name string, force bool) (OfflineNodeResult, error) {
	if err := m.client.patchNodeState(ctx, name, true, "offline"); err != nil {
		return OfflineNodeResult{}, err
	}
	pods, err := m.client.listSandboxPods(ctx)
	if err != nil {
		return OfflineNodeResult{}, err
	}
	pending := make([]kubePod, 0)
	for _, p := range pods {
		if p.Spec.NodeName == name && !isFinishedPod(p.Status.Phase) {
			pending = append(pending, p)
		}
	}
	if len(pending) > 0 && !force {
		node, getErr := m.getNode(ctx, name)
		if getErr != nil {
			return OfflineNodeResult{}, getErr
		}
		names := podNames(pending)
		node.Status = "offline_pending"
		node.SandboxPods = len(names)
		return OfflineNodeResult{
			Node:        node,
			PendingPods: names,
			Message:     "node cordoned; confirm force=true to evict sandbox pods",
		}, nil
	}
	evicted := make([]string, 0, len(pending))
	for _, p := range pending {
		if err := m.client.evictPod(ctx, p.Metadata.Namespace, p.Metadata.Name); err != nil {
			return OfflineNodeResult{}, err
		}
		evicted = append(evicted, p.Metadata.Name)
	}
	node, err := m.getNode(ctx, name)
	if err != nil {
		return OfflineNodeResult{}, err
	}
	if len(evicted) > 0 {
		node.Status = "offline_pending"
		node.SandboxPods = len(evicted)
		return OfflineNodeResult{Node: node, EvictedPods: evicted, Message: "node cordoned; sandbox pod eviction requested"}, nil
	}
	node.Status = "offline"
	return OfflineNodeResult{Node: node, Message: "node cordoned and has no running sandbox pods"}, nil
}

func (m *KubeSandboxNodeManager) JoinCommand(context.Context) (JoinCommand, error) {
	if cmd := strings.TrimSpace(os.Getenv("COCOLA_K3S_JOIN_COMMAND")); cmd != "" {
		return JoinCommand{Command: cmd, Note: "Run this on a Linux machine to join it as a k3s agent."}, nil
	}
	server := strings.TrimSpace(os.Getenv("COCOLA_K3S_SERVER_URL"))
	if server == "" {
		server = "https://<server>:6443"
	}
	tokenRef := strings.TrimSpace(os.Getenv("COCOLA_K3S_TOKEN_REF"))
	if tokenRef == "" {
		tokenRef = "<token from /var/lib/rancher/k3s/server/node-token>"
	}
	return JoinCommand{
		Command: fmt.Sprintf("curl -sfL https://get.k3s.io | K3S_URL=%s K3S_TOKEN=%s sh -", server, tokenRef),
		Note:    "cocola only displays the join command; the operator runs it on the target machine.",
	}, nil
}

func (m *KubeSandboxNodeManager) getNode(ctx context.Context, name string) (SandboxNode, error) {
	n, err := m.client.getNode(ctx, name)
	if err != nil {
		return SandboxNode{}, err
	}
	pods, err := m.client.listSandboxPods(ctx)
	if err != nil {
		return SandboxNode{}, err
	}
	count := 0
	for _, p := range pods {
		if p.Spec.NodeName == name && !isFinishedPod(p.Status.Phase) {
			count++
		}
	}
	return nodeSummary(n, count), nil
}

func nodeSummary(n kubeNode, sandboxPods int) SandboxNode {
	ready, reason := nodeReady(n)
	schedulable := !n.Spec.Unschedulable
	mode := n.Metadata.Annotations[sandboxNodeModeAnnotation]
	status := "active"
	switch {
	case !ready:
		status = "unhealthy"
	case mode == "offline" && !schedulable && sandboxPods > 0:
		status = "offline_pending"
	case mode == "offline" && !schedulable:
		status = "offline"
	case !schedulable && sandboxPods > 0:
		status = "offline_pending"
	case !schedulable:
		status = "disabled"
	}
	return SandboxNode{
		Name:              n.Metadata.Name,
		Status:            status,
		Ready:             ready,
		Schedulable:       schedulable,
		CPUCapacity:       n.Status.Capacity["cpu"],
		MemoryCapacity:    n.Status.Capacity["memory"],
		CPUAllocatable:    n.Status.Allocatable["cpu"],
		MemoryAllocatable: n.Status.Allocatable["memory"],
		SandboxPods:       sandboxPods,
		MaxSandboxPods:    parseMaxSandboxPods(n.Metadata.Annotations[sandboxNodeMaxAnnotation]),
		Reason:            reason,
		Labels:            n.Metadata.Labels,
	}
}

func parseMaxSandboxPods(raw string) *int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return nil
	}
	return &n
}

func nodeReady(n kubeNode) (bool, string) {
	for _, c := range n.Status.Conditions {
		if c.Type == "Ready" {
			if c.Status == "True" {
				return true, c.Reason
			}
			msg := c.Reason
			if c.Message != "" {
				msg += ": " + c.Message
			}
			return false, msg
		}
	}
	return false, "Ready condition missing"
}

func podNames(pods []kubePod) []string {
	out := make([]string, 0, len(pods))
	for _, p := range pods {
		out = append(out, p.Metadata.Name)
	}
	sort.Strings(out)
	return out
}

func isFinishedPod(phase string) bool {
	return phase == "Succeeded" || phase == "Failed"
}
