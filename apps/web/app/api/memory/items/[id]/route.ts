import { type NextRequest } from "next/server";
import { proxyMemory } from "@/lib/memory-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  return proxyMemory(req, `/v1/memory/items/${encodeURIComponent((await params).id)}`, "GET");
}

export async function DELETE(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  return proxyMemory(req, `/v1/memory/items/${encodeURIComponent((await params).id)}`, "DELETE");
}
