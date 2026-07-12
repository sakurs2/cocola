import { type NextRequest } from "next/server";
import { adminHeaders, isAuthFail, requireAdmin } from "@/lib/server-auth";

const ADMIN_URL = process.env.COCOLA_ADMIN_URL ?? "http://127.0.0.1:8092";

/** Proxy one authenticated Web admin request to the private Admin API. */
export async function proxyAdmin(req: NextRequest, path: string): Promise<Response> {
  if (!path.startsWith("/admin/")) {
    throw new Error(`invalid admin API path: ${path}`);
  }
  const authResult = await requireAdmin();
  if (isAuthFail(authResult)) return authResult.response;

  const method = req.method.toUpperCase();
  const body =
    method === "GET" || method === "HEAD" || req.body === null
      ? undefined
      : await req.arrayBuffer();
  const contentType = body ? (req.headers.get("content-type") ?? undefined) : undefined;

  try {
    const upstream = await fetch(`${ADMIN_URL}${path}${req.nextUrl.search}`, {
      method,
      cache: "no-store",
      headers: adminHeaders(authResult.user, contentType),
      body,
    });
    const responseBody = await upstream.arrayBuffer();
    const headers = new Headers();
    const upstreamContentType = upstream.headers.get("content-type");
    const disposition = upstream.headers.get("content-disposition");
    if (upstreamContentType) headers.set("content-type", upstreamContentType);
    if (disposition) headers.set("content-disposition", disposition);
    return new Response(responseBody.byteLength ? responseBody : null, {
      status: upstream.status,
      headers,
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return Response.json({ error: `admin-api unreachable: ${message}` }, { status: 502 });
  }
}
