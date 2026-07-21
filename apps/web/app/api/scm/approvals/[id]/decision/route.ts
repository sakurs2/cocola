import { NextRequest } from "next/server";
import { gatewayJSONProxy } from "@/lib/gateway-json-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return gatewayJSONProxy(
    req,
    `/internal/scm/approvals/${encodeURIComponent(id)}/decision`,
    "POST",
  );
}
