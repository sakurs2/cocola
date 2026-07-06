import { NextRequest } from "next/server";
import { adminHeaders, isAuthFail, requireAdmin, type SessionUser } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const ADMIN_URL =
  process.env.COCOLA_ADMIN_URL ?? process.env.COCOLA_ADMIN_BASE_URL ?? "http://127.0.0.1:8092";

export async function GET(req: NextRequest, { params }: { params: { traceId: string } }) {
  const authResult = await requireAdmin();
  if (isAuthFail(authResult)) return authResult.response;
  return proxyAdmin(req, `/admin/traces/${encodeURIComponent(params.traceId)}`, authResult.user);
}

async function proxyAdmin(req: NextRequest, path: string, user: SessionUser) {
  const url = new URL(req.url);
  try {
    const upstream = await fetch(`${ADMIN_URL}${path}${url.search}`, {
      method: req.method,
      cache: "no-store",
      headers: adminHeaders(user),
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
