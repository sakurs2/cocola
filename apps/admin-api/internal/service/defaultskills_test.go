package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

type countingBundleStore struct {
	objects map[string][]byte
	puts    int
	err     error
	onPut   func()
}

func (m *countingBundleStore) PutBytes(
	_ context.Context,
	key string,
	data []byte,
	_ string,
) error {
	if m.err != nil {
		return m.err
	}
	if m.objects == nil {
		m.objects = map[string][]byte{}
	}
	m.puts++
	m.objects[key] = append([]byte(nil), data...)
	if m.onPut != nil {
		onPut := m.onPut
		m.onPut = nil
		onPut()
	}
	return nil
}

func (m *countingBundleStore) GetBytes(
	_ context.Context,
	key string,
) ([]byte, string, error) {
	return append([]byte(nil), m.objects[key]...), "application/zip", nil
}

func TestReconcileDefaultSkillsCreatesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemory()
	bundles := &countingBundleStore{}
	now := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	svc := New(mem, nil, func() time.Time { return now }).WithSkillBundleStore(bundles)
	set := testDefaultSkillSet(t, "1.0.77", map[string]string{
		"skills/lark-doc/SKILL.md": `---
name: lark-doc
description: Read and write documents.
---
Use lark-cli docs commands.
`,
		"skills/lark-doc/references/fetch.md": "Fetch a document.\n",
		"skills/lark-wiki/SKILL.md": `---
name: lark-wiki
description: Read and write wiki nodes.
---
Use lark-cli wiki commands.
`,
	}, []string{"lark-doc", "lark-wiki"})

	result, err := svc.ReconcileDefaultSkills(ctx, set)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 2 || result.Updated != 0 || result.Unchanged != 0 {
		t.Fatalf("first result = %#v", result)
	}
	if bundles.puts != 2 {
		t.Fatalf("first bundle puts = %d, want 2", bundles.puts)
	}
	skills, err := svc.ListSkills(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 {
		t.Fatalf("Skill count = %d, want 2", len(skills))
	}
	for _, skill := range skills {
		if skill.Scope != "admin" || skill.SourceType != "bundled" ||
			skill.Version != "1.0.77" || !skill.Enabled ||
			skill.BundleObjectKey == "" {
			t.Fatalf("unexpected bundled Skill: %#v", skill)
		}
	}

	result, err = svc.ReconcileDefaultSkills(ctx, set)
	if err != nil {
		t.Fatal(err)
	}
	if result.Unchanged != 2 || result.Created != 0 || result.Updated != 0 {
		t.Fatalf("second result = %#v", result)
	}
	if bundles.puts != 2 {
		t.Fatalf("idempotent bundle puts = %d, want 2", bundles.puts)
	}
}

func TestReconcileDefaultSkillsPreservesDisableAndManualTakeover(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemory()
	bundles := &countingBundleStore{}
	now := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	svc := New(mem, nil, func() time.Time { return now }).WithSkillBundleStore(bundles)
	initial := testDefaultSkillSet(t, "1.0.77", map[string]string{
		"skills/lark-doc/SKILL.md": `---
name: lark-doc
description: Read documents.
---
Read documents.
`,
	}, []string{"lark-doc"})
	if _, err := svc.ReconcileDefaultSkills(ctx, initial); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetSkillEnabled(ctx, "lark-doc", false, "admin@example.com"); err != nil {
		t.Fatal(err)
	}

	upgraded := testDefaultSkillSet(t, "1.0.78", map[string]string{
		"skills/lark-doc/SKILL.md": `---
name: lark-doc
description: Read and write documents.
---
Read and write documents.
`,
	}, []string{"lark-doc"})
	now = now.Add(time.Hour)
	result, err := svc.ReconcileDefaultSkills(ctx, upgraded)
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 1 {
		t.Fatalf("upgrade result = %#v", result)
	}
	skill, err := mem.GetSkill(ctx, "lark-doc")
	if err != nil {
		t.Fatal(err)
	}
	if skill.Enabled || skill.Version != "1.0.78" {
		t.Fatalf("disabled upgraded Skill = %#v", skill)
	}

	skill.SourceType = "archive"
	skill.Description = "Administrator-owned content."
	if err := mem.UpdateSkill(ctx, skill); err != nil {
		t.Fatal(err)
	}
	result, err = svc.ReconcileDefaultSkills(ctx, upgraded)
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 1 {
		t.Fatalf("takeover result = %#v", result)
	}
	skill, err = mem.GetSkill(ctx, "lark-doc")
	if err != nil {
		t.Fatal(err)
	}
	if skill.Description != "Administrator-owned content." {
		t.Fatalf("manual takeover was overwritten: %#v", skill)
	}
}

