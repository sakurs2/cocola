import { type NextRequest } from "next/server";
import { proxyAdmin } from "@/lib/admin-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Context = { params: Promise<{ path: string[] }> };

async function path({ params }: Context) {
  return `/admin/skills/${(await params).path.map(encodeURIComponent).join("/")}`;
}

export async function GET(req: NextRequest, context: Context) {
  return proxyAdmin(req, await path(context));
}

export async function POST(req: NextRequest, context: Context) {
  return proxyAdmin(req, await path(context));
}

export async function DELETE(req: NextRequest, context: Context) {
  return proxyAdmin(req, await path(context));
}
