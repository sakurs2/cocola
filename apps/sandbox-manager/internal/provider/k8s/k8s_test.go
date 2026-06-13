package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

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
