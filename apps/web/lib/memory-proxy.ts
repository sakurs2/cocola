import { type NextRequest } from "next/server";
import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";

export async function proxyMemory(
  req: NextRequest,
  path: string,
  method: "GET" | "PATCH" | "DELETE",
) {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;

  const query = req.nextUrl.search;
  try {
    const upstream = await fetch(`${GATEWAY_URL}${path}${query}`, {
      method,
      cache: "no-store",
      headers: {
        ...authHeaders,
        ...(method === "PATCH" ? { "content-type": "application/json" } : {}),
      },
      ...(method === "PATCH" ? { body: await req.text() } : {}),
    });
    const body = await upstream.text();
    return new Response(body || null, {
      status: upstream.status,
      headers: {
        ...(body ? { "content-type": "application/json" } : {}),
        "cache-control": "no-store",
      },
    });
  } catch (cause) {
    const message = cause instanceof Error ? cause.message : String(cause);
    return Response.json({ error: `gateway unreachable: ${message}` }, { status: 502 });
  }
}