func TestReconcileDefaultSkillsPreservesConcurrentDisableAndTakeover(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemory()
	bundles := &countingBundleStore{}
	now := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	svc := New(mem, nil, func() time.Time { return now }).WithSkillBundleStore(bundles)
	initial := testDefaultSkillSet(t, "1.0.77", map[string]string{
		"skills/lark-doc/SKILL.md": `---
name: lark-doc
description: Read documents.
---
Read documents.
`,
	}, []string{"lark-doc"})
	if _, err := svc.ReconcileDefaultSkills(ctx, initial); err != nil {
		t.Fatal(err)
	}

	upgraded := testDefaultSkillSet(t, "1.0.78", map[string]string{
		"skills/lark-doc/SKILL.md": `---
name: lark-doc
description: Read and write documents.
---
Read and write documents.
`,
	}, []string{"lark-doc"})
	bundles.onPut = func() {
		if _, err := svc.SetSkillEnabled(ctx, "lark-doc", false, "admin@example.com"); err != nil {
			t.Fatalf("concurrent disable: %v", err)
		}
	}
	result, err := svc.ReconcileDefaultSkills(ctx, upgraded)
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 1 {
		t.Fatalf("disable race result = %#v", result)
	}
	skill, err := mem.GetSkill(ctx, "lark-doc")
	if err != nil {
		t.Fatal(err)
	}
	if skill.Enabled || skill.Version != "1.0.78" {
		t.Fatalf("concurrently disabled Skill = %#v", skill)
	}

	newer := testDefaultSkillSet(t, "1.0.79", map[string]string{
		"skills/lark-doc/SKILL.md": `---
name: lark-doc
description: New bundled content.
---
New bundled content.
`,
	}, []string{"lark-doc"})
	bundles.onPut = func() {
		current, getErr := mem.GetSkill(ctx, "lark-doc")
		if getErr != nil {
			t.Fatalf("get concurrent takeover Skill: %v", getErr)
		}
		current.SourceType = "archive"
		current.Description = "Administrator-owned content."
		if updateErr := mem.UpdateSkill(ctx, current); updateErr != nil {
			t.Fatalf("concurrent takeover: %v", updateErr)
		}
	}
	result, err = svc.ReconcileDefaultSkills(ctx, newer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 1 {
		t.Fatalf("takeover race result = %#v", result)
	}
	skill, err = mem.GetSkill(ctx, "lark-doc")
	if err != nil {
		t.Fatal(err)
	}
	if skill.SourceType != "archive" ||
		skill.Description != "Administrator-owned content." ||
		skill.Version != "1.0.78" {
		t.Fatalf("concurrent takeover was overwritten: %#v", skill)
	}
}

func TestReconcileDefaultSkillsValidatesAssetBeforeMutation(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemory()
	bundles := &countingBundleStore{}
	svc := New(mem, nil, time.Now).WithSkillBundleStore(bundles)
	set := testDefaultSkillSet(t, "1.0.77", map[string]string{
		"skills/lark-doc/SKILL.md": `---
name: lark-doc
description: Read documents.
---
Read documents.
`,
	}, []string{"lark-doc"})

	set.ArchiveSHA256 = "bad"
	if _, err := svc.ReconcileDefaultSkills(ctx, set); !errors.Is(err, ErrInvalidArg) {
		t.Fatalf("checksum error = %v, want ErrInvalidArg", err)
	}
	set = testDefaultSkillSet(t, "1.0.77", map[string]string{
		"skills/lark-doc/SKILL.md": `---
name: lark-doc
description: Read documents.
---
Read documents.
`,
	}, []string{"lark-doc", "lark-wiki"})
	if _, err := svc.ReconcileDefaultSkills(ctx, set); !errors.Is(err, ErrInvalidArg) {
		t.Fatalf("ID manifest error = %v, want ErrInvalidArg", err)
	}
	skills, err := svc.ListSkills(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 || bundles.puts != 0 {
		t.Fatalf("validation mutated state: skills=%d puts=%d", len(skills), bundles.puts)
	}
}

func TestReconcileDefaultSkillsPropagatesBundleFailure(t *testing.T) {
	bundles := &countingBundleStore{err: errors.New("object store unavailable")}
	svc := New(store.NewMemory(), nil, time.Now).WithSkillBundleStore(bundles)
	set := testDefaultSkillSet(t, "1.0.77", map[string]string{
		"skills/lark-doc/SKILL.md": `---
name: lark-doc
description: Read documents.
---
Read documents.
`,
	}, []string{"lark-doc"})
	if _, err := svc.ReconcileDefaultSkills(context.Background(), set); err == nil {
		t.Fatal("expected bundle failure")
	}
}

func testDefaultSkillSet(
	t *testing.T,
	version string,
	files map[string]string,
	skillIDs []string,
) DefaultSkillSet {
	t.Helper()
	archive := skillArchive(t, files)
	sum := sha256.Sum256(archive)
	return DefaultSkillSet{
		Name:          "lark-cli",
		Version:       version,
		UpstreamURL:   "https://github.com/larksuite/cli",
		UpstreamRef:   "v" + version,
		Archive:       archive,
		ArchiveSHA256: hex.EncodeToString(sum[:]),
		SkillIDs:      skillIDs,
	}
}
