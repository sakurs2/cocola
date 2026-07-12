import { type NextRequest } from "next/server";
import { proxyAdmin } from "@/lib/admin-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export function GET(req: NextRequest) {
  return proxyAdmin(req, "/admin/token-usage/export");
}
