// Package k8s implements SandboxProvider on top of Kubernetes. By default each
// sandbox runs as a plain runc Pod with Kubernetes user namespaces enabled
// (hostUsers=false, container root mapped to a non-privileged host uid), which
// needs zero node-level installation. gVisor is an optional enhancement: set
// COCOLA_K8S_RUNTIME_CLASS=runsc to pin a RuntimeClass for userspace-kernel
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
//
// Hibernate vs Docker (why a binding ConfigMap exists): the Docker provider can
// pause/unpause a frozen container in place, so its label query always finds the
// container. In K8s, Pause DELETES the Pod, so a Pod label-selector can no longer
// resolve a paused sandbox — which would break cross-replica Resume. To keep the
// horizontal-scale promise, Create also writes a small per-sandbox ConfigMap (the
// "binding") that carries the four labels plus everything needed to rebuild the
// Pod. The binding survives hibernate and is the durable, cross-replica source of
// truth; resolve() reads it, and Resume() rebuilds the Pod from it.
package k8s

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"

	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
)

// ProviderName is the registry key used in config to select this backend.
const ProviderName = "k8s"

// Labels mirror the Docker provider's so the cross-replica resolve story is
// identical: any sandbox-manager replica can act on any sandbox because the Pod
// (and its binding ConfigMap) carries the binding metadata.
const (
	labelManaged   = "cocola.bytedance.com/managed"
	labelSandboxID = "cocola.bytedance.com/sandbox-id"
	labelUserID    = "cocola.bytedance.com/user-id"
	labelSessionID = "cocola.bytedance.com/session-id"
)

const (
	defaultImage        = "alpine:3.20"
	defaultNamespace    = "cocola-sandboxes"
	gvisorRuntimeClass  = "runsc" // optional gVisor RuntimeClass (COCOLA_K8S_RUNTIME_CLASS=runsc)
	defaultExecTimeout  = 60 * time.Second
	defaultReadyTimeout = 30 * time.Second

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
	runtimeClass string // empty -> runc (no RuntimeClassName); set to "runsc" to opt into gVisor
	hostUsers    *bool  // false -> map container root to a non-priv host uid (userns); nil -> cluster default
	storageClass string // empty -> cluster default StorageClass
	gatewayDNS   string // in-cluster llm-gateway base URL for egress allowlist

	restConfig   *rest.Config  // for building SPDY exec streams (nil in tests)
	exec         podExecutor   // Pod exec subresource (injectable for tests)
	readyTimeout time.Duration // how long Exec waits for a resumed Pod to be Ready

	mu        sync.RWMutex
	sandboxes map[string]*record // sandbox_id -> binding (fast-path cache)
}

