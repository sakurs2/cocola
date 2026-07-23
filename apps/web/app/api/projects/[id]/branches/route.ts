import { NextRequest } from "next/server";
import { gatewayJSONProxy } from "@/lib/gateway-json-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const cursor = req.nextUrl.searchParams.get("cursor")?.trim();
  const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
  return gatewayJSONProxy(req, `/v1/projects/${encodeURIComponent(id)}/branches${query}`, "GET");
}
