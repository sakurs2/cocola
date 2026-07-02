package convo

// Reducer aggregates the agent's SSE event stream into an assistant message's
// Part slice, mirroring the frontend reducer in apps/web/app/runtime-provider.tsx
// (reducePart/appendTo/fillToolResult). Persisting the SAME shape the browser
// renders is what makes route A a zero-drift mirror: a stored message replays
// straight through convertMessage.
//
// Event vocabulary (kind): text | thinking | tool_use | tool_result | error;
// result / system / sandbox / done carry no message-body content and are
// dropped (identical to the frontend). Unknown kinds are ignored.
type Reducer struct {
	parts []Part
}

// NewReducer returns an empty aggregator.
func NewReducer() *Reducer { return &Reducer{} }

// Apply folds one event (kind + its data map) into the accumulating parts.
func (r *Reducer) Apply(kind string, data map[string]string) {
	switch kind {
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
	case "error":
		r.appendText(PartText, "\n\n⚠️ "+errText(data))
	default:
		// result / system / sandbox / done / unknown: no body content.
	}
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
