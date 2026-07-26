import { NextRequest } from "next/server";
import { gatewayJSONProxy } from "@/lib/gateway-json-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return gatewayJSONProxy(
    req,
    `/v1/connectors/feishu/registrations/${encodeURIComponent(id)}`,
    "GET",
  );
}

export async function DELETE(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return gatewayJSONProxy(
    req,
    `/v1/connectors/feishu/registrations/${encodeURIComponent(id)}`,
    "DELETE",
  );
}
