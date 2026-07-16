import { type NextRequest } from "next/server";
import { proxyAdmin } from "@/lib/admin-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Context = { params: Promise<{ storageId: string }> };

export async function POST(req: NextRequest, { params }: Context) {
  const storageId = encodeURIComponent((await params).storageId);
  return proxyAdmin(req, `/admin/session-storage/${storageId}/measure`);
}
