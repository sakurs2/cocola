// Package k8s implements SandboxProvider on top of Kubernetes, with each sandbox
// running as a Pod under the gVisor runtime (RuntimeClass=runsc) for strong
// isolation. It is the production counterpart to the Docker provider: same
// interface, same implicit contracts (provider-minted sandbox ids, the four
// cocola labels for cross-replica resolve, cross-session user persistence), but
// the backend is a cluster rather than a single Docker daemon.
//
// Persistence model (ADR-0008 T1b/T2): instead of host bind-mounts, each sandbox
// mounts two PersistentVolumeClaims —
//
//	user PVC    -> /data/userdata/<uid>  AND  /home/cocola/.claude  (per user, survives Destroy)
//	session PVC -> /workspace/<sid>                                  (per session, survives Pause)
//
// plus a read-only plugins mount. This makes hibernate cheap and safe: Pause
// deletes the Pod but keeps the PVCs; Resume recreates the Pod against the same
// PVCs so `claude --resume` continues the conversation.
package k8s

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
)

// ProviderName is the registry key used in config to select this backend.
const ProviderName = "k8s"

// Labels mirror the Docker provider's so the cross-replica resolve story is
// identical: any sandbox-manager replica can act on any sandbox because the Pod
// itself carries the binding metadata.
const (
	labelManaged   = "cocola.bytedance.com/managed"
	labelSandboxID = "cocola.bytedance.com/sandbox-id"
	labelUserID    = "cocola.bytedance.com/user-id"
	labelSessionID = "cocola.bytedance.com/session-id"
)

const (
	defaultImage        = "alpine:3.20"
	defaultNamespace    = "cocola-sandboxes"
	defaultRuntimeClass = "runsc" // gVisor
	defaultExecTimeout  = 60 * time.Second

	guestUserData     = "/data/userdata"
	guestWorkspace    = "/workspace"
	guestPlugins      = "/data/plugins"
	guestClaudeConfig = "/home/cocola/.claude"
	// sandboxUID matches the brain image's non-root user (Dockerfile: uid 10001);
	// the Pod's fsGroup is set to it so the mounted PVCs are group-writable and
	// the in-sandbox claude CLI can persist its session files.
	sandboxUID = 10001

	containerName = "sandbox"
	pluginsPVC    = "cocola-plugins" // shared, pre-provisioned RO claim
)

// Provider is a Kubernetes-backed SandboxProvider.
type Provider struct {
	cli       kubernetes.Interface
	namespace string

	image        string
	runtimeClass string
	storageClass string // empty -> cluster default StorageClass
	gatewayDNS   string // in-cluster llm-gateway base URL for egress allowlist

	mu        sync.RWMutex
	sandboxes map[string]*record // sandbox_id -> pod record (fast-path cache)
}

// record is the cached binding for a sandbox this replica has already resolved.
type record struct {
	podName   string
	userID    string
	sessionID string
}

// Option configures the Provider.
type Option func(*Provider)

// WithNamespace overrides the sandbox namespace.
func WithNamespace(ns string) Option { return func(p *Provider) { p.namespace = ns } }

// WithClientset injects a clientset (used by tests with a fake).
func WithClientset(cli kubernetes.Interface) Option {
	return func(p *Provider) { p.cli = cli }
}

// New constructs a Kubernetes provider. It uses the in-cluster ServiceAccount
// config when running inside the cluster, falling back to COCOLA_K8S_KUBECONFIG
// (or the default kubeconfig path) for out-of-cluster development.
func New(opts ...Option) (*Provider, error) {
	p := &Provider{
		namespace:    envOr("COCOLA_K8S_NAMESPACE", defaultNamespace),
		image:        envOr("COCOLA_K8S_IMAGE", defaultImage),
		runtimeClass: envOr("COCOLA_K8S_RUNTIME_CLASS", defaultRuntimeClass),
		storageClass: os.Getenv("COCOLA_K8S_STORAGE_CLASS"),
		gatewayDNS:   os.Getenv("COCOLA_SANDBOX_LLM_BASE_URL"),
		sandboxes:    map[string]*record{},
	}
	for _, o := range opts {
		o(p)
	}
	if p.cli == nil {
		cli, err := buildClientset()
		if err != nil {
			return nil, err
		}
		p.cli = cli
	}
	return p, nil
}

