import { type NextRequest } from "next/server";
import { proxyAdmin } from "@/lib/admin-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  return proxyAdmin(
    req,
    `/admin/conversation-runs/${(await params).path.map(encodeURIComponent).join("/")}`,
  );
}
