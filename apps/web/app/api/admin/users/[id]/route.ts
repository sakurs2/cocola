import { type NextRequest } from "next/server";
import { proxyAdmin } from "@/lib/admin-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Context = { params: Promise<{ id: string }> };

async function path({ params }: Context) {
  return `/admin/users/${encodeURIComponent((await params).id)}`;
}

export async function PATCH(req: NextRequest, context: Context) {
  return proxyAdmin(req, await path(context));
}

export async function DELETE(req: NextRequest, context: Context) {
  return proxyAdmin(req, await path(context));
}
