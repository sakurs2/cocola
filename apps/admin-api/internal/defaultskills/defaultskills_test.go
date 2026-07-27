package defaultskills

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path"
	"sort"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/service"
	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

type testBundleStore struct {
	objects map[string][]byte
}

func (s *testBundleStore) PutBytes(
	_ context.Context,
	key string,
	data []byte,
	_ string,
) error {
	if s.objects == nil {
		s.objects = map[string][]byte{}
	}
	s.objects[key] = append([]byte(nil), data...)
	return nil
}

func (s *testBundleStore) GetBytes(
	_ context.Context,
	key string,
) ([]byte, string, error) {
	return append([]byte(nil), s.objects[key]...), "application/zip", nil
}

func TestLarkCLIEmbeddedAssetMatchesManifest(t *testing.T) {
	set, err := LarkCLI()
	if err != nil {
		t.Fatal(err)
	}
	if set.Version != "1.0.77" || set.UpstreamRef != "v1.0.77" {
		t.Fatalf("unexpected embedded version: %#v", set)
	}
	sum := sha256.Sum256(set.Archive)
	if got := hex.EncodeToString(sum[:]); got != set.ArchiveSHA256 {
		t.Fatalf("archive SHA = %s, want %s", got, set.ArchiveSHA256)
	}
	zr, err := zip.NewReader(bytes.NewReader(set.Archive), int64(len(set.Archive)))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	hasLicense := false
	for _, file := range zr.File {
		if file.Name == "LICENSE" {
			hasLicense = true
		}
		if path.Base(file.Name) != "SKILL.md" {
			continue
		}
		seen[path.Base(path.Dir(file.Name))] = true
	}
	got := make([]string, 0, len(seen))
	for id := range seen {
		got = append(got, id)
	}
	sort.Strings(got)
	want := append([]string(nil), set.SkillIDs...)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("embedded Skill count = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("embedded Skill ID at %d = %q, want %q", i, got[i], want[i])
		}
	}
	if !hasLicense {
		t.Fatal("embedded lark-cli Skill archive is missing upstream LICENSE")
	}
	dockerfile, err := os.ReadFile("../../../../deploy/sandbox-runtime/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	versionLine := []byte("ARG LARK_CLI_VERSION=" + set.Version)
	if !bytes.Contains(dockerfile, versionLine) {
		t.Fatalf("Sandbox image does not pin %q", versionLine)
	}
}

func TestLarkCLIEmbeddedAssetReconcilesAllSkills(t *testing.T) {
	set, err := LarkCLI()
	if err != nil {
		t.Fatal(err)
	}
	mem := store.NewMemory()
	bundles := &testBundleStore{}
	svc := service.New(mem, nil, time.Now).WithSkillBundleStore(bundles)
	result, err := svc.ReconcileDefaultSkills(context.Background(), service.DefaultSkillSet{
		Name:          set.Name,
		Version:       set.Version,
		UpstreamURL:   set.UpstreamURL,
		UpstreamRef:   set.UpstreamRef,
		Archive:       set.Archive,
		ArchiveSHA256: set.ArchiveSHA256,
		SkillIDs:      set.SkillIDs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 27 || len(bundles.objects) != 27 {
		t.Fatalf("reconcile result = %#v, bundle count = %d", result, len(bundles.objects))
	}
	skills, err := svc.ListSkills(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 27 {
		t.Fatalf("catalog Skill count = %d, want 27", len(skills))
	}
}
