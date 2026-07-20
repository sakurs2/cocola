package convo

import (
	"encoding/json"
	"testing"
)

// TestReducerMirrorsFrontend verifies the aggregation matches the frontend
// reducePart semantics: text/thinking coalesce, tool_use adds a tool-call,
// tool_result pairs by id, sandbox/result/done drop.
func TestReducerAggregation(t *testing.T) {
	r := NewReducer()
	r.Apply("text", map[string]string{"text": "Hel"})
	r.Apply("text", map[string]string{"text": "lo"}) // coalesce into same text part
	r.Apply("thinking", map[string]string{"thinking": "hmm"})
	r.Apply("tool_use", map[string]string{"id": "t1", "name": "bash", "input": "{\"cmd\":\"ls\"}"})
	r.Apply("sandbox", map[string]string{"sandbox_id": "s"}) // must be ignored
	r.Apply("tool_result", map[string]string{"tool_use_id": "t1", "content": "done"})
	r.Apply("file", map[string]string{
		"id":           "a1",
		"filename":     "report.txt",
		"mime":         "text/plain",
		"size":         "42",
		"download_url": "/api/conversations/c/artifacts/a1",
	})
	r.Apply("done", map[string]string{})

	p := r.Parts()
	if len(p) != 4 {
		t.Fatalf("want 4 parts, got %d: %+v", len(p), p)
	}
	if p[0].Type != PartText || p[0].Text != "Hello" {
		t.Fatalf("text coalesce failed: %+v", p[0])
	}
	if p[1].Type != PartReasoning || p[1].Text != "hmm" {
		t.Fatalf("reasoning failed: %+v", p[1])
	}
	if p[2].Type != PartToolCall || p[2].ToolName != "bash" || p[2].Result == nil || *p[2].Result != "done" {
		t.Fatalf("tool-call/result pairing failed: %+v", p[2])
	}
	if p[3].Type != PartFile || p[3].ID != "a1" || p[3].Size != 42 {
		t.Fatalf("file part failed: %+v", p[3])
	}
}

// TestReducerUnmatchedToolResult: a result with no matching tool_use becomes text.
func TestReducerUnmatchedToolResult(t *testing.T) {
	r := NewReducer()
	r.Apply("tool_result", map[string]string{"tool_use_id": "ghost", "content": "orphan", "is_error": "true"})
	p := r.Parts()
	if len(p) != 1 || p[0].Type != PartText {
		t.Fatalf("unmatched result should surface as text: %+v", p)
	}
}

