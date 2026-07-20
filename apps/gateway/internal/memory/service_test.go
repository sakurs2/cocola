package memory

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestBuildRecallResultSurfacesSanitizedOutcomes(t *testing.T) {
	items := []memoryResult{{
		URI: "viking://user/memories/preferences/editor.md", Content: "Uses dark mode",
	}}
	tests := []struct {
		name       string
		profile    string
		items      []memoryResult
		profileErr error
		findErr    error
		status     string
		count      int
		errorCode  string
	}{
		{name: "hit", items: items, status: RecallStatusHit, count: 1},
		{name: "miss", status: RecallStatusMiss},
		{name: "missing profile is a normal miss", profileErr: ErrNotFound, status: RecallStatusMiss},
		{
			name: "partial recall", profile: "Prefers concise answers",
			findErr: context.DeadlineExceeded, status: RecallStatusDegraded,
			count: 1, errorCode: "MEMORY_RECALL_TIMEOUT",
		},
		{
			name: "unavailable", profileErr: errors.New("transport failed"),
			findErr: errors.New("transport failed"), status: RecallStatusUnavailable,
			errorCode: "MEMORY_UNAVAILABLE",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := buildRecallResult(test.profile, test.items, test.profileErr, test.findErr)
			if result.Status != test.status || result.Count != test.count ||
				result.ErrorCode != test.errorCode {
				t.Fatalf("result = %+v, want status=%s count=%d error=%s",
					result, test.status, test.count, test.errorCode)
			}
		})
	}
}

func TestFormatRecallCapsItemsAndCharacters(t *testing.T) {
	items := make([]memoryResult, 0, 8)
	for index := 0; index < 8; index++ {
		items = append(items, memoryResult{
			URI:     "viking://user/memories/preferences/item-" + string(rune('a'+index)) + ".md",
			Content: "memory",
		})
	}

	context, uris := formatRecall("profile", items)
	if len(uris) != maxRecallItems {
		t.Fatalf("got %d recalled URIs, want %d total items", len(uris), maxRecallItems)
	}
	if uris[0] != "viking://user/memories/profile.md" {
		t.Fatalf("profile URI missing from used contexts: %#v", uris)
	}

	context, _ = formatRecall(strings.Repeat("记", maxRecallChars*2), nil)
	if got := utf8.RuneCountInString(context); got > maxRecallChars {
		t.Fatalf("recall context has %d runes, want at most %d", got, maxRecallChars)
	}
}

func TestOpenVikingAccountFallsBackForDefaultTenant(t *testing.T) {
	if got := (Identity{}).openVikingAccount(); got != "default" {
		t.Fatalf("empty tenant account = %q, want default", got)
	}
	if got := (Identity{TenantID: "tenant-a"}).openVikingAccount(); got != "tenant-a" {
		t.Fatalf("explicit tenant account = %q, want tenant-a", got)
	}
}

func TestFormatRecallFallsBackToAbstract(t *testing.T) {
	context, uris := formatRecall("", []memoryResult{{
		URI: "viking://user/memories/entities/cocola.md", Abstract: "Cocola project",
	}})
	if !strings.Contains(context, "Cocola project") {
		t.Fatalf("abstract missing from context: %q", context)
	}
	if len(uris) != 1 || uris[0] != "viking://user/memories/entities/cocola.md" {
		t.Fatalf("unexpected recalled URIs: %#v", uris)
	}
}

func TestCollectItemsDeduplicatesAndHidesMetadata(t *testing.T) {
	uri := "viking://user/memories/preferences/editor.md"
	raw := []any{
		map[string]any{"uri": uri, "abstract": "first"},
		map[string]any{"child": map[string]any{"uri": uri}},
		map[string]any{"uri": "viking://user/memories/preferences/.overview.md"},
		map[string]any{"uri": "viking://user/memories/cases/unsupported.md"},
	}

	items := collectItems(raw)
	if len(items) != 1 || items[0].URI != uri {
		t.Fatalf("unexpected items: %#v", items)
	}
}

func TestDecodeItemIDRejectsDirectoriesAndHiddenMetadata(t *testing.T) {
	invalid := []string{
		"viking://user/memories/preferences/",
		"viking://user/memories/preferences/.abstract.md",
		"viking://user/memories/preferences/../profile.md",
		"viking://user/memories/preferences/%2e%2e/profile.md",
		"viking://user/memories/preferences/nested%2f..%2fprofile.md",
		"viking://agent/memories/preferences/editor.md",
	}
	for _, uri := range invalid {
		opaque := base64.RawURLEncoding.EncodeToString([]byte(uri))
		if _, err := decodeItemID(opaque); err == nil {
			t.Fatalf("decodeItemID accepted %q", uri)
		}
	}
}

func TestFinalTextPartsOnlyKeepsFinalAnswer(t *testing.T) {
	raw := []byte(`[
		{"type":"text","text":"I will inspect it."},
		{"type":"reasoning","text":"private"},
		{"type":"tool-call","text":"tool output"},
		{"type":"text","text":"Final answer"},
		{"type":"file","text":"ignored file"}
	]`)

	text, err := finalTextParts(raw)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Final answer" {
		t.Fatalf("got %q, want final answer only", text)
	}
}

