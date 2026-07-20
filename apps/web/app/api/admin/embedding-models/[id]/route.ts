import { type NextRequest } from "next/server";
import { proxyAdmin } from "@/lib/admin-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Context = { params: Promise<{ id: string }> };

export async function PATCH(req: NextRequest, { params }: Context) {
  return proxyAdmin(req, `/admin/embedding-models/${encodeURIComponent((await params).id)}`);
}
