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
