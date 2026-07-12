import { NextRequest } from "next/server";
import { isAuthFail, requireUser, runtimeAuthHeaders } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const ADMIN_URL = process.env.COCOLA_ADMIN_URL ?? "http://127.0.0.1:8092";

export async function GET(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  return proxyMe(req, `${await mePath(params)}${req.nextUrl.search}`);
}

export async function POST(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  return proxyMe(req, await mePath(params), {
    method: "POST",
    body: await req.arrayBuffer(),
    contentType: req.headers.get("content-type") ?? "application/octet-stream",
  });
}

export async function DELETE(
  req: NextRequest,
  { params }: { params: Promise<{ path: string[] }> },
) {
  return proxyMe(req, await mePath(params));
}

async function mePath(params: Promise<{ path: string[] }>) {
  const { path } = await params;
  return `/me/skills/${path.map(encodeURIComponent).join("/")}`;
}

async function proxyMe(
  req: NextRequest,
  path: string,
  init?: RequestInit & { contentType?: string },
) {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const authHeaders = await runtimeAuthHeaders(authResult.user);
  if (authHeaders instanceof Response) return authHeaders;
  try {
    const upstream = await fetch(`${ADMIN_URL}${path}`, {
      method: init?.method ?? req.method,
      cache: "no-store",
      headers: {
        ...(init?.contentType ? { "content-type": init.contentType } : {}),
        ...authHeaders,
      },
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
