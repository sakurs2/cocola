import { readFile } from "node:fs/promises";
import path from "node:path";
import { normalizeLobeIconSlug } from "@/lib/model-icons";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const iconDir = path.join(process.cwd(), "node_modules", "@lobehub", "icons-static-svg", "icons");

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

export async function GET(_: Request, { params }: { params: { slug: string } }) {
  const slug = normalizeLobeIconSlug(params.slug);
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
