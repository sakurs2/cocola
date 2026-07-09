import { NextRequest } from "next/server";
import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// The llm-gateway URL is server-side config (COCOLA_LLM_GATEWAY_URL), never
// exposed to the browser. GET /v1/quota returns the caller's own standings
// (daily user scope + monthly tenant scope), keyed to the verified runtime
// token.
const LLM_GATEWAY_URL =
  process.env.COCOLA_LLM_GATEWAY_URL ?? "http://127.0.0.1:8081";

export async function GET(_req: NextRequest) {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;
  try {
    const upstream = await fetch(`${LLM_GATEWAY_URL}/v1/quota`, {
      method: "GET",
      cache: "no-store",
      headers: { ...authHeaders },
    });
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
