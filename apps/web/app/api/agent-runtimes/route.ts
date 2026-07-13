import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";

export async function GET() {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const headers = await runtimeAuthHeaders(authResult.user);
  if (headers instanceof Response) return headers;
  try {
    const upstream = await fetch(`${GATEWAY_URL}/v1/agent-runtimes`, {
      cache: "no-store",
      headers,
    });
    return new Response(upstream.body, {
      status: upstream.status,
      headers: { "content-type": "application/json", "cache-control": "no-store" },
    });
  } catch {
    return Response.json([], { status: 502 });
  }
}
