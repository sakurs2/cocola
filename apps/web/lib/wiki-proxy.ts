import { type NextRequest } from "next/server";
import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";
import { wikiProxyRequestInit, type WikiProxyMethod } from "@/lib/wiki-proxy-request";

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";

const RESPONSE_HEADERS = [
  "content-type",
  "content-length",
  "content-disposition",
  "etag",
  "cache-control",
  "x-content-type-options",
] as const;

export async function proxyWiki(
  req: NextRequest,
  path: string,
  method: WikiProxyMethod,
): Promise<Response> {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;
  const query = new URL(req.url).search;
  const headers = new Headers(authHeaders);
  for (const key of ["content-type", "if-match"]) {
    const value = req.headers.get(key);
    if (value) headers.set(key, value);
  }
  try {
    const upstream = await fetch(
      `${GATEWAY_URL}${path}${query}`,
      wikiProxyRequestInit(req, method, headers),
    );
    const responseHeaders = new Headers();
    for (const key of RESPONSE_HEADERS) {
      const value = upstream.headers.get(key);
      if (value) responseHeaders.set(key, value);
    }
    if (!responseHeaders.has("cache-control")) {
      responseHeaders.set("cache-control", "private, no-store");
    }
    return new Response(upstream.body, {
      status: upstream.status,
      headers: responseHeaders,
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return Response.json(
      { error: { code: "GATEWAY_UNAVAILABLE", message: `gateway unreachable: ${message}` } },
      { status: 502 },
    );
  }
}
