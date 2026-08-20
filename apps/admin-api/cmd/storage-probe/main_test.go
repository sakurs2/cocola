package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeDockerAPI struct {
	info       dockerInfo
	containers map[string]string
	logs       map[string][]byte
}

func (f *fakeDockerAPI) Info(context.Context) (dockerInfo, error) { return f.info, nil }

func (f *fakeDockerAPI) ContainerForService(_ context.Context, _, service string) (string, error) {
	return f.containers[service], nil
}

func (f *fakeDockerAPI) Logs(_ context.Context, containerID string, _ int) ([]byte, error) {
	return f.logs[containerID], nil
}

func TestHostAgentAuthSessionRootsAndDeletion(t *testing.T) {
	root := t.TempDir()
	session := filepath.Join(root, "users", "user-a", "sessions", "session-a")
	if err := os.MkdirAll(session, 0o755); err != nil {
		t.Fatal(err)
	}
	probe, err := newProbeServer(root, "local", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	probe.hostAgentKey = "secret"

	unauthorized := httptest.NewRecorder()
	probe.handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/session-roots", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	list := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/v1/session-roots", nil)
	listRequest.Header.Set(hostAgentKeyHeader, "secret")
	probe.handler().ServeHTTP(list, listRequest)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "users/user-a/sessions/session-a") {
		t.Fatalf("session roots = %d %s", list.Code, list.Body.String())
	}

	deleteRecorder := httptest.NewRecorder()
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/v1/session-root?path=users/user-a/sessions/session-a", nil)
	deleteRequest.Header.Set(hostAgentKeyHeader, "secret")
	probe.handler().ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	if _, err := os.Stat(session); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session root still exists: %v", err)
	}
}

func TestHostAgentHealthRequiresReadableStorageRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "storage")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	probe, err := newProbeServer(root, "local", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}

	health := httptest.NewRecorder()
	probe.handler().ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusServiceUnavailable || !strings.Contains(health.Body.String(), "storage root is unavailable") {
		t.Fatalf("health status = %d %s", health.Code, health.Body.String())
	}
}

func TestHostAgentLogsSessionRootScanFailure(t *testing.T) {
	root := t.TempDir()
	usersRoot := filepath.Join(root, "users")
	if err := os.WriteFile(usersRoot, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	probe, err := newProbeServer(root, "local", time.Second)
	if err != nil {
		t.Fatal(err)
	}

	previousOutput := log.Writer()
	var output bytes.Buffer
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previousOutput) })

	list := httptest.NewRecorder()
	probe.handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/v1/session-roots", nil))
	if list.Code != http.StatusInternalServerError {
		t.Fatalf("session roots status = %d %s", list.Code, list.Body.String())
	}
	if !strings.Contains(output.String(), "session storage scan failed: path=\""+usersRoot+"\"") {
		t.Fatalf("session root error was not logged with its source path: %s", output.String())
	}
}

func TestHostAgentNodeAndComponentLogs(t *testing.T) {
	root := t.TempDir()
	probe, err := newProbeServer(root, "local", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	probe.composeProject = "cocola"
	probe.docker = &fakeDockerAPI{
		info:       dockerInfo{Name: "docker-host", NCPU: 8, MemTotal: 16 << 30, OperatingSystem: "Linux"},
		containers: map[string]string{"web": "web-id", "gateway": "gateway-id"},
		logs:       map[string][]byte{"web-id": []byte("first\nsecond\n"), "gateway-id": []byte("gateway\n")},
	}

	node := httptest.NewRecorder()
	probe.handler().ServeHTTP(node, httptest.NewRequest(http.MethodGet, "/v1/node", nil))
	if node.Code != http.StatusOK {
		t.Fatalf("node status = %d %s", node.Code, node.Body.String())
	}
	var nodeBody nodeInfoResponse
	if err := json.Unmarshal(node.Body.Bytes(), &nodeBody); err != nil {
		t.Fatal(err)
	}
	if nodeBody.Name != "local" || nodeBody.CPUCapacity != "8" || nodeBody.MemoryCapacity == "" ||
		nodeBody.Labels["cocola.dev/docker-host"] != "docker-host" {
		t.Fatalf("node response = %+v", nodeBody)
	}

	logs := httptest.NewRecorder()
	probe.handler().ServeHTTP(logs, httptest.NewRequest(http.MethodGet, "/v1/component-logs?file=web.log&lines=1", nil))
	if logs.Code != http.StatusOK {
		t.Fatalf("logs status = %d %s", logs.Code, logs.Body.String())
	}
	var logsBody componentLogsResponse
	if err := json.Unmarshal(logs.Body.Bytes(), &logsBody); err != nil {
		t.Fatal(err)
	}
	if len(logsBody.Files) != 2 || logsBody.Selected != "web.log" || len(logsBody.Lines) != 1 || logsBody.Lines[0] != "second" {
		t.Fatalf("logs response = %+v", logsBody)
	}
}

func TestDemuxDockerStream(t *testing.T) {
	frame := func(stream byte, payload string) []byte {
		header := make([]byte, 8)
		header[0] = stream
		binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
		return append(header, payload...)
	}
	raw := append(frame(1, "stdout\n"), frame(2, "stderr\n")...)
	if !looksLikeDockerMultiplexed(raw) {
		t.Fatal("valid Docker multiplex stream was not detected")
	}
	if got := demuxDockerStream(raw); !bytes.Equal(got, []byte("stdout\nstderr\n")) {
		t.Fatalf("demux = %q", got)
	}
	if looksLikeDockerMultiplexed([]byte("ordinary log output\n")) {
		t.Fatal("plain log output was detected as a Docker multiplex stream")
	}
}

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