// podExecutor abstracts the Kubernetes Pod exec subresource. Production uses a
// SPDY stream; tests inject a fake because the fake clientset cannot serve the
// streaming exec protocol.
type podExecutor interface {
	stream(ctx context.Context, namespace, pod, container string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// binding is the durable, self-contained description of a sandbox: everything
// Resume needs to rebuild an identical Pod after hibernate. It is cached in
// memory and persisted in the per-sandbox ConfigMap.
type binding struct {
	SandboxID string             `json:"sandbox_id"`
	UserID    string             `json:"user_id"`
	SessionID string             `json:"session_id"`
	Image     string             `json:"image"`
	Env       map[string]string  `json:"env,omitempty"`
	Resources provider.Resources `json:"resources"`
}

// record is the cached binding for a sandbox this replica has already resolved.
type record struct {
	podName string
	bind    binding
}

// Option configures the Provider.
type Option func(*Provider)

// WithNamespace overrides the sandbox namespace.
func WithNamespace(ns string) Option { return func(p *Provider) { p.namespace = ns } }

// WithClientset injects a clientset (used by tests with a fake).
func WithClientset(cli kubernetes.Interface) Option {
	return func(p *Provider) { p.cli = cli }
}

// WithExecutor injects a Pod executor (used by tests; the fake clientset cannot
// serve the streaming exec subresource).
func WithExecutor(e podExecutor) Option {
	return func(p *Provider) { p.exec = e }
}

// New constructs a Kubernetes provider. It uses the in-cluster ServiceAccount
// config when running inside the cluster, falling back to COCOLA_K8S_KUBECONFIG
// (or the default kubeconfig path) for out-of-cluster development.
func New(opts ...Option) (*Provider, error) {
	p := &Provider{
		namespace:    envOr("COCOLA_K8S_NAMESPACE", defaultNamespace),
		image:        envOr("COCOLA_K8S_IMAGE", defaultImage),
		runtimeClass: os.Getenv("COCOLA_K8S_RUNTIME_CLASS"), // empty default -> plain runc
		hostUsers:    parseHostUsers(envOr("COCOLA_K8S_HOST_USERS", "false")),
		storageClass: os.Getenv("COCOLA_K8S_STORAGE_CLASS"),
		gatewayDNS:   os.Getenv("COCOLA_SANDBOX_LLM_BASE_URL"),
		readyTimeout: defaultReadyTimeout,
		sandboxes:    map[string]*record{},
	}
	for _, o := range opts {
		o(p)
	}
	if p.cli == nil {
		cli, cfg, err := buildClientset()
		if err != nil {
			return nil, err
		}
		p.cli = cli
		p.restConfig = cfg
	}
	// Default to the real SPDY executor unless a test injected one.
	if p.exec == nil && p.restConfig != nil {
		p.exec = &spdyExecutor{cfg: p.restConfig}
	}
	return p, nil
}

// buildClientset prefers in-cluster config, then an explicit/!default kubeconfig.
// It also returns the *rest.Config so the provider can build SPDY exec streams.
func buildClientset() (kubernetes.Interface, *rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		cli, cerr := kubernetes.NewForConfig(cfg)
		return cli, cfg, cerr
	}
	kubeconfig := os.Getenv("COCOLA_K8S_KUBECONFIG")
	if kubeconfig == "" {
		if home, herr := os.UserHomeDir(); herr == nil {
			kubeconfig = home + "/.kube/config"
		}
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, nil, fmt.Errorf("k8s: build config: %w", err)
	}
	cli, cerr := kubernetes.NewForConfig(cfg)
	return cli, cfg, cerr
}

// Create provisions the two PVCs (if absent), persists the binding ConfigMap,
// and starts a long-lived idle Pod under gVisor that the agent can exec into.
func (p *Provider) Create(ctx context.Context, spec provider.SandboxSpec) (*provider.Sandbox, error) {
	sid := "sbx-" + uuid.NewString()
	img := spec.Image
	if img == "" {
		img = p.image
	}
	b := binding{
		SandboxID: sid,
		UserID:    spec.UserID,
		SessionID: spec.SessionID,
		Image:     img,
		Env:       spec.Env,
		Resources: spec.Resources,
	}

	userClaim := userPVCName(spec.UserID)
	sessClaim := sessionPVCName(spec.SessionID)
	if err := p.ensurePVC(ctx, userClaim, spec.Resources.DiskMiB); err != nil {
		return nil, err
	}
	if err := p.ensurePVC(ctx, sessClaim, spec.Resources.DiskMiB); err != nil {
		return nil, err
	}

	if err := p.writeBinding(ctx, b); err != nil {
		return nil, err
	}

	// Enforce egress before the Pod can run (ADR-0009). Semantics mirror the
	// Docker provider: a nil allowlist means "no egress policy configured"
	// (legacy wide-open default — no NetworkPolicy is created); a non-nil
	// allowlist (including an empty one) activates the firewall with the
	// DNS + in-cluster (llm-gateway) baseline always allowed.
	if spec.Networking.EgressAllowlist != nil {
		if err := p.ensureNetworkPolicy(ctx, sid, spec.Networking.EgressAllowlist); err != nil {
			return nil, err
		}
	}

	pod := p.podSpec(b)
	if _, err := p.cli.CoreV1().Pods(p.namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("k8s: create pod: %w", err)
	}

	p.mu.Lock()
	p.sandboxes[sid] = &record{podName: podName(sid), bind: b}
	p.mu.Unlock()

	return &provider.Sandbox{
		ID:        sid,
		UserID:    spec.UserID,
		SessionID: spec.SessionID,
		Endpoint:  fmt.Sprintf("k8s://%s/%s", p.namespace, podName(sid)),
	}, nil
}

// podSpec builds the sandbox Pod from a binding: runc by default (optionally a
// RuntimeClass when set), user namespaces (hostUsers), two PVC mounts + RO
// plugins, the four binding labels, injected env, non-root securityContext, and
// an idle command so the container stays alive for exec. Because it is driven
// entirely by the binding, Create and Resume produce byte-identical Pods.
func (p *Provider) podSpec(b binding) *corev1.Pod {
	uid := safe(b.UserID)
	sess := safe(b.SessionID)

	env := make([]corev1.EnvVar, 0, len(b.Env))
	for k, v := range b.Env {
		env = append(env, corev1.EnvVar{Name: k, Value: v})
	}

	uidVal := int64(sandboxUID)
	runtimeClass := p.runtimeClass

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName(b.SandboxID),
			Namespace: p.namespace,
			Labels: map[string]string{
				labelManaged:   "true",
				labelSandboxID: b.SandboxID,
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
				Image:      b.Image,
				Command:    []string{"sh", "-c", "trap : TERM INT; sleep infinity & wait"},
				WorkingDir: guestWorkspace + "/" + sess,
				Env:        env,
				Resources:  resourceReqs(b.Resources),
				VolumeMounts: []corev1.VolumeMount{
					{Name: "userdata", MountPath: guestUserData + "/" + uid, SubPath: "userdata"},
					{Name: "userdata", MountPath: guestClaudeConfig, SubPath: "claude"},
					{Name: "workspace", MountPath: guestWorkspace + "/" + sess},
					{Name: "plugins", MountPath: guestPlugins, ReadOnly: true},
				},
			}},
			Volumes: []corev1.Volume{
				{Name: "userdata", VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: userPVCName(b.UserID)},
				}},
				{Name: "workspace", VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: sessionPVCName(b.SessionID)},
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
	if p.hostUsers != nil {
		pod.Spec.HostUsers = p.hostUsers
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

// Pause hibernates the sandbox by deleting its Pod while keeping both PVCs and
// the binding ConfigMap. This is the K8s analogue of Docker's freeze, but cheaper
// at rest: a hibernated sandbox consumes no CPU/memory, only disk. Pause is
// idempotent — deleting an already-absent Pod is treated as success.
func (p *Provider) Pause(ctx context.Context, sid string) error {
	rec, err := p.resolve(ctx, sid)
	if err != nil {
		return err
	}
	if err := p.cli.CoreV1().Pods(p.namespace).Delete(ctx, rec.podName, metav1.DeleteOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return nil // already hibernated
		}
		return fmt.Errorf("k8s: pause (delete pod): %w", err)
	}
	return nil
}

