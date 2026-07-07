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
import * as Popover from "@radix-ui/react-popover";
import { Command } from "cmdk";
import { motion } from "framer-motion";
import {
  ArrowDownIcon,
  BrainCircuit,
  Check,
  ChevronDown,
  ChevronRight,
  CopyIcon,
  Download,
  Eye,
  FileText,
  MessagesSquare,
  PaperclipIcon,
  Search,
  SendHorizontalIcon,
  Square,
  Wrench,
  XIcon,
  Zap,
} from "lucide-react";
import Image from "next/image";
import { useEffect, useState, type FC } from "react";
import { useCocola, type ModelIconConfig, type UiMessageMetadata } from "@/app/runtime-provider";
import { TooltipIconButton } from "@/components/assistant-ui/tooltip-icon-button";
import { MarkdownText } from "@/components/assistant-ui/markdown-text";
import {
  LOCAL_SIMPLE_ICON_PATHS,
  SIMPLE_ICON_FALLBACK_BADGES,
  lobeIconPath,
  normalizeLobeIconSlug,
} from "@/lib/model-icons";
import { cn } from "@/lib/utils";

// Product Thread for cocola, authored against the white workspace design tokens.
// assistant-ui owns chat semantics; this file owns the composed product chrome.

export const Thread: FC = () => {
  return (
    <ThreadPrimitive.Root
      className="relative flex h-full flex-col overflow-hidden bg-transparent"
      style={{ ["--thread-max-width" as string]: "46rem" }}
    >
      <ThreadPrimitive.If empty>
        <div className="cocola-cloud-field" aria-hidden="true" />
      </ThreadPrimitive.If>
      <ThreadPrimitive.Viewport className="relative z-10 flex flex-1 flex-col items-center overflow-y-auto scroll-smooth px-5 pt-8">
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
          <div className="sticky bottom-0 z-30 mt-3 flex w-full max-w-[var(--thread-max-width)] flex-col items-center justify-end bg-gradient-to-t from-background via-background to-background/0 pt-4 pb-5">
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
  const noModel = !selectedModel;

  return (
    <ThreadPrimitive.Empty>
      <motion.div
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.22, ease: "easeOut" }}
        className="flex w-full max-w-[var(--thread-max-width)] flex-grow flex-col items-center justify-center"
      >
        <div className="flex flex-col items-center gap-3 text-center">
          <div className="cocola-hero-badge flex size-12 items-center justify-center rounded-2xl text-white">
            <MessagesSquare className="size-5" />
          </div>
          <p className="text-3xl font-semibold tracking-normal text-foreground">
            {selectedModel?.label ?? "No model configured"}
          </p>
          <p className="max-w-md text-sm leading-6 text-muted-foreground">
            Ask, inspect files, run tools, and keep every session grounded in its workspace.
          </p>
        </div>
        {noModel ? (
          <p className="mt-3 text-center text-sm text-muted-foreground">
            Ask an admin to enable a model before starting a conversation.
          </p>
        ) : null}

        <div className="mt-7 w-full">
          <Composer />
        </div>

        <div className="cocola-suggest-card mt-6 w-full rounded-2xl border p-2">
          <div className="mb-1 flex items-center gap-1.5 px-2 text-xs font-medium uppercase text-muted-foreground">
            <Zap className="size-3.5" />
            Suggested
          </div>
          <div className="flex flex-col">
            {SUGGESTIONS.map(({ title, subtitle, prompt }) => (
              <ThreadPrimitive.Suggestion
                key={title}
                prompt={prompt}
                send
                className="cocola-suggest-item flex flex-col items-start gap-0.5 rounded-xl px-3 py-2.5 text-left hover:bg-white/70 hover:text-accent-foreground"
              >
                <span className="text-sm font-medium text-foreground">{title}</span>
                <span className="text-xs text-muted-foreground">{subtitle}</span>
              </ThreadPrimitive.Suggestion>
            ))}
          </div>
        </div>
      </motion.div>
    </ThreadPrimitive.Empty>
  );
};

