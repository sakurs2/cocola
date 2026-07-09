"use client";

import {
  AlertTriangle,
  Bot,
  Check,
  CopyIcon,
  Loader2,
  RefreshCw,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { MarkdownContent } from "@/components/assistant-ui/markdown-text";
import {
  RailFile,
  RailReasoning,
  RailText,
  RailTool,
} from "@/components/assistant-ui/rail";
import { ModelIcon } from "@/components/assistant-ui/thread";
import { cn } from "@/lib/utils";
import { type ModelIconConfig } from "@/app/runtime-provider";

type ToolPart = {
  type: "tool-call";
  toolCallId?: string;
  toolName?: string;
  argsText?: string;
  result?: string;
  isError?: boolean;
};

type FilePart = {
  type: "file";
  id?: string;
  filename?: string;
  mimeType?: string;
  mime?: string;
  size?: number;
  downloadUrl?: string;
  download_url?: string;
};

type MessagePart =
  | { type: "text"; text?: string }
  | { type: "reasoning"; text?: string }
  | ToolPart
  | FilePart;

type WireMessage = {
  id: string;
  role: "user" | "assistant";
  parts?: MessagePart[];
  metadata?: {
    model_label?: string;
    model_alias?: string;
    model_icon?: ModelIconConfig;
  };
  created_at?: string;
};

type LoadState =
  | { status: "loading"; messages: WireMessage[]; error: "" }
  | { status: "ready"; messages: WireMessage[]; error: "" }
  | { status: "error"; messages: WireMessage[]; error: string };

export function ConversationReadOnly({ conversationId }: { conversationId: string }) {
  const [state, setState] = useState<LoadState>({
    status: "loading",
    messages: [],
    error: "",
  });
  const [refreshing, setRefreshing] = useState(false);
  const refreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const load = useCallback(async () => {
    const startedAt = Date.now();
    if (refreshTimerRef.current) clearTimeout(refreshTimerRef.current);
    setRefreshing(true);
    setState((prev) => ({ status: "loading", messages: prev.messages, error: "" }));
    try {
      const res = await fetch(`/api/conversations/${encodeURIComponent(conversationId)}/messages`, {
        cache: "no-store",
      });
      if (!res.ok) throw new Error(await errorText(res));
      const rows = (await res.json()) as WireMessage[];
      setState({ status: "ready", messages: Array.isArray(rows) ? rows : [], error: "" });
    } catch (err) {
      setState({
        status: "error",
        messages: [],
        error: err instanceof Error ? err.message : String(err),
      });
    } finally {
      const remaining = Math.max(450 - (Date.now() - startedAt), 0);
      refreshTimerRef.current = setTimeout(() => {
        setRefreshing(false);
        refreshTimerRef.current = null;
      }, remaining);
    }
  }, [conversationId]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    return () => {
      if (refreshTimerRef.current) clearTimeout(refreshTimerRef.current);
    };
  }, []);

  return (
    <main className="min-h-screen bg-background text-foreground">
      <header className="sticky top-0 z-20 border-b border-border bg-background/95 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-4xl items-center gap-3 px-4">
          <div className="grid size-8 place-items-center rounded-md bg-primary text-primary-foreground">
            <Bot className="size-4" />
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-sm font-semibold">Conversation</h1>
            <p className="truncate font-mono text-xs text-muted-foreground">{conversationId}</p>
          </div>
          <button
            type="button"
            title="Refresh"
            aria-label="Refresh"
            onClick={() => void load()}
            className="inline-flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
          >
            <RefreshCw className={cn("size-4", refreshing && "animate-spin")} />
          </button>
        </div>
      </header>

      <div className="mx-auto flex max-w-4xl flex-col gap-4 px-4 py-6">
        {state.status === "loading" && state.messages.length === 0 ? (
          <div className="flex items-center justify-center gap-2 py-16 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" />
            Loading conversation
          </div>
        ) : null}

        {state.status === "error" ? (
          <div className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            <AlertTriangle className="mt-0.5 size-4 shrink-0" />
            <span className="min-w-0">{state.error}</span>
          </div>
        ) : null}

        {state.status === "ready" && state.messages.length === 0 ? (
          <div className="rounded-lg border border-border bg-card px-4 py-10 text-center text-sm text-muted-foreground">
            No messages in this conversation.
          </div>
        ) : null}

        <div
          className="flex flex-col items-center"
          style={{ ["--thread-max-width" as string]: "52rem" }}
        >
          {state.messages.map((message) => (
            <MessageBubble key={message.id} message={message} />
          ))}
        </div>
      </div>
    </main>
  );
}

