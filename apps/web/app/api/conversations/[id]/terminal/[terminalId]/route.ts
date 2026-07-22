import { NextRequest } from "next/server";
import { gatewayJSONProxy } from "@/lib/gateway-json-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string; terminalId: string }> };

function path(id: string, terminalID: string) {
  return `/v1/conversations/${encodeURIComponent(id)}/terminal/` + encodeURIComponent(terminalID);
}

export async function GET(req: NextRequest, { params }: Params) {
  const { id, terminalId } = await params;
  return gatewayJSONProxy(req, path(id, terminalId), "GET");
}

export async function DELETE(req: NextRequest, { params }: Params) {
  const { id, terminalId } = await params;
  return gatewayJSONProxy(req, path(id, terminalId), "DELETE");
}
