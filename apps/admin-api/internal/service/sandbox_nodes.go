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
	Name                  string            `json:"name"`
	Status                string            `json:"status"`
	Ready                 bool              `json:"ready"`
	Schedulable           bool              `json:"schedulable"`
	DiskPressure          bool              `json:"disk_pressure"`
	CPUCapacity           string            `json:"cpu_capacity"`
	MemoryCapacity        string            `json:"memory_capacity"`
	CPUAllocatable        string            `json:"cpu_allocatable"`
	MemoryAllocatable     string            `json:"memory_allocatable"`
	SandboxPods           int               `json:"sandbox_pods"`
	MaxSandboxPods        *int              `json:"max_sandbox_pods,omitempty"`
	SessionCount          int               `json:"session_count"`
	SessionRequestedBytes int64             `json:"session_requested_bytes"`
	WorkspaceResetCount   int               `json:"workspace_reset_count"`
	Reason                string            `json:"reason,omitempty"`
	Labels                map[string]string `json:"labels,omitempty"`
}

type OfflineNodeResult struct {
	Node             SandboxNode `json:"node"`
	PendingPods      []string    `json:"pending_pods,omitempty"`
	AffectedSessions int         `json:"affected_sessions,omitempty"`
	Message          string      `json:"message"`
}

type JoinCommand struct {
	Command string `json:"command"`
	Note    string `json:"note"`
}

func (a *Admin) ListSandboxNodes(ctx context.Context) (SandboxNodeList, error) {
	if a.sandboxNodes == nil {
		return SandboxNodeList{}, ErrNotConfigured
	}
	if a.sessionStorage == nil {
		return SandboxNodeList{}, ErrNotConfigured
	}
	out, err := a.sandboxNodes.ListNodes(ctx)
	if err != nil {
		return out, err
	}
	usage, err := a.sessionStorage.NodeUsage(ctx)
	if err != nil {
		return SandboxNodeList{}, err
	}
	for i := range out.Nodes {
		nodeUsage := usage[out.Nodes[i].Name]
		out.Nodes[i].SessionCount = nodeUsage.SessionCount
		out.Nodes[i].SessionRequestedBytes = nodeUsage.RequestedBytes
		out.Nodes[i].WorkspaceResetCount = nodeUsage.ResetCount
	}
	return out, nil
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
	if a.sessionStorage == nil {
		return OfflineNodeResult{}, ErrNotConfigured
	}
	if !force {
		usage, err := a.sessionStorage.NodeUsage(ctx)
		if err != nil {
			return OfflineNodeResult{}, err
		}
		if affected := usage[name].SessionCount; affected > 0 {
			nodes, err := a.sandboxNodes.ListNodes(ctx)
			if err != nil {
				return OfflineNodeResult{}, err
			}
			for _, node := range nodes.Nodes {
				if node.Name == name {
					nodeUsage := usage[name]
					node.SessionCount = nodeUsage.SessionCount
					node.SessionRequestedBytes = nodeUsage.RequestedBytes
					node.WorkspaceResetCount = nodeUsage.ResetCount
					return OfflineNodeResult{
						Node: node, AffectedSessions: affected,
						Message: "confirmation required: node holds local session workspaces",
					}, nil
				}
			}
		}
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

func (m *KubeSandboxNodeManager) OfflineNode(ctx context.Context, name string, _ bool) (OfflineNodeResult, error) {
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
	if len(pending) > 0 {
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
			Message:     "node cordoned; running sandboxes remain until they stop or are reclaimed",
		}, nil
	}
	node, err := m.getNode(ctx, name)
	if err != nil {
		return OfflineNodeResult{}, err
	}
	node.Status = "offline"
	return OfflineNodeResult{Node: node, Message: "node cordoned and has no running sandbox pods"}, nil
}

func (m *KubeSandboxNodeManager) JoinCommand(context.Context) (JoinCommand, error) {
	if cmd := strings.TrimSpace(os.Getenv("COCOLA_K3S_JOIN_COMMAND")); cmd != "" {
		return JoinCommand{
			Command: cmd,
			Note:    "Before joining, mount the intended local disk at /var/lib/cocola/storage. Workspaces on that node are lost if the disk fails.",
		}, nil
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
		Note:    "Before joining, ensure /var/lib/cocola/storage is on the intended local disk. Local workspace data is lost if that disk fails.",
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
		DiskPressure:      nodeDiskPressure(n),
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

func nodeDiskPressure(n kubeNode) bool {
	for _, condition := range n.Status.Conditions {
		if condition.Type == "DiskPressure" {
			return condition.Status == "True"
		}
	}
	return false
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
