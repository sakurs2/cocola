import { type NextRequest } from "next/server";
import { proxyAdmin } from "@/lib/admin-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  return proxyAdmin(req, `/admin/scheduled-task-runs/${encodeURIComponent((await params).id)}`);
}
