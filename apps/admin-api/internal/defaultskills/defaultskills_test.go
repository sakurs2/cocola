package defaultskills

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
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

func TestCommunityCoreEmbeddedAssetMatchesManifest(t *testing.T) {
	set, err := CommunityCore()
	if err != nil {
		t.Fatal(err)
	}
	if set.Name != "community-core" ||
		set.Version != "1.0.0" ||
		set.UpstreamURL != "https://github.com/anthropics/skills" ||
		set.UpstreamRef != "b29e7cf65e5cb78a5ac33d582270551bc74a14eb" {
		t.Fatalf("unexpected embedded community set: %#v", set)
	}
	if len(set.SkillIDs) != 1 || set.SkillIDs[0] != "frontend-design" {
		t.Fatalf("unexpected community Skill IDs: %v", set.SkillIDs)
	}
	sum := sha256.Sum256(set.Archive)
	if got := hex.EncodeToString(sum[:]); got != set.ArchiveSHA256 {
		t.Fatalf("archive SHA = %s, want %s", got, set.ArchiveSHA256)
	}

	zr, err := zip.NewReader(bytes.NewReader(set.Archive), int64(len(set.Archive)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, file := range zr.File {
		rc, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		files[file.Name] = data
	}
	skillMD := files["skills/frontend-design/SKILL.md"]
	license := files["skills/frontend-design/LICENSE.txt"]
	if got := sha256.Sum256(skillMD); hex.EncodeToString(got[:]) !=
		"1608ea77fbb6fc30d13a97d12cfa8ebf31358d40f0dd97beed24829d6b3f45dd" {
		t.Fatalf("unexpected upstream frontend-design SKILL.md SHA: %x", got)
	}
	if got := sha256.Sum256(license); hex.EncodeToString(got[:]) !=
		"0d542e0c8804e39aa7f37eb00da5a762149dc682d7829451287e11b938e94594" {
		t.Fatalf("unexpected upstream frontend-design LICENSE SHA: %x", got)
	}
	if !bytes.HasPrefix(skillMD, []byte("---\nname: frontend-design\n")) ||
		!bytes.Contains(license, []byte("Apache License")) {
		t.Fatal("community archive is missing the expected Skill or Apache license")
	}
	standaloneLicense, err := os.ReadFile("assets/ANTHROPIC_FRONTEND_DESIGN_LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(standaloneLicense, license) {
		t.Fatal("standalone frontend-design license differs from the bundled copy")
	}
}

func TestCommunityCoreEmbeddedAssetReconciles(t *testing.T) {
	set, err := CommunityCore()
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
	if result.Created != 1 || len(bundles.objects) != 1 {
		t.Fatalf("reconcile result = %#v, bundle count = %d", result, len(bundles.objects))
	}
	skill, err := mem.GetSkill(context.Background(), "frontend-design")
	if err != nil {
		t.Fatal(err)
	}
	if skill.Scope != "admin" ||
		skill.SourceType != "bundled" ||
		skill.SourceURL != set.UpstreamURL ||
		skill.SourceRef != set.UpstreamRef ||
		skill.SourcePath != "skills/frontend-design" ||
		!skill.Enabled {
		t.Fatalf("unexpected frontend-design catalog entry: %#v", skill)
	}
}

func TestAllReturnsEveryEmbeddedDefaultSet(t *testing.T) {
	sets, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 2 || sets[0].Name != "lark-cli" || sets[1].Name != "community-core" {
		t.Fatalf("unexpected default sets: %#v", sets)
	}
}