func TestReducerUpsertsVersionedEnvironmentSnapshotAsFirstPart(t *testing.T) {
	r := NewReducer()
	r.Apply("text", map[string]string{"text": "hello"})
	r.Apply("environment_prepare", map[string]string{
		"snapshot": `{"schema_version":1,"part_id":"environment","state":"preparing","components":[]}`,
	})
	r.Apply("environment_prepare", map[string]string{
		"snapshot": `{"schema_version":2,"part_id":"environment","state":"ready","components":[{"kind":"future-capability","status":"ready","label":"Future","future_field":{"kept":true}}]}`,
	})

	parts := r.Parts()
	if len(parts) != 2 || parts[0].Type != PartEnvironment || parts[1].Type != PartText {
		t.Fatalf("environment snapshot should upsert at the front: %+v", parts)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(parts[0].Environment, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot["schema_version"] != float64(2) || snapshot["state"] != "ready" {
		t.Fatalf("environment snapshot was not replaced: %#v", snapshot)
	}
	components, ok := snapshot["components"].([]any)
	if !ok || len(components) != 1 {
		t.Fatalf("environment components missing: %#v", snapshot)
	}
	component := components[0].(map[string]any)
	if _, ok := component["future_field"]; !ok {
		t.Fatalf("unknown future field was dropped: %#v", component)
	}
}

func TestReducerUpsertsSessionStatusWithoutSplittingText(t *testing.T) {
	r := NewReducer()
	r.Apply("environment_prepare", map[string]string{
		"snapshot": `{"schema_version":1,"part_id":"environment","state":"ready","components":[]}`,
	})
	r.Apply("text", map[string]string{"text": "Hel"})
	r.Apply("environment_status", map[string]string{
		"version":    "1",
		"phase":      "ready",
		"components": `[{"kind":"mcp","id":"docs","label":"Docs","status":"connected","tool_count":2}]`,
	})
	r.Apply("text", map[string]string{"text": "lo"})
	r.Apply("environment_status", map[string]string{
		"version":    "2",
		"phase":      "degraded",
		"components": `[{"kind":"future","id":"next","label":"Next","status":"unavailable","tool_count":0,"future_field":{"kept":true}}]`,
	})

	parts := r.Parts()
	if len(parts) != 3 || parts[0].Type != PartEnvironment ||
		parts[1].Type != PartSessionStatus || parts[2].Type != PartText {
		t.Fatalf("session status should sit before message content: %+v", parts)
	}
	if parts[2].Text != "Hello" {
		t.Fatalf("session status split text aggregation: %+v", parts[2])
	}
	var snapshot map[string]any
	if err := json.Unmarshal(parts[1].SessionStatus, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot["version"] != float64(2) || snapshot["phase"] != "degraded" {
		t.Fatalf("session status was not replaced: %#v", snapshot)
	}
	components := snapshot["components"].([]any)
	component := components[0].(map[string]any)
	if _, ok := component["future_field"]; !ok {
		t.Fatalf("unknown session status field was dropped: %#v", component)
	}
}

func TestReducerRejectsInvalidSessionStatus(t *testing.T) {
	r := NewReducer()
	r.Apply("environment_status", map[string]string{
		"phase": "unknown", "components": `[]`,
	})
	r.Apply("environment_status", map[string]string{
		"phase": "ready", "components": `{}`,
	})
	if len(r.Parts()) != 0 {
		t.Fatalf("invalid session status should be ignored: %+v", r.Parts())
	}
}

func TestReducerUpsertsProgressByID(t *testing.T) {
	r := NewReducer()
	r.Apply("progress", map[string]string{
		"id":    "plan",
		"items": `[{"text":"inspect","completed":false}]`,
	})
	r.Apply("progress", map[string]string{
		"id":    "other",
		"items": `[{"text":"separate","completed":false}]`,
	})
	r.Apply("progress", map[string]string{
		"id":    "plan",
		"items": `[{"text":"inspect","completed":true}]`,
	})

	parts := r.Parts()
	if len(parts) != 2 {
		t.Fatalf("progress parts = %d, want 2: %+v", len(parts), parts)
	}
	if parts[0].Type != PartProgress || parts[0].ProgressID != "plan" ||
		string(parts[0].ProgressItems) != `[{"text":"inspect","completed":true}]` {
		t.Fatalf("progress was not replaced by id: %+v", parts[0])
	}
	if parts[1].ProgressID != "other" {
		t.Fatalf("independent progress item was overwritten: %+v", parts[1])
	}
}

func TestReducerUpsertsAndRemovesMemoryRecall(t *testing.T) {
	r := NewReducer()
	r.Apply("memory_recall", map[string]string{"status": "running"})
	r.Apply("memory_recall", map[string]string{
		"status": "degraded", "count": "2", "error_code": "MEMORY_RECALL_TIMEOUT",
	})

	parts := r.Parts()
	if len(parts) != 1 || parts[0].Type != PartMemoryRecall {
		t.Fatalf("memory recall should be one replaceable part: %+v", parts)
	}
	if parts[0].MemoryStatus != "degraded" || parts[0].MemoryCount != 2 ||
		parts[0].MemoryErrorCode != "MEMORY_RECALL_TIMEOUT" {
		t.Fatalf("memory recall outcome was not replaced: %+v", parts[0])
	}

	r.Apply("memory_recall", map[string]string{"status": "miss"})
	if len(r.Parts()) != 0 {
		t.Fatalf("a recall miss should not leave UI noise: %+v", r.Parts())
	}
}
