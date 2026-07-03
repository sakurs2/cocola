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
import { parseFrames, type AgentEvent } from "@/lib/sse";
import { Base64AttachmentAdapter } from "@/lib/base64-attachment-adapter";

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
  conversationId: string;
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

type UiPart =
  | { type: "text"; text: string }
  | { type: "reasoning"; text: string }
  | UiToolCall
  | UiFilePart;

type UiMessage = {
  id: string;
  role: "user" | "assistant";
  parts: UiPart[];
  createdAt: number;
  metadata?: UiMessageMetadata;
};

export type SandboxInfo = {
  sandboxId: string;
  endpoint: string;
  reused: boolean;
};

// One row in the sidebar conversation list (gateway GET /v1/conversations).
export type ConversationSummary = {
  id: string;
  title: string;
  updated_at: string;
};

export type ModelIconConfig = {
  type: "simple-icons" | "image";
  slug: string;
  src?: string;
};

export type ModelOption = {
  alias: string;
  label: string;
  icon: ModelIconConfig;
};

export type UiMessageMetadata = {
  model_alias?: string;
  model_label?: string;
  model_icon?: ModelIconConfig;
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
  newConversation: () => void;
  // Persisted conversation list (sidebar) + loaders. conversations is refreshed
  // after each turn and on mount; loadConversation replays a stored conversation
  // into the thread and points session_id at it so follow-ups continue it.
  conversations: ConversationSummary[];
  refreshConversations: () => void;
  loadConversation: (id: string) => Promise<void>;
  renameConversation: (id: string, title: string) => Promise<void>;
  deleteConversation: (id: string) => Promise<void>;
  activeConversationId: string;
  runningConversationIds: Set<string>;
  unreadCompletedConversationIds: Set<string>;
  selectedArtifact: ArtifactPreview | null;
  openArtifact: (artifact: ArtifactPreview) => void;
  closeArtifact: () => void;
  models: ModelOption[];
  selectedModelAlias: string;
  selectedModel: ModelOption;
  setSelectedModelAlias: (alias: string) => void;
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

function isTruthy(v: string | undefined): boolean {
  return v === "true" || v === "True" || v === "1";
}

function parseSize(v: string | undefined): number {
  const n = Number.parseInt(v ?? "", 10);
  return Number.isFinite(n) && n > 0 ? n : 0;
}

function normalizeMetadata(raw: UiMessageMetadata | undefined): UiMessageMetadata | undefined {
  if (!raw) return undefined;
  return {
    ...(typeof raw.model_alias === "string" ? { model_alias: raw.model_alias } : {}),
    ...(typeof raw.model_label === "string" ? { model_label: raw.model_label } : {}),
    ...((raw.model_icon?.type === "simple-icons" || raw.model_icon?.type === "image") &&
    typeof raw.model_icon.slug === "string"
      ? {
          model_icon: {
            type: raw.model_icon.type,
            slug: raw.model_icon.slug,
            ...(typeof raw.model_icon.src === "string" ? { src: raw.model_icon.src } : {}),
          },
        }
      : {}),
  };
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

// Reduce a single agent event into the assistant message's parts. Pure.
function reducePart(parts: UiPart[], ev: AgentEvent): UiPart[] {
  const d = ev.data ?? {};
  switch (ev.kind) {
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
    case "error":
      return appendTo(parts, "text", `\n\n⚠️ ${d.error ?? "unknown error"}`);
    // result / system / sandbox / done carry no message-body content.
    default:
      return parts; // tolerate unknown kinds — never crash the stream
  }
}

// Map our local message to assistant-ui's ThreadMessageLike.
function convertMessage(message: UiMessage): ThreadMessageLike {
  const content = message.parts.map((p) => {
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
      custom: message.metadata ?? {},
    },
  };
}

// ---- Provider ---------------------------------------------------------------

// The base64 adapter is stateless, so a single module-level instance is safe to
// share across renders (avoids re-creating it and thrashing the runtime).
const attachmentAdapter = new Base64AttachmentAdapter();

const DEFAULT_MODEL: ModelOption = {
  alias: "cocola-default",
  label: "Cocola Default",
  icon: { type: "simple-icons", slug: "anthropic" },
};

export function CocolaRuntimeProvider({ children }: { children: ReactNode }) {
  // Message and running state are keyed by conversation id (session_id). A
  // background stream keeps writing into its own buffer even after navigation.
  const [convMessages, setConvMessages] = useState<Record<string, UiMessage[]>>({});
  const [runningIds, setRunningIds] = useState<Set<string>>(() => new Set());
  // Lazy init: one random session_id per browser tab. NEVER a shared constant
  // ("s1" made every client resume the SAME claude conversation — cross-user
  // context bleed once the session_map is durable, and no way to start over).
  const [sessionId, setSessionId] = useState(genId);
  const [sandboxes, setSandboxes] = useState<Record<string, SandboxInfo | null>>({});
  const [conversations, setConversations] = useState<ConversationSummary[]>([]);
  const [unreadCompletedIds, setUnreadCompletedIds] = useState<Set<string>>(() => new Set());
  const [selectedArtifact, setSelectedArtifact] = useState<ArtifactPreview | null>(null);
  const [models, setModels] = useState<ModelOption[]>([DEFAULT_MODEL]);
  const [selectedModelAlias, setSelectedModelAlias] = useState(DEFAULT_MODEL.alias);
  const abortMap = useRef<Map<string, AbortController>>(new Map());
  const sessionIdRef = useRef(sessionId);

  const messages = useMemo(() => convMessages[sessionId] ?? [], [convMessages, sessionId]);
  const isRunning = runningIds.has(sessionId);
  const sandbox = sandboxes[sessionId] ?? null;
  const selectedModel = useMemo(
    () => models.find((m) => m.alias === selectedModelAlias) ?? models[0] ?? DEFAULT_MODEL,
    [models, selectedModelAlias],
  );

  useEffect(() => {
    sessionIdRef.current = sessionId;
  }, [sessionId]);

  const openArtifact = useCallback((artifact: ArtifactPreview) => {
    setSelectedArtifact(artifact);
  }, []);

  const closeArtifact = useCallback(() => {
    setSelectedArtifact(null);
  }, []);

  const setRunning = useCallback((convId: string, on: boolean) => {
    setRunningIds((prev) => {
      const next = new Set(prev);
      if (on) next.add(convId);
      else next.delete(convId);
      return next;
    });
  }, []);

  const applyEvent = useCallback((convId: string, assistantId: string, ev: AgentEvent) => {
    if (ev.kind === "sandbox") {
      const d = ev.data ?? {};
      setSandboxes((prev) => ({
        ...prev,
        [convId]: {
          sandboxId: d.sandbox_id ?? "",
          endpoint: d.endpoint ?? "",
          reused: isTruthy(d.reused),
        },
      }));
      return;
    }
    setConvMessages((prev) => {
      const cur = prev[convId] ?? [];
      const next = cur.map((m) =>
        m.id === assistantId ? { ...m, parts: reducePart(m.parts, ev) } : m,
      );
      return { ...prev, [convId]: next };
    });
  }, []);

  // Pull the sidebar conversation list from the gateway (scoped server-side to
  // the verified identity). Best-effort: a failure just leaves the list as-is.
  const refreshConversations = useCallback(() => {
    void (async () => {
      try {
        const res = await fetch("/api/conversations");
        if (!res.ok) return;
        const rows = (await res.json()) as ConversationSummary[];
        if (Array.isArray(rows)) setConversations(rows);
      } catch {
        // ignore — sidebar list is non-critical
      }
    })();
  }, []);

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

      const convId = sessionId;
      const model = selectedModel;
      const assistantMetadata: UiMessageMetadata = {
        model_alias: model.alias,
        model_label: model.label,
        model_icon: model.icon,
      };
      const assistantId = genId();
      setConvMessages((prev) => {
        const cur = prev[convId] ?? [];
        return {
          ...prev,
          [convId]: [
            ...cur,
            { id: genId(), role: "user", parts: [{ type: "text", text }], createdAt: Date.now() },
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
      setRunning(convId, true);
      setUnreadCompletedIds((prev) => {
        const next = new Set(prev);
        next.delete(convId);
        return next;
      });

      // Surface the conversation immediately in the sidebar, then reconcile
      // with the server's persisted title/updated_at when the stream finishes.
      setConversations((prev) => {
        const now = new Date().toISOString();
        const existing = prev.find((c) => c.id === convId);
        const rest = prev.filter((c) => c.id !== convId);
        const title = existing?.title || text.slice(0, 40) || "New Chat";
        return [{ id: convId, title, updated_at: now }, ...rest];
      });

      const ctrl = new AbortController();
      abortMap.current.set(convId, ctrl);
      let aborted = false;
      try {
        const res = await fetch("/api/chat", {
          method: "POST",
          headers: {
            "content-type": "application/json",
          },
          body: JSON.stringify({
            prompt: text,
            session_id: convId,
            model_alias: model.alias,
            model_label: model.label,
            model_icon: model.icon,
            ...(attachments.length > 0 ? { attachments } : {}),
          }),
          signal: ctrl.signal,
        });
        if (!res.body) throw new Error("no response body");

        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          const { events, rest } = parseFrames(buffer);
          buffer = rest;
          for (const ev of events) applyEvent(convId, assistantId, ev);
        }
      } catch (err) {
        if (!(err instanceof DOMException && err.name === "AbortError")) {
          const msg = err instanceof Error ? err.message : String(err);
          applyEvent(convId, assistantId, { kind: "error", data: { error: msg } });
        } else {
          aborted = true;
        }
      } finally {
        setRunning(convId, false);
        abortMap.current.delete(convId);
        if (!aborted) {
          setUnreadCompletedIds((prev) => {
            const next = new Set(prev);
            next.add(convId);
            return next;
          });
        }
        refreshConversations();
      }
    },
    [sessionId, selectedModel, applyEvent, refreshConversations, setRunning],
  );

  const onCancel = useCallback(async () => {
    const ctrl = abortMap.current.get(sessionId);
    ctrl?.abort();
    abortMap.current.delete(sessionId);
    setRunning(sessionId, false);
  }, [sessionId, setRunning]);

  // Replay a stored conversation into the thread: fetch its messages, map them
  // back into local state, and point session_id at it so a follow-up turn
  // continues the SAME conversation (and lets the backend --resume it).
  const loadConversation = useCallback(
    async (id: string) => {
      setSandboxes((prev) => ({ ...prev, [id]: prev[id] ?? null }));
      setSessionId(id);
      setSelectedArtifact(null);
      setUnreadCompletedIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
      if ((convMessages[id]?.length ?? 0) > 0) return;
      try {
        const res = await fetch(`/api/conversations/${encodeURIComponent(id)}/messages`);
        if (!res.ok) return;
        const rows = (await res.json()) as WireMessage[];
        const loaded: UiMessage[] = (Array.isArray(rows) ? rows : []).map((m) => ({
          id: m.id,
          role: m.role,
          parts: m.parts ?? [],
          metadata: normalizeMetadata(m.metadata),
          createdAt: m.created_at ? new Date(m.created_at).getTime() : Date.now(),
        }));
        setConvMessages((prev) => ({ ...prev, [id]: loaded }));
      } catch {
        // ignore — leave the current thread untouched on failure
      }
    },
    [convMessages],
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

  const deleteConversation = useCallback(
    async (id: string) => {
      const ctrl = abortMap.current.get(id);
      ctrl?.abort();
      abortMap.current.delete(id);
      setRunning(id, false);

      const res = await fetch(`/api/conversations/${encodeURIComponent(id)}`, {
        method: "DELETE",
      });
      if (!res.ok) {
        refreshConversations();
        throw new Error(`delete failed (${res.status})`);
      }

      setConversations((prev) => prev.filter((c) => c.id !== id));
      setUnreadCompletedIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
      setConvMessages((prev) => {
        const next = { ...prev };
        delete next[id];
        return next;
      });
      setSandboxes((prev) => {
        const next = { ...prev };
        delete next[id];
        return next;
      });
      setSelectedArtifact((prev) => (prev?.conversationId === id ? null : prev));
      if (sessionIdRef.current === id) {
        const fresh = genId();
        sessionIdRef.current = fresh;
        setSessionId(fresh);
        setConvMessages((prev) => ({ ...prev, [fresh]: [] }));
        setSandboxes((prev) => ({ ...prev, [fresh]: null }));
      }
    },
    [refreshConversations, setRunning],
  );

  // Start a fresh conversation. Other conversations' in-flight streams continue
  // in the background; the fresh session_id prevents backend --resume.
  const newConversation = useCallback(() => {
    const fresh = genId();
    setSessionId(fresh);
    setSelectedArtifact(null);
    setConvMessages((prev) => ({ ...prev, [fresh]: [] }));
    setSandboxes((prev) => ({ ...prev, [fresh]: null }));
  }, []);

  // Initial load of the sidebar list.
  useEffect(() => {
    refreshConversations();
  }, [refreshConversations]);

  useEffect(() => {
    void (async () => {
      try {
        const res = await fetch("/api/models");
        if (!res.ok) return;
        const rows = (await res.json()) as ModelOption[];
        const next = Array.isArray(rows) && rows.length > 0 ? rows : [DEFAULT_MODEL];
        const fallbackAlias = next[0]?.alias ?? DEFAULT_MODEL.alias;
        setModels(next);
        setSelectedModelAlias((prev) =>
          next.some((m) => m.alias === prev) ? prev : fallbackAlias,
        );
      } catch {
        // Keep the built-in fallback model; model discovery is non-critical.
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
      activeConversationId: sessionId,
      runningConversationIds: runningIds,
      unreadCompletedConversationIds: unreadCompletedIds,
      selectedArtifact,
      openArtifact,
      closeArtifact,
      models,
      selectedModelAlias,
      selectedModel,
      setSelectedModelAlias,
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
      runningIds,
      unreadCompletedIds,
      selectedArtifact,
      openArtifact,
      closeArtifact,
      models,
      selectedModelAlias,
      selectedModel,
    ],
  );

  return (
    <CocolaContext.Provider value={ctx}>
      <AssistantRuntimeProvider runtime={runtime}>{children}</AssistantRuntimeProvider>
    </CocolaContext.Provider>
  );
}
