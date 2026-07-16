import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";
import { type NextRequest } from "next/server";

const ADMIN_URL = process.env.COCOLA_ADMIN_URL ?? "http://127.0.0.1:8092";

export async function proxyWorkspace(req: NextRequest, sessionID: string, endpoint: string) {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;

  const upstreamPath = `/me/workspaces/${encodeURIComponent(sessionID)}/${endpoint}${req.nextUrl.search}`;
  try {
    const upstream = await fetch(`${ADMIN_URL}${upstreamPath}`, {
      method: "GET",
      cache: "no-store",
      headers: authHeaders,
    });
    const headers = new Headers();
    for (const name of [
      "content-type",
      "cache-control",
      "content-disposition",
      "content-security-policy",
      "x-content-type-options",
    ]) {
      const value = upstream.headers.get(name);
      if (value) headers.set(name, value);
    }
    return new Response(upstream.body, { status: upstream.status, headers });
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    return Response.json({ error: `admin-api unreachable: ${message}` }, { status: 502 });
  }
}
