import { NextRequest } from "next/server";
import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";

export async function GET(
  req: NextRequest,
  { params }: { params: { id: string; artifactId: string } },
) {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;
  const id = encodeURIComponent(params.id);
  const artifactId = encodeURIComponent(params.artifactId);
  try {
    const upstream = await fetch(`${GATEWAY_URL}/v1/conversations/${id}/artifacts/${artifactId}`, {
      method: "GET",
      cache: "no-store",
      headers: authHeaders,
    });
    const headers = new Headers();
    for (const key of [
      "content-type",
      "content-length",
      "content-disposition",
      "content-security-policy",
      "x-content-type-options",
      "cross-origin-resource-policy",
      "cache-control",
    ]) {
      const value = upstream.headers.get(key);
      if (value) headers.set(key, value);
    }
    if (!headers.has("cache-control")) headers.set("cache-control", "private, no-store");
    return new Response(upstream.body, {
      status: upstream.status,
      headers,
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return new Response(JSON.stringify({ error: `gateway unreachable: ${msg}` }), {
      status: 502,
      headers: { "content-type": "application/json" },
    });
  }
}
