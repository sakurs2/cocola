package memory

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

func TestBuildRecallResultSurfacesSanitizedOutcomes(t *testing.T) {
	items := []memoryResult{{
		URI: "viking://user/memories/preferences/editor.md", Content: "Uses dark mode",
	}}
	tests := []struct {
		name      string
		items     []memoryResult
		searchErr error
		status    string
		count     int
		errorCode string
	}{
		{name: "hit", items: items, status: RecallStatusHit, count: 1},
		{name: "miss", status: RecallStatusMiss},
		{
			name: "timeout", searchErr: context.DeadlineExceeded,
			status: RecallStatusUnavailable, errorCode: "MEMORY_RECALL_TIMEOUT",
		},
		{
			name: "unavailable", searchErr: errors.New("transport failed"),
			status: RecallStatusUnavailable, errorCode: "MEMORY_UNAVAILABLE",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := buildRecallResult(test.items, test.searchErr)
			if result.Status != test.status || result.Count != test.count ||
				result.ErrorCode != test.errorCode {
				t.Fatalf("result = %+v, want status=%s count=%d error=%s",
					result, test.status, test.count, test.errorCode)
			}
		})
	}
	result := buildRecallResult(items, nil)
	if strings.Contains(result.Context, "viking://") {
		t.Fatalf("user-visible recall content leaked provider URI: %q", result.Context)
	}
}

