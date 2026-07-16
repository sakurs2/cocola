package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProbeReportsFilesystemAndDirectoryUsage(t *testing.T) {
	root := t.TempDir()
	volume := filepath.Join(root, "pvc-test")
	if err := os.MkdirAll(filepath.Join(volume, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(volume, "workspace", "result.txt"), make([]byte, 8192), 0o600); err != nil {
		t.Fatal(err)
	}
	probe, err := newProbeServer(root, "node-a", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	probe.now = func() time.Time { return time.Unix(100, 0) }

	filesystem := httptest.NewRecorder()
	probe.handler().ServeHTTP(filesystem, httptest.NewRequest(http.MethodGet, "/v1/filesystem", nil))
	if filesystem.Code != http.StatusOK {
		t.Fatalf("filesystem status = %d, body = %s", filesystem.Code, filesystem.Body.String())
	}
	var fsBody filesystemResponse
	if err := json.Unmarshal(filesystem.Body.Bytes(), &fsBody); err != nil {
		t.Fatal(err)
	}
	if fsBody.NodeName != "node-a" || fsBody.TotalBytes <= 0 || fsBody.AvailableBytes <= 0 {
		t.Fatalf("filesystem response = %+v", fsBody)
	}

	usage := httptest.NewRecorder()
	probe.handler().ServeHTTP(usage, httptest.NewRequest(http.MethodGet, "/v1/usage?path=pvc-test", nil))
	if usage.Code != http.StatusOK {
		t.Fatalf("usage status = %d, body = %s", usage.Code, usage.Body.String())
	}
	var usageBody usageResponse
	if err := json.Unmarshal(usage.Body.Bytes(), &usageBody); err != nil {
		t.Fatal(err)
	}
	if usageBody.NodeName != "node-a" || usageBody.AllocatedBytes <= 0 || usageBody.FileCount != 1 || usageBody.DirectoryCount != 2 {
		t.Fatalf("usage response = %+v", usageBody)
	}
}

func TestProbeRejectsTraversalAndEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	probe, err := newProbeServer(root, "node-a", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../outside", "escape"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/usage?path="+path, nil).WithContext(context.Background())
		probe.handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("path %q status = %d, want 400", path, recorder.Code)
		}
	}
}

func TestWriteUsageError(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "permission", err: os.ErrPermission, status: http.StatusForbidden},
		{name: "missing", err: os.ErrNotExist, status: http.StatusNotFound},
		{name: "timeout", err: context.DeadlineExceeded, status: http.StatusGatewayTimeout},
		{name: "fallback", err: errors.New("disk error"), status: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeUsageError(recorder, tt.err, http.StatusInternalServerError, "failed")
			if recorder.Code != tt.status {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.status)
			}
		})
	}
}
