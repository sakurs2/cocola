import { type NextRequest } from "next/server";
import { proxyAdmin } from "@/lib/admin-proxy";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  return proxyAdmin(req, "/admin/memory/reset");
}
