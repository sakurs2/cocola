"use client";

import { AlertTriangle, Bot, Check, CopyIcon, Loader2, RefreshCw } from "lucide-react";
import { Button, Card, Tooltip } from "@heroui/react";
import { EmptyState } from "@heroui-pro/react/empty-state";
import { useCallback, useEffect, useRef, useState } from "react";
import { AnswerMarkdownContent, MarkdownContent } from "@/components/assistant-ui/markdown-text";
import {
  RailEnvironment,
  RailFile,
  RailMemoryRecall,
  RailProgress,
  RailProcessSummary,
  RailReasoning,
  RailSCMApproval,
  RailText,
  RailTool,
} from "@/components/assistant-ui/rail";
import { parseEnvironmentPreparationSnapshot } from "@/lib/environment";
import { ModelIcon } from "@/components/ui/model-icon";
import {
  QuestionCard,
  RunSummary,
  StructuredResultCard,
} from "@/components/assistant-ui/rich-message-parts";
import { cn } from "@/lib/utils";
import { type ModelIconConfig } from "@/lib/model-icons";
import {
  normalizeRichMessagePart,
  type UiQuestionPart,
  type UiRunSummaryPart,
  type UiStructuredResultPart,
} from "@/lib/rich-message-normalization";
import {
  finalAgentOutputText,
  inferAgentDurationMs,
  splitAgentTurnParts,
} from "@/lib/agent-turn-summary.mjs";

type ToolPart = {
  type: "tool-call";
  toolCallId?: string;
  toolName?: string;
  argsText?: string;
  result?: string;
  isError?: boolean;
  outcome?: string;
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

type EnvironmentPart = {
  type: "environment";
  environment?: unknown;
};

type ProgressPart = {
  type: "progress";
  progressId?: string;
  items?: unknown[];
};

type MemoryRecallPart = {
  type: "memory-recall";
  status?: "running" | "hit" | "miss" | "degraded" | "unavailable";
  count?: number;
  content?: string;
};

type SCMApprovalPart = {
  type: "scm-approval";
  approvalId?: string;
  approvalStatus?: "pending" | "approved" | "denied" | "expired";
  approvalCategory?: string;
  approvalLabel?: string;
};

type PlanPart = {
  type: "plan";
  planId: string;
  version: number;
  status: string;
  contentMarkdown: string;
};

type MessagePart =
  | { type: "text"; text?: string }
  | { type: "reasoning"; text?: string }
  | ToolPart
  | FilePart
  | EnvironmentPart
  | ProgressPart
  | MemoryRecallPart
  | SCMApprovalPart
  | PlanPart
  | UiQuestionPart
  | UiRunSummaryPart
  | UiStructuredResultPart;

type WireMessage = {
  id: string;
  role: "user" | "assistant";
  parts?: MessagePart[];
  metadata?: {
    model_label?: string;
    model_alias?: string;
    model_icon?: ModelIconConfig;
    duration_ms?: number;
  };
  created_at?: string;
};

type LoadState =
  | { status: "loading"; messages: WireMessage[]; error: "" }
  | { status: "ready"; messages: WireMessage[]; error: "" }
  | { status: "error"; messages: WireMessage[]; error: string };

function normalizeReadonlyMessages(raw: unknown): WireMessage[] {
  if (!Array.isArray(raw)) return [];
  return raw.flatMap((rawMessage): WireMessage[] => {
    if (!rawMessage || typeof rawMessage !== "object") return [];
    const message = rawMessage as Record<string, unknown>;
    const id = typeof message.id === "string" ? message.id : "";
    const role = message.role === "user" || message.role === "assistant" ? message.role : null;
    if (!id || role === null) return [];
    const parts = (Array.isArray(message.parts) ? message.parts : []).flatMap(
      (rawPart): MessagePart[] => {
        const richPart = normalizeRichMessagePart(rawPart);
        if (richPart !== undefined) return richPart === null ? [] : [richPart];
        if (!rawPart || typeof rawPart !== "object") return [];
        return typeof (rawPart as Record<string, unknown>).type === "string"
          ? [rawPart as MessagePart]
          : [];
      },
    );
    return [
      {
        id,
        role,
        parts,
        ...(message.metadata && typeof message.metadata === "object"
          ? { metadata: message.metadata as WireMessage["metadata"] }
          : {}),
        ...(typeof message.created_at === "string" ? { created_at: message.created_at } : {}),
      },
    ];
  });
}

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
      const rows = normalizeReadonlyMessages(await res.json());
      setState({ status: "ready", messages: rows, error: "" });
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
          <div className="grid size-8 place-items-center rounded-md bg-accent text-accent-foreground">
            <Bot className="size-4" />
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-sm font-semibold">Conversation</h1>
            <p className="truncate font-mono text-xs text-muted">{conversationId}</p>
          </div>
          <Tooltip delay={0}>
            <Button
              isIconOnly
              aria-label="Refresh"
              className="text-muted size-8 min-w-8"
              variant="ghost"
              onPress={() => void load()}
            >
              <RefreshCw className={cn("size-4", refreshing && "animate-spin")} />
            </Button>
            <Tooltip.Content>{refreshing ? "Refreshing…" : "Refresh"}</Tooltip.Content>
          </Tooltip>
        </div>
      </header>

      <div className="mx-auto flex max-w-4xl flex-col gap-4 px-4 py-6">
        {state.status === "loading" && state.messages.length === 0 ? (
          <div className="flex items-center justify-center gap-2 py-16 text-sm text-muted">
            <Loader2 className="size-4 animate-spin" />
            Loading conversation
          </div>
        ) : null}

        {state.status === "error" ? (
          <div className="flex items-start gap-2 rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">
            <AlertTriangle className="mt-0.5 size-4 shrink-0" />
            <span className="min-w-0">{state.error}</span>
          </div>
        ) : null}

        {state.status === "ready" && state.messages.length === 0 ? (
          <Card className="p-6">
            <EmptyState>
              <EmptyState.Header>
                <EmptyState.Media variant="icon">
                  <Bot className="size-5" />
                </EmptyState.Media>
                <EmptyState.Title>No messages</EmptyState.Title>
                <EmptyState.Description>
                  This conversation does not contain any messages yet.
                </EmptyState.Description>
              </EmptyState.Header>
            </EmptyState>
          </Card>
        ) : null}

        <div
          className="flex flex-col items-center"
          style={{ ["--thread-max-width" as string]: "58rem" }}
        >
          {state.messages.map((message, index) => (
            <MessageBubble
              key={message.id}
              message={message}
              previousUserCreatedAt={
                index > 0 && state.messages[index - 1]?.role === "user"
                  ? state.messages[index - 1]?.created_at
                  : undefined
              }
            />
          ))}
        </div>
      </div>
    </main>
  );
}

