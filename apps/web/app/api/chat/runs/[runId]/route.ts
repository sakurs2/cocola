import { NextRequest } from "next/server";
import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";

async function authenticatedHeaders() {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  return runtimeAuthHeaders(authResult.user);
}

export async function GET(req: NextRequest, { params }: { params: Promise<{ runId: string }> }) {
  const headers = await authenticatedHeaders();
  if (headers instanceof Response) return headers;
  const { runId } = await params;
  let upstream: Response;
  try {
    upstream = await fetch(`${GATEWAY_URL}/v1/chat/runs/${encodeURIComponent(runId)}`, {
      method: "GET",
      cache: "no-store",
      headers,
      signal: req.signal,
    });
  } catch (error) {
    return Response.json(
      { error: error instanceof Error ? error.message : String(error) },
      { status: 502 },
    );
  }
  if (!upstream.ok) {
    return new Response(await upstream.text(), {
      status: upstream.status,
      headers: { "content-type": upstream.headers.get("content-type") ?? "application/json" },
    });
  }
  return new Response(upstream.body, {
    status: 200,
    headers: {
      "content-type": "text/event-stream",
      "cache-control": "no-cache, no-transform",
      connection: "keep-alive",
      "x-accel-buffering": "no",
      "x-cocola-run-id": runId,
    },
  });
}

export async function DELETE(
  _req: NextRequest,
  { params }: { params: Promise<{ runId: string }> },
) {
  const headers = await authenticatedHeaders();
  if (headers instanceof Response) return headers;
  const { runId } = await params;
  try {
    const upstream = await fetch(`${GATEWAY_URL}/v1/chat/runs/${encodeURIComponent(runId)}`, {
      method: "DELETE",
      cache: "no-store",
      headers,
    });
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
