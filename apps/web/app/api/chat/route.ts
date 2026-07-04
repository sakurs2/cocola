import { NextRequest } from "next/server";
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
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;
  const body = await req.text();

  let upstream: Response;
  try {
    upstream = await fetch(`${GATEWAY_URL}/v1/chat`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        ...authHeaders,
      },
      body,
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
      headers: { "content-type": "text/event-stream" },
    });
  }

  // If the gateway rejected the request (e.g. 401/400), it replies JSON, not
  // SSE. Convert that into a single SSE error frame so the page has one code
  // path for everything.
  const ct = upstream.headers.get("content-type") ?? "";
  if (!upstream.ok || !ct.includes("text/event-stream")) {
    const text = await upstream.text();
    const frame = `event: error\ndata: ${JSON.stringify({
      kind: "error",
      data: { error: `gateway ${upstream.status}: ${text}` },
    })}\n\n`;
    return new Response(frame, {
      status: 200,
      headers: { "content-type": "text/event-stream" },
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
    },
  });
}
