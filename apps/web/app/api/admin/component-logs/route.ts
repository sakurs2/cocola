import { NextRequest } from "next/server";
import { isAuthFail, requireAdmin } from "@/lib/server-auth";
import { readdir, readFile, stat } from "node:fs/promises";
import path from "node:path";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type LogFile = {
  name: string;
  size: number;
  updated_at: string;
};

const MAX_LINES = 2000;

export async function GET(req: NextRequest) {
  const authResult = await requireAdmin();
  if (isAuthFail(authResult)) return authResult.response;

  const logDir = componentLogDir();
  let files: LogFile[] = [];
  try {
    const entries = await readdir(logDir);
    files = (
      await Promise.all(
        entries.filter(isAllowedLogName).map(async (name) => {
          const info = await stat(path.join(logDir, name));
          return { name, size: info.size, updated_at: info.mtime.toISOString() };
        }),
      )
    ).sort((a, b) => b.updated_at.localeCompare(a.updated_at));
  } catch {
    return Response.json({ files: [], selected: "", lines: [], log_dir: logDir });
  }

  const url = new URL(req.url);
  const requested = url.searchParams.get("file") ?? "";
  const selected = files.some((file) => file.name === requested)
    ? requested
    : (files[0]?.name ?? "");
  const limit = clamp(Number(url.searchParams.get("lines") ?? 500), 1, MAX_LINES);
  const lines = selected ? await tailLines(path.join(logDir, selected), limit) : [];

  return Response.json({ files, selected, lines, log_dir: logDir });
}

function componentLogDir() {
  if (process.env.COCOLA_COMPONENT_LOG_DIR) {
    return path.resolve(process.env.COCOLA_COMPONENT_LOG_DIR);
  }
  return path.resolve(process.cwd(), "../..", ".run-logs");
}

function isAllowedLogName(name: string) {
  return /^[a-zA-Z0-9._-]+\.log$/.test(name);
}

function clamp(n: number, min: number, max: number) {
  if (!Number.isFinite(n)) return min;
  return Math.max(min, Math.min(max, Math.floor(n)));
}

async function tailLines(filePath: string, limit: number) {
  const raw = await readFile(filePath, "utf8");
  const lines = raw.split(/\r?\n/);
  return lines.slice(Math.max(0, lines.length - limit));
}
