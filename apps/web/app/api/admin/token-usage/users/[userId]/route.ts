import { type NextRequest } from "next/server";
import { proxyAdmin } from "@/lib/admin-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export function GET(req: NextRequest, { params }: { params: { userId: string } }) {
  return proxyAdmin(req, `/admin/token-usage/users/${encodeURIComponent(params.userId)}`);
}
