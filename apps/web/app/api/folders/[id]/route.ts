import { NextRequest } from "next/server";
import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";

async function proxy(req: NextRequest, id: string, method: "PATCH" | "DELETE"): Promise<Response> {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;

  try {
    const upstream = await fetch(`${GATEWAY_URL}/v1/folders/${encodeURIComponent(id)}`, {
      method,
      cache: "no-store",
      headers: {
        ...(method === "PATCH"
          ? { "content-type": req.headers.get("content-type") ?? "application/json" }
          : {}),
        ...authHeaders,
      },
      ...(method === "PATCH" ? { body: await req.text() } : {}),
    });
    const body = await upstream.text();
    return new Response(body || null, {
      status: upstream.status,
      headers: body ? { "content-type": "application/json" } : {},
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return Response.json({ error: `gateway unreachable: ${message}` }, { status: 502 });
  }
}

export async function PATCH(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return proxy(req, id, "PATCH");
}

export async function DELETE(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return proxy(req, id, "DELETE");
}
