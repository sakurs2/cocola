import { type NextRequest } from "next/server";
import { proxyMemory } from "@/lib/memory-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export function GET(req: NextRequest, { params }: { params: { id: string } }) {
  return proxyMemory(req, `/v1/memory/items/${encodeURIComponent(params.id)}`, "GET");
}

export function DELETE(req: NextRequest, { params }: { params: { id: string } }) {
  return proxyMemory(req, `/v1/memory/items/${encodeURIComponent(params.id)}`, "DELETE");
}
