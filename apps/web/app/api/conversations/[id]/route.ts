import { NextRequest } from "next/server";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";

export async function PATCH(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const auth = req.headers.get("authorization") ?? "";
  const body = await req.text();
  try {
    const upstream = await fetch(`${GATEWAY_URL}/v1/conversations/${encodeURIComponent(id)}`, {
      method: "PATCH",
      cache: "no-store",
      headers: {
        "content-type": req.headers.get("content-type") ?? "application/json",
        ...(auth ? { authorization: auth } : {}),
      },
      body,
    });
    const text = await upstream.text();
    return new Response(text, {
      status: upstream.status,
      headers: { "content-type": "application/json" },
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return new Response(JSON.stringify({ error: `gateway unreachable: ${msg}` }), {
      status: 502,
      headers: { "content-type": "application/json" },
    });
  }
}

export async function DELETE(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const auth = req.headers.get("authorization") ?? "";
  try {
    const upstream = await fetch(`${GATEWAY_URL}/v1/conversations/${encodeURIComponent(id)}`, {
      method: "DELETE",
      cache: "no-store",
      headers: { ...(auth ? { authorization: auth } : {}) },
    });
    const text = await upstream.text();
    return new Response(text || null, {
      status: upstream.status,
      headers: text ? { "content-type": "application/json" } : {},
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return new Response(JSON.stringify({ error: `gateway unreachable: ${msg}` }), {
      status: 502,
      headers: { "content-type": "application/json" },
    });
  }
}
