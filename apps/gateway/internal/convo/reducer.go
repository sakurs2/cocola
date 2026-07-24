package convo

import (
	"encoding/json"
	"strconv"
)

// Reducer aggregates the agent's SSE event stream into an assistant message's
// Part slice, mirroring the frontend reducer in apps/web/app/runtime-provider.tsx
// (reducePart/appendTo/fillToolResult). Persisting the SAME shape the browser
// renders is what makes route A a zero-drift mirror: a stored message replays
// straight through convertMessage.
//
// Event vocabulary (kind): environment_prepare | environment_status |
// memory_recall | text | thinking | tool_use | tool_result | file | plan_ready | error;
// result / system / sandbox / done carry no message-body content and are dropped
// (identical to the frontend).
type Reducer struct {
	parts []Part
}

// NewReducer returns an empty aggregator.
func NewReducer() *Reducer { return &Reducer{} }

// Apply folds one event (kind + its data map) into the accumulating parts.
func (r *Reducer) Apply(kind string, data map[string]string) {
	switch kind {
	case "environment_prepare":
		r.upsertEnvironment(data["snapshot"])
	case "environment_status":
		r.upsertSessionStatus(data)
	case "memory_recall":
		r.upsertMemoryRecall(data)
	case "scm_approval":
		r.upsertSCMApproval(data)
	case "text":
		r.appendText(PartText, data["text"])
	case "thinking":
		r.appendText(PartReasoning, data["thinking"])
	case "tool_use":
		id := data["id"]
		name := data["name"]
		if name == "" {
			name = "tool"
		}
		r.parts = append(r.parts, Part{
			Type:       PartToolCall,
			ToolCallID: id,
			ToolName:   name,
			ArgsText:   data["input"],
		})
	case "tool_result":
		r.fillToolResult(data["tool_use_id"], data["content"], truthy(data["is_error"]))
	case "file":
		r.appendFile(data)
	case "progress":
		r.upsertProgress(data["id"], data["items"])
	case "plan_ready":
		r.upsertPlan(data)
	case "error":
		r.appendText(PartText, "\n\n⚠️ "+errText(data))
	default:
		// result / system / sandbox / done / unknown: no body content.
	}
}

func (r *Reducer) upsertPlan(data map[string]string) {
	version, _ := strconv.Atoi(data["version"])
	content := data["content_markdown"]
	status := data["status"]
	if data["id"] == "" || version < 1 || status == "" ||
		content == "" || len(content) > 128<<10 {
		return
	}
	part := Part{
		Type: PartPlan, PlanID: data["id"], PlanVersion: version,
		Status: status, PlanContentMarkdown: content,
	}
	for index := range r.parts {
		if r.parts[index].Type == PartPlan && r.parts[index].PlanID == part.PlanID {
			r.parts[index] = part
			return
		}
	}
	r.parts = append(r.parts, part)
}

func (r *Reducer) upsertSCMApproval(data map[string]string) {
	id, status := data["id"], data["status"]
	if id == "" || (status != "pending" && status != "approved" &&
		status != "denied" && status != "expired") {
		return
	}
	part := Part{Type: PartSCMApproval, ApprovalID: id, ApprovalStatus: status,
		ApprovalCategory: data["category"], ApprovalLabel: data["label"]}
	for index := range r.parts {
		if r.parts[index].Type == PartSCMApproval && r.parts[index].ApprovalID == id {
			r.parts[index] = part
			return
		}
	}
	r.parts = append(r.parts, part)
}

func (r *Reducer) upsertMemoryRecall(data map[string]string) {
	status := data["status"]
	if status == "skipped" {
		return
	}
	if status != "running" && status != "hit" && status != "miss" &&
		status != "degraded" && status != "unavailable" {
		return
	}
	count, _ := strconv.Atoi(data["count"])
	if count < 0 || count > 100 {
		count = 0
	}
	part := Part{
		Type: PartMemoryRecall, Status: status,
		MemoryCount: count, MemoryErrorCode: data["error_code"],
		MemoryContent: data["content"],
	}
	for i := range r.parts {
		if r.parts[i].Type == PartMemoryRecall {
			r.parts[i] = part
			return
		}
	}
	// A normal miss stays in the transient running part's slot and is hidden by
	// clients. Keeping the part cardinality stable prevents completed-message
	// PartByIndex renderers from observing a stale out-of-bounds index.
	insertAt := 0
	for insertAt < len(r.parts) &&
		(r.parts[insertAt].Type == PartEnvironment || r.parts[insertAt].Type == PartSessionStatus) {
		insertAt++
	}
	next := make([]Part, 0, len(r.parts)+1)
	next = append(next, r.parts[:insertAt]...)
	next = append(next, part)
	next = append(next, r.parts[insertAt:]...)
	r.parts = next
}

