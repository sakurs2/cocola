package config

import (
	"strings"
	"testing"
)

func TestResolveImageReferencesUsesPinnedGHCRImages(t *testing.T) {
	refs, err := ResolveImageReferences(ImageResolutionOptions{Version: "v0.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join([]string{
		refs.Registry, refs.Redis, refs.Postgres, refs.Forgejo, refs.MinIO,
		refs.MinIOClient, refs.OpenViking, refs.OpenSandboxServer,
		refs.OpenSandboxEgress, refs.SandboxRuntime,
	}, "\n")
	for _, forbidden := range []string{"docker.io", "codeberg.org", "ghcr.nju.edu.cn"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("references unexpectedly contain %q:\n%s", forbidden, joined)
		}
	}
	for _, expected := range []string{
		"ghcr.io/sakurs2/cocola-redis:" + redisVersion + "@" + redisManifestDigest,
		"ghcr.io/sakurs2/cocola-postgres:" + postgresVersion + "@" + postgresManifestDigest,
		"ghcr.io/sakurs2/cocola-forgejo:" + forgejoVersion + "@" + forgejoManifestDigest,
		"ghcr.io/sakurs2/cocola-minio:" + minioVersion + "@" + minioManifestDigest,
		"ghcr.io/sakurs2/cocola-minio-mc:" + minioClientVersion + "@" + minioClientManifestDigest,
		"ghcr.io/sakurs2/cocola-opensandbox-server:" + openSandboxServerVersion + "@" + openSandboxServerDigest,
		"ghcr.io/sakurs2/cocola-opensandbox-egress:" + openSandboxEgressVersion + "@" + openSandboxEgressDigest,
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("references missing %q:\n%s", expected, joined)
		}
	}
	if refs.Registry != DefaultRegistry ||
		refs.OpenViking != "ghcr.io/volcengine/openviking:"+openVikingVersion+"@"+openVikingDigest {
		t.Fatalf("GHCR references are incomplete: %+v", refs)
	}
	if refs.OpenSandboxExecd != openSandboxAliyunRegistry+"/execd:v1.0.19" {
		t.Fatalf("OpenSandbox execd reference unexpectedly changed: %s", refs.OpenSandboxExecd)
	}
}

func TestGHCREndpointRewritesEveryGHCRImage(t *testing.T) {
	refs, err := ResolveImageReferences(ImageResolutionOptions{
		Version: "v1.0.0", GHCREndpoint: "GHCR.NJU.EDU.CN",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, reference := range []string{
		refs.Registry, refs.Redis, refs.Postgres, refs.Forgejo, refs.MinIO,
		refs.MinIOClient, refs.OpenViking, refs.OpenSandboxServer,
		refs.OpenSandboxEgress, refs.SandboxRuntime,
	} {
		if !strings.HasPrefix(reference, "ghcr.nju.edu.cn/") {
			t.Fatalf("GHCR endpoint was not applied to %q", reference)
		}
	}
	if strings.Contains(refs.OpenSandboxExecd, "ghcr.nju.edu.cn") {
		t.Fatalf("OpenSandbox execd was rewritten: %s", refs.OpenSandboxExecd)
	}
}

func TestCustomRegistryOnlyOverridesCocolaImages(t *testing.T) {
	refs, err := ResolveImageReferences(ImageResolutionOptions{
		Version: "v1.0.0", CocolaRegistry: "registry.example/team",
		GHCREndpoint: "ghcr.nju.edu.cn",
	})
	if err != nil {
		t.Fatal(err)
	}
	if refs.Registry != "registry.example/team" || refs.SandboxRuntime != "registry.example/team/cocola-sandbox-runtime:v1.0.0" {
		t.Fatalf("custom registry was not preserved: %+v", refs)
	}
	if !strings.HasPrefix(refs.OpenViking, "ghcr.nju.edu.cn/") ||
		!strings.HasPrefix(refs.Redis, "ghcr.nju.edu.cn/") {
		t.Fatalf("custom Cocola registry unexpectedly changed third-party endpoint: %+v", refs)
	}
}

func TestImageReferenceValidation(t *testing.T) {
	if _, err := ResolveImageReferences(ImageResolutionOptions{
		Version: "v1.0.0", CocolaRegistry: "registry.example/team;echo",
	}); err == nil {
		t.Fatal("unsafe registry was accepted")
	}
	for _, endpoint := range []string{
		"https://ghcr.io", "ghcr.io/team", "user@ghcr.io", "ghcr.io:0",
		"ghcr.io:65536", "ghcr.io:not-a-port", "bad_host.example",
		"bad-.example", "good.-bad.example",
	} {
		if _, err := NormalizeGHCREndpoint(endpoint); err == nil {
			t.Fatalf("unsafe GHCR endpoint %q was accepted", endpoint)
		}
	}
	for input, expected := range map[string]string{
		"":                       DefaultGHCREndpoint,
		" GHCR.NJU.EDU.CN ":      "ghcr.nju.edu.cn",
		"registry.example:5443":  "registry.example:5443",
		"registry.example:00443": "registry.example:443",
	} {
		actual, err := NormalizeGHCREndpoint(input)
		if err != nil || actual != expected {
			t.Fatalf("NormalizeGHCREndpoint(%q) = %q, %v; want %q", input, actual, err, expected)
		}
	}
}
