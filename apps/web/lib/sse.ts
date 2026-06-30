// SSE frame parsing for the cocola gateway stream.
//
// Single source of truth shared by the assistant-ui runtime adapter and the
// archived raw debug page. The gateway speaks text/event-stream: frames are
// separated by a blank line, each frame carries `event:` and `data:` lines.
// We only need the JSON in `data:` (it already carries `kind`).

// One decoded agent event. `data` values are always strings on the wire —
// agent-runtime JSON-encodes structured values (`_stringify`), so fields like
// `tool_use.input` are JSON strings the consumer must parse.
export type AgentEvent = { kind: string; data?: Record<string, string> };

const FRAME_SEPARATOR = "\n\n";

// Parse a chunk of the SSE byte stream into complete events plus any trailing
// partial frame (returned as `rest`, to be prepended to the next chunk).
export function parseFrames(buffer: string): { events: AgentEvent[]; rest: string } {
  const events: AgentEvent[] = [];
  const parts = buffer.split(FRAME_SEPARATOR);
  const rest = parts.pop() ?? ""; // last part may be an incomplete frame
  for (const frame of parts) {
    const dataLines = frame
      .split("\n")
      .filter((l) => l.startsWith("data:"))
      .map((l) => l.slice(5).trim());
    if (dataLines.length === 0) continue;
    try {
      events.push(JSON.parse(dataLines.join("\n")) as AgentEvent);
    } catch {
      // ignore keep-alives / malformed frames
    }
  }
  return { events, rest };
}
