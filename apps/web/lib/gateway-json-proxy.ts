import { NextRequest } from "next/server";
import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";

export async function gatewayJSONProxy(
  req: NextRequest,
  path: string,
  method: "GET" | "POST" | "PATCH" | "DELETE",
): Promise<Response> {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;
  const hasBody = method === "POST" || method === "PATCH" || method === "DELETE";
  try {
    const upstream = await fetch(`${GATEWAY_URL}${path}`, {
      method,
      cache: "no-store",
      headers: {
        ...(hasBody
          ? { "content-type": req.headers.get("content-type") ?? "application/json" }
          : {}),
        ...authHeaders,
        ...(req.headers.get("origin") ? { origin: req.headers.get("origin")! } : {}),
      },
      ...(hasBody ? { body: await req.text() } : {}),
      signal: req.signal,
    });
    const body = await upstream.text();
    return new Response(body || null, {
      status: upstream.status,
      headers: {
        ...(body
          ? { "content-type": upstream.headers.get("content-type") ?? "application/json" }
          : {}),
        "cache-control": "no-store",
      },
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return Response.json(
      { error: { code: "GATEWAY_UNAVAILABLE", message: `gateway unreachable: ${message}` } },
      { status: 502 },
    );
  }
}