func TestAllTextPartsExcludesNonTextContent(t *testing.T) {
	raw := []byte(`[
		{"type":"text","text":"Question"},
		{"type":"file","text":"secret file contents"},
		{"type":"tool-call","text":"tool details"}
	]`)

	text, err := allTextParts(raw)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Question" {
		t.Fatalf("got %q, want user text only", text)
	}
}

func TestCaptureRetryDelayIsBounded(t *testing.T) {
	tests := []struct {
		name     string
		attempts int
		age      time.Duration
		want     time.Duration
		dead     bool
	}{
		{name: "first", attempts: 1, want: time.Minute},
		{name: "horizon remainder", attempts: 3, age: 23*time.Hour + 59*time.Minute, want: time.Minute},
		{name: "attempt limit", attempts: 8, dead: true},
		{name: "horizon", attempts: 2, age: 24 * time.Hour, dead: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, dead := captureRetryDelay(test.attempts, test.age, 8, 24*time.Hour)
			if got != test.want || dead != test.dead {
				t.Fatalf("got (%s, %t), want (%s, %t)", got, dead, test.want, test.dead)
			}
		})
	}
}

func TestCaptureSessionResetOnlyForReclaimedJobs(t *testing.T) {
	for _, status := range []string{"submitted", "retry"} {
		if !captureNeedsSessionReset(status) {
			t.Fatalf("status %q should reset its deterministic session", status)
		}
	}
	for _, status := range []string{"pending", "completed", "dead", "cancelled"} {
		if captureNeedsSessionReset(status) {
			t.Fatalf("status %q should not reset its session", status)
		}
	}
}

func TestCommitRecoveryDecision(t *testing.T) {
	tests := []struct {
		name    string
		task    *openVikingTask
		archive commitArchiveState
		want    commitRecoveryDecision
	}{
		{name: "running task", task: &openVikingTask{ID: "task-1", Status: "running"}, want: commitRecoveryAdopt},
		{name: "completed task", task: &openVikingTask{ID: "task-1", Status: "completed"}, want: commitRecoveryComplete},
		{name: "failed task", task: &openVikingTask{ID: "task-1", Status: "failed"}, want: commitRecoveryResubmit},
		{name: "completed archive", archive: commitArchiveCompleted, want: commitRecoveryComplete},
		{name: "queued archive", archive: commitArchivePending, want: commitRecoveryWait},
		{name: "failed archive", archive: commitArchiveFailed, want: commitRecoveryResubmit},
		{name: "no commit evidence", archive: commitArchiveAbsent, want: commitRecoveryResubmit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := decideCommitRecovery(test.task, test.archive); got != test.want {
				t.Fatalf("decision = %d, want %d", got, test.want)
			}
		})
	}
}

func TestProcessAvailableStopsBeforeClaimWhenWorkerIsCancelled(t *testing.T) {
	workerCtx, cancel := context.WithCancel(context.Background())
	cancel()
	service := &Service{workerCtx: workerCtx}
	done := make(chan struct{})
	go func() {
		service.processAvailable(20)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled worker tried to process the batch")
	}
}

func TestLatestCommitTaskKeepsNewestTerminalRecord(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("task_type"); got != "session_commit" {
			t.Fatalf("task_type = %q", got)
		}
		if got := r.URL.Query().Get("resource_id"); got != "cocola-run-1" {
			t.Fatalf("resource_id = %q", got)
		}
		writeOpenVikingTestJSON(t, w, http.StatusOK, map[string]any{
			"status": "ok",
			"result": []any{
				map[string]any{"task_id": "new-failed", "status": "failed"},
				map[string]any{"task_id": "old-completed", "status": "completed"},
			},
		})
	}))
	defer server.Close()

	client := newOpenVikingClient(server.URL, "root-key")
	task, found, err := client.latestCommitTask(
		context.Background(), Identity{TenantID: "tenant", UserID: "user"}, "cocola-run-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !found || task.ID != "new-failed" || task.Status != "failed" {
		t.Fatalf("unexpected task: found=%t task=%+v", found, task)
	}
}

func TestCommitArchiveStateUsesDurableMarkers(t *testing.T) {
	tests := []struct {
		name   string
		exists string
		want   commitArchiveState
	}{
		{name: "completed", exists: ".done", want: commitArchiveCompleted},
		{name: "failed", exists: ".failed.json", want: commitArchiveFailed},
		{name: "queued", exists: "messages.jsonl", want: commitArchivePending},
		{name: "absent", want: commitArchiveAbsent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/fs/stat" {
					t.Fatalf("path = %q", r.URL.Path)
				}
				uri := r.URL.Query().Get("uri")
				if test.exists != "" && strings.HasSuffix(uri, "/"+test.exists) {
					writeOpenVikingTestJSON(t, w, http.StatusOK, map[string]any{
						"status": "ok", "result": map[string]any{"uri": uri},
					})
					return
				}
				writeOpenVikingTestJSON(t, w, http.StatusNotFound, map[string]any{
					"status": "error",
					"error":  map[string]any{"code": "NOT_FOUND", "message": "missing"},
				})
			}))
			defer server.Close()

			client := newOpenVikingClient(server.URL, "root-key")
			got, err := client.commitArchiveState(
				context.Background(), Identity{TenantID: "tenant", UserID: "user"}, "cocola-run-1",
			)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("state = %d, want %d", got, test.want)
			}
		})
	}
}

func writeOpenVikingTestJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}
