import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const ADMIN_URL =
  process.env.COCOLA_ADMIN_URL ?? process.env.COCOLA_ADMIN_BASE_URL ?? "http://127.0.0.1:8092";

export async function GET() {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;

  let upstream: Response;
  try {
    upstream = await fetch(`${ADMIN_URL}/me/events`, {
      method: "GET",
      cache: "no-store",
      headers: authHeaders,
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    const frame = `event: error\ndata: ${JSON.stringify({
      type: "connection.error",
      data: { error: `admin-api unreachable: ${msg}` },
    })}\n\n`;
    return new Response(frame, {
      status: 200,
      headers: { "content-type": "text/event-stream" },
    });
  }

  const ct = upstream.headers.get("content-type") ?? "";
  if (!upstream.ok || !ct.includes("text/event-stream")) {
    const text = await upstream.text();
    const frame = `event: error\ndata: ${JSON.stringify({
      type: "connection.error",
      data: { error: `admin-api ${upstream.status}: ${text}` },
    })}\n\n`;
    return new Response(frame, {
      status: 200,
      headers: { "content-type": "text/event-stream" },
    });
  }

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
