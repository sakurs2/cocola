import { NextRequest } from "next/server";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const ADMIN_URL =
  process.env.COCOLA_ADMIN_URL ?? process.env.COCOLA_ADMIN_BASE_URL ?? "http://127.0.0.1:8092";

export async function GET(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  return proxyAdmin(req, await adminPath(params));
}

export async function POST(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  const body = await req.text();
  return proxyAdmin(req, await adminPath(params), {
    method: "POST",
    body,
    contentType: req.headers.get("content-type") ?? "application/json",
  });
}

async function adminPath(params: Promise<{ path: string[] }>) {
  const { path } = await params;
  return `/admin/sandbox-nodes/${path.map(encodeURIComponent).join("/")}`;
}

async function proxyAdmin(
  req: NextRequest,
  path: string,
  init?: RequestInit & { contentType?: string },
) {
  try {
    const upstream = await fetch(`${ADMIN_URL}${path}`, {
      method: init?.method ?? req.method,
      cache: "no-store",
      headers: adminHeaders(req, init?.contentType),
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

function adminHeaders(req: NextRequest, contentType?: string): HeadersInit {
  const headers: Record<string, string> = {
    "x-cocola-admin": req.headers.get("x-cocola-admin") ?? "web-demo",
  };
  if (contentType) headers["content-type"] = contentType;
  const key = process.env.COCOLA_ADMIN_KEY;
  if (key) headers.authorization = `Bearer ${key}`;
  return headers;
}
