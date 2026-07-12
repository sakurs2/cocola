import { NextRequest } from "next/server";
import { adminHeaders, isAuthFail, requireAdmin } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const ADMIN_URL = process.env.COCOLA_ADMIN_URL ?? "http://127.0.0.1:8092";

export async function POST(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const authResult = await requireAdmin();
  if (isAuthFail(authResult)) return authResult.response;
  const { id } = await params;
  try {
    const upstream = await fetch(`${ADMIN_URL}/admin/users/${encodeURIComponent(id)}/password`, {
      method: "POST",
      cache: "no-store",
      headers: adminHeaders(authResult.user, req.headers.get("content-type") ?? "application/json"),
      body: await req.text(),
    });
    const text = await upstream.text();
    return new Response(text || null, {
      status: upstream.status,
      headers: text
        ? { "content-type": upstream.headers.get("content-type") ?? "application/json" }
        : {},
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return Response.json({ error: `admin-api unreachable: ${msg}` }, { status: 502 });
  }
}
