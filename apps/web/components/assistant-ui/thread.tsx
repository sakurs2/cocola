"use client";

import {
  ActionBarPrimitive,
  AttachmentPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  type FileMessagePartProps,
  type ReasoningMessagePartProps,
  ThreadPrimitive,
  type ToolCallMessagePartProps,
  useMessage,
} from "@assistant-ui/react";
import {
  ArrowDownIcon,
  BrainCircuit,
  ChevronDown,
  ChevronRight,
  CopyIcon,
  Download,
  Eye,
  FileText,
  MessagesSquare,
  PaperclipIcon,
  SendHorizontalIcon,
  Square,
  Wrench,
  XIcon,
  Zap,
} from "lucide-react";
import Image from "next/image";
import { useState, type FC } from "react";
import { useCocola, type ModelIconConfig, type UiMessageMetadata } from "@/app/runtime-provider";
import { TooltipIconButton } from "@/components/assistant-ui/tooltip-icon-button";
import { MarkdownText } from "@/components/assistant-ui/markdown-text";
import { cn } from "@/lib/utils";

// Open WebUI style product Thread for cocola, authored against the design
// tokens in app/globals.css (dark palette). Empty state centers a logo + model
// name over a pill composer with a "Suggested" list; once a conversation
// starts, the composer docks to the bottom. Surfaces the ExternalStore
// capabilities the adapter implements: send / cancel plus inline file
// attachments (paperclip → chip → sent, backed by Base64AttachmentAdapter).
// Voice and branch controls remain unsupported.

export const Thread: FC = () => {
  return (
    <ThreadPrimitive.Root
      className="flex h-full flex-col bg-background"
      style={{ ["--thread-max-width" as string]: "44rem" }}
    >
      <ThreadPrimitive.Viewport className="flex flex-1 flex-col items-center overflow-y-auto scroll-smooth px-4 pt-8">
        <ThreadWelcome />

        <ThreadPrimitive.Messages
          components={{
            UserMessage,
            AssistantMessage,
          }}
        />

        <ThreadPrimitive.If empty={false}>
          <div className="min-h-8 flex-grow" />
        </ThreadPrimitive.If>

        {/* Docked composer, only while a conversation is in progress. On the
            empty state the composer lives centered inside ThreadWelcome. */}
        <ThreadPrimitive.If empty={false}>
          <div className="sticky bottom-0 z-30 mt-3 flex w-full max-w-[var(--thread-max-width)] flex-col items-center justify-end rounded-t-lg bg-background pt-3 pb-4 shadow-[0_-18px_28px_hsl(var(--background))]">
            <ScrollToBottom />
            <Composer />
          </div>
        </ThreadPrimitive.If>
      </ThreadPrimitive.Viewport>
    </ThreadPrimitive.Root>
  );
};

const ScrollToBottom: FC = () => (
  <ThreadPrimitive.ScrollToBottom asChild>
    <TooltipIconButton
      tooltip="Scroll to bottom"
      variant="outline"
      className="absolute -top-10 rounded-full disabled:invisible"
    >
      <ArrowDownIcon className="h-4 w-4" />
    </TooltipIconButton>
  </ThreadPrimitive.ScrollToBottom>
);

const SUGGESTIONS: { title: string; subtitle: string; prompt: string }[] = [
  {
    title: "Show me a code snippet",
    subtitle: "of a website's sticky header",
    prompt: "Show me a code snippet of a website's sticky header",
  },
  {
    title: "Summarize the latest changes",
    subtitle: "in this repo",
    prompt: "Summarize the latest changes in this repo",
  },
  {
    title: "Explain how the SSE proxy works",
    subtitle: "in the cocola gateway",
    prompt: "Explain how the SSE proxy works in the cocola gateway",
  },
];

