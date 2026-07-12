import { NextRequest } from "next/server";
import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";

export async function GET(req: NextRequest) {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const headers = await runtimeAuthHeaders(authResult.user);
  if (headers instanceof Response) return headers;
  const conversationId = req.nextUrl.searchParams.get("conversation_id") ?? "";
  try {
    const upstream = await fetch(
      `${GATEWAY_URL}/v1/chat/runs/active?conversation_id=${encodeURIComponent(conversationId)}`,
      { method: "GET", cache: "no-store", headers },
    );
    return new Response(await upstream.text(), {
      status: upstream.status,
      headers: { "content-type": upstream.headers.get("content-type") ?? "application/json" },
    });
  } catch (error) {
    return Response.json(
      { error: error instanceof Error ? error.message : String(error) },
      { status: 502 },
    );
  }
}
