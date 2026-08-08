import { NextRequest } from "next/server";
import { gatewayJSONProxy } from "@/lib/gateway-json-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Context = { params: Promise<{ id: string; task: string }> };

export async function GET(req: NextRequest, { params }: Context) {
  const { id, task } = await params;
  return gatewayJSONProxy(
    req,
    `/v1/projects/${encodeURIComponent(id)}/tasks/${encodeURIComponent(task)}/change-request`,
    "GET",
  );
}

export async function POST(req: NextRequest, { params }: Context) {
  const { id, task } = await params;
  return gatewayJSONProxy(
    req,
    `/v1/projects/${encodeURIComponent(id)}/tasks/${encodeURIComponent(task)}/change-request`,
    "POST",
  );
}