// buildClientset prefers in-cluster config, then an explicit/!default kubeconfig.
func buildClientset() (kubernetes.Interface, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return kubernetes.NewForConfig(cfg)
	}
	kubeconfig := os.Getenv("COCOLA_K8S_KUBECONFIG")
	if kubeconfig == "" {
		if home, herr := os.UserHomeDir(); herr == nil {
			kubeconfig = home + "/.kube/config"
		}
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("k8s: build config: %w", err)
	}
	return kubernetes.NewForConfig(cfg)
}

// Create provisions the two PVCs (if absent) and starts a long-lived idle Pod
// under gVisor that the agent can exec into.
func (p *Provider) Create(ctx context.Context, spec provider.SandboxSpec) (*provider.Sandbox, error) {
	sid := "sbx-" + uuid.NewString()
	userClaim := userPVCName(spec.UserID)
	sessClaim := sessionPVCName(spec.SessionID)

	if err := p.ensurePVC(ctx, userClaim, spec.Resources.DiskMiB); err != nil {
		return nil, err
	}
	if err := p.ensurePVC(ctx, sessClaim, spec.Resources.DiskMiB); err != nil {
		return nil, err
	}

	pod := p.podSpec(sid, spec, userClaim, sessClaim)
	if _, err := p.cli.CoreV1().Pods(p.namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("k8s: create pod: %w", err)
	}

	p.mu.Lock()
	p.sandboxes[sid] = &record{podName: podName(sid), userID: spec.UserID, sessionID: spec.SessionID}
	p.mu.Unlock()

	return &provider.Sandbox{
		ID:        sid,
		UserID:    spec.UserID,
		SessionID: spec.SessionID,
		Endpoint:  fmt.Sprintf("k8s://%s/%s", p.namespace, podName(sid)),
	}, nil
}

// podSpec builds the sandbox Pod: gVisor runtime, two PVC mounts + RO plugins,
// the four binding labels, injected env, non-root securityContext, and an idle
// command so the container stays alive for exec.
func (p *Provider) podSpec(sid string, spec provider.SandboxSpec, userClaim, sessClaim string) *corev1.Pod {
	img := spec.Image
	if img == "" {
		img = p.image
	}
	uid := safe(spec.UserID)
	sess := safe(spec.SessionID)

	env := make([]corev1.EnvVar, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, corev1.EnvVar{Name: k, Value: v})
	}

	uidVal := int64(sandboxUID)
	runtimeClass := p.runtimeClass

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName(sid),
			Namespace: p.namespace,
			Labels: map[string]string{
				labelManaged:   "true",
				labelSandboxID: sid,
				labelUserID:    uid,
				labelSessionID: sess,
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsUser:  &uidVal,
				RunAsGroup: &uidVal,
				FSGroup:    &uidVal,
			},
			Containers: []corev1.Container{{
				Name:       containerName,
				Image:      img,
				Command:    []string{"sh", "-c", "trap : TERM INT; sleep infinity & wait"},
				WorkingDir: guestWorkspace + "/" + sess,
				Env:        env,
				Resources:  resourceReqs(spec.Resources),
				VolumeMounts: []corev1.VolumeMount{
					{Name: "userdata", MountPath: guestUserData + "/" + uid, SubPath: "userdata"},
					{Name: "userdata", MountPath: guestClaudeConfig, SubPath: "claude"},
					{Name: "workspace", MountPath: guestWorkspace + "/" + sess},
					{Name: "plugins", MountPath: guestPlugins, ReadOnly: true},
				},
			}},
			Volumes: []corev1.Volume{
				{Name: "userdata", VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: userClaim},
				}},
				{Name: "workspace", VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: sessClaim},
				}},
				{Name: "plugins", VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pluginsPVC, ReadOnly: true},
				}},
			},
		},
	}
	if runtimeClass != "" {
		pod.Spec.RuntimeClassName = &runtimeClass
	}
	return pod
}

