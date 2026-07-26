import { NextRequest } from "next/server";
import { gatewayJSONProxy } from "@/lib/gateway-json-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return gatewayJSONProxy(req, "/v1/connectors/feishu", "GET");
}

export async function PATCH(req: NextRequest) {
  return gatewayJSONProxy(req, "/v1/connectors/feishu", "PATCH");
}

export async function DELETE(req: NextRequest) {
  return gatewayJSONProxy(req, "/v1/connectors/feishu", "DELETE");
}
