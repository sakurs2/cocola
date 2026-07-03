package service

import "testing"

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
