package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

type memoryBundleStore struct {
	objects map[string][]byte
}

func (m *memoryBundleStore) PutBytes(ctx context.Context, key string, data []byte, contentType string) error {
	if m.objects == nil {
		m.objects = map[string][]byte{}
	}
	m.objects[key] = append([]byte(nil), data...)
	return nil
}

func (m *memoryBundleStore) GetBytes(ctx context.Context, key string) ([]byte, string, error) {
	return append([]byte(nil), m.objects[key]...), "application/zip", nil
}

func TestSkillArchiveImportAndUserPreference(t *testing.T) {
	ctx := context.Background()
	bundles := &memoryBundleStore{}
	svc := New(store.NewMemory(), nil, func() time.Time {
		return time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	}).WithSkillBundleStore(bundles)

	archive := skillArchive(t, map[string]string{
		"skills/web-search/SKILL.md": `---
name: Web Search
description: Search and summarize web pages.
version: 1.0.0
---
Use browser tools to inspect pages and cite sources.
`,
		"skills/web-search/scripts/run.sh": "echo ok\n",
	})

	candidates, err := svc.ScanSkillArchive(ctx, archive)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 1 || !candidates[0].Valid || candidates[0].ID != "web-search" {
		t.Fatalf("unexpected candidates: %#v", candidates)
	}

	imported, _, err := svc.ImportSkillArchive(ctx, "admin", "", "admin@example.com", archive, nil)
	if err != nil {
		t.Fatalf("import admin: %v", err)
	}
	if len(imported) != 1 || imported[0].BundleObjectKey == "" {
		t.Fatalf("imported skill missing bundle key: %#v", imported)
	}
	if imported[0].RuntimeID != "web-search" {
		t.Fatalf("imported Runtime ID = %q, want web-search", imported[0].RuntimeID)
	}
	if len(bundles.objects) != 1 {
		t.Fatalf("bundle store object count = %d, want 1", len(bundles.objects))
	}

	effective, err := svc.ListEffectiveSkills(ctx, "u1")
	if err != nil {
		t.Fatalf("effective: %v", err)
	}
	if len(effective) != 1 {
		t.Fatalf("effective before disable = %d, want 1", len(effective))
	}
	if err := svc.SetUserSkillEnabled(ctx, "u1", "web-search", false); err != nil {
		t.Fatalf("disable user skill pref: %v", err)
	}
	effective, err = svc.ListEffectiveSkills(ctx, "u1")
	if err != nil {
		t.Fatalf("effective after disable: %v", err)
	}
	if len(effective) != 0 {
		t.Fatalf("effective after disable = %d, want 0", len(effective))
	}
}

