import { NextRequest } from "next/server";
import { randomBytes } from "node:crypto";
import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";

// Same-origin SSE proxy: browser -> this route -> cocola gateway.
//
// Why a proxy at all? The gateway (apps/gateway) sets NO CORS headers and serves
// SSE at POST /v1/chat. A browser cannot call it cross-origin, and EventSource
// cannot POST. So the page POSTs here (same origin), and we forward the request
// to the gateway and stream its response body straight back, unbuffered. This
// keeps the browser decoupled from the gateway's address and credentials.
//
// The gateway URL is server-side config (COCOLA_GATEWAY_URL), never exposed to
// the browser. The browser holds only an Auth.js session; this route gets a
// short-lived cocola runtime token from admin-api and forwards it to gateway.

export const runtime = "nodejs";
// Never cache or statically optimize a streaming proxy.
export const dynamic = "force-dynamic";

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";

export async function POST(req: NextRequest) {
  const requestStartedAt = Date.now();
  const authStartedAt = Date.now();
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authDuration = Date.now() - authStartedAt;
  const tokenStartedAt = Date.now();
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;
  const tokenDuration = Date.now() - tokenStartedAt;
  const body = await req.text();
  const traceparent = `00-${randomBytes(16).toString("hex")}-${randomBytes(8).toString("hex")}-01`;

  let upstream: Response;
  try {
    upstream = await fetch(`${GATEWAY_URL}/v1/chat`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        traceparent,
        "x-cocola-chat-started-at-ms": String(requestStartedAt),
        "x-cocola-session-auth-ms": String(authDuration),
        "x-cocola-runtime-token-ms": String(tokenDuration),
        ...authHeaders,
      },
      body,
      signal: req.signal,
      // Stream the response instead of buffering it.
      // @ts-expect-error - duplex is required by Node fetch for streaming bodies
      duplex: "half",
    });
  } catch (err) {
    // Gateway unreachable: surface a single SSE error frame so the page can
    // render it the same way it renders an in-band error event.
    const msg = err instanceof Error ? err.message : String(err);
    const frame = `event: error\ndata: ${JSON.stringify({
      kind: "error",
      data: { error: `gateway unreachable: ${msg}` },
    })}\n\n`;
    return new Response(frame, {
      status: 200,
      headers: {
        "content-type": "text/event-stream",
        "x-cocola-upstream-status": "502",
      },
    });
  }

  // If the gateway rejected the request (e.g. 401/400), it replies JSON, not
  // SSE. Convert that into a single SSE error frame so the page has one code
  // path for everything.
  const ct = upstream.headers.get("content-type") ?? "";
  if (!upstream.ok || !ct.includes("text/event-stream")) {
    const text = await upstream.text();
    let runId = upstream.headers.get("x-cocola-run-id") ?? "";
    if (!runId && upstream.status === 409) {
      try {
        const conflict = JSON.parse(text) as { run_id?: string };
        runId = conflict.run_id ?? "";
      } catch {
        // Keep the original upstream diagnostic below.
      }
    }
    const frame = `event: error\ndata: ${JSON.stringify({
      kind: "error",
      data: { error: `gateway ${upstream.status}: ${text}` },
    })}\n\n`;
    return new Response(frame, {
      status: 200,
      headers: {
        "content-type": "text/event-stream",
        "x-cocola-upstream-status": String(upstream.status),
        ...(runId ? { "x-cocola-run-id": runId } : {}),
      },
    });
  }

  // Happy path: pipe the gateway's SSE body straight through.
  return new Response(upstream.body, {
    status: 200,
    headers: {
      "content-type": "text/event-stream",
      "cache-control": "no-cache, no-transform",
      connection: "keep-alive",
      "x-accel-buffering": "no",
      ...(upstream.headers.get("x-cocola-run-id")
        ? { "x-cocola-run-id": upstream.headers.get("x-cocola-run-id")! }
        : {}),
    },
  });
}
