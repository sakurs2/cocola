package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseKubeconfigNestedListItemBeforeName(t *testing.T) {
	raw := []byte(`
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: Q0E=
    server: https://0.0.0.0:6443
  name: k3d-cocola-sandbox
contexts:
- context:
    cluster: k3d-cocola-sandbox
    user: admin@k3d-cocola-sandbox
  name: k3d-cocola-sandbox
current-context: k3d-cocola-sandbox
kind: Config
preferences: {}
users:
- name: admin@k3d-cocola-sandbox
  user:
    client-certificate-data: Q0VSVA==
    client-key-data: S0VZ
`)

	cfg, err := parseKubeconfig(raw)
	if err != nil {
		t.Fatalf("parseKubeconfig() error = %v", err)
	}
	if cfg.Server != "https://127.0.0.1:6443" {
		t.Fatalf("server = %q", cfg.Server)
	}
	if string(cfg.CAData) != "CA" {
		t.Fatalf("CAData = %q", string(cfg.CAData))
	}
	if string(cfg.ClientCertData) != "CERT" {
		t.Fatalf("ClientCertData = %q", string(cfg.ClientCertData))
	}
	if string(cfg.ClientKeyData) != "KEY" {
		t.Fatalf("ClientKeyData = %q", string(cfg.ClientKeyData))
	}
}

func TestListSessionPVCsIncludesManagedMetadataAndTerminatingState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/opensandbox/persistentvolumeclaims" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"items":[
  {"metadata":{"name":"old","namespace":"opensandbox","deletionTimestamp":"2026-07-16T00:00:00Z","labels":{"app.kubernetes.io/managed-by":"cocola","cocola.dev/storage-id":"storage-1","cocola.dev/generation":"1","cocola.dev/node-name":"node-a","cocola.dev/requested-bytes":"2147483648"}},"spec":{"volumeName":"pv-old","storageClassName":"cocola-local-session"},"status":{"phase":"Bound"}},
  {"metadata":{"name":"foreign","namespace":"opensandbox","labels":{"app.kubernetes.io/managed-by":"other"}},"status":{"phase":"Bound"}}
]}`))
	}))
	defer server.Close()

	client := newKubeClient(kubeConfig{Server: server.URL})
	pvcs, err := client.listSessionPVCs(context.Background(), "opensandbox")
	if err != nil {
		t.Fatal(err)
	}
	if len(pvcs) != 1 {
		t.Fatalf("managed PVCs = %d, want 1", len(pvcs))
	}
	pvc := pvcs[0]
	if pvc.Name != "old" || pvc.Phase != "Terminating" || pvc.StorageID != "storage-1" ||
		pvc.Generation != 1 || pvc.NodeName != "node-a" || pvc.RequestedBytes != 2147483648 ||
		pvc.VolumeName != "pv-old" || pvc.StorageClass != "cocola-local-session" {
		t.Fatalf("PVC = %+v", pvc)
	}
}

func TestStorageProbeKubernetesProxyAndPV(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/api/v1/persistentvolumes/pv-a":
			_, _ = w.Write([]byte(`{"metadata":{"name":"pv-a"},"spec":{"storageClassName":"cocola-local-session","local":{"path":"/var/lib/cocola/storage/pvc-a"}}}`))
		case "/api/v1/namespaces/opensandbox/pods/probe-a:8095/proxy/v1/filesystem":
			_, _ = w.Write([]byte(`{"node_name":"node-a","total_bytes":100,"used_bytes":40,"available_bytes":60,"measured_at":"2026-07-16T00:00:00Z"}`))
		case "/api/v1/namespaces/opensandbox/pods/probe-a:8095/proxy/v1/usage":
			if got := r.URL.Query().Get("path"); got != "pvc-a" {
				t.Fatalf("usage path = %q", got)
			}
			_, _ = w.Write([]byte(`{"node_name":"node-a","allocated_bytes":42,"file_count":2,"directory_count":1,"measured_at":"2026-07-16T00:00:00Z"}`))
		case "/api/v1/namespaces/opensandbox/pods/probe-a:8095/proxy/v1/workspace/entries":
			if r.URL.Query().Get("root") != "pvc-a/workspace" || r.URL.Query().Get("path") != "src" || r.URL.Query().Get("cursor") != "next" {
				t.Fatalf("workspace entries query = %q", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"path":"src","entries":[{"name":"main.go","path":"src/main.go","kind":"file","size":12,"modified_at":"2026-07-16T00:00:00Z","previewable":true,"preview_kind":"code"}],"next_cursor":""}`))
		case "/api/v1/namespaces/opensandbox/pods/probe-a:8095/proxy/v1/workspace/file":
			if r.URL.Query().Get("root") != "pvc-a/workspace" || r.URL.Query().Get("path") != "src/main.go" {
				t.Fatalf("workspace file query = %q", r.URL.RawQuery)
			}
			w.Header().Set("content-type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("package main\n"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newKubeClient(kubeConfig{Server: server.URL})
	pv, err := client.getSessionPV(context.Background(), "pv-a")
	if err != nil || pv.LocalPath != "/var/lib/cocola/storage/pvc-a" {
		t.Fatalf("PV = %+v, err = %v", pv, err)
	}
	filesystem, err := client.storageProbeFilesystem(context.Background(), "opensandbox", "probe-a")
	if err != nil || filesystem.AvailableBytes != 60 {
		t.Fatalf("filesystem = %+v, err = %v", filesystem, err)
	}
	usage, err := client.storageProbeUsage(context.Background(), "opensandbox", "probe-a", "pvc-a")
	if err != nil || usage.AllocatedBytes != 42 || usage.FileCount != 2 {
		t.Fatalf("usage = %+v, err = %v", usage, err)
	}
	entries, err := client.storageProbeWorkspaceEntries(context.Background(), "opensandbox", "probe-a", "pvc-a/workspace", "src", "next")
	if err != nil || len(entries.Entries) != 1 || entries.Entries[0].Path != "src/main.go" {
		t.Fatalf("workspace entries = %+v, err = %v", entries, err)
	}
	file, err := client.storageProbeWorkspaceFile(context.Background(), "opensandbox", "probe-a", "pvc-a/workspace", "src/main.go")
	if err != nil || string(file.Data) != "package main\n" || file.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("workspace file = %+v, err = %v", file, err)
	}
}

func TestStorageProbeWorkspaceFilePreservesStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		_, _ = w.Write([]byte(`{"error":"workspace preview is unsupported"}`))
	}))
	defer server.Close()

	client := newKubeClient(kubeConfig{Server: server.URL})
	_, err := client.storageProbeWorkspaceFile(context.Background(), "opensandbox", "probe-a", "pvc-a/workspace", ".env")
	var statusErr *kubeStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("workspace status error = %v", err)
	}
}
