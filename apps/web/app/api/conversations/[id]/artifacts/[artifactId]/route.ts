import { NextRequest } from "next/server";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const GATEWAY_URL = process.env.COCOLA_GATEWAY_URL ?? "http://127.0.0.1:8080";

export async function GET(
  req: NextRequest,
  { params }: { params: { id: string; artifactId: string } },
) {
  const auth = req.headers.get("authorization") ?? "";
  const id = encodeURIComponent(params.id);
  const artifactId = encodeURIComponent(params.artifactId);
  try {
    const upstream = await fetch(`${GATEWAY_URL}/v1/conversations/${id}/artifacts/${artifactId}`, {
      method: "GET",
      cache: "no-store",
      headers: { ...(auth ? { authorization: auth } : {}) },
    });
    const headers = new Headers();
    for (const key of ["content-type", "content-length", "content-disposition"]) {
      const value = upstream.headers.get(key);
      if (value) headers.set(key, value);
    }
    return new Response(upstream.body, {
      status: upstream.status,
      headers,
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return new Response(JSON.stringify({ error: `gateway unreachable: ${msg}` }), {
      status: 502,
      headers: { "content-type": "application/json" },
    });
  }
}
