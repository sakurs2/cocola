import { type NextRequest } from "next/server";
import { proxyAdmin } from "@/lib/admin-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export function GET(req: NextRequest, { params }: { params: { path: string[] } }) {
  return proxyAdmin(
    req,
    `/admin/conversation-runs/${params.path.map(encodeURIComponent).join("/")}`,
  );
}