func TestCaptureSkipReasonEnforcesProductBoundary(t *testing.T) {
	tests := []struct {
		name  string
		input CaptureInput
		want  string
	}{
		{name: "interactive", input: CaptureInput{Source: "chat", InteractionMode: "execute"}},
		{name: "scheduled", input: CaptureInput{Source: "scheduled_task"}, want: "skipped_scheduled"},
		{name: "plan mode", input: CaptureInput{InteractionMode: "plan"}, want: "skipped_plan"},
		{name: "approved plan execution", input: CaptureInput{PlanID: "plan-1"}, want: "skipped_plan"},
		{name: "project", input: CaptureInput{ProjectID: "project-1"}, want: "skipped_project"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := captureSkipReason(test.input); got != test.want {
				t.Fatalf("captureSkipReason() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBeginRecallOnlyNotifiesWhenEnabled(t *testing.T) {
	tests := []struct {
		name     string
		settings Settings
		want     bool
	}{
		{name: "enabled", settings: Settings{GlobalEnabled: true, UseEnabled: true}, want: true},
		{name: "disabled globally", settings: Settings{UseEnabled: true}},
		{name: "disabled by user", settings: Settings{GlobalEnabled: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			got := beginRecall(test.settings, func() { calls++ })
			if got != test.want {
				t.Fatalf("beginRecall() = %v, want %v", got, test.want)
			}
			wantCalls := 0
			if test.want {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("onStart calls = %d, want %d", calls, wantCalls)
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
	contextText, uris := formatRecall(items)
	if len(uris) != maxRecallItems {
		t.Fatalf("got %d recalled URIs, want %d", len(uris), maxRecallItems)
	}

	contextText, _ = formatRecall([]memoryResult{{
		URI: "viking://user/memories/profile.md", Content: strings.Repeat("记", maxRecallBytes*2),
	}})
	if got := len(contextText); got > maxRecallBytes {
		t.Fatalf("recall context has %d bytes, want at most %d", got, maxRecallBytes)
	}
	if !utf8.ValidString(contextText) {
		t.Fatal("recall byte truncation produced invalid UTF-8")
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

func TestOpenVikingSearchUsesBoundedMemoryContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search/search" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("X-OpenViking-Account") != "tenant" ||
			r.Header.Get("X-OpenViking-User") != "user" {
			t.Fatalf("identity headers missing: %#v", r.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if got, ok := body["score_threshold"].(float64); !ok || got != minRecallScore {
			t.Fatalf("score_threshold = %#v, want %v", body["score_threshold"], minRecallScore)
		}
		if body["target_uri"] != "viking://user/memories/" || body["context_type"] != "memory" {
			t.Fatalf("unexpected search scope: %#v", body)
		}
		if body["limit"] != float64(maxRecallItems) {
			t.Fatalf("limit = %#v, want %d", body["limit"], maxRecallItems)
		}
		writeOpenVikingTestJSON(t, w, http.StatusOK, map[string]any{
			"status": "ok",
			"result": map[string]any{
				"memories": []any{
					map[string]any{
						"uri": "viking://user/user/memories/preferences/below.md", "score": 0.349,
					},
					map[string]any{
						"uri": "viking://user/user/memories/preferences/boundary.md", "score": 0.35,
					},
					map[string]any{
						"uri": "viking://user/user/memories/preferences/above.md", "score": 0.8,
					},
					map[string]any{
						"uri": "viking://user/other/memories/preferences/leak.md", "score": 1.0,
					},
				},
			},
		})
	}))
	defer server.Close()

	client := newOpenVikingClient(server.URL, "root-key")
	items, err := client.searchMemories(
		context.Background(), Identity{TenantID: "tenant", UserID: "user"}, "editor", maxRecallItems,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %#v, want boundary and above-threshold results", items)
	}
	if items[0].Score != minRecallScore || items[1].Score != 0.8 {
		t.Fatalf("unexpected scores: %#v", items)
	}
}

func TestFormatRecallFallsBackToAbstract(t *testing.T) {
	contextText, uris := formatRecall([]memoryResult{{
		URI: "viking://user/memories/entities/cocola.md", Abstract: "Cocola project",
	}})
	if !strings.Contains(contextText, "Cocola project") {
		t.Fatalf("abstract missing from context: %q", contextText)
	}
	if len(uris) != 1 || uris[0] != "viking://user/memories/entities/cocola.md" {
		t.Fatalf("unexpected recalled URIs: %#v", uris)
	}
}

func TestCollectItemsNormalizesIdentityAndHidesMetadata(t *testing.T) {
	canonical := "viking://user/user/memories/preferences/editor.md"
	raw := []any{
		map[string]any{"uri": canonical, "abstract": "first"},
		map[string]any{"child": map[string]any{"uri": canonical}},
		map[string]any{"uri": "viking://user/user/memories/preferences/.overview.md"},
		map[string]any{"uri": "viking://user/other/memories/preferences/leak.md"},
		map[string]any{"uri": "viking://user/user/memories/cases/unsupported.md"},
	}

	items := collectItems(raw, "user")
	if len(items) != 1 || items[0].URI != "viking://user/memories/preferences/editor.md" {
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
		{name: "first", attempts: 1, want: 15 * time.Second},
		{name: "second", attempts: 2, want: time.Minute},
		{name: "horizon remainder", attempts: 3, age: 5*time.Hour + 59*time.Minute, want: time.Minute},
		{name: "attempt limit", attempts: 5, dead: true},
		{name: "horizon", attempts: 2, age: 6 * time.Hour, dead: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, dead := captureRetryDelay(test.attempts, test.age, 5, 6*time.Hour)
			if got != test.want || dead != test.dead {
				t.Fatalf("got (%s, %t), want (%s, %t)", got, dead, test.want, test.dead)
			}
		})
	}
}

func TestCaptureLoadFailureClassification(t *testing.T) {
	if !permanentCaptureLoadFailure(pgx.ErrNoRows) {
		t.Fatal("missing run must be a permanent capture failure")
	}
	if !permanentCaptureLoadFailure(fmt.Errorf("decode: %w", errCapturePayload)) {
		t.Fatal("invalid persisted JSON must be a permanent capture failure")
	}
	if permanentCaptureLoadFailure(errors.New("temporary database timeout")) {
		t.Fatal("transient database failures must remain retryable")
	}
}

func TestCapturePollExpiresEvenWhenProviderKeepsReturningRunning(t *testing.T) {
	createdAt := time.Unix(100, 0)
	if capturePollExpired(createdAt, createdAt.Add(6*time.Hour-time.Second), 6*time.Hour) {
		t.Fatal("capture expired before its retry horizon")
	}
	if !capturePollExpired(createdAt, createdAt.Add(6*time.Hour), 6*time.Hour) {
		t.Fatal("running provider tasks must expire at the retry horizon")
	}
}

func TestOpenVikingEnsureSessionCreatesRestrictedPolicy(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions/cocola-run-1":
			writeOpenVikingTestJSON(t, w, http.StatusNotFound, map[string]any{
				"status": "error", "error": map[string]any{"code": "NOT_FOUND", "message": "missing"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			policy, ok := body["memory_policy"].(map[string]any)
			if !ok || policy["working_memory"].(map[string]any)["enabled"] != false {
				t.Fatalf("unexpected memory policy: %#v", body["memory_policy"])
			}
			types, ok := policy["memory_types"].([]any)
			if !ok || len(types) != 4 {
				t.Fatalf("memory types = %#v, want four user types", policy["memory_types"])
			}
			writeOpenVikingTestJSON(t, w, http.StatusOK, map[string]any{
				"status": "ok", "result": map[string]any{"session_id": "cocola-run-1"},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := newOpenVikingClient(server.URL, "root-key")
	count, err := client.ensureSession(
		context.Background(), Identity{TenantID: "tenant", UserID: "user"}, "cocola-run-1",
	)
	if err != nil || count != 0 || requests != 2 {
		t.Fatalf("ensureSession = (%d, %v), requests=%d", count, err, requests)
	}
}

func TestOpenVikingAddTurnUsesOneBatchAndContextParts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/sessions/cocola-run-1/messages/batch" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Messages []struct {
				Role    string           `json:"role"`
				Content string           `json:"content"`
				Parts   []map[string]any `json:"parts"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Messages) != 2 || body.Messages[0].Role != "user" ||
			body.Messages[0].Content != "Question" || body.Messages[1].Role != "assistant" {
			t.Fatalf("unexpected messages: %#v", body.Messages)
		}
		parts := body.Messages[1].Parts
		if len(parts) != 2 || parts[0]["type"] != "text" || parts[0]["text"] != "Answer" ||
			parts[1]["type"] != "context" || parts[1]["context_type"] != "memory" {
			t.Fatalf("unexpected assistant parts: %#v", parts)
		}
		writeOpenVikingTestJSON(t, w, http.StatusOK, map[string]any{
			"status": "ok", "result": map[string]any{"message_count": 2},
		})
	}))
	defer server.Close()

	client := newOpenVikingClient(server.URL, "root-key")
	err := client.addTurn(
		context.Background(), Identity{TenantID: "tenant", UserID: "user"}, "cocola-run-1",
		"Question", "Answer", []string{
			"viking://user/memories/preferences/editor.md",
			"viking://agent/memories/preferences/invalid.md",
		},
	)
	if err != nil {
		t.Fatal(err)
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

func TestTerminalTaskStatusRecognizesProviderAliases(t *testing.T) {
	for _, status := range []string{
		"cancelled", "completed", "success", "succeeded", "failed", "error",
	} {
		if !terminalTaskStatus(status) {
			t.Fatalf("status %q must be terminal", status)
		}
	}
	for _, status := range []string{"", "pending", "processing", "running"} {
		if terminalTaskStatus(status) {
			t.Fatalf("status %q must remain active", status)
		}
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

func writeOpenVikingTestJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}