func TestSkillCreatorIDIsReservedForThePlatformPackage(t *testing.T) {
	svc := New(store.NewMemory(), nil, time.Now)
	archive := skillArchive(t, map[string]string{
		"SKILL.md": `---
name: skill-creator
description: Attempt to replace the platform Skill.
---
Do not import this package.
`,
	})

	candidates, err := svc.ScanSkillArchive(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Valid {
		t.Fatalf("reserved candidate = %#v", candidates)
	}
	if len(candidates[0].Errors) != 1 ||
		candidates[0].Errors[0] != "skill-creator is a reserved platform Skill id" {
		t.Fatalf("reserved errors = %#v", candidates[0].Errors)
	}
}

func TestEffectivePersonalSkillOverridesSharedByRuntimeID(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemory()
	svc := New(mem, nil, func() time.Time {
		return time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	})

	if _, err := svc.CreateSkill(ctx, store.Skill{
		ID: "frontend-design", RuntimeID: "frontend-design", Name: "Shared",
		Enabled: true, Scope: "admin", SkillMD: "# Shared",
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSkill(ctx, store.Skill{
		ID: "user-32970b55-frontend-design", RuntimeID: "frontend-design", Name: "Personal",
		Enabled: true, Scope: "user", OwnerUserID: "alice", SkillMD: "# Personal",
	}, "alice"); err != nil {
		t.Fatal(err)
	}

	effective, err := svc.ListEffectiveSkills(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(effective) != 1 {
		t.Fatalf("effective skill count = %d, want 1", len(effective))
	}
	if effective[0].ID != "user-32970b55-frontend-design" || effective[0].RuntimeID != "frontend-design" {
		t.Fatalf("unexpected effective personal skill: %#v", effective[0])
	}
}

func TestAgentSkillCatalogAndResolutionSemantics(t *testing.T) {
	ctx := context.Background()
	svc := New(store.NewMemory(), nil, time.Now)
	for _, skill := range []store.Skill{
		{ID: "shared", RuntimeID: "shared", Name: "Shared", Scope: "admin", Enabled: true, SkillMD: "# Shared"},
		{ID: "admin-disabled", RuntimeID: "admin-disabled", Name: "Disabled", Scope: "admin", SkillMD: "# Disabled"},
		{ID: "alice-private", RuntimeID: "private", Name: "Private", Scope: "user", OwnerUserID: "alice", SkillMD: "# Private"},
		{ID: "bob-private", RuntimeID: "bob-private", Name: "Bob", Scope: "user", OwnerUserID: "bob", Enabled: true, SkillMD: "# Bob"},
		{ID: "duplicate-a", RuntimeID: "duplicate", Name: "Duplicate A", Scope: "admin", Enabled: true, SkillMD: "# A"},
		{ID: "duplicate-b", RuntimeID: "duplicate", Name: "Duplicate B", Scope: "admin", Enabled: true, SkillMD: "# B"},
	} {
		if _, err := svc.CreateSkill(ctx, skill, "admin"); err != nil {
			t.Fatalf("CreateSkill(%s): %v", skill.ID, err)
		}
	}
	if err := svc.SetUserSkillEnabled(ctx, "alice", "shared", false); err != nil {
		t.Fatal(err)
	}

	catalog, err := svc.ListAgentSkillCatalog(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]AgentSkillCatalogItem, len(catalog))
	for _, item := range catalog {
		byID[item.ID] = item
	}
	if !byID["shared"].Available || byID["shared"].DefaultEnabled {
		t.Fatalf("user-disabled shared skill = %+v", byID["shared"])
	}
	if !byID["alice-private"].Available || byID["alice-private"].DefaultEnabled {
		t.Fatalf("default-off personal skill = %+v", byID["alice-private"])
	}
	if byID["admin-disabled"].Available ||
		byID["admin-disabled"].UnavailableReason != "disabled_by_administrator" {
		t.Fatalf("admin-disabled skill = %+v", byID["admin-disabled"])
	}
	if _, exists := byID["bob-private"]; exists {
		t.Fatalf("cross-user personal skill leaked into catalog")
	}

	resolved, err := svc.ResolveAgentSkills(ctx, "alice", []string{
		"shared", "alice-private", "admin-disabled", "bob-private",
		"duplicate-a", "duplicate-b", "deleted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Skills) != 3 {
		t.Fatalf("resolved skills = %+v", resolved.Skills)
	}
	if got := []string{
		resolved.Skills[0].ID, resolved.Skills[1].ID, resolved.Skills[2].ID,
	}; got[0] != "shared" || got[1] != "alice-private" || got[2] != "duplicate-a" {
		t.Fatalf("resolved skills = %+v", resolved.Skills)
	}
	wantUnavailable := []string{"admin-disabled", "bob-private", "duplicate-b", "deleted"}
	if !slices.Equal(resolved.UnavailableIDs, wantUnavailable) {
		t.Fatalf("unavailable IDs = %#v, want %#v", resolved.UnavailableIDs, wantUnavailable)
	}
}

func TestUserSkillCatalogPreservesAdministratorDisablement(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemory()
	svc := New(mem, nil, time.Now)
	for _, skill := range []store.Skill{
		{
			ID: "shared", RuntimeID: "shared", Name: "Shared", Scope: "admin",
			Enabled: true, SkillMD: "# Shared",
		},
		{
			ID: "admin-disabled", RuntimeID: "admin-disabled", Name: "Disabled",
			Scope: "admin", SkillMD: "# Disabled",
		},
		{
			ID: "personal", RuntimeID: "personal", Name: "Personal", Scope: "user",
			OwnerUserID: "alice", SkillMD: "# Personal",
		},
	} {
		if _, err := svc.CreateSkill(ctx, skill, "admin"); err != nil {
			t.Fatalf("CreateSkill(%s): %v", skill.ID, err)
		}
	}
	if err := svc.SetUserSkillEnabled(ctx, "alice", "shared", false); err != nil {
		t.Fatal(err)
	}
	// A stale or manually written user preference must never override the
	// administrator's catalog-level disablement.
	if err := mem.SetUserSkillPreference(ctx, store.UserSkillPreference{
		UserID: "alice", SkillID: "admin-disabled", Enabled: true, UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	catalog, err := svc.ListUserSkillCatalog(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]UserSkillCatalogItem, len(catalog))
	for _, item := range catalog {
		byID[item.ID] = item
	}
	if item := byID["shared"]; !item.Available || item.Enabled {
		t.Fatalf("user-disabled shared Skill = %+v", item)
	}
	if item := byID["admin-disabled"]; item.Available || item.Enabled ||
		item.UnavailableReason != "disabled_by_administrator" {
		t.Fatalf("administrator-disabled Skill = %+v", item)
	}
	if item := byID["personal"]; !item.Available || item.Enabled {
		t.Fatalf("default-off personal Skill = %+v", item)
	}
	detail, err := svc.GetUserSkillCatalogItem(ctx, "alice", "admin-disabled")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Available || detail.Enabled || detail.UnavailableReason != "disabled_by_administrator" {
		t.Fatalf("administrator-disabled Skill detail = %+v", detail)
	}
}

func TestEnabledSkillRequiresMaterializablePayload(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemory()
	svc := New(mem, nil, time.Now)

	if _, err := svc.CreateSkill(ctx, store.Skill{
		ID: "metadata-only", Name: "Metadata only", Enabled: true,
	}, "admin"); !errors.Is(err, ErrInvalidArg) {
		t.Fatalf("CreateSkill error = %v, want ErrInvalidArg", err)
	}
	if _, err := svc.CreateSkill(ctx, store.Skill{
		ID: "disabled", Name: "Disabled",
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetSkillEnabled(ctx, "disabled", true, "admin"); !errors.Is(err, ErrInvalidArg) {
		t.Fatalf("SetSkillEnabled error = %v, want ErrInvalidArg", err)
	}
}

func TestSkillResultContractIsValidatedAndNormalized(t *testing.T) {
	ctx := context.Background()
	svc := New(store.NewMemory(), nil, time.Now)
	archive := skillArchive(t, map[string]string{
		"skills/test-results/SKILL.md": `---
name: Test Results
description: Run tests and return structured results.
cocola:
  result:
    version: 1
    renderer: table
    schema:
      type: object
      properties:
        columns:
          type: array
        rows:
          type: array
      required:
        - columns
        - rows
---
Run the requested tests.
`,
	})

	candidates, err := svc.ScanSkillArchive(ctx, archive)
	if err != nil || len(candidates) != 1 || !candidates[0].Valid {
		t.Fatalf("scan candidates = %#v, %v", candidates, err)
	}
	var contract skillResultContract
	if err := json.Unmarshal(candidates[0].ResultContract, &contract); err != nil {
		t.Fatalf("decode result contract: %v", err)
	}
	if contract.Version != 1 || contract.Renderer != "table" ||
		len(contract.ContractHash) != len("sha256:")+64 {
		t.Fatalf("normalized result contract = %#v", contract)
	}
}

func TestSkillResultContractRejectsRemoteSchemaReference(t *testing.T) {
	ctx := context.Background()
	svc := New(store.NewMemory(), nil, time.Now)
	archive := skillArchive(t, map[string]string{
		"skills/unsafe/SKILL.md": `---
name: Unsafe
description: Invalid remote schema.
cocola:
  result:
    version: 1
    renderer: summary
    schema:
      type: object
      properties:
        value:
          $ref: https://example.invalid/value.json
---
Return a result.
`,
	})

	candidates, err := svc.ScanSkillArchive(ctx, archive)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("scan candidates = %#v, %v", candidates, err)
	}
	if candidates[0].Valid || len(candidates[0].Errors) == 0 {
		t.Fatalf("remote schema reference was accepted: %#v", candidates[0])
	}
}

func skillArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
