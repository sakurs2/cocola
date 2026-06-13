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
