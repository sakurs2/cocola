import { NextRequest } from "next/server";
import { gatewayJSONProxy } from "@/lib/gateway-json-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Context = { params: Promise<{ id: string }> };

export async function GET(req: NextRequest, { params }: Context) {
  const { id } = await params;
  return gatewayJSONProxy(req, `/v1/projects/${encodeURIComponent(id)}`, "GET");
}

export async function PATCH(req: NextRequest, { params }: Context) {
  const { id } = await params;
  return gatewayJSONProxy(req, `/v1/projects/${encodeURIComponent(id)}`, "PATCH");
}

export async function DELETE(req: NextRequest, { params }: Context) {
  const { id } = await params;
  return gatewayJSONProxy(req, `/v1/projects/${encodeURIComponent(id)}`, "DELETE");
}
