import { NextRequest } from "next/server";
import { adminHeaders, isAuthFail, requireAdmin, type SessionUser } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const ADMIN_URL = process.env.COCOLA_ADMIN_URL ?? "http://127.0.0.1:8092";

export async function GET(req: NextRequest) {
  const authResult = await requireAdmin();
  if (isAuthFail(authResult)) return authResult.response;
  return proxyAdmin(req, "/admin/model-providers", authResult.user);
}

export async function POST(req: NextRequest) {
  const authResult = await requireAdmin();
  if (isAuthFail(authResult)) return authResult.response;
  return proxyAdmin(req, "/admin/model-providers", authResult.user, {
    method: "POST",
    body: await req.text(),
    contentType: req.headers.get("content-type") ?? "application/json",
  });
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
