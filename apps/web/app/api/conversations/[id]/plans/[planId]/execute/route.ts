import { randomBytes } from "node:crypto";
import { NextRequest } from "next/server";
import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";

export async function POST(
  req: NextRequest,
  { params }: { params: Promise<{ id: string; planId: string }> },
) {
  const { id, planId } = await params;
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;
  const traceparent = `00-${randomBytes(16).toString("hex")}-${randomBytes(8).toString("hex")}-01`;
  try {
    const upstream = await fetch(
      `${GATEWAY_URL}/v1/conversations/${encodeURIComponent(id)}/plans/${encodeURIComponent(planId)}/execute`,
      {
        method: "POST",
        headers: {
          "content-type": "application/json",
          traceparent,
          "x-cocola-chat-started-at-ms": String(Date.now()),
          ...authHeaders,
        },
        body: await req.text(),
        signal: req.signal,
      },
    );
    const contentType = upstream.headers.get("content-type") ?? "application/json";
    return new Response(upstream.body, {
      status: upstream.status,
      headers: {
        "content-type": contentType,
        "cache-control": contentType.includes("text/event-stream")
          ? "no-cache, no-transform"
          : "no-store",
        ...(contentType.includes("text/event-stream")
          ? { connection: "keep-alive", "x-accel-buffering": "no" }
          : {}),
        ...(upstream.headers.get("x-cocola-run-id")
          ? { "x-cocola-run-id": upstream.headers.get("x-cocola-run-id")! }
          : {}),
      },
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return Response.json(
      {
        error: {
          code: "GATEWAY_UNAVAILABLE",
          message: `gateway unreachable: ${message}`,
        },
      },
      { status: 502, headers: { "cache-control": "no-store" } },
    );
  }
}
