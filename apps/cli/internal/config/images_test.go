package config

import (
	"strings"
	"testing"
)

func TestResolveImageReferencesUsesOnlySelectedPreset(t *testing.T) {
	tests := []struct {
		name      string
		source    ImageSource
		forbidden []string
		required  []string
	}{
		{
			name: "mainland China mirror", source: ImageSourceCNMirror,
			forbidden: []string{"ghcr.io", "docker.io", "codeberg.org"},
			required:  []string{"ghcr.nju.edu.cn", "docker.nju.edu.cn"},
		},
		{
			name: "direct", source: ImageSourceDirect,
			forbidden: []string{"ghcr.nju.edu.cn", "docker.nju.edu.cn", "codeberg.org"},
			required:  []string{"ghcr.io", "docker.io"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			refs, err := ResolveImageReferences(test.source, "v0.2.0", "")
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join([]string{
				refs.Registry, refs.Redis, refs.Postgres, refs.Forgejo, refs.MinIO,
				refs.MinIOClient, refs.OpenViking, refs.OpenSandboxServer,
				refs.OpenSandboxEgress, refs.SandboxRuntime,
			}, "\n")
			for _, expected := range test.required {
				if !strings.Contains(joined, expected) {
					t.Fatalf("references do not contain %q:\n%s", expected, joined)
				}
			}
			for _, forbidden := range test.forbidden {
				if strings.Contains(joined, forbidden) {
					t.Fatalf("references unexpectedly contain %q:\n%s", forbidden, joined)
				}
			}
			if !strings.Contains(refs.Forgejo, "16.0.1@sha256:3eb3107") {
				t.Fatalf("Forgejo reference is not pinned by version and digest: %s", refs.Forgejo)
			}
			if !strings.Contains(refs.OpenViking, "v0.4.12@sha256:0d993") {
				t.Fatalf("OpenViking reference lost its digest: %s", refs.OpenViking)
			}
		})
	}
}

func TestCustomRegistryOnlyOverridesCocolaImages(t *testing.T) {
	refs, err := ResolveImageReferences(ImageSourceCNMirror, "v1.0.0", "registry.example/team")
	if err != nil {
		t.Fatal(err)
	}
	if refs.Registry != "registry.example/team" || refs.SandboxRuntime != "registry.example/team/cocola-sandbox-runtime:v1.0.0" {
		t.Fatalf("custom registry was not preserved: %+v", refs)
	}
	if !strings.HasPrefix(refs.OpenViking, "ghcr.nju.edu.cn/") || !strings.HasPrefix(refs.Redis, "docker.nju.edu.cn/") {
		t.Fatalf("custom Cocola registry unexpectedly disabled the selected third-party source: %+v", refs)
	}
}

func TestParseImageSourceRejectsUnknownValues(t *testing.T) {
	if _, err := ParseImageSource("automatic"); err == nil {
		t.Fatal("unknown image source was accepted")
	}
}

func TestImageRegistryRejectsShellMetacharacters(t *testing.T) {
	if _, err := ResolveImageReferences(ImageSourceDirect, "v1.0.0", "registry.example/team;echo"); err == nil {
		t.Fatal("unsafe registry was accepted")
	}
}
