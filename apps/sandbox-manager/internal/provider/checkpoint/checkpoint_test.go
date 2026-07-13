package checkpoint

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
)

type fakeCheckpointObjectCleaner struct {
	objects      map[string]struct{}
	removeErrors map[string]error
}

func TestConfigValidateRequiresDurableBackends(t *testing.T) {
	complete := Config{
		MinioEndpoint: "minio:9000", MinioAccessKey: "cocola",
		MinioSecretKey: "secret", MinioBucket: "cocola", PGDSN: "postgres://cocola",
	}
	if err := complete.Validate(); err != nil {
		t.Fatalf("complete config rejected: %v", err)
	}

	missingSecret := complete
	missingSecret.MinioSecretKey = ""
	if err := missingSecret.Validate(); err == nil {
		t.Fatal("checkpoint config without MinIO secret was accepted")
	}
	missingPostgres := complete
	missingPostgres.PGDSN = ""
	if err := missingPostgres.Validate(); err == nil {
		t.Fatal("checkpoint config without Postgres was accepted")
	}
}

func TestRecordCheckpointRequiresOwner(t *testing.T) {
	p := &Provider{}
	if err := p.recordSuccess(context.Background(), "", "session", "checkpoint", 1); err == nil {
		t.Fatal("ownerless checkpoint success was accepted")
	}
	if err := p.recordFailure(context.Background(), "", "session", "failed"); err == nil {
		t.Fatal("ownerless checkpoint failure was accepted")
	}
}

func TestRecordSuccessPreservesSessionOwner(t *testing.T) {
	dsn := os.Getenv("COCOLA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("COCOLA_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()

	sessionID := "checkpoint-owner-" + uuid.NewString()
	defer func() { _, _ = conn.Exec(ctx, `DELETE FROM session_map WHERE session_id=$1`, sessionID) }()
	p := &Provider{cfg: Config{PGDSN: dsn}}
	if err := p.recordSuccess(ctx, "user-1", sessionID, "checkpoint-1", 42); err != nil {
		t.Fatal(err)
	}

	var owner, key string
	if err := conn.QueryRow(ctx, `
SELECT user_id, checkpoint_object_key FROM session_map WHERE session_id=$1
`, sessionID).Scan(&owner, &key); err != nil {
		t.Fatal(err)
	}
	if owner != "user-1" || key != "checkpoint-1" {
		t.Fatalf("checkpoint metadata = owner %q key %q", owner, key)
	}
	if err := p.recordSuccess(ctx, "user-2", sessionID, "checkpoint-2", 84); err == nil {
		t.Fatal("cross-owner checkpoint update succeeded")
	}
	if err := conn.QueryRow(ctx, `
SELECT user_id, checkpoint_object_key FROM session_map WHERE session_id=$1
`, sessionID).Scan(&owner, &key); err != nil {
		t.Fatal(err)
	}
	if owner != "user-1" || key != "checkpoint-1" {
		t.Fatalf("cross-owner update changed metadata to owner %q key %q", owner, key)
	}
}

func TestRecordSuccessInheritsConversationRuntime(t *testing.T) {
	dsn := os.Getenv("COCOLA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("COCOLA_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()

	sessionID := "checkpoint-runtime-" + uuid.NewString()
	if _, err := conn.Exec(ctx, `
INSERT INTO conversations (id, user_id, tenant_id, title, runtime_id, created_at, updated_at)
VALUES ($1, 'user-codex', '', 'Codex', 'codex', now(), now())
`, sessionID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(ctx, `DELETE FROM conversations WHERE id=$1`, sessionID) }()

	p := &Provider{cfg: Config{PGDSN: dsn}}
	if err := p.recordSuccess(ctx, "user-codex", sessionID, "checkpoint-codex", 42); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(ctx, `DELETE FROM session_map WHERE session_id=$1`, sessionID) }()

	var runtimeID string
	if err := conn.QueryRow(ctx, `
SELECT runtime_id FROM session_map WHERE session_id=$1
`, sessionID).Scan(&runtimeID); err != nil {
		t.Fatal(err)
	}
	if runtimeID != "codex" {
		t.Fatalf("checkpoint session runtime = %q, want codex", runtimeID)
	}
}

func (f *fakeCheckpointObjectCleaner) ListObjects(
	_ context.Context, _ string, opts minio.ListObjectsOptions,
) <-chan minio.ObjectInfo {
	keys := make([]string, 0, len(f.objects))
	for key := range f.objects {
		if strings.HasPrefix(key, opts.Prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	objects := make(chan minio.ObjectInfo, len(keys))
	for _, key := range keys {
		objects <- minio.ObjectInfo{Key: key}
	}
	close(objects)
	return objects
}

func (f *fakeCheckpointObjectCleaner) RemoveObject(
	_ context.Context, _, objectName string, _ minio.RemoveObjectOptions,
) error {
	if err := f.removeErrors[objectName]; err != nil {
		return err
	}
	delete(f.objects, objectName)
	return nil
}

func TestRemoveSupersededCheckpointObjectsKeepsOnlyCurrentSessionSnapshot(t *testing.T) {
	const (
		prefix  = "checkpoints/user/session/"
		current = prefix + "current.tar.zst"
		other   = "checkpoints/user/other/keep.tar.zst"
	)
	store := &fakeCheckpointObjectCleaner{objects: map[string]struct{}{
		prefix + "old-1.tar.zst": {},
		prefix + "old-2.tar.zst": {},
		current:                  {},
		other:                    {},
	}}

	if err := removeSupersededCheckpointObjects(
		context.Background(), store, "cocola", prefix, current,
	); err != nil {
		t.Fatal(err)
	}

	if len(store.objects) != 2 {
		t.Fatalf("remaining objects = %v, want current and unrelated snapshot", store.objects)
	}
	if _, ok := store.objects[current]; !ok {
		t.Fatal("current checkpoint was deleted")
	}
	if _, ok := store.objects[other]; !ok {
		t.Fatal("checkpoint from another session was deleted")
	}
}

func TestRemoveSupersededCheckpointObjectsReportsFailuresAndContinues(t *testing.T) {
	const (
		prefix  = "checkpoints/user/session/"
		current = prefix + "current.tar.zst"
		failed  = prefix + "old-failed.tar.zst"
		removed = prefix + "old-removed.tar.zst"
	)
	store := &fakeCheckpointObjectCleaner{
		objects: map[string]struct{}{current: {}, failed: {}, removed: {}},
		removeErrors: map[string]error{
			failed: errors.New("minio unavailable"),
		},
	}

	err := removeSupersededCheckpointObjects(
		context.Background(), store, "cocola", prefix, current,
	)
	if err == nil || !strings.Contains(err.Error(), failed) {
		t.Fatalf("cleanup error = %v, want failed object identity", err)
	}
	if _, ok := store.objects[removed]; ok {
		t.Fatal("cleanup stopped after the first deletion failure")
	}
	if _, ok := store.objects[current]; !ok {
		t.Fatal("current checkpoint was deleted")
	}
}