// Resume wakes a hibernated sandbox by recreating the Pod from the durable
// binding, remounting the same PVCs so the in-sandbox state (including
// ~/.claude session files) is exactly where it was left. Resume is idempotent —
// if the Pod already exists (sandbox never hibernated), it is a no-op.
func (p *Provider) Resume(ctx context.Context, sid string) error {
	rec, err := p.resolve(ctx, sid)
	if err != nil {
		return err
	}
	// PVCs are normally still present, but ensure them defensively in case the
	// session claim was reclaimed; reuse keeps existing data intact.
	if err := p.ensurePVC(ctx, userPVCName(rec.bind.UserID), rec.bind.Resources.DiskMiB); err != nil {
		return err
	}
	if err := p.ensurePVC(ctx, sessionPVCName(rec.bind.SessionID), rec.bind.Resources.DiskMiB); err != nil {
		return err
	}
	pod := p.podSpec(rec.bind)
	if _, err := p.cli.CoreV1().Pods(p.namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil // already running
		}
		return fmt.Errorf("k8s: resume (create pod): %w", err)
	}
	return nil
}

// Health reports whether the sandbox Pod is running and Ready. A hibernated (or
// not-yet-created) sandbox has no Pod and is reported unhealthy with a detail
// string rather than an error, so callers can distinguish "asleep" from "broken".
func (p *Provider) Health(ctx context.Context, sid string) (*provider.HealthStatus, error) {
	pod, err := p.cli.CoreV1().Pods(p.namespace).Get(ctx, podName(sid), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return &provider.HealthStatus{Healthy: false, Detail: "pod absent (paused or not created)"}, nil
		}
		return nil, fmt.Errorf("k8s: get pod: %w", err)
	}
	if pod.Status.Phase != corev1.PodRunning {
		return &provider.HealthStatus{Healthy: false, Detail: fmt.Sprintf("phase=%s", pod.Status.Phase)}, nil
	}
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return &provider.HealthStatus{Healthy: true, Detail: "running"}, nil
		}
	}
	return &provider.HealthStatus{Healthy: false, Detail: "running but not ready"}, nil
}

