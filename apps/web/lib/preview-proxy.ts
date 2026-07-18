import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";
import { type NextRequest } from "next/server";

// Same-origin reverse-proxy for cocola's Preview Proxy.
//
//   browser  ->  /api/preview/{id}/{port}/{...rest}  (this route, same origin)
//            ->  {GATEWAY_URL}/v1/preview/{id}/{port}/{...rest}
//            ->  sandbox-manager resolves the in-sandbox port
//            ->  the user's dev server (Vite/Next/etc.)
//
// Why a same-origin hop? The gateway sets no CORS headers and requires a
// verified cocola runtime token; the browser only holds an Auth.js session. So
// the iframe points here, we mint a short-lived runtime token from admin-api
// (via runtimeAuthHeaders) and forward the request to the gateway, streaming the
// response body straight back. This mirrors the chat/workspace proxies and
// keeps the gateway address + credentials server-side only.

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";

// Hop-by-hop and identity headers we must not forward upstream: the runtime
// token replaces the browser's auth, and the gateway/dev-server set their own
// connection + length framing.
const STRIP_REQUEST_HEADERS = new Set([
  "authorization",
  "cookie",
  "host",
  "connection",
  "content-length",
  "transfer-encoding",
  "x-forwarded-host",
]);

const STRIP_RESPONSE_HEADERS = new Set([
  "content-encoding",
  "content-length",
  "transfer-encoding",
  "connection",
  "keep-alive",
]);

export async function proxyPreview(
  req: NextRequest,
  sessionID: string,
  port: string,
  rest: string[],
) {
  const portNum = Number(port);
  if (!Number.isInteger(portNum) || portNum <= 0 || portNum > 65535) {
    return Response.json({ error: "port must be in 1..65535" }, { status: 400 });
  }

  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;

  const suffix = rest.map((seg) => encodeURIComponent(seg)).join("/");
  const upstreamPath =
    `/v1/preview/${encodeURIComponent(sessionID)}/${portNum}/${suffix}${req.nextUrl.search}`;

  const headers = new Headers();
  req.headers.forEach((value, name) => {
    if (!STRIP_REQUEST_HEADERS.has(name.toLowerCase())) headers.set(name, value);
  });
  for (const [name, value] of Object.entries(authHeaders as Record<string, string>)) {
    headers.set(name, value);
  }

  const method = req.method.toUpperCase();
  const hasBody = method !== "GET" && method !== "HEAD";

  let upstream: Response;
  try {
    upstream = await fetch(`${GATEWAY_URL}${upstreamPath}`, {
      method,
      headers,
      cache: "no-store",
      body: hasBody ? req.body : undefined,
      signal: req.signal,
      // @ts-expect-error - duplex is required by Node fetch for streaming bodies
      duplex: hasBody ? "half" : undefined,
      redirect: "manual",
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return Response.json({ error: `gateway unreachable: ${msg}` }, { status: 502 });
  }

  const outHeaders = new Headers();
  upstream.headers.forEach((value, name) => {
    if (!STRIP_RESPONSE_HEADERS.has(name.toLowerCase())) outHeaders.set(name, value);
  });
  outHeaders.set("cache-control", "no-store");

  return new Response(upstream.body, { status: upstream.status, headers: outHeaders });
}
