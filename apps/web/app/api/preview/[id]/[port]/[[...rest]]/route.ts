import { proxyPreview } from "@/lib/preview-proxy";
import { type NextRequest } from "next/server";

// Same-origin catch-all for the Preview Proxy iframe. Every method/path under
// /api/preview/{id}/{port}/... is forwarded to the gateway's /v1/preview route
// (see lib/preview-proxy.ts). The optional [[...rest]] catch-all also matches
// the bare /api/preview/{id}/{port} (dev-server root).

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string; port: string; rest?: string[] }> };

async function handle(req: NextRequest, { params }: Params) {
  const { id, port, rest } = await params;
  return proxyPreview(req, id, port, rest ?? []);
}

export const GET = handle;
export const POST = handle;
export const PUT = handle;
export const PATCH = handle;
export const DELETE = handle;
export const HEAD = handle;
export const OPTIONS = handle;