const ThreadWelcome: FC = () => {
  const { selectedModel } = useCocola();

  return (
    <ThreadPrimitive.Empty>
      <div className="flex w-full max-w-[var(--thread-max-width)] flex-grow flex-col items-center justify-center">
        <div className="flex items-center gap-3">
          <div className="flex size-9 items-center justify-center rounded-full bg-foreground text-background">
            <MessagesSquare className="size-5" />
          </div>
          <p className="text-3xl font-semibold text-foreground">{selectedModel.label}</p>
        </div>

        <div className="mt-8 w-full">
          <Composer />
        </div>

        <div className="mt-6 w-full">
          <div className="mb-2 flex items-center gap-1.5 px-1 text-sm text-muted-foreground">
            <Zap className="size-3.5" />
            Suggested
          </div>
          <div className="flex flex-col">
            {SUGGESTIONS.map(({ title, subtitle, prompt }) => (
              <ThreadPrimitive.Suggestion
                key={title}
                prompt={prompt}
                send
                className="flex flex-col items-start gap-0.5 rounded-lg px-2 py-2.5 text-left transition-colors hover:bg-muted"
              >
                <span className="text-sm font-medium text-foreground">{title}</span>
                <span className="text-xs text-muted-foreground">{subtitle}</span>
              </ThreadPrimitive.Suggestion>
            ))}
          </div>
        </div>
      </div>
    </ThreadPrimitive.Empty>
  );
};

const Composer: FC = () => (
  <ComposerPrimitive.Root className="relative z-10 flex w-full flex-col rounded-[1.5rem] border border-input bg-card px-3 py-2 shadow-lg transition-colors focus-within:border-ring">
    <ComposerAttachments />
    <ComposerPrimitive.Input
      rows={1}
      autoFocus
      placeholder="Send a message... (@ to mention, / for commands)"
      className="max-h-40 min-h-12 flex-grow resize-none border-none bg-transparent px-2 py-3 text-sm outline-none placeholder:text-muted-foreground focus:ring-0 disabled:cursor-not-allowed"
    />
    <div className="flex w-full items-center justify-between gap-2">
      <div className="flex min-w-0 items-center gap-1.5">
        <ComposerPrimitive.AddAttachment asChild>
          <TooltipIconButton
            tooltip="Attach file"
            variant="ghost"
            className="size-8 shrink-0 rounded-full p-2 text-muted-foreground"
          >
            <PaperclipIcon className="size-4" />
          </TooltipIconButton>
        </ComposerPrimitive.AddAttachment>
        <ModelPicker />
      </div>
      <ComposerAction />
    </div>
  </ComposerPrimitive.Root>
);