// Destroy deletes the sandbox Pod and its binding ConfigMap. The user PVC is
// intentionally retained (cross-session persistence); the session PVC is left
// for the orchestrator's Release path to reclaim alongside the binding.
func (p *Provider) Destroy(ctx context.Context, sid string) error {
	rec, err := p.resolve(ctx, sid)
	if err != nil {
		return err
	}
	if err := p.cli.CoreV1().Pods(p.namespace).Delete(ctx, rec.podName, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("k8s: delete pod: %w", err)
		}
	}
	if err := p.cli.CoreV1().ConfigMaps(p.namespace).Delete(ctx, bindingName(sid), metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("k8s: delete binding: %w", err)
		}
	}
	if err := p.cli.NetworkingV1().NetworkPolicies(p.namespace).Delete(ctx, netpolName(sid), metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("k8s: delete networkpolicy: %w", err)
		}
	}
	p.mu.Lock()
	delete(p.sandboxes, sid)
	p.mu.Unlock()
	return nil
}

// Exec runs a command inside the sandbox Pod via the exec subresource and streams
// stdout/stderr back as ExecEvents, mirroring the Docker provider's contract.
//
// Self-heal (K8s analogue of Docker's thawIfPaused): the reaper hibernates idle
// sandboxes by deleting the Pod. A later turn legitimately reuses the same
// sandbox, so Exec transparently Resumes a missing Pod and waits for it to become
// Ready before streaming, instead of failing with "pod not found".
func (p *Provider) Exec(ctx context.Context, sid string, req provider.ExecRequest) (<-chan provider.ExecEvent, error) {
	rec, err := p.resolve(ctx, sid)
	if err != nil {
		return nil, err
	}
	if len(req.Cmd) == 0 {
		return nil, fmt.Errorf("k8s: empty command")
	}
	if p.exec == nil {
		return nil, fmt.Errorf("k8s: no pod executor configured")
	}
	if err := p.ensureRunning(ctx, sid); err != nil {
		return nil, err
	}

	cmd := req.Cmd
	// Honour Cwd/Env without a custom shell contract: prefix with `env -C`.
	if req.Cwd != "" || len(req.Env) > 0 {
		wrapped := []string{"env"}
		if req.Cwd != "" {
			wrapped = append(wrapped, "-C", req.Cwd)
		}
		for k, v := range req.Env {
			wrapped = append(wrapped, k+"="+v)
		}
		cmd = append(wrapped, req.Cmd...)
	}

	timeout := defaultExecTimeout
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}

	out := make(chan provider.ExecEvent, 32)
	go func() {
		defer close(out)
		runCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		var stdin io.Reader
		if len(req.Stdin) > 0 {
			stdin = bytes.NewReader(req.Stdin)
		}
		stdoutW := &chanWriter{kind: provider.ExecEventStdout, out: out}
		stderrW := &chanWriter{kind: provider.ExecEventStderr, out: out}

		err := p.exec.stream(runCtx, p.namespace, rec.podName, containerName, cmd, stdin, stdoutW, stderrW)
		if err == nil {
			out <- provider.ExecEvent{Kind: provider.ExecEventExit, Exit: 0}
			return
		}
		// remotecommand surfaces a non-zero exit as CodeExitError; treat that as a
		// normal Exit event, anything else as a stream error.
		var codeErr utilexec.CodeExitError
		if errors.As(err, &codeErr) {
			out <- provider.ExecEvent{Kind: provider.ExecEventExit, Exit: int32(codeErr.Code)}
			return
		}
		out <- provider.ExecEvent{Kind: provider.ExecEventError, Err: err}
	}()
	return out, nil
}

