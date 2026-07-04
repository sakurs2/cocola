import { NextRequest } from "next/server";
import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";

// Same-origin proxy for the conversation LIST: browser -> this route -> gateway.
// Mirrors api/chat/route.ts (the gateway sets no CORS and owns token
// verification), but this is plain JSON, not SSE. The browser holds only an
// Auth.js session; this route forwards a short-lived runtime token.
//
// cache:"no-store" is REQUIRED: Next.js 14 persists GET fetch() responses in its
// Data Cache by default, and `export const dynamic` only governs route render
// caching -- NOT the inner fetch. Without this, an early empty-list response
// ([]) gets cached and the sidebar stays empty forever even after rows exist.

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";

export async function GET(req: NextRequest) {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;
  try {
    const upstream = await fetch(`${GATEWAY_URL}/v1/conversations`, {
      method: "GET",
      cache: "no-store",
      headers: authHeaders,
    });
    const body = await upstream.text();
    return new Response(body, {
      status: upstream.status,
      headers: { "content-type": "application/json" },
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return new Response(JSON.stringify({ error: `gateway unreachable: ${msg}` }), {
      status: 502,
      headers: { "content-type": "application/json" },
    });
  }
}
