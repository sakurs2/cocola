import { type NextRequest } from "next/server";
import { proxyAdmin } from "@/lib/admin-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(req: NextRequest, { params }: { params: Promise<{ alias: string }> }) {
  const { alias } = await params;
  return proxyAdmin(req, `/admin/models/${encodeURIComponent(alias)}/default`);
}
