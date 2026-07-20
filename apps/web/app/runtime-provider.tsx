"use client";

// Cocola ExternalStore runtime adapter.
//
// Bridges the gateway SSE stream (consumed via /api/chat, the same-origin proxy)
// into assistant-ui's ExternalStoreRuntime. We own the message state with plain
// useState (single-session main path; no redux/zustand needed) and feed
// assistant-ui through `convertMessage`, which maps cocola's local message shape
// to assistant-ui's ThreadMessageLike. Streaming mutates the in-flight assistant
// message in place via immutable updates (best practice).
//
// Backend event vocabulary (see docs/plan/web-product-ui-assistant-ui.md §3):
//   text | thinking | tool_use | tool_result | result | system | sandbox |
//   error | done.  Unknown kinds are tolerated (no-op), never crash the stream.

import {
  AssistantRuntimeProvider,
  useExternalStoreRuntime,
  type ThreadMessageLike,
} from "@assistant-ui/react";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { signOut } from "next-auth/react";
import { parseFrames, type AgentEvent } from "@/lib/sse";
import { Base64AttachmentAdapter } from "@/lib/base64-attachment-adapter";
import {
  parseEnvironmentPreparationSnapshot,
  type EnvironmentPreparationSnapshot,
} from "@/lib/environment";
import { inferAgentDurationMs } from "@/lib/agent-turn-summary.mjs";

// ---- Local message model (carries cocola semantics) ------------------------

type UiToolCall = {
  type: "tool-call";
  toolCallId: string;
  toolName: string;
  argsText: string;
  result?: string;
  isError?: boolean;
};

export type ArtifactPreview = {
  id: string;
  sessionId: string;
  filename: string;
  mimeType: string;
  size: number;
  downloadUrl: string;
};

type UiFilePart = {
  type: "file";
  id: string;
  filename: string;
  mimeType: string;
  size: number;
  downloadUrl: string;
};

type UiEnvironmentPart = {
  type: "environment";
  environment: EnvironmentPreparationSnapshot;
};

type UiSessionStatusPart = {
  type: "session-status";
  sessionStatus: EnvironmentStatus;
};

type UiProgressPart = {
  type: "progress";
  progressId: string;
  items: unknown[];
};

export type MemoryRecallStatus = "running" | "hit" | "miss" | "degraded" | "unavailable";

export type UiMemoryRecallPart = {
  type: "memory-recall";
  status: MemoryRecallStatus;
  count: number;
  errorCode?: string;
};

type UiPart =
  | { type: "text"; text: string }
  | { type: "reasoning"; text: string }
  | UiToolCall
  | UiFilePart
  | UiEnvironmentPart
  | UiSessionStatusPart
  | UiProgressPart
  | UiMemoryRecallPart;

type UiMessage = {
  id: string;
  role: "user" | "assistant";
  parts: UiPart[];
  createdAt: number;
  metadata?: UiMessageMetadata;
};

type RunCursor = {
  conversationId: string;
  runId: string;
  assistantId: string;
};

const RUN_CURSOR_KEY = "cocola.chat-runs.v1";
const CHAT_START_MAX_ATTEMPTS = 8;
const RUN_RECONNECT_MAX_ATTEMPTS = 20;

function readRunCursors(): RunCursor[] {
  if (typeof window === "undefined") return [];
  try {
    const raw = JSON.parse(sessionStorage.getItem(RUN_CURSOR_KEY) ?? "[]") as unknown;
    if (!Array.isArray(raw)) return [];
    return raw.flatMap((item): RunCursor[] => {
      if (!item || typeof item !== "object") return [];
      const cursor = item as Record<string, unknown>;
      return typeof cursor.conversationId === "string" &&
        typeof cursor.runId === "string" &&
        typeof cursor.assistantId === "string"
        ? [cursor as RunCursor]
        : [];
    });
  } catch {
    return [];
  }
}

function writeRunCursors(cursors: Map<string, RunCursor>) {
  if (typeof window === "undefined") return;
  try {
    sessionStorage.setItem(RUN_CURSOR_KEY, JSON.stringify([...cursors.values()]));
  } catch (error) {
    console.warn("chat run cursor could not be persisted", error);
  }
}

export type SandboxInfo = {
  sandboxId: string;
  endpoint: string;
  reused: boolean;
};

export type EnvironmentComponentStatus =
  | "pending"
  | "configured"
  | "loaded"
  | "connected"
  | "failed"
  | "needs-auth"
  | "disabled"
  | "unavailable"
  | "timeout";

export type EnvironmentComponent = {
  kind: "mcp" | string;
  id: string;
  label: string;
  status: EnvironmentComponentStatus;
  toolCount: number;
  version?: string;
  error?: string;
};

export type EnvironmentStatus = {
  version: number;
  phase: "preparing" | "ready" | "degraded";
  components: EnvironmentComponent[];
  updatedAt: number;
};

// One row in the sidebar conversation list (gateway GET /v1/conversations).
export type ConversationSummary = {
  id: string;
  title: string;
  chat_type?: "chat" | "scheduled_task" | string;
  folder_id?: string;
  updated_at: string;
  runtime_id: string;
};

export type ConversationFolder = {
  id: string;
  name: string;
  created_at: string;
  updated_at: string;
};

type UserEvent = {
  id: string;
  type: string;
  user_id: string;
  occurred_at: string;
  resource?: {
    kind?: string;
    id?: string;
  };
  data?: Record<string, unknown>;
};

type UserEventSnapshot = {
  events?: UserEvent[];
};

export type ModelIconConfig = {
  type: "lobe-icons" | "simple-icons" | "image";
  slug?: string;
  src?: string;
};

export type ModelOption = {
  id: string;
  alias: string;
  label: string;
  provider?: string;
  family?: string;
  iconSlug?: string;
  icon: ModelIconConfig;
  protocols: string[];
  isDefault: boolean;
};

export type AgentRuntimeOption = {
  id: string;
  label: string;
  model_protocol: string;
  is_default: boolean;
};

export type SkillOption = {
  id: string;
  name: string;
  description: string;
  version: string;
  scope: string;
};

export type UiMessageMetadata = {
  skill_id?: string;
  model_route_id?: string;
  model_alias?: string;
  model_label?: string;
  model_provider?: string;
  model_family?: string;
  model_icon_slug?: string;
  model_icon?: ModelIconConfig;
  duration_ms?: number;
};

// Wire shape of a persisted message (gateway GET /v1/conversations/{id}/messages).
// Parts mirror UiPart exactly (that is the whole point of route A), so mapping
// back into local state is a near-identity.
type WireMessage = {
  id: string;
  role: "user" | "assistant";
  parts: UiPart[];
  metadata?: UiMessageMetadata;
  created_at: string;
};

// ---- Session shell context (session_id / sandbox banner) -------------------

type CocolaContextValue = {
  sessionId: string;
  setSessionId: (v: string) => void;
  sandbox: SandboxInfo | null;
  // Reset to a fresh conversation: clears messages + sandbox and rotates
  // session_id to a NEW random id. A never-before-seen session_id has no
  // resume entry in the backend session_map, so Route A's claude CLI starts
  // from zero — this is the boundary that severs prior-turn history.
  newConversation: (folderId?: string) => string;
  // Persisted conversation list (sidebar) + loaders. conversations is refreshed
  // after each turn and on mount; loadConversation replays a stored conversation
  // into the thread and points session_id at it so follow-ups continue it.
  conversations: ConversationSummary[];
  refreshConversations: () => void;
  loadConversation: (id: string) => Promise<void>;
  renameConversation: (id: string, title: string) => Promise<void>;
  deleteConversation: (id: string) => Promise<void>;
  folders: ConversationFolder[];
  foldersLoaded: boolean;
  refreshFolders: () => void;
  createFolder: (name: string) => Promise<ConversationFolder>;
  renameFolder: (id: string, name: string) => Promise<ConversationFolder>;
  deleteFolder: (id: string) => Promise<void>;
  moveConversation: (id: string, folderId: string | null) => Promise<void>;
  activeSessionId: string;
  runningSessionIds: Set<string>;
  unreadCompletedSessionIds: Set<string>;
  environmentStatus: EnvironmentStatus | null;
  selectedArtifact: ArtifactPreview | null;
  openArtifact: (artifact: ArtifactPreview) => void;
  closeArtifact: () => void;
  models: ModelOption[];
  selectedModelID: string;
  selectedModel: ModelOption | null;
  modelsLoaded: boolean;
  setSelectedModelID: (id: string) => void;
  runtimes: AgentRuntimeOption[];
  selectedRuntime: AgentRuntimeOption | null;
  runtimeLocked: boolean;
  setSelectedRuntimeId: (id: string) => void;
  skills: SkillOption[];
  skillsLoaded: boolean;
  selectedSkill: SkillOption | null;
  setSelectedSkillId: (id: string | null) => void;
};

const CocolaContext = createContext<CocolaContextValue | null>(null);

export function useCocola(): CocolaContextValue {
  const ctx = useContext(CocolaContext);
  if (!ctx) throw new Error("useCocola must be used within CocolaRuntimeProvider");
  return ctx;
}

