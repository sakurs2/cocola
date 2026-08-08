import { NextRequest } from "next/server";
import { gatewayJSONProxy } from "@/lib/gateway-json-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(
  req: NextRequest,
  { params }: { params: Promise<{ id: string; task: string }> },
) {
  const { id, task } = await params;
  return gatewayJSONProxy(
    req,
    `/v1/projects/${encodeURIComponent(id)}/tasks/${encodeURIComponent(task)}/merge`,
    "POST",
  );
}
