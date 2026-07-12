import { NextRequest } from "next/server";
import { adminHeaders, isAuthFail, requireAdmin, type SessionUser } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const ADMIN_URL = process.env.COCOLA_ADMIN_URL ?? "http://127.0.0.1:8092";

export async function GET(req: NextRequest, { params }: { params: { path: string[] } }) {
  const authResult = await requireAdmin();
  if (isAuthFail(authResult)) return authResult.response;
  const suffix = params.path.map(encodeURIComponent).join("/");
  return proxy(req, `/admin/conversation-runs/${suffix}`, authResult.user);
}

async function proxy(req: NextRequest, path: string, user: SessionUser) {
  const url = new URL(req.url);
  try {
    const upstream = await fetch(`${ADMIN_URL}${path}${url.search}`, {
      cache: "no-store",
      headers: adminHeaders(user),
    });
    return new Response(await upstream.text(), {
      status: upstream.status,
      headers: { "content-type": upstream.headers.get("content-type") ?? "application/json" },
    });
  } catch (error) {
    return Response.json(
      { error: `admin-api unreachable: ${error instanceof Error ? error.message : String(error)}` },
      { status: 502 },
    );
  }
}
