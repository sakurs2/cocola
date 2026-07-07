import { isAuthFail, requireAdmin } from "@/lib/server-auth";
import { readFile } from "node:fs/promises";
import path from "node:path";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const SPEC_FILES: Record<string, string> = {
  gateway: "gateway.openapi.yaml",
  "admin-api": "admin-api.openapi.yaml",
  "llm-gateway": "llm-gateway.openapi.yaml",
};

export async function GET(_req: Request, { params }: { params: { spec: string } }) {
  const authResult = await requireAdmin();
  if (isAuthFail(authResult)) return authResult.response;

  const filename = SPEC_FILES[params.spec];
  if (!filename) {
    return Response.json({ error: "unknown api doc spec" }, { status: 404 });
  }

  try {
    const text = await readSpec(filename);
    return new Response(text, {
      headers: {
        "cache-control": "no-store",
        "content-type": "application/yaml; charset=utf-8",
      },
    });
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    return Response.json({ error: `api doc spec unavailable: ${message}` }, { status: 500 });
  }
}

async function readSpec(filename: string) {
  const candidates = [
    path.join(process.cwd(), "docs", "api", filename),
    path.join(process.cwd(), "..", "..", "docs", "api", filename),
  ];
  let lastErr: unknown;
  for (const candidate of candidates) {
    try {
      return await readFile(candidate, "utf8");
    } catch (err) {
      lastErr = err;
    }
  }
  throw lastErr instanceof Error ? lastErr : new Error("spec file not found");
}
