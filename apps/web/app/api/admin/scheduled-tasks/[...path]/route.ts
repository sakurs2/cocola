import { NextRequest } from "next/server";
import { adminHeaders, isAuthFail, requireAdmin, type SessionUser } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const ADMIN_URL = process.env.COCOLA_ADMIN_URL ?? "http://127.0.0.1:8092";

export async function GET(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  const authResult = await requireAdmin();
  if (isAuthFail(authResult)) return authResult.response;
  return proxyAdmin(req, await adminPath(params), authResult.user);
}

export async function DELETE(
  req: NextRequest,
  { params }: { params: Promise<{ path: string[] }> },
) {
  const authResult = await requireAdmin();
  if (isAuthFail(authResult)) return authResult.response;
  return proxyAdmin(req, await adminPath(params), authResult.user);
}

async function adminPath(params: Promise<{ path: string[] }>) {
  const { path } = await params;
  return `/admin/scheduled-tasks/${path.map(encodeURIComponent).join("/")}`;
}

async function proxyAdmin(
  req: NextRequest,
  path: string,
  user: SessionUser,
  init?: RequestInit & { contentType?: string },
) {
  try {
    const upstream = await fetch(`${ADMIN_URL}${path}`, {
      method: init?.method ?? req.method,
      cache: "no-store",
      headers: adminHeaders(user, init?.contentType),
      body: init?.body,
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
