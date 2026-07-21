import { NextRequest } from "next/server";
import { gatewayJSONProxy } from "@/lib/gateway-json-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  const cursor = req.nextUrl.searchParams.get("cursor");
  const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
  return gatewayJSONProxy(req, `/v1/scm/github/repositories${query}`, "GET");
}
