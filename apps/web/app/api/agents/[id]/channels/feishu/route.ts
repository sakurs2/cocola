import { NextRequest } from "next/server";
import { gatewayJSONProxy } from "@/lib/gateway-json-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Context = { params: Promise<{ id: string }> };

export async function GET(req: NextRequest, { params }: Context) {
  const { id } = await params;
  return gatewayJSONProxy(req, `/v1/agents/${encodeURIComponent(id)}/channels/feishu`, "GET");
}

export async function DELETE(req: NextRequest, { params }: Context) {
  const { id } = await params;
  return gatewayJSONProxy(req, `/v1/agents/${encodeURIComponent(id)}/channels/feishu`, "DELETE");
}
