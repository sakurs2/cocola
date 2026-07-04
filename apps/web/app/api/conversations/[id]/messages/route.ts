import { NextRequest } from "next/server";
import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";

// Same-origin proxy for one conversation's MESSAGES (history). The gateway
// enforces ownership against the verified token, so a caller can only load
// their own conversation; a miss returns 404 which we pass through unchanged.
//
// cache:"no-store" is REQUIRED for the same reason as the list route: Next.js
// 14 persists GET fetch() in its Data Cache by default, which would freeze a
// conversation's history at its first-seen state.

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";

export async function GET(
  req: NextRequest,
  { params }: { params: Promise<{ id: string }> },
) {
  const { id } = await params;
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;
  try {
    const upstream = await fetch(
      `${GATEWAY_URL}/v1/conversations/${encodeURIComponent(id)}/messages`,
      { method: "GET", cache: "no-store", headers: authHeaders },
    );
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