type sessionStatusEnvelope struct {
	Version    int             `json:"version"`
	Phase      string          `json:"phase"`
	Components json.RawMessage `json:"components"`
}

func (r *Reducer) upsertSessionStatus(data map[string]string) {
	phase := data["phase"]
	if phase != "preparing" && phase != "ready" && phase != "degraded" {
		return
	}
	componentsJSON := data["components"]
	if componentsJSON == "" || len(componentsJSON) > maxEnvironmentSnapshotBytes {
		return
	}
	var components []json.RawMessage
	if err := json.Unmarshal([]byte(componentsJSON), &components); err != nil || components == nil {
		return
	}
	version, _ := strconv.Atoi(data["version"])
	if version < 1 {
		version = 1
	}
	raw, err := json.Marshal(sessionStatusEnvelope{
		Version:    version,
		Phase:      phase,
		Components: json.RawMessage(componentsJSON),
	})
	if err != nil {
		return
	}
	for i := range r.parts {
		if r.parts[i].Type == PartSessionStatus {
			r.parts[i].SessionStatus = raw
			return
		}
	}
	part := Part{Type: PartSessionStatus, SessionStatus: raw}
	insertAt := 0
	if len(r.parts) > 0 && r.parts[0].Type == PartEnvironment {
		insertAt = 1
	}
	next := make([]Part, 0, len(r.parts)+1)
	next = append(next, r.parts[:insertAt]...)
	next = append(next, part)
	next = append(next, r.parts[insertAt:]...)
	r.parts = next
}

func (r *Reducer) upsertProgress(id, items string) {
	if id == "" || len(items) > maxEnvironmentSnapshotBytes || !json.Valid([]byte(items)) {
		return
	}
	raw := append(json.RawMessage(nil), items...)
	for i := range r.parts {
		if r.parts[i].Type == PartProgress && r.parts[i].ProgressID == id {
			r.parts[i].ProgressItems = raw
			return
		}
	}
	r.parts = append(r.parts, Part{Type: PartProgress, ProgressID: id, ProgressItems: raw})
}

const maxEnvironmentSnapshotBytes = 64 << 10

type environmentEnvelope struct {
	PartID string `json:"part_id"`
}

func (r *Reducer) upsertEnvironment(snapshot string) {
	if snapshot == "" || len(snapshot) > maxEnvironmentSnapshotBytes || !json.Valid([]byte(snapshot)) {
		return
	}
	var next environmentEnvelope
	if err := json.Unmarshal([]byte(snapshot), &next); err != nil || next.PartID == "" {
		return
	}
	raw := append(json.RawMessage(nil), snapshot...)
	for i := range r.parts {
		if r.parts[i].Type != PartEnvironment {
			continue
		}
		var current environmentEnvelope
		if json.Unmarshal(r.parts[i].Environment, &current) == nil && current.PartID == next.PartID {
			r.parts[i].Environment = raw
			return
		}
	}
	r.parts = append([]Part{{Type: PartEnvironment, Environment: raw}}, r.parts...)
}

func (r *Reducer) appendFile(data map[string]string) {
	size, _ := strconv.ParseInt(data["size"], 10, 64)
	mime := data["mime"]
	if mime == "" {
		mime = data["mimeType"]
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	r.parts = append(r.parts, Part{
		Type:        PartFile,
		ID:          data["id"],
		Filename:    data["filename"],
		MimeType:    mime,
		Size:        size,
		DownloadURL: data["download_url"],
	})
}

// Parts returns the aggregated parts (nil if nothing was applied).
func (r *Reducer) Parts() []Part { return r.parts }

// appendText appends to the trailing part when it is the same text-like kind,
// otherwise starts a new part (matches the frontend appendTo).
func (r *Reducer) appendText(kind, chunk string) {
	if n := len(r.parts); n > 0 && r.parts[n-1].Type == kind {
		r.parts[n-1].Text += chunk
		return
	}
	r.parts = append(r.parts, Part{Type: kind, Text: chunk})
}

// fillToolResult pairs a tool_result back onto its tool_use by id. If unmatched,
// it surfaces the content as text so nothing is silently lost (frontend parity).
func (r *Reducer) fillToolResult(toolUseID, content string, isErr bool) {
	for i := range r.parts {
		if r.parts[i].Type == PartToolCall && r.parts[i].ToolCallID == toolUseID {
			c := content
			r.parts[i].Result = &c
			r.parts[i].IsError = isErr
			return
		}
	}
	if isErr {
		r.appendText(PartText, "\n[tool error] "+content+"\n")
	} else {
		r.appendText(PartText, "\n[tool result] "+content+"\n")
	}
}

func truthy(v string) bool {
	return v == "true" || v == "True" || v == "1"
}

func errText(data map[string]string) string {
	if e := data["error"]; e != "" {
		return e
	}
	return "unknown error"
}
