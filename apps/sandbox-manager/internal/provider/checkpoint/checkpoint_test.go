package checkpoint

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
)

type fakeCheckpointObjectCleaner struct {
	objects      map[string]struct{}
	removeErrors map[string]error
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