const Composer: FC = () => {
  const { selectedModel } = useCocola();
  const noModel = !selectedModel;

  return (
    <motion.div
      className="w-full"
      whileFocus={{ y: -1 }}
      transition={{ type: "spring", stiffness: 420, damping: 32 }}
    >
      <ComposerPrimitive.Root className="composer-lift relative z-10 flex w-full flex-col rounded-2xl border px-3 py-2">
        <ComposerAttachments />
        <ComposerPrimitive.Input
          rows={1}
          autoFocus={!noModel}
          disabled={noModel}
          placeholder={
            noModel ? "No model configured" : "Send a message... (@ to mention, / for commands)"
          }
          className="max-h-40 min-h-12 flex-grow resize-none border-none bg-transparent px-2 py-3 text-sm outline-none placeholder:text-muted-foreground focus:ring-0 disabled:cursor-not-allowed"
        />
        <div className="flex w-full items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-1.5">
            <ComposerPrimitive.AddAttachment asChild>
              <TooltipIconButton
                tooltip={noModel ? "No model configured" : "Attach file"}
                variant="ghost"
                disabled={noModel}
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
    </motion.div>
  );
};

const ModelPicker: FC = () => {
  const { models, selectedModel, selectedModelAlias, setSelectedModelAlias } = useCocola();
  const [open, setOpen] = useState(false);
  const noModel = !selectedModel;

  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <button
          type="button"
          className="flex max-w-[14rem] min-w-0 items-center gap-2 rounded-full border border-transparent px-2 py-1.5 text-sm font-medium text-foreground transition-colors hover:border-border hover:bg-muted focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
          aria-label={noModel ? "No model configured" : "Select model"}
          disabled={noModel}
        >
          <ModelIcon icon={selectedModel?.icon} className="size-5" />
          <span className="truncate">{selectedModel?.label ?? "No model"}</span>
          {noModel ? null : <ChevronDown className="size-4 shrink-0 text-muted-foreground" />}
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          side="top"
          align="start"
          sideOffset={10}
          className="z-50 w-72 overflow-hidden rounded-2xl border border-border bg-popover text-popover-foreground shadow-xl"
        >
          <Command>
            <div className="flex items-center gap-2 border-b border-border px-3">
              <Search className="size-4 text-muted-foreground" />
              <Command.Input
                placeholder="Find a model..."
                className="h-10 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
              />
            </div>
            <Command.List className="max-h-72 overflow-auto p-1.5">
              <Command.Empty className="px-3 py-8 text-center text-sm text-muted-foreground">
                No model found.
              </Command.Empty>
              {models.map((model) => (
                <Command.Item
                  key={model.alias}
                  value={`${model.label} ${model.alias}`}
                  className="flex cursor-pointer items-center gap-2 rounded-xl px-2 py-2 text-sm outline-none data-[selected=true]:bg-accent data-[selected=true]:text-accent-foreground"
                  onSelect={() => {
                    setSelectedModelAlias(model.alias);
                    setOpen(false);
                  }}
                >
                  <ModelIcon icon={model.icon} className="size-6" />
                  <span className="min-w-0 flex-1 truncate font-medium">{model.label}</span>
                  {model.alias === selectedModelAlias ? <Check className="size-4" /> : null}
                </Command.Item>
              ))}
            </Command.List>
          </Command>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
};

export const ModelIcon: FC<{ icon?: ModelIconConfig; className?: string }> = ({
  icon,
  className,
}) => {
  const [lobeFailed, setLobeFailed] = useState(false);
  const normalizedSlug = normalizeLobeIconSlug(icon?.slug);
  const canUseLobeIcon =
    (icon?.type === "lobe-icons" || icon?.type === "simple-icons") && normalizedSlug !== "";
  const lobePath = canUseLobeIcon && !lobeFailed ? lobeIconPath(normalizedSlug) : "";
  const simpleIconPath =
    !lobePath && icon?.type === "simple-icons" && icon.slug
      ? LOCAL_SIMPLE_ICON_PATHS[icon.slug.toLowerCase()]
      : "";

  useEffect(() => {
    setLobeFailed(false);
  }, [icon?.slug, icon?.src, icon?.type]);

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
  if (lobePath) {
    return (
      <span
        className={cn(
          "flex shrink-0 items-center justify-center overflow-hidden rounded-full border border-border bg-white",
          className,
        )}
        aria-hidden="true"
      >
        <Image
          src={lobePath}
          alt=""
          width={96}
          height={96}
          unoptimized
          className="size-[72%] object-contain"
          onError={() => setLobeFailed(true)}
        />
      </span>
    );
  }
  if (simpleIconPath) {
    return (
      <span
        className={cn(
          "flex shrink-0 items-center justify-center overflow-hidden rounded-full border border-border bg-white",
          className,
        )}
        aria-hidden="true"
      >
        <Image
          src={simpleIconPath}
          alt=""
          width={96}
          height={96}
          unoptimized
          className="size-[72%] object-contain"
        />
      </span>
    );
  }
  const fallbackBadge =
    (icon?.type === "simple-icons" || icon?.type === "lobe-icons") && icon.slug
      ? SIMPLE_ICON_FALLBACK_BADGES[icon.slug.toLowerCase()] ||
        SIMPLE_ICON_FALLBACK_BADGES[normalizedSlug]
      : "";
  if (!fallbackBadge) {
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
        "flex shrink-0 items-center justify-center rounded-full border border-border bg-muted text-[9px] font-bold leading-none text-foreground",
        className,
      )}
      aria-hidden="true"
    >
      {fallbackBadge}
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

