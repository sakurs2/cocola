package k8s

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	utilexec "k8s.io/client-go/util/exec"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
)

func newTestProvider(t *testing.T) (*Provider, *fake.Clientset) {
	t.Helper()
	cs := fake.NewSimpleClientset()
	p, err := New(
		WithClientset(cs),
		WithNamespace("test-ns"),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p, cs
}

func TestCreate_StartsPodWithLabelsAndPVCs(t *testing.T) {
	p, cs := newTestProvider(t)
	p.runtimeClass = "runsc"

	sb, err := p.Create(context.Background(), provider.SandboxSpec{
		UserID:    "user-A",
		SessionID: "sess-1",
		Env:       map[string]string{"FOO": "bar"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sb.ID == "" || sb.UserID != "user-A" || sb.SessionID != "sess-1" {
		t.Fatalf("unexpected sandbox: %+v", sb)
	}

	// Pod exists, carries the four binding labels, runs under gVisor.
	pods, _ := cs.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if len(pods.Items) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(pods.Items))
	}
	pod := pods.Items[0]
	if pod.Labels[labelSandboxID] != sb.ID {
		t.Fatalf("sandbox-id label = %q, want %q", pod.Labels[labelSandboxID], sb.ID)
	}
	if pod.Labels[labelManaged] != "true" {
		t.Fatal("missing managed label")
	}
	if pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName != "runsc" {
		t.Fatalf("expected RuntimeClassName=runsc, got %v", pod.Spec.RuntimeClassName)
	}
	// Two PVCs provisioned (user + session).
	pvcs, _ := cs.CoreV1().PersistentVolumeClaims("test-ns").List(context.Background(), metav1.ListOptions{})
	if len(pvcs.Items) != 2 {
		t.Fatalf("expected 2 PVCs, got %d", len(pvcs.Items))
	}
	// Env injected.
	if got := envValue(pod.Spec.Containers[0].Env, "FOO"); got != "bar" {
		t.Fatalf("env FOO = %q, want bar", got)
	}
	// Non-root securityContext.
	if pod.Spec.SecurityContext == nil || pod.Spec.SecurityContext.RunAsUser == nil ||
		*pod.Spec.SecurityContext.RunAsUser != sandboxUID {
		t.Fatal("expected RunAsUser=sandboxUID")
	}
}

func TestCreate_DefaultRunc_NoRuntimeClass(t *testing.T) {
	p, cs := newTestProvider(t)
	// Default provider leaves runtimeClass empty -> plain runc, no RuntimeClassName.
	if _, err := p.Create(context.Background(), provider.SandboxSpec{
		UserID:    "user-A",
		SessionID: "sess-1",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	pods, _ := cs.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if len(pods.Items) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(pods.Items))
	}
	if rc := pods.Items[0].Spec.RuntimeClassName; rc != nil {
		t.Fatalf("expected no RuntimeClassName (runc), got %q", *rc)
	}
}

func TestCreate_UserNamespacesEnabledByDefault(t *testing.T) {
	p, cs := newTestProvider(t)
	// Default COCOLA_K8S_HOST_USERS=false -> hostUsers=false enables user namespaces.
	if _, err := p.Create(context.Background(), provider.SandboxSpec{
		UserID:    "user-A",
		SessionID: "sess-1",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	pods, _ := cs.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	hu := pods.Items[0].Spec.HostUsers
	if hu == nil || *hu != false {
		t.Fatalf("expected Spec.HostUsers=false (userns), got %v", hu)
	}
}

func TestParseHostUsers(t *testing.T) {
	cases := map[string]*bool{
		"false":   boolPtr(false),
		"FALSE":   boolPtr(false),
		"0":       boolPtr(false),
		"true":    boolPtr(true),
		"1":       boolPtr(true),
		"":        nil,
		"default": nil,
	}
	for in, want := range cases {
		got := parseHostUsers(in)
		switch {
		case want == nil && got != nil:
			t.Fatalf("parseHostUsers(%q) = %v, want nil", in, *got)
		case want != nil && got == nil:
			t.Fatalf("parseHostUsers(%q) = nil, want %v", in, *want)
		case want != nil && got != nil && *want != *got:
			t.Fatalf("parseHostUsers(%q) = %v, want %v", in, *got, *want)
		}
	}
}

func boolPtr(b bool) *bool { return &b }

func TestCreate_ReusesExistingUserPVC(t *testing.T) {
	p, cs := newTestProvider(t)

	// Pre-create the user PVC to simulate a returning user.
	_, _ = cs.CoreV1().PersistentVolumeClaims("test-ns").Create(context.Background(),
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: userPVCName("user-A"), Namespace: "test-ns"}},
		metav1.CreateOptions{})

	if _, err := p.Create(context.Background(), provider.SandboxSpec{UserID: "user-A", SessionID: "sess-9"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Still exactly two claims: the pre-existing user PVC was reused, only the
	// session PVC was added.
	pvcs, _ := cs.CoreV1().PersistentVolumeClaims("test-ns").List(context.Background(), metav1.ListOptions{})
	if len(pvcs.Items) != 2 {
		t.Fatalf("expected 2 PVCs (1 reused + 1 new), got %d", len(pvcs.Items))
	}
}

func TestDestroy_DeletesPodKeepsUserPVC(t *testing.T) {
	p, cs := newTestProvider(t)

	sb, err := p.Create(context.Background(), provider.SandboxSpec{UserID: "user-A", SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := p.Destroy(context.Background(), sb.ID); err != nil {
		t.Fatalf("Destroy: %v", err)
	}

	pods, _ := cs.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if len(pods.Items) != 0 {
		t.Fatalf("expected pod deleted, got %d", len(pods.Items))
	}
	// User PVC must survive Destroy (cross-session persistence).
	if _, err := cs.CoreV1().PersistentVolumeClaims("test-ns").Get(context.Background(), userPVCName("user-A"), metav1.GetOptions{}); err != nil {
		t.Fatalf("user PVC should survive Destroy: %v", err)
	}
}

func TestSafe(t *testing.T) {
	cases := map[string]string{
		"":            "x",
		"User/A":      "user-a",
		"a..b":        "a-b",
		"has space":   "has-space",
		"under_score": "under-score",
	}
	for in, want := range cases {
		if got := safe(in); got != want {
			t.Errorf("safe(%q) = %q, want %q", in, got, want)
		}
	}
}

func envValue(env []corev1.EnvVar, key string) string {
	for _, e := range env {
		if e.Name == key {
			return e.Value
		}
	}
	return ""
}

func TestPause_DeletesPodKeepsPVCsAndBinding(t *testing.T) {
	p, cs := newTestProvider(t)
	sb, err := p.Create(context.Background(), provider.SandboxSpec{UserID: "user-A", SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := p.Pause(context.Background(), sb.ID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	pods, _ := cs.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if len(pods.Items) != 0 {
		t.Fatalf("expected pod deleted on pause, got %d", len(pods.Items))
	}
	// Both PVCs survive hibernate.
	pvcs, _ := cs.CoreV1().PersistentVolumeClaims("test-ns").List(context.Background(), metav1.ListOptions{})
	if len(pvcs.Items) != 2 {
		t.Fatalf("expected 2 PVCs to survive pause, got %d", len(pvcs.Items))
	}
	// Binding survives hibernate (needed for cross-replica resume).
	if _, err := cs.CoreV1().ConfigMaps("test-ns").Get(context.Background(), bindingName(sb.ID), metav1.GetOptions{}); err != nil {
		t.Fatalf("binding should survive pause: %v", err)
	}
}

func TestPause_IdempotentWhenAlreadyHibernated(t *testing.T) {
	p, _ := newTestProvider(t)
	sb, _ := p.Create(context.Background(), provider.SandboxSpec{UserID: "u", SessionID: "s"})
	if err := p.Pause(context.Background(), sb.ID); err != nil {
		t.Fatalf("first Pause: %v", err)
	}
	if err := p.Pause(context.Background(), sb.ID); err != nil {
		t.Fatalf("second Pause should be a no-op: %v", err)
	}
}

func TestResume_RecreatesPodFromBinding(t *testing.T) {
	p, cs := newTestProvider(t)
	p.runtimeClass = "runsc"
	sb, _ := p.Create(context.Background(), provider.SandboxSpec{
		UserID: "user-A", SessionID: "sess-1", Env: map[string]string{"FOO": "bar"},
	})
	if err := p.Pause(context.Background(), sb.ID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if err := p.Resume(context.Background(), sb.ID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	pods, _ := cs.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if len(pods.Items) != 1 {
		t.Fatalf("expected pod recreated on resume, got %d", len(pods.Items))
	}
	pod := pods.Items[0]
	// Rebuilt Pod is identical to the original: gVisor + labels + env preserved.
	if pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName != "runsc" {
		t.Fatal("resumed pod lost gVisor runtime class")
	}
	if pod.Labels[labelSandboxID] != sb.ID {
		t.Fatal("resumed pod lost sandbox-id label")
	}
	if got := envValue(pod.Spec.Containers[0].Env, "FOO"); got != "bar" {
		t.Fatalf("resumed pod lost env FOO, got %q", got)
	}
}

func TestResume_IdempotentWhenRunning(t *testing.T) {
	p, cs := newTestProvider(t)
	sb, _ := p.Create(context.Background(), provider.SandboxSpec{UserID: "u", SessionID: "s"})
	// No pause: resuming a running sandbox is a no-op, not an error.
	if err := p.Resume(context.Background(), sb.ID); err != nil {
		t.Fatalf("Resume on running sandbox: %v", err)
	}
	pods, _ := cs.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if len(pods.Items) != 1 {
		t.Fatalf("expected exactly 1 pod, got %d", len(pods.Items))
	}
}

func TestHealth_RunningReadyPausedAbsent(t *testing.T) {
	p, cs := newTestProvider(t)
	sb, _ := p.Create(context.Background(), provider.SandboxSpec{UserID: "u", SessionID: "s"})

	// Fake clientset does not run a scheduler, so patch the Pod to Running+Ready.
	pod, _ := cs.CoreV1().Pods("test-ns").Get(context.Background(), podName(sb.ID), metav1.GetOptions{})
	pod.Status.Phase = corev1.PodRunning
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	if _, err := cs.CoreV1().Pods("test-ns").UpdateStatus(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	hs, err := p.Health(context.Background(), sb.ID)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !hs.Healthy {
		t.Fatalf("expected healthy, got %+v", hs)
	}

	// After pause the Pod is gone: unhealthy, but not an error.
	if err := p.Pause(context.Background(), sb.ID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	hs, err = p.Health(context.Background(), sb.ID)
	if err != nil {
		t.Fatalf("Health after pause: %v", err)
	}
	if hs.Healthy {
		t.Fatalf("expected unhealthy after pause, got %+v", hs)
	}
}

// TestResolve_CrossReplica simulates a second sandbox-manager replica: a fresh
// Provider with an EMPTY cache must still resolve and resume a sandbox created
// (and hibernated) by another replica, using only the durable binding ConfigMap.
func TestResolve_CrossReplica(t *testing.T) {
	cs := fake.NewSimpleClientset()
	replicaA, err := New(WithClientset(cs), WithNamespace("test-ns"))
	if err != nil {
		t.Fatalf("New A: %v", err)
	}
	sb, err := replicaA.Create(context.Background(), provider.SandboxSpec{UserID: "user-A", SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := replicaA.Pause(context.Background(), sb.ID); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	// Replica B shares the cluster but has never seen this sandbox.
	replicaB, err := New(WithClientset(cs), WithNamespace("test-ns"))
	if err != nil {
		t.Fatalf("New B: %v", err)
	}
	if _, cached := replicaB.sandboxes[sb.ID]; cached {
		t.Fatal("replica B should start with an empty cache")
	}
	if err := replicaB.Resume(context.Background(), sb.ID); err != nil {
		t.Fatalf("replica B Resume from binding: %v", err)
	}
	pods, _ := cs.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if len(pods.Items) != 1 {
		t.Fatalf("expected replica B to recreate the pod, got %d", len(pods.Items))
	}
}

// fakeExecutor records the last exec call and produces scripted stdout. If a tar
// handler is set it is invoked so WriteFile/ReadFile round-trips can be tested.
type fakeExecutor struct {
	lastCmd  []string
	lastPod  string
	stdout   []byte
	err      error
	onStream func(cmd []string, stdin io.Reader, stdout, stderr io.Writer) error
}

func (f *fakeExecutor) stream(_ context.Context, _, pod, _ string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) error {
	f.lastCmd = cmd
	f.lastPod = pod
	if f.onStream != nil {
		return f.onStream(cmd, stdin, stdout, stderr)
	}
	if len(f.stdout) > 0 {
		_, _ = stdout.Write(f.stdout)
	}
	return f.err
}

func newProviderWithExec(t *testing.T, fe *fakeExecutor) (*Provider, *fake.Clientset) {
	t.Helper()
	cs := fake.NewSimpleClientset()
	p, err := New(WithClientset(cs), WithNamespace("test-ns"), WithExecutor(fe))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.readyTimeout = 2 * time.Second
	return p, cs
}

// markRunning forces a Pod to Running+Ready in the fake clientset so ensureRunning
// sees it as healthy without a scheduler.
func markRunning(t *testing.T, cs *fake.Clientset, sid string) {
	t.Helper()
	pod, err := cs.CoreV1().Pods("test-ns").Get(context.Background(), podName(sid), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pod: %v", err)
	}
	pod.Status.Phase = corev1.PodRunning
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	if _, err := cs.CoreV1().Pods("test-ns").UpdateStatus(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
}

func collect(ch <-chan provider.ExecEvent) (string, string, int32, error) {
	var so, se strings.Builder
	var exit int32
	var streamErr error
	for ev := range ch {
		switch ev.Kind {
		case provider.ExecEventStdout:
			so.Write(ev.Stdout)
		case provider.ExecEventStderr:
			se.Write(ev.Stderr)
		case provider.ExecEventExit:
			exit = ev.Exit
		case provider.ExecEventError:
			streamErr = ev.Err
		}
	}
	return so.String(), se.String(), exit, streamErr
}

func TestExec_StreamsStdoutAndExit(t *testing.T) {
	fe := &fakeExecutor{stdout: []byte("hello\n")}
	p, cs := newProviderWithExec(t, fe)
	sb, _ := p.Create(context.Background(), provider.SandboxSpec{UserID: "u", SessionID: "s"})
	markRunning(t, cs, sb.ID)

	ch, err := p.Exec(context.Background(), sb.ID, provider.ExecRequest{Cmd: []string{"echo", "hello"}})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	so, _, exit, serr := collect(ch)
	if serr != nil {
		t.Fatalf("stream error: %v", serr)
	}
	if so != "hello\n" || exit != 0 {
		t.Fatalf("stdout=%q exit=%d", so, exit)
	}
	if fe.lastPod != podName(sb.ID) {
		t.Fatalf("exec targeted wrong pod: %q", fe.lastPod)
	}
}

func TestExec_NonZeroExitReportedAsExitEvent(t *testing.T) {
	fe := &fakeExecutor{err: utilexec.CodeExitError{Err: errors.New("boom"), Code: 7}}
	p, cs := newProviderWithExec(t, fe)
	sb, _ := p.Create(context.Background(), provider.SandboxSpec{UserID: "u", SessionID: "s"})
	markRunning(t, cs, sb.ID)

	ch, _ := p.Exec(context.Background(), sb.ID, provider.ExecRequest{Cmd: []string{"false"}})
	_, _, exit, serr := collect(ch)
	if serr != nil {
		t.Fatalf("non-zero exit must not be a stream error: %v", serr)
	}
	if exit != 7 {
		t.Fatalf("exit = %d, want 7", exit)
	}
}

// TestExec_SelfHealResumesHibernatedPod is the K8s analogue of the Docker
// thaw-before-exec regression: the reaper deleted the Pod, but Exec must resume
// it transparently and still run the command.
func TestExec_SelfHealResumesHibernatedPod(t *testing.T) {
	fe := &fakeExecutor{stdout: []byte("awake\n")}
	p, cs := newProviderWithExec(t, fe)
	sb, _ := p.Create(context.Background(), provider.SandboxSpec{UserID: "u", SessionID: "s"})
	// Hibernate: Pod is gone.
	if err := p.Pause(context.Background(), sb.ID); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	// Resume (called inside Exec) recreates the Pod, but the fake scheduler won't
	// mark it Ready — so poll for the recreated Pod and patch it Running+Ready,
	// emulating the kubelet, while Exec blocks in ensureRunning.
	go func() {
		// Poll until the resumed Pod exists, then mark it Running+Ready.
		for i := 0; i < 40; i++ {
			if _, err := cs.CoreV1().Pods("test-ns").Get(context.Background(), podName(sb.ID), metav1.GetOptions{}); err == nil {
				markRunning(t, cs, sb.ID)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()

	ch, err := p.Exec(context.Background(), sb.ID, provider.ExecRequest{Cmd: []string{"echo", "awake"}})
	if err != nil {
		t.Fatalf("Exec (self-heal): %v", err)
	}
	so, _, exit, serr := collect(ch)
	if serr != nil || exit != 0 || so != "awake\n" {
		t.Fatalf("self-heal exec: stdout=%q exit=%d err=%v", so, exit, serr)
	}
	pods, _ := cs.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if len(pods.Items) != 1 {
		t.Fatalf("expected pod resumed, got %d", len(pods.Items))
	}
}

func TestWriteReadFile_RoundTrip(t *testing.T) {
	want := []byte("file-contents-123")
	// Simulate the in-Pod tar by capturing the WriteFile archive and replaying it
	// on ReadFile, so the provider's own tar framing is exercised end to end.
	var stored []byte
	fe := &fakeExecutor{onStream: func(cmd []string, stdin io.Reader, stdout, stderr io.Writer) error {
		switch {
		case len(cmd) > 1 && cmd[0] == "tar" && cmd[1] == "-x": // WriteFile
			data, _ := io.ReadAll(stdin)
			stored = data
			return nil
		case len(cmd) > 1 && cmd[0] == "tar" && cmd[1] == "-c": // ReadFile
			_, _ = stdout.Write(stored)
			return nil
		}
		return errors.New("unexpected cmd")
	}}
	p, cs := newProviderWithExec(t, fe)
	sb, _ := p.Create(context.Background(), provider.SandboxSpec{UserID: "u", SessionID: "s"})
	markRunning(t, cs, sb.ID)

	if err := p.WriteFile(context.Background(), sb.ID, "/workspace/s/note.txt", want); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := p.ReadFile(context.Background(), sb.ID, "/workspace/s/note.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("round trip mismatch: got %q want %q", got, want)
	}
}

func getNetpol(t *testing.T, cs *fake.Clientset, sid string) *networkingv1.NetworkPolicy {
	t.Helper()
	np, err := cs.NetworkingV1().NetworkPolicies("test-ns").Get(context.Background(), netpolName(sid), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get networkpolicy: %v", err)
	}
	return np
}

func TestCreate_NilAllowlistCreatesNoPolicy(t *testing.T) {
	// nil allowlist = "no egress policy configured" (legacy wide-open default):
	// the provider must NOT create a NetworkPolicy at all (ADR-0009 hardening).
	p, cs := newTestProvider(t)
	sb, err := p.Create(context.Background(), provider.SandboxSpec{UserID: "u", SessionID: "s"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := cs.NetworkingV1().NetworkPolicies("test-ns").Get(
		context.Background(), netpolName(sb.ID), metav1.GetOptions{}); err == nil {
		t.Fatal("nil allowlist must not create a NetworkPolicy")
	}
}

func TestCreate_EmptyAllowlistAppliesBaseline(t *testing.T) {
	// Empty (non-nil) allowlist = firewall active with the secure baseline:
	// DNS + in-cluster (llm-gateway) egress are allowed so the gateway is never
	// cut off; everything else is dropped. There must be NO ipBlock peers.
	p, cs := newTestProvider(t)
	sb, err := p.Create(context.Background(), provider.SandboxSpec{
		UserID: "u", SessionID: "s",
		Networking: provider.Networking{EgressAllowlist: []string{}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	np := getNetpol(t, cs, sb.ID)
	if len(np.Spec.PolicyTypes) != 1 || np.Spec.PolicyTypes[0] != networkingv1.PolicyTypeEgress {
		t.Fatalf("expected Egress policy type, got %v", np.Spec.PolicyTypes)
	}
	// Baseline = DNS rule + in-cluster rule; no ipBlock peers.
	if len(np.Spec.Egress) != 2 {
		t.Fatalf("empty allowlist must yield 2 baseline egress rules (dns, cluster), got %d", len(np.Spec.Egress))
	}
	for _, r := range np.Spec.Egress {
		for _, peer := range r.To {
			if peer.IPBlock != nil {
				t.Fatalf("empty allowlist must not produce ipBlock peers, got %q", peer.IPBlock.CIDR)
			}
		}
	}
	if np.Spec.PodSelector.MatchLabels[labelSandboxID] != sb.ID {
		t.Fatal("networkpolicy must select the sandbox pod by sandbox-id")
	}
}

func TestCreate_AllowlistAddsDNSClusterAndCIDR(t *testing.T) {
	p, cs := newTestProvider(t)
	sb, err := p.Create(context.Background(), provider.SandboxSpec{
		UserID: "u", SessionID: "s",
		Networking: provider.Networking{EgressAllowlist: []string{"10.0.0.0/8", "1.2.3.4", "example.com"}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	np := getNetpol(t, cs, sb.ID)
	// Expect: DNS rule + in-cluster rule + one ipBlock rule (2 valid IP/CIDR;
	// the domain is skipped). The domain entry must NOT crash or appear as a peer.
	if len(np.Spec.Egress) != 3 {
		t.Fatalf("expected 3 egress rules (dns, cluster, ipblocks), got %d", len(np.Spec.Egress))
	}
	var cidrs []string
	for _, r := range np.Spec.Egress {
		for _, peer := range r.To {
			if peer.IPBlock != nil {
				cidrs = append(cidrs, peer.IPBlock.CIDR)
			}
		}
	}
	wantCIDR := map[string]bool{"10.0.0.0/8": true, "1.2.3.4/32": true}
	if len(cidrs) != 2 {
		t.Fatalf("expected 2 ipBlock peers, got %v", cidrs)
	}
	for _, c := range cidrs {
		if !wantCIDR[c] {
			t.Fatalf("unexpected CIDR peer %q", c)
		}
	}
}

func TestDestroy_DeletesNetworkPolicy(t *testing.T) {
	p, cs := newTestProvider(t)
	sb, _ := p.Create(context.Background(), provider.SandboxSpec{UserID: "u", SessionID: "s"})
	if err := p.Destroy(context.Background(), sb.ID); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if _, err := cs.NetworkingV1().NetworkPolicies("test-ns").Get(context.Background(), netpolName(sb.ID), metav1.GetOptions{}); err == nil {
		t.Fatal("networkpolicy should be deleted on Destroy")
	}
}
