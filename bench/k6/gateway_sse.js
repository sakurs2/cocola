// k6 load test: gateway SSE chat endpoint (POST /v1/chat).
//
// The gateway streams Server-Sent Events; k6 has no native SSE client, so we
// use the http module with `responseType: "text"` and read the whole stream to
// completion (the agent emits a terminal `done`/`error` event then closes).
// This measures end-to-end streaming latency: auth -> agent gRPC -> sandbox/LLM
// -> first byte -> full stream. We record a custom trend for time-to-first-byte
// where the runtime exposes it (http_req_waiting is a good proxy for TTFB on a
// streamed body that flushes headers immediately).
//
// Usage (services up via `cocola up`; provide TOKEN when auth is enabled):
//
//   k6 run -e BASE_URL=http://localhost:8080 bench/k6/gateway_sse.js
//
// Knobs (all via -e ENV=value):
//   BASE_URL   gateway base URL            (default http://localhost:8080)
//   TOKEN      bearer token; omit when auth is OFF
//   VUS        peak virtual users          (default 20)
//   DURATION   steady-state duration       (default 30s)
//   PROMPT     prompt text                 (default "ping")
//   MAX_TURNS  agent max turns             (default 1)
//
// CI smoke (1 VU, 5s, fail fast):
//   k6 run -e VUS=1 -e DURATION=5s bench/k6/gateway_sse.js

import http from "k6/http";
import { check } from "k6";
import { Trend, Rate, Counter } from "k6/metrics";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";
const TOKEN = __ENV.TOKEN || "";
const VUS = parseInt(__ENV.VUS || "20", 10);
const DURATION = __ENV.DURATION || "30s";
const PROMPT = __ENV.PROMPT || "ping";
const MAX_TURNS = parseInt(__ENV.MAX_TURNS || "1", 10);

// Custom metrics on top of k6 built-ins.
const ttfb = new Trend("sse_ttfb_ms", true); // time to first byte (header flush)
const streamDur = new Trend("sse_stream_ms", true); // full stream duration
const sseEvents = new Counter("sse_events_total"); // parsed event frames
const sseErrors = new Rate("sse_error_rate"); // streams that ended in error

export const options = {
  scenarios: {
    chat: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "10s", target: VUS }, // ramp up
        { duration: DURATION, target: VUS }, // steady state
        { duration: "5s", target: 0 }, // ramp down
      ],
      gracefulStop: "10s",
    },
  },
  thresholds: {
    // Capacity guardrails; tune against the baseline in bench/README.md.
    http_req_failed: ["rate<0.01"], // <1% transport-level failures
    sse_error_rate: ["rate<0.01"], // <1% in-band error events
    sse_ttfb_ms: ["p(95)<2000"], // p95 first byte under 2s
  },
};

function headers() {
  const h = { "Content-Type": "application/json", Accept: "text/event-stream" };
  if (TOKEN) h["Authorization"] = `Bearer ${TOKEN}`;
  return h;
}

export default function () {
  const body = JSON.stringify({
    prompt: PROMPT,
    session_id: `k6-${__VU}-${__ITER}`,
    max_turns: MAX_TURNS,
  });

  const res = http.post(`${BASE_URL}/v1/chat`, body, {
    headers: headers(),
    responseType: "text",
    timeout: "60s",
  });

  // Headers flush immediately on the gateway (it writes 200 + flushes before
  // streaming), so waiting time approximates time-to-first-byte.
  ttfb.add(res.timings.waiting);
  streamDur.add(res.timings.duration);

  const ok = check(res, {
    "status is 200": (r) => r.status === 200,
    "is event-stream": (r) => (r.headers["Content-Type"] || "").includes("text/event-stream"),
  });

  // Parse SSE frames to count events and detect in-band errors. The body holds
  // the whole stream because we read to completion.
  let sawError = false;
  if (res.body) {
    const lines = res.body.split("\n");
    for (const line of lines) {
      if (line.startsWith("event:")) {
        sseEvents.add(1);
        if (line.slice(6).trim() === "error") sawError = true;
      }
    }
  }
  sseErrors.add(sawError || !ok);
}
