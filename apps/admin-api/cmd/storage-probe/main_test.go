package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestWorkspaceEntriesAndFilePreview(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "pvc-test", "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"README.md":        "# Workspace\n",
		"src/index.ts":     "export const answer = 42;\n",
		"page.html":        "<script>alert('no')</script>",
		".env":             "TOKEN=secret\n",
		".envrc":           "TOKEN=secret\n",
		"credentials.json": `{"token":"secret"}`,
	}
	for name, content := range files {
		path := filepath.Join(workspace, filepath.FromSlash(name))
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("README.md", filepath.Join(workspace, "readme-link")); err != nil {
		t.Fatal(err)
	}
	probe, err := newProbeServer(root, "node-a", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	entriesRecorder := httptest.NewRecorder()
	entriesRequest := httptest.NewRequest(http.MethodGet, "/v1/workspace/entries?root=pvc-test/workspace", nil)
	probe.handler().ServeHTTP(entriesRecorder, entriesRequest)
	if entriesRecorder.Code != http.StatusOK {
		t.Fatalf("entries status = %d, body = %s", entriesRecorder.Code, entriesRecorder.Body.String())
	}
	var entries workspaceEntriesResponse
	if err := json.Unmarshal(entriesRecorder.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries.Entries) != 7 || entries.Entries[0].Name != "src" || entries.Entries[0].Kind != "directory" {
		t.Fatalf("entries = %+v", entries.Entries)
	}
	byName := map[string]workspaceEntry{}
	for _, entry := range entries.Entries {
		byName[entry.Name] = entry
	}
	if !byName["README.md"].Previewable || byName["README.md"].PreviewKind != "markdown" {
		t.Fatalf("README metadata = %+v", byName["README.md"])
	}
	if byName[".env"].Previewable || byName[".envrc"].Previewable || byName["credentials.json"].Previewable {
		t.Fatalf("sensitive files marked previewable: env=%+v envrc=%+v credentials=%+v", byName[".env"], byName[".envrc"], byName["credentials.json"])
	}
	if byName["readme-link"].Kind != "symlink" || byName["readme-link"].Previewable {
		t.Fatalf("symlink metadata = %+v", byName["readme-link"])
	}

	fileRecorder := httptest.NewRecorder()
	fileRequest := httptest.NewRequest(http.MethodGet, "/v1/workspace/file?root=pvc-test/workspace&path=src/index.ts", nil)
	probe.handler().ServeHTTP(fileRecorder, fileRequest)
	if fileRecorder.Code != http.StatusOK || fileRecorder.Body.String() != files["src/index.ts"] {
		t.Fatalf("file status = %d, body = %q", fileRecorder.Code, fileRecorder.Body.String())
	}
	if got := fileRecorder.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("file content type = %q", got)
	}
	if fileRecorder.Header().Get("Cache-Control") != "no-store" || fileRecorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("file safety headers = %v", fileRecorder.Header())
	}

	htmlRecorder := httptest.NewRecorder()
	htmlRequest := httptest.NewRequest(http.MethodGet, "/v1/workspace/file?root=pvc-test/workspace&path=page.html", nil)
	probe.handler().ServeHTTP(htmlRecorder, htmlRequest)
	if htmlRecorder.Code != http.StatusOK || !strings.HasPrefix(htmlRecorder.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("html preview = %d %q", htmlRecorder.Code, htmlRecorder.Header().Get("Content-Type"))
	}

	for _, path := range []string{".env", ".envrc", "credentials.json", "readme-link"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/workspace/file?root=pvc-test/workspace&path="+path, nil)
		probe.handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("path %q status = %d, want 415", path, recorder.Code)
		}
	}
}

func TestWorkspaceRejectsTraversalAndEscapingRoot(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "pvc-test", "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pvc-test", "runtime.txt"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	probe, err := newProbeServer(root, "node-a", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"../runtime.txt", "/etc/passwd"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/workspace/file?root=pvc-test/workspace&path="+target, nil)
		probe.handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("path %q status = %d, want 400", target, recorder.Code)
		}
	}
	privateRuntime := filepath.Join(root, "pvc-linked", "runtime")
	if err := os.MkdirAll(privateRuntime, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("runtime", filepath.Join(root, "pvc-linked", "workspace")); err != nil {
		t.Fatal(err)
	}
	linkedRoot := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/workspace/entries?root=pvc-linked/workspace", nil)
	probe.handler().ServeHTTP(linkedRoot, request)
	if linkedRoot.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("linked workspace root status = %d, want 415", linkedRoot.Code)
	}
}

func TestWorkspaceEntriesPaginationAndDirectoryLimit(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "pvc-test", "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < workspacePageSize+1; index++ {
		name := filepath.Join(workspace, fmt.Sprintf("file-%03d.txt", index))
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	probe, err := newProbeServer(root, "node-a", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	first := httptest.NewRecorder()
	probe.handler().ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/v1/workspace/entries?root=pvc-test/workspace", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first page = %d %s", first.Code, first.Body.String())
	}
	var firstPage workspaceEntriesResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstPage); err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Entries) != workspacePageSize || firstPage.NextCursor == "" {
		t.Fatalf("first page = %+v", firstPage)
	}
	second := httptest.NewRecorder()
	secondURL := "/v1/workspace/entries?root=pvc-test/workspace&cursor=" + firstPage.NextCursor
	probe.handler().ServeHTTP(second, httptest.NewRequest(http.MethodGet, secondURL, nil))
	var secondPage workspaceEntriesResponse
	if err := json.Unmarshal(second.Body.Bytes(), &secondPage); err != nil {
		t.Fatal(err)
	}
	if second.Code != http.StatusOK || len(secondPage.Entries) != 1 || secondPage.NextCursor != "" {
		t.Fatalf("second page = %d %+v", second.Code, secondPage)
	}

	largeWorkspace := filepath.Join(root, "pvc-large", "workspace")
	if err := os.MkdirAll(largeWorkspace, 0o755); err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= maxDirectoryEntries; index++ {
		name := filepath.Join(largeWorkspace, fmt.Sprintf("entry-%04d", index))
		if err := os.Symlink("missing", name); err != nil {
			t.Fatal(err)
		}
	}
	tooLarge := httptest.NewRecorder()
	probe.handler().ServeHTTP(tooLarge, httptest.NewRequest(http.MethodGet, "/v1/workspace/entries?root=pvc-large/workspace", nil))
	if tooLarge.Code != http.StatusUnprocessableEntity {
		t.Fatalf("large directory status = %d, body = %s", tooLarge.Code, tooLarge.Body.String())
	}
}

func TestWorkspacePreviewSizeAndConcurrencyLimits(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "pvc-test", "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "large.txt"), make([]byte, maxTextPreviewBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	probe, err := newProbeServer(root, "node-a", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	large := httptest.NewRecorder()
	probe.handler().ServeHTTP(large, httptest.NewRequest(http.MethodGet, "/v1/workspace/file?root=pvc-test/workspace&path=large.txt", nil))
	if large.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large preview status = %d", large.Code)
	}
	for index := 0; index < workspaceConcurrency; index++ {
		probe.workspaceSlot <- struct{}{}
	}
	busy := httptest.NewRecorder()
	probe.handler().ServeHTTP(busy, httptest.NewRequest(http.MethodGet, "/v1/workspace/entries?root=pvc-test/workspace", nil))
	if busy.Code != http.StatusTooManyRequests {
		t.Fatalf("busy status = %d", busy.Code)
	}
}
