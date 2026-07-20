import { type NextRequest } from "next/server";
import { proxyMemory } from "@/lib/memory-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export function GET(req: NextRequest) {
  return proxyMemory(req, "/v1/memory/items", "GET");
}

export function DELETE(req: NextRequest) {
  return proxyMemory(req, "/v1/memory/items", "DELETE");
}
