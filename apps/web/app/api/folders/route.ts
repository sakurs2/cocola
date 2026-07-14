import { NextRequest } from "next/server";
import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";

async function proxy(req: NextRequest, method: "GET" | "POST") {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;

  try {
    const upstream = await fetch(`${GATEWAY_URL}/v1/folders`, {
      method,
      cache: "no-store",
      headers: {
        ...(method === "POST"
          ? { "content-type": req.headers.get("content-type") ?? "application/json" }
          : {}),
        ...authHeaders,
      },
      ...(method === "POST" ? { body: await req.text() } : {}),
    });
    const body = await upstream.text();
    return new Response(body || null, {
      status: upstream.status,
      headers: {
        ...(body ? { "content-type": "application/json" } : {}),
        "cache-control": "no-store",
      },
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return Response.json({ error: `gateway unreachable: ${message}` }, { status: 502 });
  }
}

export async function GET(req: NextRequest) {
  return proxy(req, "GET");
}

export async function POST(req: NextRequest) {
  return proxy(req, "POST");
}
