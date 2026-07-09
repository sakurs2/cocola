import { NextRequest } from "next/server";
import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// The llm-gateway URL is server-side config (COCOLA_LLM_GATEWAY_URL), never
// exposed to the browser. GET /v1/usage is keyed to the verified runtime token,
// so a caller can only ever read their OWN usage; any client-supplied user_id is
// ignored by the gateway when auth is enabled.
const LLM_GATEWAY_URL =
  process.env.COCOLA_LLM_GATEWAY_URL ?? "http://127.0.0.1:8081";

export async function GET(req: NextRequest) {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;
  // Forward only the whitelisted read params; identity comes from the token.
  const src = req.nextUrl.searchParams;
  const params = new URLSearchParams();
  const limit = src.get("limit");
  if (limit) params.set("limit", limit);
  const qs = params.toString();
  try {
    const upstream = await fetch(
      `${LLM_GATEWAY_URL}/v1/usage${qs ? `?${qs}` : ""}`,
      { method: "GET", cache: "no-store", headers: { ...authHeaders } },
    );
    const text = await upstream.text();
    return new Response(text || null, {
      status: upstream.status,
      headers: text
        ? { "content-type": upstream.headers.get("content-type") ?? "application/json" }
        : {},
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return Response.json({ error: `llm-gateway unreachable: ${msg}` }, { status: 502 });
  }
}
