import { NextRequest } from "next/server";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const ADMIN_URL =
  process.env.COCOLA_ADMIN_URL ?? process.env.COCOLA_ADMIN_BASE_URL ?? "http://127.0.0.1:8092";

export async function GET(req: NextRequest) {
  return proxyAdmin(req, "/admin/sandbox-nodes");
}

async function proxyAdmin(req: NextRequest, path: string, init?: RequestInit) {
  try {
    const upstream = await fetch(`${ADMIN_URL}${path}`, {
      method: init?.method ?? req.method,
      cache: "no-store",
      headers: adminHeaders(req),
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

function adminHeaders(req: NextRequest): HeadersInit {
  const headers: Record<string, string> = {
    "x-cocola-admin": req.headers.get("x-cocola-admin") ?? "web-demo",
  };
  const key = process.env.COCOLA_ADMIN_KEY;
  if (key) headers.authorization = `Bearer ${key}`;
  return headers;
}