const ComposerAction: FC = () => {
  const { selectedModel } = useCocola();
  const noModel = !selectedModel;

  return (
    <>
      <ThreadPrimitive.If running={false}>
        <ComposerPrimitive.Send asChild>
          <TooltipIconButton
            tooltip={noModel ? "No model configured" : "Send"}
            variant="default"
            disabled={noModel}
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
};

const UserMessage: FC = () => (
  <MessagePrimitive.Root className="message-enter grid w-full max-w-[var(--thread-max-width)] auto-rows-auto grid-cols-[minmax(72px,1fr)_auto] gap-y-1 py-3">
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
        <div className="max-w-[calc(var(--thread-max-width)*0.8)] whitespace-pre-wrap break-words rounded-2xl bg-primary px-4 py-2.5 text-sm leading-6 text-primary-foreground shadow-sm">
          <MessagePrimitive.Parts />
        </div>
      </MessagePrimitive.If>
    </div>
  </MessagePrimitive.Root>
);

const AssistantMessage: FC = () => (
  <MessagePrimitive.Root className="message-enter relative grid w-full max-w-[var(--thread-max-width)] grid-cols-[auto_1fr] grid-rows-[auto_1fr] py-3">
    <div className="col-span-2 col-start-1 row-start-1 my-1.5 max-w-full break-words rounded-2xl border border-border bg-card/90 px-4 py-3 leading-7 text-foreground shadow-sm">
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
  const label = metadata?.model_label || selectedModel?.label || "Model";
  const icon = metadata?.model_icon || selectedModel?.icon;

  return (
    <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
      <span className="inline-flex min-w-0 items-center gap-1.5 rounded-full border border-border bg-background px-2 py-1 shadow-sm">
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
  const { activeSessionId, openArtifact } = useCocola();
  const meta = parseArtifactData(data);
  const name = filename || "file";
  const type = mimeType || "application/octet-stream";
  const downloadUrl = meta.url || data;

  return (
    <div className="my-3 flex max-w-xl items-center gap-3 rounded-xl border border-border bg-background p-3 text-sm shadow-sm">
      <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border border-border bg-muted text-muted-foreground">
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
              sessionId: activeSessionId,
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
  <details className="aui-details group my-3 overflow-hidden rounded-xl border border-border bg-background text-sm shadow-sm">
    <summary className="flex cursor-pointer select-none items-center gap-2 px-3 py-2 text-muted-foreground [&::-webkit-details-marker]:hidden">
      <ChevronRight className="size-3.5 shrink-0 transition-transform group-open:rotate-90" />
      <BrainCircuit className="size-3.5 shrink-0" />
      <span className="font-medium text-foreground">Reasoning</span>
      <span className="ml-auto rounded-full border border-border bg-muted px-2 py-0.5 text-[11px] leading-4">
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
        "aui-details group my-3 overflow-hidden rounded-xl border bg-background text-sm shadow-sm",
        isError ? "border-destructive/50" : "border-border",
      )}
    >
      <summary className="flex cursor-pointer select-none items-center gap-2 px-3 py-2 text-muted-foreground [&::-webkit-details-marker]:hidden">
        <ChevronRight className="size-3.5 shrink-0 transition-transform group-open:rotate-90" />
        <span className="flex size-6 shrink-0 items-center justify-center rounded-lg border border-border bg-muted">
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
  <section className="overflow-hidden rounded-lg border border-border bg-muted/35">
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
