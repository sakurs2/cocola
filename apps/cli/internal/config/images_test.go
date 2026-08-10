package config

import (
	"strings"
	"testing"
)

func TestResolveImageReferencesUsesOfficialRegistries(t *testing.T) {
	refs, err := ResolveImageReferences("v0.2.0", "")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join([]string{
		refs.Registry, refs.Redis, refs.Postgres, refs.Forgejo, refs.MinIO,
		refs.MinIOClient, refs.OpenViking, refs.OpenSandboxServer,
		refs.OpenSandboxExecd, refs.OpenSandboxEgress, refs.SandboxRuntime,
	}, "\n")
	for _, forbidden := range []string{"ghcr.nju.edu.cn", "docker.nju.edu.cn", "cocola-forgejo"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("references unexpectedly contain retired mirror %q:\n%s", forbidden, joined)
		}
	}
	if refs.Registry != DefaultRegistry || refs.Redis != "docker.io/library/redis:7.4.10-alpine3.21" ||
		refs.Postgres != "docker.io/library/postgres:16.14-alpine3.23" ||
		refs.OpenSandboxServer != "docker.io/opensandbox/server:v0.1.14" ||
		refs.OpenSandboxEgress != "docker.io/opensandbox/egress:v1.1.2" {
		t.Fatalf("official image references are incomplete: %+v", refs)
	}
	if !strings.HasPrefix(refs.Forgejo, "codeberg.org/forgejo/forgejo:16.0.1@sha256:3eb3107") {
		t.Fatalf("Forgejo reference is not pinned by version and digest: %s", refs.Forgejo)
	}
	if !strings.Contains(refs.OpenViking, "v0.4.12@sha256:0d993") {
		t.Fatalf("OpenViking reference lost its digest: %s", refs.OpenViking)
	}
	if refs.OpenSandboxExecd != openSandboxAliyunRegistry+"/execd:v1.0.19" {
		t.Fatalf("OpenSandbox execd reference unexpectedly changed: %s", refs.OpenSandboxExecd)
	}
}

func TestCustomRegistryOnlyOverridesCocolaImages(t *testing.T) {
	refs, err := ResolveImageReferences("v1.0.0", "registry.example/team")
	if err != nil {
		t.Fatal(err)
	}
	if refs.Registry != "registry.example/team" || refs.SandboxRuntime != "registry.example/team/cocola-sandbox-runtime:v1.0.0" {
		t.Fatalf("custom registry was not preserved: %+v", refs)
	}
	if !strings.HasPrefix(refs.OpenViking, "ghcr.io/") || !strings.HasPrefix(refs.Redis, "docker.io/") {
		t.Fatalf("custom Cocola registry unexpectedly changed third-party sources: %+v", refs)
	}
}

func TestImageRegistryRejectsShellMetacharacters(t *testing.T) {
	if _, err := ResolveImageReferences("v1.0.0", "registry.example/team;echo"); err == nil {
		t.Fatal("unsafe registry was accepted")
	}
}
