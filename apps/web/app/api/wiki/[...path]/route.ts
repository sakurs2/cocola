import { type NextRequest } from "next/server";
import { proxyWiki } from "@/lib/wiki-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Context = { params: Promise<{ path: string[] }> };

async function gatewayPath(context: Context) {
  const segments = (await context.params).path ?? [];
  return `/v1/wiki/${segments.map(encodeURIComponent).join("/")}`;
}

export async function GET(req: NextRequest, context: Context) {
  return proxyWiki(req, await gatewayPath(context), "GET");
}

export async function POST(req: NextRequest, context: Context) {
  return proxyWiki(req, await gatewayPath(context), "POST");
}

export async function PUT(req: NextRequest, context: Context) {
  return proxyWiki(req, await gatewayPath(context), "PUT");
}

export async function PATCH(req: NextRequest, context: Context) {
  return proxyWiki(req, await gatewayPath(context), "PATCH");
}

export async function DELETE(req: NextRequest, context: Context) {
  return proxyWiki(req, await gatewayPath(context), "DELETE");
}
