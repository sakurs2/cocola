export type AgentStatus = "active" | "archived";

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
  | "version"
>;

export type AgentConversationSnapshot = Omit<AgentSnapshot, "instructions">;

export type AgentModelOption = {
  id: string;
  alias: string;
  label: string;
  provider?: string;
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

export function normalizeAgentModels(value: unknown): AgentModelOption[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((raw): AgentModelOption[] => {
    if (!raw || typeof raw !== "object") return [];
    const row = raw as Record<string, unknown>;
    const id = typeof row.id === "string" ? row.id.trim() : "";
    const alias = typeof row.alias === "string" ? row.alias.trim() : "";
    const label = typeof row.label === "string" ? row.label.trim() : "";
    if (!id || !alias || !label) return [];
    return [
      {
        id,
        alias,
        label,
        ...(typeof row.provider === "string" && row.provider.trim()
          ? { provider: row.provider.trim() }
          : {}),
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
