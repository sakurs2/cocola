import { NextRequest } from "next/server";
import { isAuthFail, requireAdmin } from "@/lib/server-auth";
import { open, stat } from "node:fs/promises";
import path from "node:path";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type LogFile = {
  name: string;
  label: string;
  size: number;
};

const MAX_LINES = 2000;
const MAX_TAIL_BYTES = 2 * 1024 * 1024;
const COMPONENT_LOG_FILES = [
  { name: "web.log", label: "Web" },
  { name: "gateway.log", label: "Gateway" },
  { name: "admin-api.log", label: "Admin API" },
  { name: "agent-runtime.log", label: "Agent Runtime" },
  { name: "llm-gateway.log", label: "LLM Gateway" },
  { name: "sandbox-manager.log", label: "Sandbox Manager" },
] as const;

export async function GET(req: NextRequest) {
  const authResult = await requireAdmin();
  if (isAuthFail(authResult)) return authResult.response;

  const logDir = componentLogDir();
  const files = (
    await Promise.all(
      COMPONENT_LOG_FILES.map(async ({ name, label }): Promise<LogFile | null> => {
        try {
          const info = await stat(path.join(logDir, name));
          if (!info.isFile()) return null;
          return { name, label, size: info.size };
        } catch {
          return null;
        }
      }),
    )
  ).filter((file): file is LogFile => file !== null);

  const url = new URL(req.url);
  const requested = url.searchParams.get("file") ?? "";
  const selected = files.some((file) => file.name === requested)
    ? requested
    : (files[0]?.name ?? "");
  const limit = clamp(Number(url.searchParams.get("lines") ?? 500), 1, MAX_LINES);
  let lines: string[] = [];
  if (selected) {
    try {
      lines = await tailLines(path.join(logDir, selected), limit);
    } catch {
      lines = [];
    }
  }

  return Response.json({ files, selected, lines });
}

function componentLogDir() {
  if (process.env.COCOLA_COMPONENT_LOG_DIR) {
    return path.resolve(process.env.COCOLA_COMPONENT_LOG_DIR);
  }
  return path.resolve(process.cwd(), "../..", ".run-logs");
}

function clamp(n: number, min: number, max: number) {
  if (!Number.isFinite(n)) return min;
  return Math.max(min, Math.min(max, Math.floor(n)));
}

async function tailLines(filePath: string, limit: number) {
  const file = await open(filePath, "r");
  let raw = "";
  try {
    const info = await file.stat();
    const readSize = Math.min(info.size, MAX_TAIL_BYTES);
    const buffer = Buffer.alloc(readSize);
    const { bytesRead } = await file.read(buffer, 0, readSize, info.size - readSize);
    raw = buffer.subarray(0, bytesRead).toString("utf8");
    if (info.size > readSize) {
      const firstCompleteLine = raw.indexOf("\n");
      raw = firstCompleteLine >= 0 ? raw.slice(firstCompleteLine + 1) : "";
    }
  } finally {
    await file.close();
  }
  const lines = raw.split(/\r?\n/);
  if (lines.at(-1) === "") lines.pop();
  return lines.slice(Math.max(0, lines.length - limit));
}
