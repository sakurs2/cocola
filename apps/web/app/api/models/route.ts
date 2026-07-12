import { adminHeaders, isAuthFail, requireUser } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type ModelIcon = {
  type: "lobe-icons" | "simple-icons" | "image";
  slug?: string;
  src?: string;
};

const ADMIN_URL = process.env.COCOLA_ADMIN_URL ?? "http://127.0.0.1:8092";

export async function GET() {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;

  try {
    const upstream = await fetch(`${ADMIN_URL}/admin/models/public`, {
      method: "GET",
      cache: "no-store",
      headers: adminHeaders(authResult.user),
    });
    if (!upstream.ok) return Response.json([]);
    const models = await upstream.json();
    return Response.json(Array.isArray(models) ? models : []);
  } catch {
    return Response.json([]);
  }
}
