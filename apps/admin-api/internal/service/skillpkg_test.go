package service

import (
	"archive/zip"
	"bytes"
	"context"
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

func TestEffectivePersonalSkillOverridesSharedByRuntimeID(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemory()
	svc := New(mem, nil, func() time.Time {
		return time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	})

	if _, err := svc.CreateSkill(ctx, store.Skill{
		ID: "frontend-design", RuntimeID: "frontend-design", Name: "Shared",
		Enabled: true, Scope: "admin",
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSkill(ctx, store.Skill{
		ID: "user-32970b55-frontend-design", RuntimeID: "frontend-design", Name: "Personal",
		Enabled: true, Scope: "user", OwnerUserID: "alice",
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
