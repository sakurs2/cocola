import { proxyWorkspace } from "@/lib/workspace-proxy";
import { type NextRequest } from "next/server";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export function GET(req: NextRequest, { params }: { params: { id: string } }) {
  return proxyWorkspace(req, params.id, "file");
}
