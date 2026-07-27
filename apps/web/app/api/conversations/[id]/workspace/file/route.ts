import { proxyWorkspace } from "@/lib/workspace-proxy";
import { type NextRequest } from "next/server";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  return proxyWorkspace(req, (await params).id, "file");
}
