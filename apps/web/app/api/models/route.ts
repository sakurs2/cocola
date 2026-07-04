import { promises as fs } from "fs";
import path from "path";
import { isAuthFail, requireUser } from "@/lib/server-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type ModelIcon = {
  type: "simple-icons" | "image";
  slug: string;
  src?: string;
};

type ModelOption = {
  alias: string;
  label: string;
  icon: ModelIcon;
};

type RouteConfig = {
  runtime?: unknown;
  label?: unknown;
  icon?: unknown;
  enabled?: unknown;
  visible?: unknown;
};

type LLMConfig = {
  default_alias?: unknown;
  routes?: Record<string, RouteConfig>;
};

const CURRENT_RUNTIME = process.env.COCOLA_AGENT_RUNTIME ?? "claude-code";
const DEFAULT_ICON: ModelIcon = { type: "simple-icons", slug: "anthropic" };
const DEFAULT_MODEL: ModelOption = {
  alias: "cocola-default",
  label: "Cocola Default",
  icon: DEFAULT_ICON,
};

export async function GET() {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) return authResult.response;
  const config = await loadConfig();
  const models = selectModels(config);
  return Response.json(models.length > 0 ? models : [DEFAULT_MODEL]);
}

async function loadConfig(): Promise<LLMConfig | null> {
  const explicit = process.env.COCOLA_LLM_CONFIG || "deploy/llm-config.json";
  for (const candidate of configCandidates(explicit)) {
    try {
      const raw = await fs.readFile(candidate, "utf8");
      return JSON.parse(raw) as LLMConfig;
    } catch (err) {
      if (isNotFound(err)) continue;
      console.warn(`failed to load llm config from ${candidate}:`, err);
      return null;
    }
  }
  return null;
}

function configCandidates(configPath: string): string[] {
  if (path.isAbsolute(configPath)) return [configPath];
  return [
    path.resolve(process.cwd(), configPath),
    path.resolve(process.cwd(), "..", "..", configPath),
  ];
}

function selectModels(config: LLMConfig | null): ModelOption[] {
  const routes = config?.routes;
  if (!routes || typeof routes !== "object") return [];

  const models = Object.entries(routes)
    .filter(([, route]) => isEnabled(route.enabled) && isEnabled(route.visible))
    .filter(([, route]) => String(route.runtime ?? "claude-code") === CURRENT_RUNTIME)
    .map(([alias, route]) => ({
      alias,
      label: typeof route.label === "string" && route.label.trim() ? route.label : alias,
      icon: parseIcon(route.icon),
    }));

  const defaultAlias = typeof config?.default_alias === "string" ? config.default_alias : "";
  if (!defaultAlias) return models;
  return models.sort((a, b) => {
    if (a.alias === defaultAlias) return -1;
    if (b.alias === defaultAlias) return 1;
    return 0;
  });
}

function parseIcon(raw: unknown): ModelIcon {
  if (!raw || typeof raw !== "object") return DEFAULT_ICON;
  const icon = raw as { type?: unknown; slug?: unknown; src?: unknown };
  if (
    (icon.type === "simple-icons" || icon.type === "image") &&
    typeof icon.slug === "string" &&
    icon.slug.trim()
  ) {
    return {
      type: icon.type,
      slug: icon.slug.trim(),
      ...(typeof icon.src === "string" && icon.src.trim() ? { src: icon.src.trim() } : {}),
    };
  }
  return DEFAULT_ICON;
}

function isEnabled(value: unknown): boolean {
  if (value === undefined || value === null) return true;
  if (typeof value === "boolean") return value;
  if (typeof value === "string") return !["0", "false", "no", "off"].includes(value.toLowerCase());
  return Boolean(value);
}

function isNotFound(err: unknown): boolean {
  return Boolean(err && typeof err === "object" && "code" in err && err.code === "ENOENT");
}