function MessageBubble({ message }: { message: WireMessage }) {
  const isUser = message.role === "user";
  const parts = message.parts ?? [];

  if (isUser) {
    return (
      <article className="grid w-full max-w-[var(--thread-max-width)] auto-rows-auto grid-cols-[minmax(72px,1fr)_auto] gap-y-1 py-3">
        <div className="col-start-2 row-start-1 flex flex-col items-end gap-1.5">
          <div className="max-w-[calc(var(--thread-max-width)*0.8)] whitespace-pre-wrap break-words rounded-2xl bg-muted px-4 py-2 text-sm text-foreground">
            {parts.length > 0 ? (
              parts.map((part, index) => (
                <MessagePartView key={`${message.id}-${index}`} part={part} role={message.role} />
              ))
            ) : (
              <span className="text-muted-foreground">No content</span>
            )}
          </div>
        </div>
      </article>
    );
  }

  return (
    <article className="relative grid w-full max-w-[var(--thread-max-width)] grid-cols-[auto_1fr] grid-rows-[auto_1fr] py-3">
      <div className="col-span-2 col-start-1 row-start-1 my-1.5 max-w-full break-words leading-7 text-foreground">
        <AssistantHeader message={message} />
        <div>
          {parts.length > 0 ? (
            parts.map((part, index) => (
              <MessagePartView key={`${message.id}-${index}`} part={part} role={message.role} />
            ))
          ) : (
            <TypingDots />
          )}
        </div>
      </div>
      <CopyMessageButton message={message} />
    </article>
  );
}

function AssistantHeader({ message }: { message: WireMessage }) {
  const label = message.metadata?.model_label || message.metadata?.model_alias || "Model";
  const icon = message.metadata?.model_icon;

  return (
    <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
      <span className="inline-flex min-w-0 items-center gap-1.5 rounded-full border border-border bg-muted/35 px-2 py-1">
        <ModelIcon icon={icon} className="size-4" />
        <span className="truncate font-medium text-foreground">{label}</span>
      </span>
      {message.created_at ? <span>{formatDate(message.created_at)}</span> : null}
    </div>
  );
}

function MessagePartView({ part, role }: { part: MessagePart; role: "user" | "assistant" }) {
  if (part.type === "text") {
    // User text stays inside the bubble as plain text; assistant text renders as
    // a rail "回答" node with markdown, identical to the live thread.
    if (role === "user") {
      return <span>{part.text ?? ""}</span>;
    }
    return (
      <RailText>
        <MarkdownContent value={part.text ?? ""} />
      </RailText>
    );
  }
  if (part.type === "reasoning") {
    return <RailReasoning text={part.text ?? ""} />;
  }
  if (part.type === "tool-call") {
    return (
      <RailTool
        toolName={part.toolName || "tool"}
        argsText={part.argsText}
        result={part.result}
        isError={part.isError}
      />
    );
  }
  if (part.type === "file") {
    // Read-only page has no Artifact side panel, so omit onPreview → download only.
    return (
      <RailFile
        filename={part.filename || "file"}
        mimeType={part.mimeType || part.mime || "application/octet-stream"}
        size={part.size ?? 0}
        downloadUrl={part.downloadUrl || part.download_url || ""}
      />
    );
  }
  return null;
}

function CopyMessageButton({ message }: { message: WireMessage }) {
  const [copied, setCopied] = useState(false);
  const text = messageText(message);

  const copy = async () => {
    if (!text) return;
    await navigator.clipboard.writeText(text);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  };

  return (
    <div className="col-start-1 row-start-2 -ml-1 flex gap-1 text-muted-foreground">
      <button
        type="button"
        title={copied ? "Copied" : "Copy"}
        aria-label={copied ? "Copied" : "Copy"}
        disabled={!text}
        onClick={() => void copy()}
        className="inline-flex size-8 items-center justify-center rounded-md transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-40"
      >
        {copied ? <Check className="size-4 text-emerald-400" /> : <CopyIcon className="size-4" />}
      </button>
    </div>
  );
}

function TypingDots() {
  return (
    <div className="flex items-center gap-1 py-1" role="status" aria-label="Assistant is typing">
      <span className="size-2 animate-bounce rounded-full bg-muted-foreground/60 [animation-delay:-0.3s]" />
      <span className="size-2 animate-bounce rounded-full bg-muted-foreground/60 [animation-delay:-0.15s]" />
      <span className="size-2 animate-bounce rounded-full bg-muted-foreground/60" />
    </div>
  );
}

function messageText(message: WireMessage): string {
  return (message.parts ?? [])
    .map((part) => {
      if (part.type === "text" || part.type === "reasoning") return part.text ?? "";
      if (part.type === "tool-call") {
        return [
          part.toolName ? `[tool] ${part.toolName}` : "[tool]",
          part.argsText ? `Arguments:\n${formatPayload(part.argsText)}` : "",
          part.result !== undefined ? `Result:\n${formatPayload(part.result)}` : "",
        ]
          .filter(Boolean)
          .join("\n");
      }
      if (part.type === "file") {
        return `[file] ${part.filename || "file"} ${part.downloadUrl || part.download_url || ""}`.trim();
      }
      return "";
    })
    .filter(Boolean)
    .join("\n\n");
}

function formatPayload(value: unknown): string {
  if (typeof value === "string") {
    const trimmed = value.trim();
    if (!trimmed) return "";
    try {
      return JSON.stringify(JSON.parse(trimmed), null, 2);
    } catch {
      return value;
    }
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function formatDate(value: string) {
  const ms = Date.parse(value);
  if (!Number.isFinite(ms)) return "";
  return new Intl.DateTimeFormat(undefined, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(ms));
}

async function errorText(res: Response) {
  try {
    const body = (await res.json()) as { error?: string | { message?: string } };
    if (typeof body.error === "string") return body.error;
    if (body.error?.message) return body.error.message;
  } catch {
    // fall through
  }
  return `${res.status} ${res.statusText}`;
}
