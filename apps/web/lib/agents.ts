import { normalizeModelIconConfig, type ModelIconConfig } from "@/lib/model-icons";

export type AgentStatus = "active" | "archived";

export type AgentKnowledgeSource =
  | {
      type: "feishu_doc" | "feishu_wiki" | "feishu_sheet" | "feishu_base";
      label: string;
      url: string;
      node_id?: never;
    }
  | {
      type: "cocola_wiki";
      label: string;
      node_id: string;
      url?: never;
    };

export type AgentSuggestedPrompt = {
  title: string;
  prompt: string;
};

export type AgentSkillCatalogItem = {
  id: string;
  runtime_id: string;
  name: string;
  description: string;
  source: "system" | "personal";
  available: boolean;
  default_enabled: boolean;
  unavailable_reason?: string;
};

export type AgentProfile = {
  id: string;
  name: string;
  description: string;
  instructions: string;
  avatar_key: string;
  avatar_color: string;
  runtime_id: string;
  model_route_id: string;
  model_alias: string;
  skill_ids: string[];
  knowledge_sources: AgentKnowledgeSource[];
  suggested_prompts: AgentSuggestedPrompt[];
  status: AgentStatus;
  version: number;
  created_at: string;
  updated_at: string;
  archived_at?: string;
};

export type AgentSnapshot = Pick<
  AgentProfile,
  | "id"
  | "name"
  | "description"
  | "instructions"
  | "avatar_key"
  | "avatar_color"
  | "runtime_id"
  | "model_route_id"
  | "model_alias"
  | "skill_ids"
  | "version"
>;

export type AgentConversationSnapshot = Omit<AgentSnapshot, "instructions">;

export type AgentModelOption = {
  id: string;
  alias: string;
  label: string;
  provider?: string;
  icon: ModelIconConfig;
  protocols: string[];
  isDefault: boolean;
};

export type AgentRuntimeCatalogItem = {
  id: string;
  label: string;
  modelProtocol: string;
  isDefault: boolean;
};

export const AGENT_AVATAR_KEYS = [
  "sparkle",
  "robot",
  "code",
  "chart",
  "document",
  "search",
  "briefcase",
  "support",
] as const;

export const AGENT_AVATAR_COLORS = [
  "slate",
  "blue",
  "cyan",
  "emerald",
  "amber",
  "orange",
  "rose",
  "violet",
] as const;

export const DEFAULT_AGENT_AVATAR_KEY = "sparkle";
export const DEFAULT_AGENT_AVATAR_COLOR = "blue";

export function agentKnowledgeSourceKey(source: AgentKnowledgeSource): string {
  return source.type === "cocola_wiki"
    ? `${source.type}:${source.node_id}`
    : `${source.type}:${source.url}`;
}

export function normalizeAgentModels(value: unknown): AgentModelOption[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((raw): AgentModelOption[] => {
    if (!raw || typeof raw !== "object") return [];
    const row = raw as Record<string, unknown>;
    const id = typeof row.id === "string" ? row.id.trim() : "";
    const alias = typeof row.alias === "string" ? row.alias.trim() : "";
    const label = typeof row.label === "string" ? row.label.trim() : "";
    if (!id || !alias || !label) return [];
    const provider = typeof row.provider === "string" ? row.provider.trim() : "";
    const family = typeof row.family === "string" ? row.family.trim() : "";
    const iconSlug = typeof row.icon_slug === "string" ? row.icon_slug.trim() : "";
    const icon = normalizeModelIconConfig(row.icon);
    const normalizedIcon =
      icon?.type === "image" && icon.src
        ? icon
        : iconSlug
          ? { type: "lobe-icons" as const, slug: iconSlug }
          : (icon ?? { type: "lobe-icons" as const, slug: family || provider || alias });
    return [
      {
        id,
        alias,
        label,
        ...(provider ? { provider } : {}),
        icon: normalizedIcon,
        protocols: Array.isArray(row.protocols)
          ? row.protocols.filter((item): item is string => typeof item === "string")
          : [],
        isDefault: row.is_default === true,
      },
    ];
  });
}

export function normalizeAgentRuntimes(value: unknown): AgentRuntimeCatalogItem[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((raw): AgentRuntimeCatalogItem[] => {
    if (!raw || typeof raw !== "object") return [];
    const row = raw as Record<string, unknown>;
    const id = typeof row.id === "string" ? row.id.trim() : "";
    const label = typeof row.label === "string" ? row.label.trim() : "";
    const modelProtocol = typeof row.model_protocol === "string" ? row.model_protocol.trim() : "";
    if (!id || !modelProtocol) return [];
    return [
      {
        id,
        label: label || id,
        modelProtocol,
        isDefault: row.is_default === true,
      },
    ];
  });
}

export function normalizeAgentSkillCatalog(value: unknown): AgentSkillCatalogItem[] {
  const rawSkills =
    value && typeof value === "object" && Array.isArray((value as { skills?: unknown }).skills)
      ? (value as { skills: unknown[] }).skills
      : [];
  return rawSkills.flatMap((raw): AgentSkillCatalogItem[] => {
    if (!raw || typeof raw !== "object") return [];
    const row = raw as Record<string, unknown>;
    const id = typeof row.id === "string" ? row.id.trim() : "";
    const runtimeID = typeof row.runtime_id === "string" ? row.runtime_id.trim() : "";
    const name = typeof row.name === "string" ? row.name.trim() : "";
    if (!id || !runtimeID || !name) return [];
    return [
      {
        id,
        runtime_id: runtimeID,
        name,
        description: typeof row.description === "string" ? row.description.trim() : "",
        source: row.source === "personal" ? "personal" : "system",
        available: row.available === true,
        default_enabled: row.default_enabled === true,
        ...(typeof row.unavailable_reason === "string" && row.unavailable_reason
          ? { unavailable_reason: row.unavailable_reason }
          : {}),
      },
    ];
  });
}

export function configuredAgentRuntimeID(value: unknown): string {
  if (!value || typeof value !== "object") return "";
  const config = value as { agent_runtime?: { default_id?: unknown } };
  return typeof config.agent_runtime?.default_id === "string"
    ? config.agent_runtime.default_id.trim()
    : "";
}

export async function agentResponseError(response: Response): Promise<string> {
  const fallback = `Request failed (${response.status})`;
  try {
    const payload = (await response.json()) as {
      error?: string | { message?: string };
      message?: string;
    };
    if (typeof payload.error === "string" && payload.error.trim()) return payload.error.trim();
    if (
      payload.error &&
      typeof payload.error === "object" &&
      typeof payload.error.message === "string" &&
      payload.error.message.trim()
    ) {
      return payload.error.message.trim();
    }
    if (typeof payload.message === "string" && payload.message.trim())
      return payload.message.trim();
  } catch {
    // The gateway may return an empty body for some infrastructure errors.
  }
  return fallback;
}