// ensureRunning resumes a hibernated sandbox and blocks until its Pod is Ready
// (or the ready timeout elapses). If the Pod is already running it returns fast.
func (p *Provider) ensureRunning(ctx context.Context, sid string) error {
	hs, err := p.Health(ctx, sid)
	if err != nil {
		return err
	}
	if hs.Healthy {
		return nil
	}
	if err := p.Resume(ctx, sid); err != nil {
		return err
	}
	slog.Info("k8s: resumed hibernated sandbox before exec", "sandbox_id", sid)

	deadline := time.Now().Add(p.readyTimeout)
	for {
		hs, err := p.Health(ctx, sid)
		if err != nil {
			return err
		}
		if hs.Healthy {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("k8s: sandbox %s not ready after resume: %s", sid, hs.Detail)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// WriteFile streams a single file into the sandbox by piping a tar archive to
// `tar -x` running inside the Pod (the exec subresource has no copy API).
func (p *Provider) WriteFile(ctx context.Context, sid, filePath string, data []byte) error {
	rec, err := p.resolve(ctx, sid)
	if err != nil {
		return err
	}
	if p.exec == nil {
		return fmt.Errorf("k8s: no pod executor configured")
	}
	if err := p.ensureRunning(ctx, sid); err != nil {
		return err
	}
	dir := path.Dir(filePath)
	base := path.Base(filePath)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: base, Mode: 0o644, Size: int64(len(data))}); err != nil {
		return fmt.Errorf("k8s: tar header: %w", err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("k8s: tar write: %w", err)
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("k8s: tar close: %w", err)
	}
	var stderr bytes.Buffer
	cmd := []string{"tar", "-x", "-m", "-f", "-", "-C", dir}
	if err := p.exec.stream(ctx, p.namespace, rec.podName, containerName, cmd, &buf, io.Discard, &stderr); err != nil {
		return fmt.Errorf("k8s: write file (tar -x): %w: %s", err, stderr.String())
	}
	return nil
}

// ReadFile streams a single file out of the sandbox by running `tar -c` inside
// the Pod and reading the archive from stdout.
func (p *Provider) ReadFile(ctx context.Context, sid, filePath string) ([]byte, error) {
	rec, err := p.resolve(ctx, sid)
	if err != nil {
		return nil, err
	}
	if p.exec == nil {
		return nil, fmt.Errorf("k8s: no pod executor configured")
	}
	if err := p.ensureRunning(ctx, sid); err != nil {
		return nil, err
	}
	dir := path.Dir(filePath)
	base := path.Base(filePath)

	var stdout, stderr bytes.Buffer
	cmd := []string{"tar", "-c", "-f", "-", "-C", dir, base}
	if err := p.exec.stream(ctx, p.namespace, rec.podName, containerName, cmd, nil, &stdout, &stderr); err != nil {
		return nil, fmt.Errorf("k8s: read file (tar -c): %w: %s", err, stderr.String())
	}
	tr := tar.NewReader(&stdout)
	if _, err := tr.Next(); err != nil {
		return nil, fmt.Errorf("k8s: tar next: %w", err)
	}
	data, err := io.ReadAll(tr)
	if err != nil {
		return nil, fmt.Errorf("k8s: tar read: %w", err)
	}
	return data, nil
}

// chanWriter adapts an io.Writer onto the ExecEvent channel so streamed bytes are
// delivered incrementally.
type chanWriter struct {
	kind provider.ExecEventKind
	out  chan<- provider.ExecEvent
}

func (w *chanWriter) Write(b []byte) (int, error) {
	cp := make([]byte, len(b))
	copy(cp, b)
	switch w.kind {
	case provider.ExecEventStderr:
		w.out <- provider.ExecEvent{Kind: provider.ExecEventStderr, Stderr: cp}
	default:
		w.out <- provider.ExecEvent{Kind: provider.ExecEventStdout, Stdout: cp}
	}
	return len(b), nil
}

// spdyExecutor is the production podExecutor: it POSTs to the Pod exec subresource
// and upgrades the connection to a bidirectional SPDY stream.
type spdyExecutor struct {
	cfg *rest.Config
}

func (e *spdyExecutor) stream(ctx context.Context, namespace, pod, containerName string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cli, err := kubernetes.NewForConfig(e.cfg)
	if err != nil {
		return fmt.Errorf("k8s: exec clientset: %w", err)
	}
	req := cli.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdin:     stdin != nil,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(e.cfg, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("k8s: new spdy executor: %w", err)
	}
	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	})
}