// ---- Helpers ----------------------------------------------------------------

function genId(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) return crypto.randomUUID();
  return `id-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

async function apiError(response: Response, fallback: string): Promise<Error> {
  const text = await response.text().catch(() => "");
  try {
    const body = JSON.parse(text) as { error?: { message?: string } | string };
    if (typeof body.error === "string" && body.error) return new Error(body.error);
    if (
      body.error &&
      typeof body.error === "object" &&
      typeof body.error.message === "string" &&
      body.error.message
    ) {
      return new Error(body.error.message);
    }
  } catch {
    // The fallback below is clearer than a malformed upstream response.
  }
  return new Error(text || fallback);
}

function isTruthy(v: string | undefined): boolean {
  return v === "true" || v === "True" || v === "1";
}

function parseSize(v: string | undefined): number {
  const n = Number.parseInt(v ?? "", 10);
  return Number.isFinite(n) && n > 0 ? n : 0;
}

const ENVIRONMENT_PHASES = new Set<EnvironmentStatus["phase"]>(["preparing", "ready", "degraded"]);
const ENVIRONMENT_COMPONENT_STATUSES = new Set<EnvironmentComponentStatus>([
  "pending",
  "configured",
  "loaded",
  "connected",
  "failed",
  "needs-auth",
  "disabled",
  "unavailable",
  "timeout",
]);

function parseEnvironmentStatus(event: AgentEvent): EnvironmentStatus | null {
  const data = event.data ?? {};
  return parseEnvironmentStatusSnapshot({
    version: data.version,
    phase: data.phase,
    components: data.components ?? "[]",
  });
}

function parseEnvironmentStatusSnapshot(raw: unknown): EnvironmentStatus | null {
  if (!raw || typeof raw !== "object") return null;
  const snapshot = raw as Record<string, unknown>;
  const phase = stringValue(snapshot.phase) as EnvironmentStatus["phase"];
  if (!ENVIRONMENT_PHASES.has(phase)) return null;

  let rawComponents = snapshot.components;
  try {
    if (typeof rawComponents === "string") rawComponents = JSON.parse(rawComponents);
  } catch {
    return null;
  }
  if (!Array.isArray(rawComponents)) return null;
  const components = rawComponents.flatMap((raw): EnvironmentComponent[] => {
    if (!raw || typeof raw !== "object") return [];
    const item = raw as Record<string, unknown>;
    const id = stringValue(item.id).trim();
    const status = stringValue(item.status) as EnvironmentComponentStatus;
    if (!id || !ENVIRONMENT_COMPONENT_STATUSES.has(status)) return [];
    const count = Number(item.tool_count);
    const error = stringValue(item.error).trim().slice(0, 500);
    const componentVersion = stringValue(item.version).trim().slice(0, 80);
    return [
      {
        kind: stringValue(item.kind) || "mcp",
        id,
        label: stringValue(item.label).trim() || id,
        status,
        toolCount: Number.isFinite(count) && count > 0 ? count : 0,
        ...(componentVersion ? { version: componentVersion } : {}),
        ...(error ? { error } : {}),
      },
    ];
  });
  const version = Number(snapshot.version ?? 1);
  return {
    version: Number.isFinite(version) && version > 0 ? version : 1,
    phase,
    components,
    updatedAt: Date.now(),
  };
}

function normalizeIcon(raw: ModelIconConfig | undefined): ModelIconConfig | undefined {
  if (!raw) return undefined;
  return raw.type === "lobe-icons" && typeof raw.slug === "string"
    ? {
        type: raw.type,
        slug: raw.slug,
      }
    : raw.type === "simple-icons" && typeof raw.slug === "string"
      ? {
          type: raw.type,
          slug: raw.slug,
          ...(typeof raw.src === "string" ? { src: raw.src } : {}),
        }
      : raw.type === "image" && typeof raw.src === "string"
        ? {
            type: raw.type,
            src: raw.src,
          }
        : undefined;
}

function normalizeMetadata(raw: UiMessageMetadata | undefined): UiMessageMetadata | undefined {
  if (!raw) return undefined;
  const icon = normalizeIcon(raw.model_icon);
  const duration =
    typeof raw.duration_ms === "number" && Number.isFinite(raw.duration_ms) && raw.duration_ms >= 0
      ? raw.duration_ms
      : undefined;
  return {
    ...(typeof raw.skill_id === "string" ? { skill_id: raw.skill_id } : {}),
    ...(typeof raw.model_route_id === "string" ? { model_route_id: raw.model_route_id } : {}),
    ...(typeof raw.model_alias === "string" ? { model_alias: raw.model_alias } : {}),
    ...(typeof raw.model_label === "string" ? { model_label: raw.model_label } : {}),
    ...(typeof raw.model_provider === "string" ? { model_provider: raw.model_provider } : {}),
    ...(typeof raw.model_family === "string" ? { model_family: raw.model_family } : {}),
    ...(typeof raw.model_icon_slug === "string" ? { model_icon_slug: raw.model_icon_slug } : {}),
    ...(icon ? { model_icon: icon } : {}),
    ...(duration !== undefined ? { duration_ms: duration } : {}),
  };
}

function normalizePersistedParts(parts: UiPart[] | undefined): UiPart[] {
  const normalized: UiPart[] = [];
  for (const part of parts ?? []) {
    if (part.type === "environment") {
      const environment = parseEnvironmentPreparationSnapshot(part.environment);
      if (environment) normalized.push({ type: "environment", environment });
    } else if (part.type === "session-status") {
      const sessionStatus = parseEnvironmentStatusSnapshot(part.sessionStatus);
      if (sessionStatus) normalized.push({ type: "session-status", sessionStatus });
    } else if (part.type === "memory-recall") {
      const memoryRecall = normalizeMemoryRecallPart(part);
      if (memoryRecall) normalized.push(memoryRecall);
    } else {
      normalized.push(part);
    }
  }
  return normalized;
}

const MEMORY_RECALL_STATUSES = new Set<MemoryRecallStatus>([
  "running",
  "hit",
  "miss",
  "degraded",
  "unavailable",
]);

function normalizeMemoryRecallPart(raw: unknown): UiMemoryRecallPart | null {
  if (!raw || typeof raw !== "object") return null;
  const part = raw as Record<string, unknown>;
  const status = stringValue(part.status) as MemoryRecallStatus;
  if (!MEMORY_RECALL_STATUSES.has(status)) return null;
  const rawCount = Number(part.count);
  const count = Number.isFinite(rawCount) ? Math.max(0, Math.min(100, Math.floor(rawCount))) : 0;
  const errorCode = stringValue(part.errorCode).trim().slice(0, 80);
  return {
    type: "memory-recall",
    status,
    count,
    ...(errorCode ? { errorCode } : {}),
  };
}

function normalizeWireMessages(raw: unknown): UiMessage[] {
  if (!Array.isArray(raw)) return [];
  return (raw as WireMessage[]).map((message, index, messages) => {
    const createdAt = message.created_at ? new Date(message.created_at).getTime() : Date.now();
    const metadata = normalizeMetadata(message.metadata);
    const previous = index > 0 ? messages[index - 1] : undefined;
    const duration =
      message.role === "assistant" && previous?.role === "user"
        ? inferAgentDurationMs(metadata?.duration_ms, previous.created_at, message.created_at)
        : metadata?.duration_ms;
    return {
      id: message.id,
      role: message.role,
      parts: normalizePersistedParts(message.parts),
      metadata:
        duration !== undefined
          ? {
              ...(metadata ?? {}),
              duration_ms: duration,
            }
          : metadata,
      createdAt,
    };
  });
}

function sessionStatusFromParts(parts: UiPart[]): EnvironmentStatus | null {
  return (
    parts.find((part): part is UiSessionStatusPart => part.type === "session-status")
      ?.sessionStatus ?? null
  );
}

function latestSessionStatus(messages: UiMessage[]): EnvironmentStatus | null {
  return messages.reduce<EnvironmentStatus | null>(
    (latest, message) => sessionStatusFromParts(message.parts) ?? latest,
    null,
  );
}

function withSessionStatus(
  statuses: Record<string, EnvironmentStatus>,
  sessionId: string,
  status: EnvironmentStatus | null,
): Record<string, EnvironmentStatus> {
  return status ? { ...statuses, [sessionId]: status } : statuses;
}

function normalizeModelOption(raw: unknown): ModelOption | null {
  if (!raw || typeof raw !== "object") return null;
  const row = raw as Record<string, unknown>;
  const alias = typeof row.alias === "string" ? row.alias : "";
  const id = typeof row.id === "string" ? row.id : alias;
  const label = typeof row.label === "string" ? row.label : "";
  if (!id || !alias || !label) return null;
  const provider = typeof row.provider === "string" ? row.provider : "";
  const family = typeof row.family === "string" ? row.family : "";
  const iconSlug = typeof row.icon_slug === "string" ? row.icon_slug : "";
  const icon = normalizeIcon(row.icon as ModelIconConfig | undefined);
  const normalizedIcon =
    icon?.type === "image" && icon.src
      ? icon
      : iconSlug
        ? { type: "lobe-icons" as const, slug: iconSlug }
        : (icon ?? { type: "lobe-icons" as const, slug: family || provider || alias });
  return {
    id,
    alias,
    label,
    ...(provider ? { provider } : {}),
    ...(family ? { family } : {}),
    ...(iconSlug ? { iconSlug } : {}),
    icon: normalizedIcon,
    protocols: Array.isArray(row.protocols)
      ? row.protocols.filter((value): value is string => typeof value === "string")
      : [],
    isDefault: row.is_default === true,
  };
}

function isAccountDisabledResponse(res: Response): boolean {
  return res.headers.get("x-cocola-auth") === "account-disabled";
}

function redirectAccountDisabled() {
  void signOut({ callbackUrl: "/login?reason=account_disabled" });
}

function stringValue(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function eventTimeMs(primary: unknown, fallback?: unknown): number {
  const raw = stringValue(primary) || stringValue(fallback);
  if (!raw) return Date.now();
  const ms = Date.parse(raw);
  return Number.isFinite(ms) ? ms : Date.now();
}

function hasAssistantResponse(messages: UiMessage[]): boolean {
  return messages.some((m) => m.role === "assistant" && m.parts.length > 0);
}

function isTerminalAgentEvent(event: AgentEvent): boolean {
  return event.kind === "done";
}

// Append text to the trailing text part, or start a new one. Immutable.
function appendTo(parts: UiPart[], kind: "text" | "reasoning", chunk: string): UiPart[] {
  const last = parts[parts.length - 1];
  if (last && last.type === kind) {
    const updated = { ...last, text: last.text + chunk };
    return [...parts.slice(0, -1), updated];
  }
  return [...parts, { type: kind, text: chunk }];
}

// Pair a tool_result back onto its tool_use by id; if unmatched, drop a note.
function fillToolResult(
  parts: UiPart[],
  toolUseId: string,
  content: string,
  isError: boolean,
): UiPart[] {
  let matched = false;
  const next = parts.map((p) => {
    if (p.type === "tool-call" && p.toolCallId === toolUseId) {
      matched = true;
      return { ...p, result: content, isError };
    }
    return p;
  });
  if (matched) return next;
  // Unmatched result: surface as text so nothing is silently lost.
  return appendTo(
    parts,
    "text",
    isError ? `\n[tool error] ${content}\n` : `\n[tool result] ${content}\n`,
  );
}

function upsertEnvironmentPreparation(
  parts: UiPart[],
  environment: EnvironmentPreparationSnapshot,
): UiPart[] {
  const index = parts.findIndex(
    (part) => part.type === "environment" && part.environment.part_id === environment.part_id,
  );
  const next: UiEnvironmentPart = { type: "environment", environment };
  if (index < 0) return [next, ...parts];
  return parts.map((part, partIndex) => (partIndex === index ? next : part));
}

function upsertProgress(parts: UiPart[], id: string, itemsJSON: string): UiPart[] {
  let items: unknown[] = [];
  try {
    const parsed = JSON.parse(itemsJSON) as unknown;
    if (Array.isArray(parsed)) items = parsed;
  } catch {
    return parts;
  }
  const next: UiProgressPart = { type: "progress", progressId: id, items };
  const index = parts.findIndex(
    (part) => part.type === "progress" && part.progressId === next.progressId,
  );
  if (index < 0) return [...parts, next];
  return parts.map((part, partIndex) => (partIndex === index ? next : part));
}

function upsertMemoryRecall(parts: UiPart[], data: Record<string, string>): UiPart[] {
  // Recall emits a transient `running` part before the lookup completes. Keep
  // a miss in that same slot (the renderer hides it) instead of removing the
  // part: deleting it while assistant-ui switches to its completed renderer
  // can leave PartByIndex observing a stale, now out-of-bounds index.
  if (data.status === "skipped") return parts;
  const next = normalizeMemoryRecallPart({
    type: "memory-recall",
    status: data.status,
    count: data.count,
    errorCode: data.error_code,
  });
  if (!next) return parts;
  const index = parts.findIndex((part) => part.type === "memory-recall");
  if (index < 0) return [...parts, next];
  return parts.map((part, partIndex) => (partIndex === index ? next : part));
}

// Reduce a single agent event into the assistant message's parts. Pure.
function reducePart(parts: UiPart[], ev: AgentEvent): UiPart[] {
  const d = ev.data ?? {};
  switch (ev.kind) {
    case "environment_prepare": {
      const environment = parseEnvironmentPreparationSnapshot(d.snapshot);
      return environment ? upsertEnvironmentPreparation(parts, environment) : parts;
    }
    case "memory_recall":
      return upsertMemoryRecall(parts, d);
    case "text":
      return appendTo(parts, "text", d.text ?? "");
    case "thinking":
      return appendTo(parts, "reasoning", d.thinking ?? "");
    case "tool_use":
      return [
        ...parts,
        {
          type: "tool-call",
          toolCallId: d.id || genId(),
          toolName: d.name || "tool",
          argsText: d.input ?? "",
        },
      ];
    case "tool_result":
      return fillToolResult(parts, d.tool_use_id ?? "", d.content ?? "", isTruthy(d.is_error));
    case "file":
      return [
        ...parts,
        {
          type: "file",
          id: d.id || genId(),
          filename: d.filename || "file",
          mimeType: d.mime || d.mimeType || "application/octet-stream",
          size: parseSize(d.size),
          downloadUrl: d.download_url || d.downloadUrl || "",
        },
      ];
    case "progress":
      return upsertProgress(parts, d.id || "todo-list", d.items || "[]");
    case "error":
      return appendTo(parts, "text", `\n\n⚠️ ${d.error ?? "unknown error"}`);
    // result / system / sandbox / done carry no message-body content.
    default:
      return parts; // tolerate unknown kinds — never crash the stream
  }
}

// Map our local message to assistant-ui's ThreadMessageLike.
function convertMessage(message: UiMessage): ThreadMessageLike {
  const environment = message.parts.find(
    (part): part is UiEnvironmentPart => part.type === "environment",
  )?.environment;
  const content = message.parts.flatMap((p) => {
    if (p.type === "environment" || p.type === "session-status") return [];
    if (p.type === "memory-recall") {
      return {
        type: "data" as const,
        name: "memory-recall",
        data: { status: p.status, count: p.count, errorCode: p.errorCode },
      };
    }
    if (p.type === "text") return { type: "text" as const, text: p.text };
    if (p.type === "reasoning") return { type: "reasoning" as const, text: p.text };
    if (p.type === "file") {
      return {
        type: "file" as const,
        filename: p.filename,
        mimeType: p.mimeType,
        data: JSON.stringify({
          id: p.id,
          url: p.downloadUrl,
          size: p.size,
        }),
      };
    }
    if (p.type === "progress") {
      const snapshot = JSON.stringify(p.items);
      return {
        type: "tool-call" as const,
        toolCallId: p.progressId,
        toolName: "Progress",
        argsText: snapshot,
        result: snapshot,
      };
    }
    // tool-call. We pass only argsText (the raw JSON string from the wire) —
    // the renderer displays it verbatim, so there is no need to parse into the
    // typed `args` (and parsing back would fight ReadonlyJSONObject typing).
    return {
      type: "tool-call" as const,
      toolCallId: p.toolCallId,
      toolName: p.toolName,
      argsText: p.argsText,
      ...(p.result !== undefined ? { result: p.result, isError: p.isError } : {}),
    };
  });
  return {
    role: message.role,
    // assistant-ui wants non-empty content; give an empty text part as placeholder
    // while the assistant message is still streaming its first token.
    content: content.length > 0 ? content : [{ type: "text" as const, text: "" }],
    id: message.id,
    createdAt: new Date(message.createdAt),
    metadata: {
      custom: {
        ...(message.metadata ?? {}),
        ...(environment ? { environmentPreparation: environment } : {}),
        ...(environment && content.length === 0 ? { environmentOnly: true } : {}),
      },
    },
  };
}

// ---- Provider ---------------------------------------------------------------

// The base64 adapter is stateless, so a single module-level instance is safe to
// share across renders (avoids re-creating it and thrashing the runtime).
const attachmentAdapter = new Base64AttachmentAdapter();

export function CocolaRuntimeProvider({ children }: { children: ReactNode }) {
  // Message and running state are keyed by session_id. A
  // background stream keeps writing into its own buffer even after navigation.
  const [convMessages, setConvMessages] = useState<Record<string, UiMessage[]>>({});
  const [runningIds, setRunningIds] = useState<Set<string>>(() => new Set());
  // Lazy init: one random session_id per browser tab. NEVER a shared constant
  // ("s1" made every client resume the SAME claude conversation — cross-user
  // context bleed once the session_map is durable, and no way to start over).
  const [sessionId, setSessionId] = useState(genId);
  const [sandboxes, setSandboxes] = useState<Record<string, SandboxInfo | null>>({});
  const [environmentStatuses, setEnvironmentStatuses] = useState<Record<string, EnvironmentStatus>>(
    {},
  );
  const [conversations, setConversations] = useState<ConversationSummary[]>([]);
  const [folders, setFolders] = useState<ConversationFolder[]>([]);
  const [foldersLoaded, setFoldersLoaded] = useState(false);
  const [unreadCompletedIds, setUnreadCompletedIds] = useState<Set<string>>(() => new Set());
  const [selectedArtifact, setSelectedArtifact] = useState<ArtifactPreview | null>(null);
  const [models, setModels] = useState<ModelOption[]>([]);
  const [selectedModelID, setSelectedModelID] = useState("");
  const [modelsLoaded, setModelsLoaded] = useState(false);
  const [runtimes, setRuntimes] = useState<AgentRuntimeOption[]>([]);
  const [selectedRuntimeId, setSelectedRuntimeIdState] = useState("");
  const [runtimesLoaded, setRuntimesLoaded] = useState(false);
  const [skills, setSkills] = useState<SkillOption[]>([]);
  const [skillsLoaded, setSkillsLoaded] = useState(false);
  const [selectedSkillIds, setSelectedSkillIds] = useState<Record<string, string>>({});
  const abortMap = useRef<Map<string, AbortController>>(new Map());
  const runCursors = useRef<Map<string, RunCursor>>(new Map());
  const restoredRuns = useRef(false);
  const sessionIdRef = useRef(sessionId);
  const sessionFolderHintsRef = useRef<Map<string, string>>(new Map());
  const preferredRuntimeIdRef = useRef("");
  const conversationsRef = useRef(conversations);
  const realtimeScheduledRunsRef = useRef<Set<string>>(new Set());
  const deletedScheduledConversationsRef = useRef<Map<string, number>>(new Map());
  const workspaceResetAllowedRef = useRef<Set<string>>(new Set());
  const workspaceResetPromptedRef = useRef<Set<string>>(new Set());

  const messages = useMemo(() => convMessages[sessionId] ?? [], [convMessages, sessionId]);
  const isRunning = runningIds.has(sessionId);
  const sandbox = sandboxes[sessionId] ?? null;
  const environmentStatus = environmentStatuses[sessionId] ?? null;
  const selectedRuntime = useMemo(
    () => runtimes.find((runtime) => runtime.id === selectedRuntimeId) ?? null,
    [runtimes, selectedRuntimeId],
  );
  const compatibleModels = useMemo(
    () =>
      selectedRuntime
        ? models.filter((model) => model.protocols.includes(selectedRuntime.model_protocol))
        : [],
    [models, selectedRuntime],
  );
  const selectedModel = useMemo(
    () =>
      compatibleModels.find((model) => model.id === selectedModelID) ?? compatibleModels[0] ?? null,
    [compatibleModels, selectedModelID],
  );
  const selectedSkill = useMemo(() => {
    const selectedID = selectedSkillIds[sessionId] ?? "";
    return skills.find((skill) => skill.id === selectedID) ?? null;
  }, [selectedSkillIds, sessionId, skills]);
  const runtimeLocked = messages.length > 0 || conversations.some((item) => item.id === sessionId);

  const setSelectedSkillId = useCallback(
    (id: string | null) => {
      if (id && !skills.some((skill) => skill.id === id)) return;
      setSelectedSkillIds((prev) => {
        const next = { ...prev };
        if (id) next[sessionId] = id;
        else delete next[sessionId];
        return next;
      });
    },
    [sessionId, skills],
  );

  const setSelectedRuntimeId = useCallback(
    (id: string) => {
      if (runtimeLocked || !runtimes.some((runtime) => runtime.id === id)) return;
      setSelectedRuntimeIdState(id);
      preferredRuntimeIdRef.current = id;
      try {
        localStorage.setItem("cocola:last-agent-runtime", id);
      } catch {
        // Browser storage is an optional preference only.
      }
    },
    [runtimeLocked, runtimes],
  );

  useEffect(() => {
    if (selectedModel && selectedModel.id !== selectedModelID) {
      setSelectedModelID(selectedModel.id);
    }
  }, [selectedModel, selectedModelID]);

  useEffect(() => {
    const conversation = conversations.find((item) => item.id === sessionId);
    if (conversation?.runtime_id) setSelectedRuntimeIdState(conversation.runtime_id);
  }, [conversations, runtimesLoaded, sessionId]);

  useEffect(() => {
    sessionIdRef.current = sessionId;
  }, [sessionId]);

  useEffect(() => {
    conversationsRef.current = conversations;
  }, [conversations]);

  const openArtifact = useCallback((artifact: ArtifactPreview) => {
    setSelectedArtifact(artifact);
  }, []);

  const closeArtifact = useCallback(() => {
    setSelectedArtifact(null);
  }, []);

  const setRunning = useCallback((targetSessionId: string, on: boolean) => {
    setRunningIds((prev) => {
      const next = new Set(prev);
      if (on) next.add(targetSessionId);
      else next.delete(targetSessionId);
      return next;
    });
  }, []);

  const applyEvent = useCallback((targetSessionId: string, assistantId: string, ev: AgentEvent) => {
    if (ev.kind === "snapshot") {
      let parts: UiPart[] = [];
      try {
        const parsed = JSON.parse(ev.data?.parts ?? "[]") as UiPart[];
        if (Array.isArray(parsed)) parts = normalizePersistedParts(parsed);
      } catch {
        return;
      }
      setEnvironmentStatuses((prev) =>
        withSessionStatus(prev, targetSessionId, sessionStatusFromParts(parts)),
      );
      setConvMessages((prev) => {
        const current = prev[targetSessionId] ?? [];
        const index = current.findIndex((message) => message.id === assistantId);
        const assistant: UiMessage = {
          id: assistantId,
          role: "assistant",
          parts,
          createdAt: Date.now(),
        };
        const next =
          index >= 0
            ? current.map((message, messageIndex) =>
                messageIndex === index ? { ...message, parts } : message,
              )
            : [...current, assistant];
        return { ...prev, [targetSessionId]: next };
      });
      return;
    }
    if (ev.kind === "environment_status") {
      setEnvironmentStatuses((prev) =>
        withSessionStatus(prev, targetSessionId, parseEnvironmentStatus(ev)),
      );
      return;
    }
    if (ev.kind === "done" || ev.kind === "error") {
      setEnvironmentStatuses((prev) => {
        const current = prev[targetSessionId];
        if (!current || current.phase !== "preparing") return prev;
        const components = current.components.map((component) =>
          component.status === "pending"
            ? { ...component, status: "unavailable" as const }
            : component,
        );
        const degraded =
          ev.kind === "error" ||
          components.some((component) =>
            ["failed", "needs-auth", "timeout", "unavailable"].includes(component.status),
          );
        return {
          ...prev,
          [targetSessionId]: {
            ...current,
            phase: degraded ? "degraded" : "ready",
            components,
            updatedAt: Date.now(),
          },
        };
      });
    }
    if (ev.kind === "sandbox") {
      const d = ev.data ?? {};
      setSandboxes((prev) => ({
        ...prev,
        [targetSessionId]: {
          sandboxId: d.sandbox_id ?? "",
          endpoint: d.endpoint ?? "",
          reused: isTruthy(d.reused),
        },
      }));
      return;
    }
    setConvMessages((prev) => {
      const cur = prev[targetSessionId] ?? [];
      const duration =
        ev.kind === "done"
          ? inferAgentDurationMs(ev.data?.duration_ms, undefined, undefined)
          : undefined;
      const next = cur.map((m) => {
        if (m.id !== assistantId) return m;
        return {
          ...m,
          parts: reducePart(m.parts, ev),
          ...(duration !== undefined
            ? { metadata: { ...(m.metadata ?? {}), duration_ms: duration } }
            : {}),
        };
      });
      return { ...prev, [targetSessionId]: next };
    });
  }, []);

  // Pull the sidebar conversation list from the gateway (scoped server-side to
  // the verified identity). Best-effort: a failure just leaves the list as-is.
  const refreshConversations = useCallback(() => {
    void (async () => {
      try {
        const res = await fetch("/api/conversations", { cache: "no-store" });
        if (isAccountDisabledResponse(res)) {
          redirectAccountDisabled();
          return;
        }
        if (!res.ok) return;
        const rows = (await res.json()) as ConversationSummary[];
        if (Array.isArray(rows)) setConversations(rows);
      } catch {
        // ignore — sidebar list is non-critical
      }
    })();
  }, []);

  const refreshFolders = useCallback(() => {
    void (async () => {
      try {
        const res = await fetch("/api/folders", { cache: "no-store" });
        if (isAccountDisabledResponse(res)) {
          redirectAccountDisabled();
          return;
        }
        if (!res.ok) return;
        const rows = (await res.json()) as ConversationFolder[];
        if (Array.isArray(rows)) setFolders(rows);
      } catch {
        // A transient failure should not discard folders already on screen.
      } finally {
        setFoldersLoaded(true);
      }
    })();
  }, []);

  const refreshSkills = useCallback(() => {
    void (async () => {
      try {
        const res = await fetch("/api/skills/effective", { cache: "no-store" });
        if (isAccountDisabledResponse(res)) {
          redirectAccountDisabled();
          return;
        }
        if (!res.ok) return;
        const body = (await res.json()) as { skills?: unknown[] };
        const next = (Array.isArray(body.skills) ? body.skills : [])
          .flatMap((raw): SkillOption[] => {
            if (!raw || typeof raw !== "object") return [];
            const item = raw as Record<string, unknown>;
            const id = stringValue(item.id).trim();
            const name = stringValue(item.name).trim();
            if (!id || !name) return [];
            return [
              {
                id,
                name,
                description: stringValue(item.description).trim(),
                version: stringValue(item.version).trim(),
                scope: stringValue(item.scope).trim(),
              },
            ];
          })
          .sort((left, right) => left.name.localeCompare(right.name));
        const available = new Set(next.map((skill) => skill.id));
        setSkills(next);
        setSelectedSkillIds((prev) =>
          Object.fromEntries(Object.entries(prev).filter(([, id]) => available.has(id))),
        );
      } catch {
        // Keep the last authoritative list during a transient refresh failure.
      } finally {
        setSkillsLoaded(true);
      }
    })();
  }, []);

  const finishRun = useCallback(
    (cursor: RunCursor) => {
      setRunning(cursor.conversationId, false);
      abortMap.current.delete(cursor.conversationId);
      runCursors.current.delete(cursor.conversationId);
      writeRunCursors(runCursors.current);
      if (cursor.conversationId !== sessionIdRef.current) {
        setUnreadCompletedIds((prev) => new Set(prev).add(cursor.conversationId));
      }
      refreshConversations();
    },
    [refreshConversations, setRunning],
  );

  const followRun = useCallback(
    async (cursor: RunCursor, controller: AbortController, initial?: Response) => {
      let response = initial;
      let retryDelay = 250;
      let reconnectAttempts = 0;
      while (!controller.signal.aborted) {
        try {
          response ??= await fetch(`/api/chat/runs/${encodeURIComponent(cursor.runId)}`, {
            cache: "no-store",
            signal: controller.signal,
          });
          if (isAccountDisabledResponse(response)) {
            redirectAccountDisabled();
            return;
          }
          if (!response.ok || !response.body) {
            if (response.status === 404) {
              applyEvent(cursor.conversationId, cursor.assistantId, {
                kind: "error",
                data: { error: "Saved run is unavailable" },
              });
              finishRun(cursor);
              return;
            }
            throw new Error(`run stream unavailable (${response.status})`);
          }

          const reader = response.body.getReader();
          const decoder = new TextDecoder();
          let buffer = "";
          let terminal = false;
          for (;;) {
            const { value, done } = await reader.read();
            if (done) break;
            buffer += decoder.decode(value, { stream: true });
            const parsed = parseFrames(buffer);
            buffer = parsed.rest;
            for (const event of parsed.events) {
              applyEvent(cursor.conversationId, cursor.assistantId, event);
              if (
                event.kind === "error" &&
                event.data?.code === "WORKSPACE_NODE_UNAVAILABLE" &&
                !workspaceResetPromptedRef.current.has(cursor.conversationId)
              ) {
                workspaceResetPromptedRef.current.add(cursor.conversationId);
                const confirmed = window.confirm(
                  "The node holding this Workspace is unavailable. Use an empty Workspace on another node for your next retry? This cannot recover files from the previous node.",
                );
                if (confirmed) {
                  workspaceResetAllowedRef.current.add(cursor.conversationId);
                  applyEvent(cursor.conversationId, cursor.assistantId, {
                    kind: "error",
                    data: {
                      error: "Empty Workspace confirmed. Send the message again to continue.",
                    },
                  });
                } else {
                  workspaceResetPromptedRef.current.delete(cursor.conversationId);
                }
              }
              if (isTerminalAgentEvent(event)) {
                terminal = true;
                break;
              }
            }
            if (terminal) {
              await reader.cancel().catch(() => {});
              break;
            }
          }
          if (terminal || !cursor.runId) {
            finishRun(cursor);
            return;
          }
          response = undefined;
          retryDelay = 250;
        } catch (error) {
          if (controller.signal.aborted) return;
          response = undefined;
          console.warn("chat stream disconnected; reconnecting", error);
        }
        reconnectAttempts += 1;
        if (reconnectAttempts >= RUN_RECONNECT_MAX_ATTEMPTS) {
          applyEvent(cursor.conversationId, cursor.assistantId, {
            kind: "error",
            data: {
              error: "Run connection is unavailable after repeated retries. Refresh to reconnect.",
            },
          });
          abortMap.current.delete(cursor.conversationId);
          runCursors.current.delete(cursor.conversationId);
          writeRunCursors(runCursors.current);
          setRunning(cursor.conversationId, false);
          return;
        }
        await new Promise<void>((resolve) => window.setTimeout(resolve, retryDelay));
        retryDelay = Math.min(retryDelay * 2, 5000);
      }
    },
    [applyEvent, finishRun, setRunning],
  );

  useEffect(() => {
    if (restoredRuns.current) return;
    restoredRuns.current = true;
    const cursors = readRunCursors();
    for (const cursor of cursors) {
      runCursors.current.set(cursor.conversationId, cursor);
      setRunning(cursor.conversationId, true);
      void (async () => {
        try {
          const history = await fetch(
            `/api/conversations/${encodeURIComponent(cursor.conversationId)}/messages`,
            { cache: "no-store" },
          );
          if (history.ok) {
            const loaded = normalizeWireMessages(await history.json());
            setConvMessages((prev) => ({ ...prev, [cursor.conversationId]: loaded }));
            setEnvironmentStatuses((prev) =>
              withSessionStatus(prev, cursor.conversationId, latestSessionStatus(loaded)),
            );
          }
        } catch {
          // The run snapshot below remains sufficient to continue rendering.
        }
        const controller = new AbortController();
        abortMap.current.set(cursor.conversationId, controller);
        await followRun(cursor, controller);
      })();
    }
  }, [followRun, setRunning]);

  const applyUserEvent = useCallback(
    (event: UserEvent, source: "realtime" | "snapshot" = "realtime") => {
      const conversationID =
        event.resource?.kind === "conversation"
          ? stringValue(event.resource.id)
          : stringValue(event.data?.conversation_id);
      if (!conversationID) return;
      if (event.type === "scheduled_task.run.started") {
        const startedAtMs = eventTimeMs(event.data?.run_started_at, event.occurred_at);
        const deletedAtMs = deletedScheduledConversationsRef.current.get(conversationID);
        if (source === "snapshot" && deletedAtMs && startedAtMs <= deletedAtMs) return;
        deletedScheduledConversationsRef.current.delete(conversationID);
        realtimeScheduledRunsRef.current.add(conversationID);
        const title = stringValue(event.data?.title) || "Scheduled task";
        const updatedAt = event.occurred_at || new Date().toISOString();
        setConversations((prev) => {
          const existing = prev.find((c) => c.id === conversationID);
          const rest = prev.filter((c) => c.id !== conversationID);
          return [
            {
              id: conversationID,
              title: existing?.title || title,
              chat_type: existing?.chat_type || "scheduled_task",
              updated_at: updatedAt,
              runtime_id: existing?.runtime_id || "claude-code",
            },
            ...rest,
          ];
        });
        setRunning(conversationID, true);
        setUnreadCompletedIds((prev) => {
          const next = new Set(prev);
          next.delete(conversationID);
          return next;
        });
        return;
      }
      if (event.type === "scheduled_task.run.finished") {
        realtimeScheduledRunsRef.current.delete(conversationID);
        setRunning(conversationID, false);
        if (conversationID !== sessionIdRef.current) {
          setUnreadCompletedIds((prev) => {
            const next = new Set(prev);
            next.add(conversationID);
            return next;
          });
        }
        refreshConversations();
        return;
      }
      if (event.type === "scheduled_task.run.failed") {
        realtimeScheduledRunsRef.current.delete(conversationID);
        setRunning(conversationID, false);
        refreshConversations();
      }
    },
    [refreshConversations, setRunning],
  );

  const applyUserEventSnapshot = useCallback(
    (snapshot: UserEventSnapshot) => {
      const events = snapshot.events ?? [];
      for (const event of events) applyUserEvent(event, "snapshot");
      const runningScheduledConversationIds = new Set(
        events
          .filter((event) => event.type === "scheduled_task.run.started")
          .map((event) =>
            event.resource?.kind === "conversation"
              ? stringValue(event.resource.id)
              : stringValue(event.data?.conversation_id),
          )
          .filter(Boolean),
      );
      setRunningIds((prev) => {
        const scheduledConversationIds = new Set(
          conversationsRef.current
            .filter((conversation) => conversation.chat_type === "scheduled_task")
            .map((conversation) => conversation.id),
        );
        let changed = false;
        const next = new Set(prev);
        for (const id of prev) {
          if (
            (scheduledConversationIds.has(id) || id.startsWith("sched-")) &&
            !runningScheduledConversationIds.has(id)
          ) {
            next.delete(id);
            realtimeScheduledRunsRef.current.delete(id);
            changed = true;
          }
        }
        return changed ? next : prev;
      });
    },
    [applyUserEvent],
  );

  useEffect(() => {
    const events = new EventSource("/api/events");
    events.addEventListener("snapshot", (raw) => {
      try {
        const snapshot = JSON.parse((raw as MessageEvent).data) as UserEventSnapshot;
        applyUserEventSnapshot(snapshot);
      } catch {
        // ignore malformed snapshot frames
      }
    });
    events.addEventListener("user_event", (raw) => {
      try {
        applyUserEvent(JSON.parse((raw as MessageEvent).data) as UserEvent);
      } catch {
        // ignore malformed event frames
      }
    });
    return () => events.close();
  }, [applyUserEvent, applyUserEventSnapshot]);

  const onNew = useCallback(
    async (message: {
      content: readonly { type: string; text?: string }[];
      attachments?: readonly {
        content?: readonly { type: string; filename?: string; data?: string; mimeType?: string }[];
      }[];
    }) => {
      const text = message.content
        .filter(
          (p): p is { type: "text"; text: string } =>
            p.type === "text" && typeof p.text === "string",
        )
        .map((p) => p.text)
        .join("\n");

      // Collect inline file attachments. Our Base64AttachmentAdapter emits a
      // single FileMessagePart per attachment carrying RAW base64 in `data`;
      // flatten them into the push-model wire shape {filename, content_b64, mime}.
      const attachments = (message.attachments ?? []).flatMap((att) =>
        (att.content ?? [])
          .filter(
            (p): p is { type: "file"; filename?: string; data: string; mimeType?: string } =>
              p.type === "file" && typeof p.data === "string",
          )
          .map((p) => ({
            filename: p.filename ?? "file",
            content_b64: p.data,
            mime: p.mimeType ?? "application/octet-stream",
          })),
      );

      const turnSessionId = sessionId;
      const folderHint = sessionFolderHintsRef.current.get(turnSessionId) ?? "";
      const model = selectedModel;
      const agentRuntime = selectedRuntime;
      const turnSkill = selectedSkill;
      if (!model || !agentRuntime) return;
      if (abortMap.current.has(turnSessionId) || runCursors.current.has(turnSessionId)) return;
      setSelectedSkillIds((prev) => {
        if (!(turnSessionId in prev)) return prev;
        const next = { ...prev };
        delete next[turnSessionId];
        return next;
      });
      const isInitialTurn = messages.length === 0;
      const assistantMetadata: UiMessageMetadata = {
        model_route_id: model.id,
        model_alias: model.alias,
        model_label: model.label,
        ...(model.provider ? { model_provider: model.provider } : {}),
        ...(model.family ? { model_family: model.family } : {}),
        ...(model.iconSlug ? { model_icon_slug: model.iconSlug } : {}),
        model_icon: model.icon,
      };
      const userMessageId = genId();
      const assistantId = genId();
      const clientRequestId = genId();
      setConvMessages((prev) => {
        const cur = prev[turnSessionId] ?? [];
        return {
          ...prev,
          [turnSessionId]: [
            ...cur,
            {
              id: userMessageId,
              role: "user",
              parts: [{ type: "text", text }],
              createdAt: Date.now(),
              ...(turnSkill ? { metadata: { skill_id: turnSkill.id } } : {}),
            },
            {
              id: assistantId,
              role: "assistant",
              parts: [],
              createdAt: Date.now(),
              metadata: assistantMetadata,
            },
          ],
        };
      });
      setRunning(turnSessionId, true);
      if (isInitialTurn) {
        setEnvironmentStatuses((prev) => ({
          ...prev,
          [turnSessionId]: {
            version: 1,
            phase: "preparing",
            components: [],
            updatedAt: Date.now(),
          },
        }));
      }
      setUnreadCompletedIds((prev) => {
        const next = new Set(prev);
        next.delete(turnSessionId);
        return next;
      });

      // Surface the conversation immediately in the sidebar, then reconcile
      // with the server's persisted title/updated_at when the stream finishes.
      setConversations((prev) => {
        const now = new Date().toISOString();
        const existing = prev.find((c) => c.id === turnSessionId);
        const rest = prev.filter((c) => c.id !== turnSessionId);
        const title = existing?.title || text.slice(0, 40) || "New Chat";
        return [
          {
            id: turnSessionId,
            title,
            chat_type: existing?.chat_type ?? "chat",
            ...(existing?.folder_id || folderHint
              ? { folder_id: existing?.folder_id || folderHint }
              : {}),
            updated_at: now,
            runtime_id: existing?.runtime_id || agentRuntime.id,
          },
          ...rest,
        ];
      });

      const ctrl = new AbortController();
      abortMap.current.set(turnSessionId, ctrl);
      try {
        const allowWorkspaceReset = workspaceResetAllowedRef.current.has(turnSessionId);
        const requestBody = JSON.stringify({
          prompt: text,
          session_id: turnSessionId,
          client_request_id: clientRequestId,
          runtime_id: agentRuntime.id,
          ...(allowWorkspaceReset ? { allow_workspace_reset: true } : {}),
          ...(turnSkill ? { skill_id: turnSkill.id } : {}),
          model_route_id: model.id,
          model_alias: model.alias,
          model_label: model.label,
          ...(model.provider ? { model_provider: model.provider } : {}),
          ...(model.family ? { model_family: model.family } : {}),
          ...(model.iconSlug ? { model_icon_slug: model.iconSlug } : {}),
          model_icon: model.icon,
          ...(folderHint ? { folder_id: folderHint } : {}),
          ...(attachments.length > 0 ? { attachments } : {}),
        });
        let res: Response | undefined;
        let retryDelay = 250;
        let startAttempts = 0;
        while (!res && !ctrl.signal.aborted && startAttempts < CHAT_START_MAX_ATTEMPTS) {
          startAttempts += 1;
          try {
            const candidate = await fetch("/api/chat", {
              method: "POST",
              headers: { "content-type": "application/json" },
              body: requestBody,
              signal: ctrl.signal,
            });
            if (candidate.status >= 500) {
              await candidate.body?.cancel().catch(() => {});
              if (startAttempts >= CHAT_START_MAX_ATTEMPTS) {
                throw new Error(`chat start unavailable after ${startAttempts} attempts`);
              }
              await new Promise<void>((resolve) => window.setTimeout(resolve, retryDelay));
              retryDelay = Math.min(retryDelay * 2, 5000);
              continue;
            }
            res = candidate;
            if (candidate.ok && allowWorkspaceReset) {
              workspaceResetAllowedRef.current.delete(turnSessionId);
              workspaceResetPromptedRef.current.delete(turnSessionId);
            }
          } catch (error) {
            if (ctrl.signal.aborted) throw error;
            if (startAttempts >= CHAT_START_MAX_ATTEMPTS) throw error;
            await new Promise<void>((resolve) => window.setTimeout(resolve, retryDelay));
            retryDelay = Math.min(retryDelay * 2, 5000);
          }
        }
        if (!res) return;
        if (isAccountDisabledResponse(res)) {
          redirectAccountDisabled();
          return;
        }
        if (res.status === 409) {
          const conflict = (await res.json().catch(() => ({}))) as {
            run_id?: string;
            error?: { code?: string; message?: string };
          };
          if (
            conflict.error?.code === "RUNTIME_MISMATCH" ||
            conflict.error?.code === "FOLDER_MISMATCH"
          ) {
            sessionFolderHintsRef.current.delete(turnSessionId);
            setConvMessages((prev) => ({
              ...prev,
              [turnSessionId]: (prev[turnSessionId] ?? []).filter(
                (message) => message.id !== userMessageId && message.id !== assistantId,
              ),
            }));
            refreshConversations();
            throw new Error(conflict.error.message || "conversation runtime cannot be changed");
          }
          if (conflict.error?.code && conflict.error.code !== "RUN_IN_PROGRESS") {
            throw new Error(conflict.error.message || "chat start conflict");
          }
          const runId = conflict.run_id ?? "";
          if (!runId) throw new Error("conversation already has an active run");
          sessionFolderHintsRef.current.delete(turnSessionId);
          const durableAssistantId = `${runId}-assistant`;
          setConvMessages((prev) => {
            const current = (prev[turnSessionId] ?? []).filter(
              (message) => message.id !== userMessageId && message.id !== assistantId,
            );
            if (current.some((message) => message.id === durableAssistantId)) {
              return { ...prev, [turnSessionId]: current };
            }
            return {
              ...prev,
              [turnSessionId]: [
                ...current,
                {
                  id: durableAssistantId,
                  role: "assistant",
                  parts: [],
                  createdAt: Date.now(),
                },
              ],
            };
          });
          const cursor = {
            conversationId: turnSessionId,
            runId,
            assistantId: durableAssistantId,
          };
          runCursors.current.set(turnSessionId, cursor);
          writeRunCursors(runCursors.current);
          setRunning(turnSessionId, true);
          try {
            const history = await fetch(
              `/api/conversations/${encodeURIComponent(turnSessionId)}/messages`,
              { cache: "no-store", signal: ctrl.signal },
            );
            if (history.ok) {
              const loaded = normalizeWireMessages(await history.json());
              setConvMessages((prev) => ({ ...prev, [turnSessionId]: loaded }));
              setEnvironmentStatuses((prev) =>
                withSessionStatus(prev, turnSessionId, latestSessionStatus(loaded)),
              );
            }
          } catch {
            // Keep the loaded history and continue with the authoritative Run snapshot.
          }
          await followRun(cursor, ctrl);
          return;
        }
        if (!res.ok) {
          const detail = await res.text().catch(() => "");
          throw new Error(`chat start rejected (${res.status})${detail ? `: ${detail}` : ""}`);
        }
        if (!res.body) throw new Error("no response body");
        sessionFolderHintsRef.current.delete(turnSessionId);

        const runId = res.headers.get("x-cocola-run-id") ?? "";
        const durableAssistantId = runId ? `${runId}-assistant` : assistantId;
        if (durableAssistantId !== assistantId) {
          setConvMessages((prev) => ({
            ...prev,
            [turnSessionId]: (prev[turnSessionId] ?? []).map((item) =>
              item.id === assistantId ? { ...item, id: durableAssistantId } : item,
            ),
          }));
        }
        const cursor = {
          conversationId: turnSessionId,
          runId,
          assistantId: durableAssistantId,
        };
        if (runId) {
          runCursors.current.set(turnSessionId, cursor);
          writeRunCursors(runCursors.current);
        }
        await followRun(cursor, ctrl, res);
      } catch (err) {
        if (!(err instanceof DOMException && err.name === "AbortError")) {
          const msg = err instanceof Error ? err.message : String(err);
          applyEvent(turnSessionId, assistantId, { kind: "error", data: { error: msg } });
          setRunning(turnSessionId, false);
          abortMap.current.delete(turnSessionId);
        }
      }
    },
    [
      sessionId,
      selectedModel,
      selectedRuntime,
      selectedSkill,
      messages.length,
      applyEvent,
      followRun,
      refreshConversations,
      setRunning,
    ],
  );

  const onCancel = useCallback(async () => {
    const ctrl = abortMap.current.get(sessionId);
    const cursor = runCursors.current.get(sessionId);
    if (cursor?.runId) {
      try {
        const response = await fetch(`/api/chat/runs/${encodeURIComponent(cursor.runId)}`, {
          method: "DELETE",
        });
        if (isAccountDisabledResponse(response)) {
          redirectAccountDisabled();
          return;
        }
        if (!response.ok) throw new Error(`cancel failed (${response.status})`);
      } catch (error) {
        applyEvent(sessionId, cursor.assistantId, {
          kind: "error",
          data: { error: error instanceof Error ? error.message : String(error) },
        });
        return;
      }
      runCursors.current.delete(sessionId);
      writeRunCursors(runCursors.current);
    }
    ctrl?.abort();
    abortMap.current.delete(sessionId);
    setRunning(sessionId, false);
    setEnvironmentStatuses((prev) => {
      if (prev[sessionId]?.phase !== "preparing") return prev;
      const next = { ...prev };
      delete next[sessionId];
      return next;
    });
  }, [applyEvent, sessionId, setRunning]);

  const connectActiveRun = useCallback(
    async (conversationId: string) => {
      if (abortMap.current.has(conversationId) || runCursors.current.has(conversationId)) return;
      let response: Response;
      try {
        response = await fetch(
          `/api/chat/runs/active?conversation_id=${encodeURIComponent(conversationId)}`,
          { cache: "no-store" },
        );
      } catch {
        return;
      }
      if (response.status === 404 || !response.ok) return;
      const run = (await response.json()) as { run_id?: string };
      if (!run.run_id) return;
      const cursor: RunCursor = {
        conversationId,
        runId: run.run_id,
        assistantId: `${run.run_id}-assistant`,
      };
      runCursors.current.set(conversationId, cursor);
      writeRunCursors(runCursors.current);
      setRunning(conversationId, true);
      setConvMessages((prev) => {
        const current = prev[conversationId] ?? [];
        if (current.some((message) => message.id === cursor.assistantId)) return prev;
        return {
          ...prev,
          [conversationId]: [
            ...current,
            {
              id: cursor.assistantId,
              role: "assistant",
              parts: [],
              createdAt: Date.now(),
            },
          ],
        };
      });
      const controller = new AbortController();
      abortMap.current.set(conversationId, controller);
      await followRun(cursor, controller);
    },
    [followRun, setRunning],
  );

  // Replay a stored conversation into the thread: fetch its messages, map them
  // back into local state, and point session_id at it so a follow-up turn
  // continues the SAME conversation (and lets the backend --resume it).
  const loadConversation = useCallback(
    async (id: string) => {
      const conversation = conversations.find((item) => item.id === id);
      if (conversation?.runtime_id) setSelectedRuntimeIdState(conversation.runtime_id);
      setSandboxes((prev) => ({ ...prev, [id]: prev[id] ?? null }));
      sessionFolderHintsRef.current.delete(sessionIdRef.current);
      sessionFolderHintsRef.current.delete(id);
      sessionIdRef.current = id;
      setSessionId(id);
      setSelectedArtifact(null);
      setUnreadCompletedIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
      const cached = convMessages[id] ?? [];
      if (cached.length > 0) {
        setEnvironmentStatuses((prev) => withSessionStatus(prev, id, latestSessionStatus(cached)));
        if (hasAssistantResponse(cached)) setRunning(id, false);
        void connectActiveRun(id);
        return;
      }
      try {
        const res = await fetch(`/api/conversations/${encodeURIComponent(id)}/messages`);
        if (isAccountDisabledResponse(res)) {
          redirectAccountDisabled();
          return;
        }
        if (!res.ok) {
          void connectActiveRun(id);
          return;
        }
        const loaded = normalizeWireMessages(await res.json());
        setConvMessages((prev) => ({ ...prev, [id]: loaded }));
        setEnvironmentStatuses((prev) => withSessionStatus(prev, id, latestSessionStatus(loaded)));
        if (hasAssistantResponse(loaded)) setRunning(id, false);
        void connectActiveRun(id);
      } catch {
        // ignore — leave the current thread untouched on failure
        void connectActiveRun(id);
      }
    },
    [connectActiveRun, conversations, convMessages, setRunning],
  );

  const renameConversation = useCallback(
    async (id: string, title: string) => {
      const nextTitle = title.trim();
      if (!nextTitle) return;
      setConversations((prev) => prev.map((c) => (c.id === id ? { ...c, title: nextTitle } : c)));
      try {
        const res = await fetch(`/api/conversations/${encodeURIComponent(id)}`, {
          method: "PATCH",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ title: nextTitle }),
        });
        if (isAccountDisabledResponse(res)) {
          redirectAccountDisabled();
          return;
        }
        if (!res.ok) throw new Error(`rename failed (${res.status})`);
        const updated = (await res.json()) as ConversationSummary;
        setConversations((prev) =>
          prev.map((c) =>
            c.id === id
              ? {
                  ...c,
                  title: updated.title || nextTitle,
                  updated_at: updated.updated_at || c.updated_at,
                }
              : c,
          ),
        );
      } catch (err) {
        refreshConversations();
        throw err;
      }
    },
    [refreshConversations],
  );

  const removeLocalConversations = useCallback((ids: string[]) => {
    if (ids.length === 0) return;
    const removed = new Set(ids);
    for (const id of removed) {
      abortMap.current.get(id)?.abort();
      abortMap.current.delete(id);
      runCursors.current.delete(id);
      sessionFolderHintsRef.current.delete(id);
      realtimeScheduledRunsRef.current.delete(id);
      deletedScheduledConversationsRef.current.set(id, Date.now());
    }
    writeRunCursors(runCursors.current);
    setRunningIds((prev) => new Set([...prev].filter((id) => !removed.has(id))));
    setConversations((prev) => prev.filter((conversation) => !removed.has(conversation.id)));
    setUnreadCompletedIds((prev) => new Set([...prev].filter((id) => !removed.has(id))));
    setConvMessages((prev) =>
      Object.fromEntries(Object.entries(prev).filter(([id]) => !removed.has(id))),
    );
    setSandboxes((prev) =>
      Object.fromEntries(Object.entries(prev).filter(([id]) => !removed.has(id))),
    );
    setEnvironmentStatuses((prev) =>
      Object.fromEntries(Object.entries(prev).filter(([id]) => !removed.has(id))),
    );
    setSelectedArtifact((prev) => (prev && removed.has(prev.sessionId) ? null : prev));
    if (removed.has(sessionIdRef.current)) {
      const fresh = genId();
      sessionIdRef.current = fresh;
      setSessionId(fresh);
      setConvMessages((prev) => ({ ...prev, [fresh]: [] }));
      setSandboxes((prev) => ({ ...prev, [fresh]: null }));
    }
  }, []);

  const deleteConversation = useCallback(
    async (id: string) => {
      const res = await fetch(`/api/conversations/${encodeURIComponent(id)}`, {
        method: "DELETE",
      });
      if (isAccountDisabledResponse(res)) {
        redirectAccountDisabled();
        return;
      }
      if (!res.ok) {
        refreshConversations();
        if (res.status === 409) {
          throw new Error("Stop the running answer and wait for it to finish before deleting.");
        }
        throw new Error(`delete failed (${res.status})`);
      }

      removeLocalConversations([id]);
    },
    [refreshConversations, removeLocalConversations],
  );

  const createFolder = useCallback(async (name: string) => {
    const res = await fetch("/api/folders", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ name }),
    });
    if (isAccountDisabledResponse(res)) {
      redirectAccountDisabled();
      throw new Error("Account disabled");
    }
    if (!res.ok) throw await apiError(res, `create folder failed (${res.status})`);
    const folder = (await res.json()) as ConversationFolder;
    setFolders((prev) =>
      [...prev.filter((item) => item.id !== folder.id), folder].sort((a, b) =>
        a.name.localeCompare(b.name, undefined, { sensitivity: "base" }),
      ),
    );
    return folder;
  }, []);

  const renameFolder = useCallback(async (id: string, name: string) => {
    const res = await fetch(`/api/folders/${encodeURIComponent(id)}`, {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ name }),
    });
    if (isAccountDisabledResponse(res)) {
      redirectAccountDisabled();
      throw new Error("Account disabled");
    }
    if (!res.ok) throw await apiError(res, `rename folder failed (${res.status})`);
    const folder = (await res.json()) as ConversationFolder;
    setFolders((prev) =>
      prev
        .map((item) => (item.id === id ? folder : item))
        .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" })),
    );
    return folder;
  }, []);

  const deleteFolder = useCallback(
    async (id: string) => {
      const deletedConversationIDs = conversationsRef.current
        .filter((conversation) => conversation.folder_id === id)
        .map((conversation) => conversation.id);
      const res = await fetch(`/api/folders/${encodeURIComponent(id)}`, { method: "DELETE" });
      if (isAccountDisabledResponse(res)) {
        redirectAccountDisabled();
        throw new Error("Account disabled");
      }
      if (!res.ok) throw await apiError(res, `delete folder failed (${res.status})`);
      setFolders((prev) => prev.filter((folder) => folder.id !== id));
      removeLocalConversations(deletedConversationIDs);
      let deletedPendingSession = false;
      for (const [sessionID, folderID] of sessionFolderHintsRef.current) {
        if (folderID !== id) continue;
        sessionFolderHintsRef.current.delete(sessionID);
        if (sessionID === sessionIdRef.current) deletedPendingSession = true;
      }
      if (deletedPendingSession) {
        const fresh = genId();
        sessionIdRef.current = fresh;
        setSessionId(fresh);
        setConvMessages((prev) => ({ ...prev, [fresh]: [] }));
        setSandboxes((prev) => ({ ...prev, [fresh]: null }));
      }
      refreshConversations();
    },
    [refreshConversations, removeLocalConversations],
  );

  const moveConversation = useCallback(async (id: string, folderId: string | null) => {
    const res = await fetch(`/api/conversations/${encodeURIComponent(id)}/folder`, {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ folder_id: folderId }),
    });
    if (isAccountDisabledResponse(res)) {
      redirectAccountDisabled();
      throw new Error("Account disabled");
    }
    if (!res.ok) throw await apiError(res, `move conversation failed (${res.status})`);
    const updated = (await res.json()) as ConversationSummary;
    sessionFolderHintsRef.current.delete(id);
    setConversations((prev) =>
      prev.map((conversation) =>
        conversation.id === id
          ? {
              ...conversation,
              ...updated,
              folder_id: updated.folder_id || undefined,
            }
          : conversation,
      ),
    );
  }, []);

  // Start a fresh conversation. Other conversations' in-flight streams continue
  // in the background; the fresh session_id prevents backend --resume.
  const newConversation = useCallback(
    (folderId?: string) => {
      const fresh = genId();
      const preferred =
        runtimes.find((runtime) => runtime.id === preferredRuntimeIdRef.current) ??
        runtimes.find((runtime) => runtime.is_default);
      sessionFolderHintsRef.current.delete(sessionIdRef.current);
      sessionIdRef.current = fresh;
      if (folderId) sessionFolderHintsRef.current.set(fresh, folderId);
      else sessionFolderHintsRef.current.delete(fresh);
      setSessionId(fresh);
      setSelectedRuntimeIdState(preferred?.id ?? "");
      setSelectedArtifact(null);
      setConvMessages((prev) => ({ ...prev, [fresh]: [] }));
      setSandboxes((prev) => ({ ...prev, [fresh]: null }));
      return fresh;
    },
    [runtimes],
  );

  // Initial load of the sidebar list.
  useEffect(() => {
    refreshConversations();
    refreshFolders();
    refreshSkills();
  }, [refreshConversations, refreshFolders, refreshSkills]);

  useEffect(() => {
    window.addEventListener("focus", refreshSkills);
    return () => window.removeEventListener("focus", refreshSkills);
  }, [refreshSkills]);

  useEffect(() => {
    void (async () => {
      try {
        const res = await fetch("/api/models");
        if (isAccountDisabledResponse(res)) {
          redirectAccountDisabled();
          return;
        }
        if (!res.ok) return;
        const rows = (await res.json()) as ModelOption[];
        const next = Array.isArray(rows)
          ? rows.flatMap((row) => {
              const model = normalizeModelOption(row);
              return model ? [model] : [];
            })
          : [];
        const fallbackID = next.find((model) => model.isDefault)?.id ?? next[0]?.id ?? "";
        setModels(next);
        setSelectedModelID((prev) => (next.some((m) => m.id === prev) ? prev : fallbackID));
      } catch {
        setModels([]);
        setSelectedModelID("");
      } finally {
        setModelsLoaded(true);
      }
    })();
  }, []);

  useEffect(() => {
    void (async () => {
      try {
        const res = await fetch("/api/agent-runtimes", { cache: "no-store" });
        if (!res.ok) return;
        const rows = (await res.json()) as AgentRuntimeOption[];
        const next = Array.isArray(rows)
          ? rows.filter(
              (item) =>
                item &&
                typeof item.id === "string" &&
                typeof item.label === "string" &&
                typeof item.model_protocol === "string",
            )
          : [];
        setRuntimes(next);
        let preferred = "";
        try {
          preferred = localStorage.getItem("cocola:last-agent-runtime") ?? "";
        } catch {
          // Ignore unavailable browser storage.
        }
        const selected =
          next.find((item) => item.id === preferred) ?? next.find((item) => item.is_default);
        preferredRuntimeIdRef.current = selected?.id ?? "";
        setSelectedRuntimeIdState(selected?.id ?? "");
      } catch {
        setRuntimes([]);
        setSelectedRuntimeIdState("");
      } finally {
        setRuntimesLoaded(true);
      }
    })();
  }, []);

  const runtime = useExternalStoreRuntime<UiMessage>({
    messages,
    isRunning,
    onNew,
    onCancel,
    convertMessage,
    adapters: {
      attachments: attachmentAdapter,
    },
  });

  const ctx = useMemo<CocolaContextValue>(
    () => ({
      sessionId,
      setSessionId,
      sandbox,
      newConversation,
      conversations,
      refreshConversations,
      loadConversation,
      renameConversation,
      deleteConversation,
      folders,
      foldersLoaded,
      refreshFolders,
      createFolder,
      renameFolder,
      deleteFolder,
      moveConversation,
      activeSessionId: sessionId,
      runningSessionIds: runningIds,
      unreadCompletedSessionIds: unreadCompletedIds,
      environmentStatus,
      selectedArtifact,
      openArtifact,
      closeArtifact,
      models: compatibleModels,
      selectedModelID,
      selectedModel,
      modelsLoaded: modelsLoaded && runtimesLoaded,
      setSelectedModelID,
      runtimes,
      selectedRuntime,
      runtimeLocked,
      setSelectedRuntimeId,
      skills,
      skillsLoaded,
      selectedSkill,
      setSelectedSkillId,
    }),
    [
      sessionId,
      sandbox,
      newConversation,
      conversations,
      refreshConversations,
      loadConversation,
      renameConversation,
      deleteConversation,
      folders,
      foldersLoaded,
      refreshFolders,
      createFolder,
      renameFolder,
      deleteFolder,
      moveConversation,
      runningIds,
      unreadCompletedIds,
      environmentStatus,
      selectedArtifact,
      openArtifact,
      closeArtifact,
      compatibleModels,
      selectedModelID,
      selectedModel,
      modelsLoaded,
      runtimesLoaded,
      runtimes,
      selectedRuntime,
      runtimeLocked,
      setSelectedRuntimeId,
      skills,
      skillsLoaded,
      selectedSkill,
      setSelectedSkillId,
    ],
  );

  return (
    <CocolaContext.Provider value={ctx}>
      <AssistantRuntimeProvider runtime={runtime}>{children}</AssistantRuntimeProvider>
    </CocolaContext.Provider>
  );
}