function MessageBubble({
  message,
  previousUserCreatedAt,
}: {
  message: WireMessage;
  previousUserCreatedAt?: string;
}) {
  const isUser = message.role === "user";
  const parts = message.parts ?? [];

  if (isUser) {
    return (
      <article className="grid w-full max-w-[var(--thread-max-width)] auto-rows-auto grid-cols-[minmax(72px,1fr)_auto] gap-y-1 py-3">
        <div className="col-start-2 row-start-1 flex flex-col items-end gap-1.5">
          <div className="max-w-[min(calc(var(--thread-max-width)*0.72),42rem)] whitespace-pre-wrap break-words rounded-xl bg-surface-secondary px-3 py-1.5 text-sm text-foreground">
            {parts.length > 0 ? (
              parts.map((part, index) => (
                <MessagePartView key={`${message.id}-${index}`} part={part} role={message.role} />
              ))
            ) : (
              <span className="text-muted">No content</span>
            )}
          </div>
        </div>
      </article>
    );
  }

  const split = splitAgentTurnParts(parts);
  const durationMs = inferAgentDurationMs(
    message.metadata?.duration_ms,
    previousUserCreatedAt,
    message.created_at,
  );

  return (
    <article className="relative grid w-full max-w-[var(--thread-max-width)] grid-cols-[auto_1fr] grid-rows-[auto_1fr] py-3">
      <div className="col-span-2 col-start-1 row-start-1 my-1.5 max-w-full break-words leading-7 text-foreground">
        <AssistantHeader message={message} />
        <div>
          {parts.length > 0 ? (
            <>
              {split.hasProcess ? (
                <RailProcessSummary durationMs={durationMs}>
                  {split.processIndices.map((index) => (
                    <MessagePartView
                      key={`${message.id}-process-${index}`}
                      part={parts[index]!}
                      role={message.role}
                    />
                  ))}
                </RailProcessSummary>
              ) : null}
              {split.outputIndices.map((index) => (
                <MessagePartView
                  key={`${message.id}-output-${index}`}
                  part={parts[index]!}
                  role={message.role}
                />
              ))}
            </>
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
    <div className="mb-2 flex items-center gap-x-2.5">
      <ModelIcon icon={icon} className="size-7 shrink-0" bare />
      <span className="min-w-0 truncate text-base font-bold leading-none text-foreground">
        {label}
      </span>
      {message.created_at ? (
        <span className="shrink-0 text-xs text-muted">{formatDate(message.created_at)}</span>
      ) : null}
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
        <AnswerMarkdownContent value={part.text ?? ""} />
      </RailText>
    );
  }
  if (part.type === "reasoning") {
    return <RailReasoning text={part.text ?? ""} />;
  }
  if (part.type === "environment") {
    const environment = parseEnvironmentPreparationSnapshot(part.environment);
    return environment ? <RailEnvironment environment={environment} /> : null;
  }
  if (part.type === "memory-recall") {
    if (!part.status || part.status === "miss") return null;
    return <RailMemoryRecall status={part.status} count={part.count} content={part.content} />;
  }
  if (part.type === "scm-approval") {
    if (!part.approvalStatus) return null;
    return (
      <RailSCMApproval
        status={part.approvalStatus}
        category={part.approvalCategory}
        commandLabel={part.approvalLabel}
      />
    );
  }
  if (part.type === "plan") {
    return (
      <section className="my-4 overflow-hidden rounded-2xl border border-indigo-500/25 bg-surface">
        <div className="border-b border-indigo-500/15 bg-indigo-500/[0.045] px-5 py-3.5">
          <div className="text-[10px] font-bold tracking-[0.16em] text-indigo-700 uppercase">
            Execution plan
          </div>
          <h3 className="mt-0.5 text-base font-semibold">Plan v{part.version}</h3>
          <p className="mt-1 text-xs capitalize text-muted">{part.status.replaceAll("_", " ")}</p>
        </div>
        <div className="px-5 py-5">
          <MarkdownContent value={part.contentMarkdown} />
        </div>
      </section>
    );
  }
  if (part.type === "question") {
    return <QuestionCard question={part} readonly />;
  }
  if (part.type === "run-summary") {
    return <RunSummary summary={part} />;
  }
  if (part.type === "structured-result") {
    return <StructuredResultCard result={part} />;
  }
  if (part.type === "tool-call") {
    return (
      <RailTool
        toolName={part.toolName || "tool"}
        argsText={part.argsText}
        result={part.result}
        isError={part.isError}
        outcome={part.outcome}
      />
    );
  }
  if (part.type === "progress") {
    return <RailProgress items={part.items} />;
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
  const parts = message.parts ?? [];
  const text = finalAgentOutputText(parts, splitAgentTurnParts(parts).outputIndices);

  const copy = async () => {
    if (!text) return;
    await navigator.clipboard.writeText(text);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  };

  return (
    <div className="col-start-1 row-start-2 -ml-1 flex gap-1 text-muted">
      <Tooltip delay={0}>
        <Button
          isIconOnly
          aria-label={copied ? "Copied" : "Copy"}
          isDisabled={!text}
          className="size-8 min-w-8"
          variant="ghost"
          onPress={() => void copy()}
        >
          {copied ? <Check className="size-4 text-emerald-400" /> : <CopyIcon className="size-4" />}
        </Button>
        <Tooltip.Content>{copied ? "Copied" : "Copy"}</Tooltip.Content>
      </Tooltip>
    </div>
  );
}

function TypingDots() {
  return (
    <div className="flex items-center gap-1 py-1" role="status" aria-label="Assistant is typing">
      <span className="size-2 animate-bounce rounded-full bg-foreground/60 [animation-delay:-0.3s]" />
      <span className="size-2 animate-bounce rounded-full bg-foreground/60 [animation-delay:-0.15s]" />
      <span className="size-2 animate-bounce rounded-full bg-foreground/60" />
    </div>
  );
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
