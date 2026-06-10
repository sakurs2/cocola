"use client";

// Minimal streaming chat — a TEST TOOL, not the product UI.
//
// Purpose: exercise the real backend path end to end from a browser:
//   page -> /api/chat (same-origin proxy) -> gateway POST /v1/chat (SSE)
//          -> agent-runtime (gRPC) -> llm-gateway / sandbox-manager.
//
// Why fetch + ReadableStream and not EventSource? The gateway speaks SSE but the
// request is a POST with a bearer token and a JSON body; EventSource only does
// GET and cannot set headers. So we POST and parse the text/event-stream by hand.
//
// It deliberately stays ugly and dependency-free: a token box, a prompt box, and
// a raw event log. The real UI is a later milestone.

import { useRef, useState } from "react";

type AgentEvent = { kind: string; data?: Record<string, string> };

// Parse a chunk of an SSE byte stream. SSE frames are separated by a blank line;
// each frame has `event:` and `data:` lines. We only need the JSON in `data:`
// (it already carries `kind`), so we collect data lines per frame and parse.
function parseFrames(buffer: string): { events: AgentEvent[]; rest: string } {
  const events: AgentEvent[] = [];
  const parts = buffer.split("\n\n");
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

export default function Home() {
  const [token, setToken] = useState("");
  const [prompt, setPrompt] = useState("hello from cocola");
  const [sessionId, setSessionId] = useState("s1");
  const [events, setEvents] = useState<AgentEvent[]>([]);
  const [running, setRunning] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  async function send() {
    if (running) return;
    setEvents([]);
    setRunning(true);
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    try {
      const res = await fetch("/api/chat", {
        method: "POST",
        headers: {
          "content-type": "application/json",
          ...(token ? { authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify({ prompt, session_id: sessionId }),
        signal: ctrl.signal,
      });
      if (!res.body) throw new Error("no response body");

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const { events: parsed, rest } = parseFrames(buffer);
        buffer = rest;
        if (parsed.length) setEvents((prev) => [...prev, ...parsed]);
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setEvents((prev) => [...prev, { kind: "error", data: { error: msg } }]);
    } finally {
      setRunning(false);
      abortRef.current = null;
    }
  }

  function stop() {
    abortRef.current?.abort();
  }

  return (
    <main className="mx-auto flex min-h-screen max-w-3xl flex-col gap-4 px-6 py-10">
      <header>
        <h1 className="text-2xl font-semibold">cocola — chat test tool</h1>
        <p className="text-sm text-neutral-500">
          Dev-only client for the gateway SSE path. Not the product UI.
        </p>
      </header>

      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
        <label className="flex flex-col text-sm">
          <span className="text-neutral-600">Bearer token</span>
          <input
            className="rounded border border-neutral-300 px-2 py-1 font-mono text-xs"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="cocola-issued token (blank if auth disabled)"
          />
        </label>
        <label className="flex flex-col text-sm">
          <span className="text-neutral-600">Session ID</span>
          <input
            className="rounded border border-neutral-300 px-2 py-1 font-mono text-xs"
            value={sessionId}
            onChange={(e) => setSessionId(e.target.value)}
          />
        </label>
      </div>

      <label className="flex flex-col text-sm">
        <span className="text-neutral-600">Prompt</span>
        <textarea
          className="min-h-20 rounded border border-neutral-300 px-2 py-1"
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
        />
      </label>

      <div className="flex gap-2">
        <button
          className="rounded bg-neutral-900 px-4 py-1.5 text-sm text-white disabled:opacity-50"
          onClick={send}
          disabled={running}
        >
          {running ? "Streaming…" : "Send"}
        </button>
        <button
          className="rounded border border-neutral-300 px-4 py-1.5 text-sm disabled:opacity-50"
          onClick={stop}
          disabled={!running}
        >
          Stop
        </button>
      </div>

      <section className="flex flex-col gap-2">
        <h2 className="text-sm font-medium text-neutral-600">Event stream</h2>
        {events.length === 0 ? (
          <p className="text-sm text-neutral-400">No events yet.</p>
        ) : (
          <ol className="flex flex-col gap-1">
            {events.map((ev, i) => (
              <li key={i} className="rounded border border-neutral-200 bg-white px-3 py-2 text-sm">
                <span className="mr-2 rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-xs text-neutral-700">
                  {ev.kind}
                </span>
                <EventBody ev={ev} />
              </li>
            ))}
          </ol>
        )}
      </section>
    </main>
  );
}

// Render the most useful field per event kind; fall back to raw JSON so unknown
// kinds are still observable (the whole point of a test tool).
function EventBody({ ev }: { ev: AgentEvent }) {
  const d = ev.data ?? {};
  if (ev.kind === "text") return <span>{d.text}</span>;
  if (ev.kind === "thinking") return <span className="text-neutral-500 italic">{d.thinking}</span>;
  if (ev.kind === "error") return <span className="text-red-600">{d.error}</span>;
  if (ev.kind === "sandbox")
    return (
      <span className="text-neutral-600 font-mono text-xs">
        {d.sandbox_id} ({d.endpoint}) reused={d.reused}
      </span>
    );
  if (Object.keys(d).length === 0) return null;
  return <span className="font-mono text-xs text-neutral-600">{JSON.stringify(d)}</span>;
}
