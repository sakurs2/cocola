import { NextRequest } from "next/server";
import { gatewayJSONProxy } from "@/lib/gateway-json-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Context = { params: Promise<{ id: string; flowId: string }> };

function path(id: string, flowId: string) {
  return `/v1/agents/${encodeURIComponent(id)}/channels/feishu/registrations/${encodeURIComponent(
    flowId,
  )}`;
}

export async function GET(req: NextRequest, { params }: Context) {
  const { id, flowId } = await params;
  return gatewayJSONProxy(req, path(id, flowId), "GET");
}

export async function DELETE(req: NextRequest, { params }: Context) {
  const { id, flowId } = await params;
  return gatewayJSONProxy(req, path(id, flowId), "DELETE");
}
