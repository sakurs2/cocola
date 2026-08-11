import { readFile } from "node:fs/promises";
import path from "node:path";
import { normalizeLobeIconSlug } from "@/lib/model-icons";
import { adminHeaders, isAuthFail, requireUser } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const iconDir = path.join(process.cwd(), "node_modules", "@lobehub", "icons-static-svg", "icons");
const ADMIN_URL = process.env.COCOLA_ADMIN_URL ?? "http://127.0.0.1:8092";
const MANAGED_ICON_ID = /^[a-f0-9]{64}$/;

function candidates(slug: string): string[] {
  return [
    `${slug}-color.svg`,
    `${slug}.svg`,
    `${slug}-brand-color.svg`,
    `${slug}-brand.svg`,
    `${slug}-text.svg`,
    `${slug}-text-cn.svg`,
  ];
}

export async function GET(_: Request, { params }: { params: Promise<{ slug: string }> }) {
  const value = (await params).slug;
  if (MANAGED_ICON_ID.test(value)) return getManagedIcon(value);

  const slug = normalizeLobeIconSlug(value);
  if (!slug) return new Response("not found", { status: 404 });

  for (const filename of candidates(slug)) {
    try {
      const svg = await readFile(path.join(iconDir, filename));
      return new Response(svg, {
        headers: {
          "cache-control": "public, max-age=31536000, immutable",
          "content-type": "image/svg+xml; charset=utf-8",
        },
      });
    } catch {
      // Try the next Lobe icon variant for this slug.
    }
  }

  return new Response("not found", { status: 404 });
}

async function getManagedIcon(id: string) {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;

  try {
    const upstream = await fetch(`${ADMIN_URL}/admin/model-icons/${id}`, {
      method: "GET",
      cache: "force-cache",
      headers: adminHeaders(authResult.user),
    });
    if (!upstream.ok) return new Response("not found", { status: upstream.status });

    return new Response(await upstream.arrayBuffer(), {
      status: 200,
      headers: {
        "cache-control": "public, max-age=31536000, immutable",
        "content-type": upstream.headers.get("content-type") ?? "application/octet-stream",
        "x-content-type-options": "nosniff",
      },
    });
  } catch {
    return new Response("model icon unavailable", { status: 502 });
  }
}
