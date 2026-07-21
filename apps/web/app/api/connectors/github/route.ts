import { NextRequest } from "next/server";
import { gatewayJSONProxy } from "@/lib/gateway-json-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return gatewayJSONProxy(req, "/v1/connectors/github", "GET");
}

export async function DELETE(req: NextRequest) {
  return gatewayJSONProxy(req, "/v1/connectors/github", "DELETE");
}
