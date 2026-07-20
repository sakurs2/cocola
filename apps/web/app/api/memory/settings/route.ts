import { type NextRequest } from "next/server";
import { proxyMemory } from "@/lib/memory-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export function GET(req: NextRequest) {
  return proxyMemory(req, "/v1/memory/settings", "GET");
}

export function PATCH(req: NextRequest) {
  return proxyMemory(req, "/v1/memory/settings", "PATCH");
}