// ensurePVC creates a ReadWriteOnce claim if it does not already exist. Existing
// claims are reused as-is (idempotent), which is what makes user data survive
// across sessions and session data survive across hibernate.
func (p *Provider) ensurePVC(ctx context.Context, name string, diskMiB int64) error {
	if _, err := p.cli.CoreV1().PersistentVolumeClaims(p.namespace).Get(ctx, name, metav1.GetOptions{}); err == nil {
		return nil // already provisioned
	}
	size := diskMiB
	if size <= 0 {
		size = 1024 // 1Gi default
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: p.namespace,
			Labels:    map[string]string{labelManaged: "true"},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(fmt.Sprintf("%dMi", size)),
				},
			},
		},
	}
	if p.storageClass != "" {
		pvc.Spec.StorageClassName = &p.storageClass
	}
	if _, err := p.cli.CoreV1().PersistentVolumeClaims(p.namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("k8s: create pvc %s: %w", name, err)
	}
	return nil
}

// resourceReqs translates the provider resource caps into a K8s ResourceRequirements.
func resourceReqs(r provider.Resources) corev1.ResourceRequirements {
	lim := corev1.ResourceList{}
	if r.CPUCores > 0 {
		lim[corev1.ResourceCPU] = resource.MustParse(fmt.Sprintf("%dm", int64(r.CPUCores*1000)))
	}
	if r.MemoryMiB > 0 {
		lim[corev1.ResourceMemory] = resource.MustParse(fmt.Sprintf("%dMi", r.MemoryMiB))
	}
	if len(lim) == 0 {
		return corev1.ResourceRequirements{}
	}
	return corev1.ResourceRequirements{Limits: lim}
}

// Destroy deletes the sandbox Pod. The user PVC is intentionally retained
// (cross-session persistence); the session PVC is left for the orchestrator's
// Release path to reclaim alongside the binding.
func (p *Provider) Destroy(ctx context.Context, sid string) error {
	rec, err := p.resolve(ctx, sid)
	if err != nil {
		return err
	}
	if err := p.cli.CoreV1().Pods(p.namespace).Delete(ctx, rec.podName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("k8s: delete pod: %w", err)
	}
	p.mu.Lock()
	delete(p.sandboxes, sid)
	p.mu.Unlock()
	return nil
}

// --- helpers ---------------------------------------------------------------

// resolve maps a sandbox id to its Pod record. Fast path: the in-process cache
// for sandboxes this replica created. Fallback: a label-selector List, which is
// what makes sandbox-manager horizontally scalable and restart-safe — any
// replica can act on a sandbox created by any other replica because the Pod
// itself carries the binding labels.
func (p *Provider) resolve(ctx context.Context, sid string) (*record, error) {
	p.mu.RLock()
	rec, ok := p.sandboxes[sid]
	p.mu.RUnlock()
	if ok {
		return rec, nil
	}

	sel := labels.SelectorFromSet(labels.Set{
		labelManaged:   "true",
		labelSandboxID: sid,
	}).String()
	pods, err := p.cli.CoreV1().Pods(p.namespace).List(ctx, metav1.ListOptions{LabelSelector: sel})
	if err != nil {
		return nil, fmt.Errorf("k8s: list pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("k8s: sandbox not found: %s", sid)
	}
	pod := pods.Items[0]
	rec = &record{
		podName:   pod.Name,
		userID:    pod.Labels[labelUserID],
		sessionID: pod.Labels[labelSessionID],
	}
	p.mu.Lock()
	p.sandboxes[sid] = rec
	p.mu.Unlock()
	return rec, nil
}

func podName(sid string) string        { return "cocola-" + sid }
func userPVCName(userID string) string { return "cocola-user-" + safe(userID) }
func sessionPVCName(s string) string   { return "cocola-sess-" + safe(s) }

func safe(s string) string {
	if s == "" {
		return "x"
	}
	r := strings.NewReplacer("/", "-", "..", "-", " ", "-", "_", "-")
	out := strings.ToLower(r.Replace(s))
	return out
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