// --- egress enforcement ----------------------------------------------------

// ensureNetworkPolicy materialises the sandbox's egress posture as a
// NetworkPolicy selecting the sandbox Pod by its sandbox-id label (ADR-0009:
// egress lockdown is mandatory). It is only called when an egress policy is
// configured (non-nil allowlist); the secure default is a baseline, not a
// full deny:
//
//   - empty allowlist  -> baseline only: allow DNS (so names resolve) and
//     in-cluster egress (so the llm-gateway Service stays reachable for
//     Route A). All other outbound traffic is dropped. Crucially this does
//     NOT cut off the gateway — an empty allowlist still yields a working
//     sandbox that can only talk to DNS + the gateway.
//   - non-empty        -> the same DNS + in-cluster baseline, plus one ipBlock
//     peer per CIDR/IP entry in the allowlist.
//
// Note: NetworkPolicy matches on IP/CIDR and label selectors, not DNS names.
// Domain-style allowlist entries cannot be enforced by a vanilla CNI here; they
// are skipped at this layer and logged, while CIDR entries are enforced
// precisely. To pin domains exactly, deploy a DNS-aware CNI (e.g. Cilium's
// CiliumNetworkPolicy with toFQDNs); see deploy/helm values for the extension
// point.
func (p *Provider) ensureNetworkPolicy(ctx context.Context, sid string, allowlist []string) error {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      netpolName(sid),
			Namespace: p.namespace,
			Labels: map[string]string{
				labelManaged:   "true",
				labelSandboxID: sid,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{labelSandboxID: sid},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress:      p.egressRules(sid, allowlist),
		},
	}
	// Idempotent: replace any stale policy from a previous incarnation.
	if _, err := p.cli.NetworkingV1().NetworkPolicies(p.namespace).Create(ctx, np, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("k8s: create networkpolicy: %w", err)
		}
		if _, uerr := p.cli.NetworkingV1().NetworkPolicies(p.namespace).Update(ctx, np, metav1.UpdateOptions{}); uerr != nil {
			return fmt.Errorf("k8s: update networkpolicy: %w", uerr)
		}
	}
	return nil
}

// egressRules builds the egress rule set for an allowlist. The DNS + in-cluster
// (llm-gateway) baseline is ALWAYS returned — including for an empty allowlist —
// so the gateway is never cut off; CIDR/IP entries are appended as ipBlock peers.
func (p *Provider) egressRules(sid string, allowlist []string) []networkingv1.NetworkPolicyEgressRule {
	dnsUDP := corev1.ProtocolUDP
	dnsTCP := corev1.ProtocolTCP
	dnsPort := intstr.FromInt(53)
	rules := []networkingv1.NetworkPolicyEgressRule{
		// DNS resolution.
		{Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &dnsUDP, Port: &dnsPort},
			{Protocol: &dnsTCP, Port: &dnsPort},
		}},
		// In-cluster egress so the llm-gateway Service is reachable (Route A).
		{To: []networkingv1.NetworkPolicyPeer{
			{NamespaceSelector: &metav1.LabelSelector{}},
		}},
	}
	var ipPeers []networkingv1.NetworkPolicyPeer
	for _, entry := range allowlist {
		cidr := entry
		if !strings.Contains(cidr, "/") {
			if ip := net.ParseIP(entry); ip != nil {
				if ip.To4() != nil {
					cidr = entry + "/32"
				} else {
					cidr = entry + "/128"
				}
			} else {
				// Domain name — cannot be expressed as an ipBlock here.
				slog.Warn("k8s: egress allowlist domain not enforceable by NetworkPolicy (needs DNS-aware CNI), skipping",
					"sandbox_id", sid, "entry", entry)
				continue
			}
		}
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			slog.Warn("k8s: invalid egress CIDR, skipping", "sandbox_id", sid, "entry", entry)
			continue
		}
		ipPeers = append(ipPeers, networkingv1.NetworkPolicyPeer{
			IPBlock: &networkingv1.IPBlock{CIDR: cidr},
		})
	}
	if len(ipPeers) > 0 {
		rules = append(rules, networkingv1.NetworkPolicyEgressRule{To: ipPeers})
	}
	return rules
}

