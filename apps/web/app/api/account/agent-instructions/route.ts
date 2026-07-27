import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";
import { wikiProxyRequestInit } from "@/lib/wiki-proxy-request";

const ADMIN_URL = process.env.COCOLA_ADMIN_URL ?? "http://127.0.0.1:8092";

async function proxy(req: Request, method: "GET" | "PUT"): Promise<Response> {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;

  const headers = new Headers(authHeaders);
  const contentType = req.headers.get("content-type");
  if (contentType) headers.set("content-type", contentType);

  try {
    const upstream = await fetch(
      `${ADMIN_URL}/me/agent-instructions`,
      wikiProxyRequestInit(req, method, headers),
    );
    return new Response(upstream.body, {
      status: upstream.status,
      headers: {
        "cache-control": "private, no-store",
        "content-type": upstream.headers.get("content-type") ?? "application/json",
      },
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return Response.json(
      {
        error: {
          code: "ADMIN_API_UNAVAILABLE",
          message: `admin-api unreachable: ${message}`,
        },
      },
      { status: 502 },
    );
  }
}

export async function GET(req: Request) {
  return proxy(req, "GET");
}

export async function PUT(req: Request) {
  return proxy(req, "PUT");
}
