import { NextRequest } from "next/server";
import { adminHeaders, isAuthFail, requireAdmin } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const ADMIN_URL =
  process.env.COCOLA_ADMIN_URL ?? process.env.COCOLA_ADMIN_BASE_URL ?? "http://127.0.0.1:8092";

export async function GET(req: NextRequest) {
  const authResult = await requireAdmin();
  if (isAuthFail(authResult)) return authResult.response;

  try {
    const upstream = await fetch(`${ADMIN_URL}/admin/token-usage/export${req.nextUrl.search}`, {
      method: "GET",
      cache: "no-store",
      headers: adminHeaders(authResult.user),
    });
    const body = await upstream.arrayBuffer();
    const contentType =
      upstream.headers.get("content-type") ??
      (upstream.ok
        ? "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
        : "application/json");
    const headers: Record<string, string> = { "content-type": contentType };
    const disposition = upstream.headers.get("content-disposition");
    if (disposition) headers["content-disposition"] = disposition;
    return new Response(body, {
      status: upstream.status,
      headers,
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return Response.json({ error: `admin-api unreachable: ${msg}` }, { status: 502 });
  }
}