// --- binding persistence ---------------------------------------------------

// writeBinding persists the sandbox binding as a labelled ConfigMap so any
// replica can resolve and rebuild the sandbox even after the Pod is gone.
func (p *Provider) writeBinding(ctx context.Context, b binding) error {
	raw, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("k8s: marshal binding: %w", err)
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bindingName(b.SandboxID),
			Namespace: p.namespace,
			Labels: map[string]string{
				labelManaged:   "true",
				labelSandboxID: b.SandboxID,
				labelUserID:    safe(b.UserID),
				labelSessionID: safe(b.SessionID),
			},
		},
		Data: map[string]string{"binding": string(raw)},
	}
	if _, err := p.cli.CoreV1().ConfigMaps(p.namespace).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("k8s: create binding: %w", err)
	}
	return nil
}

// readBinding loads a binding ConfigMap by sandbox id.
func (p *Provider) readBinding(ctx context.Context, sid string) (binding, error) {
	var b binding
	cm, err := p.cli.CoreV1().ConfigMaps(p.namespace).Get(ctx, bindingName(sid), metav1.GetOptions{})
	if err != nil {
		return b, err
	}
	if err := json.Unmarshal([]byte(cm.Data["binding"]), &b); err != nil {
		return b, fmt.Errorf("k8s: unmarshal binding: %w", err)
	}
	return b, nil
}

// --- helpers ---------------------------------------------------------------

// resolve maps a sandbox id to its record. Fast path: the in-process cache for
// sandboxes this replica created. Fallback: read the durable binding ConfigMap,
// which is what makes sandbox-manager horizontally scalable and hibernate-safe —
// any replica can act on a sandbox (even a paused one with no Pod) because the
// binding ConfigMap carries the full description.
func (p *Provider) resolve(ctx context.Context, sid string) (*record, error) {
	p.mu.RLock()
	rec, ok := p.sandboxes[sid]
	p.mu.RUnlock()
	if ok {
		return rec, nil
	}

	b, err := p.readBinding(ctx, sid)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("k8s: sandbox not found: %s", sid)
		}
		return nil, fmt.Errorf("k8s: resolve %s: %w", sid, err)
	}
	rec = &record{podName: podName(sid), bind: b}
	p.mu.Lock()
	p.sandboxes[sid] = rec
	p.mu.Unlock()
	return rec, nil
}

func podName(sid string) string        { return "cocola-" + sid }
func bindingName(sid string) string    { return "cocola-bind-" + sid }
func netpolName(sid string) string     { return "cocola-egress-" + sid }
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

// parseHostUsers maps COCOLA_K8S_HOST_USERS to a *bool for pod.Spec.HostUsers.
// "false" (the default) enables user namespaces by mapping container root to a
// non-privileged host uid (Kubernetes >= 1.33, default-on). "true" shares the
// host user namespace. Any other value (e.g. "", "default") returns nil, leaving
// the cluster default in effect.
func parseHostUsers(v string) *bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false", "0", "no":
		b := false
		return &b
	case "true", "1", "yes":
		b := true
		return &b
	default:
		return nil
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