const ModelPicker: FC = () => {
  const { models, selectedModel, selectedModelAlias, setSelectedModelAlias } = useCocola();
  const [open, setOpen] = useState(false);

  return (
    <div className="relative inline-flex max-w-[14rem] min-w-0">
      <button
        type="button"
        className="flex max-w-[14rem] min-w-0 items-center gap-2 rounded-full px-2 py-1.5 text-sm font-medium text-foreground transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        aria-label="Select model"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <ModelIcon icon={selectedModel.icon} className="size-5" />
        <span className="truncate">{selectedModel.label}</span>
        <ChevronDown className="size-4 shrink-0 text-muted-foreground" />
      </button>
      {open ? (
        <div className="absolute bottom-full left-0 z-20 mb-2 w-full overflow-hidden rounded-lg border border-border bg-popover p-1 text-sm shadow-lg">
          {models.map((model) => (
            <button
              key={model.alias}
              type="button"
              className={cn(
                "flex w-full items-center gap-2 rounded-md px-2 py-2 text-left transition-colors hover:bg-muted",
                model.alias === selectedModelAlias
                  ? "bg-muted text-foreground"
                  : "text-muted-foreground",
              )}
              onClick={() => {
                setSelectedModelAlias(model.alias);
                setOpen(false);
              }}
            >
              <ModelIcon icon={model.icon} className="size-5" />
              <span className="min-w-0 flex-1 truncate font-medium">{model.label}</span>
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
};

const BRAND_MARKS: Record<string, { label: string; fg: string; bg: string }> = {
  anthropic: { label: "A", fg: "#0f172a", bg: "#f5f1e8" },
  openai: { label: "◎", fg: "#0f172a", bg: "#f8fafc" },
  google: { label: "G", fg: "#4285f4", bg: "#ffffff" },
  xai: { label: "xAI", fg: "#ffffff", bg: "#111827" },
  deepseek: { label: "DS", fg: "#ffffff", bg: "#2563eb" },
};

const ModelIcon: FC<{ icon?: ModelIconConfig; className?: string }> = ({ icon, className }) => {
  if (icon?.type === "image" && icon.src) {
    return (
      <span
        className={cn(
          "relative flex shrink-0 items-center justify-center overflow-hidden rounded-full border border-border bg-card",
          className,
        )}
      >
        <Image
          src={icon.src}
          alt=""
          width={256}
          height={256}
          unoptimized
          className="size-full object-contain"
          aria-hidden="true"
        />
      </span>
    );
  }
  const mark = icon?.type === "simple-icons" ? BRAND_MARKS[icon.slug.toLowerCase()] : undefined;
  if (!mark) {
    return (
      <span
        className={cn(
          "flex shrink-0 items-center justify-center rounded-full border border-border bg-background text-muted-foreground",
          className,
        )}
      >
        <BrainCircuit className="size-[70%]" />
      </span>
    );
  }
  return (
    <span
      className={cn(
        "flex shrink-0 items-center justify-center rounded-full border border-border text-[9px] font-bold leading-none",
        className,
      )}
      style={{ color: mark.fg, backgroundColor: mark.bg }}
      aria-hidden="true"
    >
      {mark.label}
    </span>
  );
};

// Pending attachment chips shown inside the composer before send. Each chip
// carries the file name plus a remove control; the runtime holds the File until
// send(), when Base64AttachmentAdapter turns it into a base64 FileMessagePart.
const ComposerAttachments: FC = () => (
  <div className="flex flex-wrap gap-1.5 empty:hidden [&:not(:empty)]:pb-1.5">
    <ComposerPrimitive.Attachments
      components={{
        Attachment: () => (
          <AttachmentPrimitive.Root className="relative flex w-fit max-w-full self-start items-center gap-2 rounded-lg border border-border bg-muted px-3 py-1.5 text-xs text-foreground">
            <PaperclipIcon className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="max-w-[16rem] truncate">
              <AttachmentPrimitive.Name />
            </span>
            <AttachmentPrimitive.Remove asChild>
              <button
                type="button"
                aria-label="Remove attachment"
                className="ml-1 rounded-full p-0.5 text-muted-foreground transition-colors hover:bg-background hover:text-foreground"
              >
                <XIcon className="size-3.5" />
              </button>
            </AttachmentPrimitive.Remove>
          </AttachmentPrimitive.Root>
        ),
      }}
    />
  </div>
);

const ComposerAction: FC = () => (
  <>
    <ThreadPrimitive.If running={false}>
      <ComposerPrimitive.Send asChild>
        <TooltipIconButton
          tooltip="Send"
          variant="default"
          className="my-1 size-8 rounded-full p-2"
        >
          <SendHorizontalIcon className="h-4 w-4" />
        </TooltipIconButton>
      </ComposerPrimitive.Send>
    </ThreadPrimitive.If>
    <ThreadPrimitive.If running>
      <ComposerPrimitive.Cancel asChild>
        <TooltipIconButton
          tooltip="Stop"
          variant="outline"
          className="my-1 size-8 rounded-full p-2"
        >
          <Square className="h-3.5 w-3.5 fill-current" />
        </TooltipIconButton>
      </ComposerPrimitive.Cancel>
    </ThreadPrimitive.If>
  </>
);

const UserMessage: FC = () => (
  <MessagePrimitive.Root className="grid w-full max-w-[var(--thread-max-width)] auto-rows-auto grid-cols-[minmax(72px,1fr)_auto] gap-y-1 py-3">
    <div className="col-start-2 row-start-1 flex flex-col items-end gap-1.5">
      <div className="flex flex-wrap justify-end gap-1.5 empty:hidden">
        <MessagePrimitive.Attachments
          components={{
            Attachment: () => (
              <AttachmentPrimitive.Root className="flex w-fit max-w-full items-center gap-2 rounded-lg border border-border bg-muted/60 px-3 py-1.5 text-xs text-foreground">
                <PaperclipIcon className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="max-w-[16rem] truncate">
                  <AttachmentPrimitive.Name />
                </span>
              </AttachmentPrimitive.Root>
            ),
          }}
        />
      </div>
      <MessagePrimitive.If hasContent>
        <div className="max-w-[calc(var(--thread-max-width)*0.8)] whitespace-pre-wrap break-words rounded-2xl bg-muted px-4 py-2 text-sm text-foreground">
          <MessagePrimitive.Parts />
        </div>
      </MessagePrimitive.If>
    </div>
  </MessagePrimitive.Root>
);

const AssistantMessage: FC = () => (
  <MessagePrimitive.Root className="relative grid w-full max-w-[var(--thread-max-width)] grid-cols-[auto_1fr] grid-rows-[auto_1fr] py-3">
    <div className="col-span-2 col-start-1 row-start-1 my-1.5 max-w-full break-words leading-7 text-foreground">
      <div className="relative">
        <MessagePrimitive.If last>
          <ThreadPrimitive.If running>
            <span className="aui-answer-border-beam" aria-hidden="true" />
          </ThreadPrimitive.If>
        </MessagePrimitive.If>
        <div className="relative z-[1]">
          <AssistantMessageHeader />
          <MessagePrimitive.Parts
            components={{
              Text: MarkdownText,
              Reasoning: ReasoningPart,
              File: ArtifactFilePart,
              tools: { Fallback: ToolFallback },
            }}
          />
          {/* Loading affordance: the runtime pushes an EMPTY assistant message the
              moment a turn starts and only then streams text into it. Until the
              first token lands the message has no content, so a "hasContent=false"
              message that is still the last one is exactly the in-flight state.
              Show typing dots there so the user sees the model is working. */}
          <MessagePrimitive.If hasContent={false}>
            <MessagePrimitive.If last>
              <TypingIndicator />
            </MessagePrimitive.If>
          </MessagePrimitive.If>
          <MessagePrimitive.If last>
            <ThreadPrimitive.If running>
              <AnsweringIndicator />
            </ThreadPrimitive.If>
          </MessagePrimitive.If>
        </div>
      </div>
    </div>
    <AssistantActionBar />
  </MessagePrimitive.Root>
);

const AssistantMessageHeader: FC = () => {
  const { selectedModel } = useCocola();
  const metadata = useMessage((m) => m.metadata.custom) as UiMessageMetadata | undefined;
  const label = metadata?.model_label || selectedModel.label;
  const icon = metadata?.model_icon || selectedModel.icon;

  return (
    <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
      <span className="inline-flex min-w-0 items-center gap-1.5 rounded-full border border-border bg-muted/35 px-2 py-1">
        <ModelIcon icon={icon} className="size-4" />
        <span className="truncate font-medium text-foreground">{label}</span>
      </span>
    </div>
  );
};

const AnsweringIndicator: FC = () => (
  <div className="mt-3 flex items-center" role="status" aria-label="Assistant response in progress">
    <span className="aui-answering-shimmer text-xs font-semibold tracking-wide">Answering</span>
  </div>
);

// Three-dot "typing" pulse rendered while an assistant turn is in flight but no
// text has streamed yet. Pure CSS (Tailwind animate-bounce + staggered delays);
// aria-label keeps it announced to screen readers.
const TypingIndicator: FC = () => (
  <div className="flex items-center gap-1 py-1" role="status" aria-label="Assistant is typing">
    <span className="size-2 animate-bounce rounded-full bg-muted-foreground/60 [animation-delay:-0.3s]" />
    <span className="size-2 animate-bounce rounded-full bg-muted-foreground/60 [animation-delay:-0.15s]" />
    <span className="size-2 animate-bounce rounded-full bg-muted-foreground/60" />
  </div>
);

const ArtifactFilePart: FC<FileMessagePartProps> = ({ filename, mimeType, data }) => {
  const { activeConversationId, openArtifact } = useCocola();
  const meta = parseArtifactData(data);
  const name = filename || "file";
  const type = mimeType || "application/octet-stream";
  const downloadUrl = meta.url || data;

  return (
    <div className="my-3 flex max-w-xl items-center gap-3 rounded-lg border border-border bg-muted/30 p-3 text-sm">
      <span className="flex size-9 shrink-0 items-center justify-center rounded-md border border-border bg-background text-muted-foreground">
        <FileText className="size-4" />
      </span>
      <div className="min-w-0 flex-1">
        <div className="truncate font-medium text-foreground">{name}</div>
        <div className="mt-0.5 truncate text-xs text-muted-foreground">
          {formatBytes(meta.size)} · {type}
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-1">
        <TooltipIconButton
          tooltip="Preview"
          variant="ghost"
          className="size-8 rounded-full p-2"
          onClick={() =>
            openArtifact({
              id: meta.id || name,
              conversationId: activeConversationId,
              filename: name,
              mimeType: type,
              size: meta.size,
              downloadUrl,
            })
          }
        >
          <Eye className="size-4" />
        </TooltipIconButton>
        <a
          href={downloadUrl}
          download={name}
          title="Download"
          aria-label={`Download ${name}`}
          className="inline-flex size-8 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        >
          <Download className="size-4" />
        </a>
      </div>
    </div>
  );
};

const parseArtifactData = (data: string): { id?: string; url: string; size: number } => {
  try {
    const parsed = JSON.parse(data) as { id?: unknown; url?: unknown; size?: unknown };
    return {
      id: typeof parsed.id === "string" ? parsed.id : undefined,
      url: typeof parsed.url === "string" ? parsed.url : "",
      size: typeof parsed.size === "number" ? parsed.size : 0,
    };
  } catch {
    return { url: data, size: 0 };
  }
};

const formatBytes = (bytes: number): string => {
  if (!bytes) return "Unknown size";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 10 || unit === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`;
};

const ReasoningPart: FC<ReasoningMessagePartProps> = ({ text, status }) => (
  <details className="aui-details group my-3 overflow-hidden rounded-lg border border-border bg-muted/30 text-sm">
    <summary className="flex cursor-pointer select-none items-center gap-2 px-3 py-2 text-muted-foreground [&::-webkit-details-marker]:hidden">
      <ChevronRight className="size-3.5 shrink-0 transition-transform group-open:rotate-90" />
      <BrainCircuit className="size-3.5 shrink-0" />
      <span className="font-medium text-foreground">Reasoning</span>
      <span className="ml-auto rounded-full border border-border bg-background px-2 py-0.5 text-[11px] leading-4">
        {status.type === "running" ? "Thinking" : "Done"}
      </span>
    </summary>
    <div className="aui-details-body border-t border-border px-3 py-2.5 text-sm leading-6 text-muted-foreground">
      {text}
    </div>
  </details>
);

const ToolFallback: FC<ToolCallMessagePartProps> = ({
  toolName,
  argsText,
  result,
  isError,
  status,
}) => (
  <ToolCard
    title={toolName}
    args={formatPayload(argsText)}
    result={formatPayload(result)}
    isError={isError}
    status={status}
  />
);

const ToolCard: FC<{
  title: string;
  args?: string;
  result?: string;
  isError?: boolean;
  status: ToolCallMessagePartProps["status"];
}> = ({ title, args, result, isError, status }) => {
  const meta = getToolStatusMeta(status, isError, result !== undefined);

  return (
    <details
      open={status.type === "running" || status.type === "requires-action" || isError}
      className={cn(
        "aui-details group my-3 overflow-hidden rounded-lg border bg-muted/30 text-sm",
        isError ? "border-destructive/50" : "border-border",
      )}
    >
      <summary className="flex cursor-pointer select-none items-center gap-2 px-3 py-2 text-muted-foreground [&::-webkit-details-marker]:hidden">
        <ChevronRight className="size-3.5 shrink-0 transition-transform group-open:rotate-90" />
        <span className="flex size-6 shrink-0 items-center justify-center rounded-md border border-border bg-background">
          <Wrench className="size-3.5" />
        </span>
        <span className="min-w-0 truncate font-mono text-xs text-foreground">{title}</span>
        <span
          className={cn(
            "ml-auto rounded-full border px-2 py-0.5 text-[11px] leading-4",
            meta.className,
          )}
        >
          {meta.label}
        </span>
      </summary>
      <div className="aui-details-body grid gap-2 border-t border-border p-3">
        {args ? <ToolPayload label="Arguments" tone="muted" value={args} /> : null}
        {result !== undefined ? (
          <ToolPayload
            label={isError ? "Error" : "Result"}
            tone={isError ? "error" : "muted"}
            value={result}
          />
        ) : null}
        {!args && result === undefined ? (
          <p className="text-xs text-muted-foreground">Waiting for output...</p>
        ) : null}
      </div>
    </details>
  );
};

const ToolPayload: FC<{
  label: string;
  value: string;
  tone: "muted" | "error";
}> = ({ label, value, tone }) => (
  <section className="overflow-hidden rounded-md border border-border bg-background/70">
    <div className="border-b border-border px-2.5 py-1.5 font-mono text-[11px] uppercase text-muted-foreground">
      {label}
    </div>
    <pre
      className={cn(
        "max-h-72 overflow-auto whitespace-pre-wrap break-words p-2.5 font-mono text-xs leading-5",
        tone === "error" ? "text-destructive" : "text-muted-foreground",
      )}
    >
      {value}
    </pre>
  </section>
);

const getToolStatusMeta = (
  status: ToolCallMessagePartProps["status"],
  isError: boolean | undefined,
  hasResult: boolean,
) => {
  if (isError) {
    return {
      label: "Error",
      className: "border-destructive/40 bg-destructive/10 text-destructive",
    };
  }
  if (status.type === "running") {
    return {
      label: "Running",
      className: "border-border bg-background text-muted-foreground",
    };
  }
  if (status.type === "requires-action") {
    return {
      label: "Action needed",
      className: "border-primary/30 bg-primary/10 text-foreground",
    };
  }
  if (status.type === "incomplete") {
    return {
      label: "Incomplete",
      className: "border-destructive/40 bg-destructive/10 text-destructive",
    };
  }
  return {
    label: hasResult ? "Done" : "No output",
    className: "border-border bg-background text-muted-foreground",
  };
};

const formatPayload = (value: unknown): string | undefined => {
  if (value === undefined) return undefined;
  if (typeof value === "string") {
    const trimmed = value.trim();
    if (!trimmed) return undefined;
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
};

const AssistantActionBar: FC = () => (
  // Copy control stays resident: autohide="never" so the action bar is always
  // visible under every assistant turn, not just on hover. hideWhenRunning is
  // kept so it does not flash mid-stream, appearing once the turn settles.
  <ActionBarPrimitive.Root
    hideWhenRunning
    autohide="never"
    className="col-start-1 row-start-2 -ml-1 flex gap-1 text-muted-foreground"
  >
    <ActionBarPrimitive.Copy asChild>
      <TooltipIconButton tooltip="Copy">
        <CopyIcon className="h-4 w-4" />
      </TooltipIconButton>
    </ActionBarPrimitive.Copy>
  </ActionBarPrimitive.Root>
);
