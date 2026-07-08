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
  MessageSquare,
  PaperclipIcon,
  Search,
  SendHorizontalIcon,
  Square,
  Wrench,
  XIcon,
  Globe,
  Terminal,
  FolderSearch,
  ListTodo,
  Loader2,
  ArrowRight,
  ArrowUp as ArrowUpIcon,
  BarChart3,
  Code2,
  Lightbulb,
  Pencil,
  Sparkles,
  ExternalLink,
} from "lucide-react";
import Image from "next/image";
import { useEffect, useState, type FC, type ReactNode } from "react";
import { useCocola, type ModelIconConfig, type UiMessageMetadata } from "@/app/runtime-provider";
import { CocolaWordmark } from "@/components/assistant-ui/cocola-wordmark";
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
      <ThreadPrimitive.Viewport className="relative z-10 flex flex-1 flex-col items-center overflow-y-auto scroll-smooth px-5 pt-8 [scrollbar-gutter:stable_both-edges]">
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

type SuggestionTile = {
  icon: typeof BarChart3;
  tile: string;
  title: string;
  subtitle: string;
  prompt: string;
};

const SUGGESTIONS: SuggestionTile[] = [
  {
    icon: BarChart3,
    tile: "bg-emerald-100 text-emerald-600",
    title: "Analyze this data",
    subtitle: "and create insights",
    prompt: "Analyze this data and create insights",
  },
  {
    icon: Pencil,
    tile: "bg-sky-100 text-sky-600",
    title: "Draft a project plan",
    subtitle: "for a new product",
    prompt: "Draft a project plan for a new product",
  },
  {
    icon: Code2,
    tile: "bg-violet-100 text-violet-600",
    title: "Write a Python script",
    subtitle: "to automate this task",
    prompt: "Write a Python script to automate this task",
  },
  {
    icon: Lightbulb,
    tile: "bg-pink-100 text-pink-600",
    title: "Brainstorm creative ideas",
    subtitle: "for a campaign",
    prompt: "Brainstorm creative ideas for a campaign",
  },
];

const ThreadWelcome: FC = () => {
  const { selectedModel, modelsLoaded } = useCocola();
  const noModel = modelsLoaded && !selectedModel;
  // Time-aware greeting resolved after mount so SSR/client markup agree.
  const [greeting, setGreeting] = useState("Welcome back");
  useEffect(() => {
    const h = new Date().getHours();
    setGreeting(h < 12 ? "Good morning" : h < 18 ? "Good afternoon" : "Good evening");
  }, []);

  return (
    <ThreadPrimitive.Empty>
      <motion.div
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.22, ease: "easeOut" }}
        className="flex w-full max-w-[var(--thread-max-width)] flex-grow flex-col items-center justify-center"
      >
        <div className="flex flex-col items-center gap-3 text-center">
          <h1 className="sr-only">{greeting}</h1>
          <CocolaWordmark className="cocola-wordmark h-48 w-auto max-w-[min(96vw,680px)]" />
          <p className="max-w-md text-center text-base leading-6 text-muted-foreground">
            Your trusty & powerful agent platform
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

        <div className="mt-8 w-full">
          <div className="mb-3 flex items-center gap-1.5 px-1 text-sm font-semibold text-foreground">
            <Sparkles className="size-4 text-primary" />
            Suggested prompts
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            {SUGGESTIONS.map(({ icon: Icon, tile, title, subtitle, prompt }) => (
              <ThreadPrimitive.Suggestion
                key={title}
                prompt={prompt}
                send
                className="cocola-prompt-card group relative flex flex-col gap-3 rounded-2xl border p-4 text-left"
              >
                <span className={cn("flex size-11 items-center justify-center rounded-xl", tile)}>
                  <Icon className="size-5" />
                </span>
                <div className="min-w-0 pr-6">
                  <div className="text-sm font-semibold leading-snug text-foreground">{title}</div>
                  <div className="text-xs text-muted-foreground">{subtitle}</div>
                </div>
                <span className="cocola-prompt-arrow absolute bottom-4 right-4 grid size-7 place-items-center rounded-full border border-border bg-white/80 text-muted-foreground">
                  <ArrowRight className="size-3.5" />
                </span>
              </ThreadPrimitive.Suggestion>
            ))}
          </div>
        </div>
      </motion.div>
    </ThreadPrimitive.Empty>
  );
};

const Composer: FC = () => {
  const { selectedModel, modelsLoaded } = useCocola();
  const noModel = modelsLoaded && !selectedModel;

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
  const { models, selectedModel, selectedModelAlias, setSelectedModelAlias, modelsLoaded } =
    useCocola();
  const [open, setOpen] = useState(false);
  const noModel = modelsLoaded && !selectedModel;

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
  const { selectedModel, modelsLoaded } = useCocola();
  const noModel = modelsLoaded && !selectedModel;

  return (
    <>
      <ThreadPrimitive.If running={false}>
        <ComposerPrimitive.Send asChild>
          <TooltipIconButton
            tooltip={noModel ? "No model configured" : "Send"}
            variant="default"
            disabled={noModel}
            className="cocola-send-btn my-1 size-9 rounded-full p-2 text-white"
          >
            <ArrowUpIcon className="h-4 w-4" />
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
        <div className="max-w-[calc(var(--thread-max-width)*0.8)] whitespace-pre-wrap break-words rounded-2xl bg-muted px-4 py-2.5 text-sm leading-6 text-foreground">
          <MessagePrimitive.Parts />
        </div>
      </MessagePrimitive.If>
    </div>
  </MessagePrimitive.Root>
);

const AssistantMessage: FC = () => (
  <MessagePrimitive.Root className="message-enter relative grid w-full max-w-[var(--thread-max-width)] grid-cols-[auto_1fr] grid-rows-[auto_1fr] py-3">
    <div className="col-span-2 col-start-1 row-start-1 max-w-full break-words px-0.5 py-1 leading-7 text-foreground">
      <div className="relative">
        <MessagePrimitive.If last>
          <ThreadPrimitive.If running>
            <span className="aui-answer-border-beam" aria-hidden="true" />
          </ThreadPrimitive.If>
        </MessagePrimitive.If>
        <div className="relative z-[1]">
          <AssistantMessageHeader />
          {/* Vertical timeline rail: one continuous line (the ::before pseudo)
              runs at x=0.875rem — exactly the center of each RailRow icon column
              (1.75rem wide) — so every node's badge sits centered on the line.
              Badges carry bg-background + z-[1] to punch through it. */}
          <div>
            <MessagePrimitive.Parts
              components={{
                Text: TextPart,
                Reasoning: ReasoningPart,
                File: ArtifactFilePart,
                tools: { Fallback: ToolFallback },
              }}
            />
          </div>
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
    <div className="mb-2 flex items-center gap-2 text-sm text-muted-foreground">
      <ModelIcon icon={icon} className="size-5 opacity-90" />
      <span className="truncate font-medium">{label}</span>
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
    <RailRow icon={FileText} label="生成文件">
      <div className="flex max-w-xl items-center gap-3 rounded-xl border border-border/60 bg-muted/40 p-3 text-sm">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-background text-muted-foreground">
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
    </RailRow>
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

// Shared vertical-rail row. Every assistant response node hangs off one
// continuous line (drawn by the parent .aui-rail container): an icon badge sits
// on the line, an action label + type-specific content sit to its right.
const RailRow: FC<{
  icon: FC<{ className?: string }>;
  label: string;
  running?: boolean;
  tone?: "default" | "error";
  children?: ReactNode;
}> = ({ icon: Icon, label, running, tone = "default", children }) => (
  <div className="grid grid-cols-[1.75rem_1fr] gap-x-2.5">
    <div className="relative flex justify-center after:absolute after:left-1/2 after:top-8 after:bottom-0 after:w-0.5 after:-translate-x-1/2 after:rounded-full after:bg-border/50">
      <span
        className={cn(
          "relative z-[1] flex size-7 items-center justify-center",
          tone === "error" ? "text-destructive" : "text-muted-foreground",
        )}
      >
        {running ? <Loader2 className="size-4 animate-spin" /> : <Icon className="size-4" />}
      </span>
    </div>
    <div className="min-w-0 pb-4 pt-1.5">
      {label ? (
        <div
          className={cn(
            "mb-1 text-xs font-medium",
            tone === "error" ? "text-destructive" : "text-muted-foreground",
          )}
        >
          {label}
        </div>
      ) : null}
      {children}
    </div>
  </div>
);

// Plain assistant text answer, rendered as a rail node.
const TextPart: FC = () => (
  <RailRow icon={MessageSquare} label="回答">
    <MarkdownText />
  </RailRow>
);

const ReasoningPart: FC<ReasoningMessagePartProps> = ({ text, status }) => {
  const running = status.type === "running";
  return (
    <RailRow icon={BrainCircuit} label={running ? "正在思考" : "推理过程"} running={running}>
      <details className="aui-details group text-sm">
        <summary className="flex w-fit cursor-pointer select-none items-center gap-1 py-0.5 text-xs text-muted-foreground transition-colors hover:text-foreground [&::-webkit-details-marker]:hidden">
          <ChevronRight className="size-3 shrink-0 transition-transform group-open:rotate-90" />
          <span>展开思考过程</span>
        </summary>
        <div className="aui-details-body mt-1 border-l-2 border-border/70 pl-3 text-[13px] leading-6 text-muted-foreground">
          {text}
        </div>
      </details>
    </RailRow>
  );
};

// Tool call rendering — content-flow style (no card chrome).
//
// The gateway only streams tool_use (name + input) and a bare tool_result
// (id + is_error, NO content payload). So we render each tool call as a light
// status row: an icon + a Chinese progress phrase, plus small chips extracted
// from the input (search query / url host / filename / command). Details can be
// expanded to inspect the raw arguments.

type ToolMeta = { icon: FC<{ className?: string }>; running: string; done: string };

// Map SDK tool names (Claude Agent SDK: Bash/Read/Write/Edit/Glob/Grep/
// WebSearch/WebFetch/Task/TodoWrite/Skill; MCP tools carry an mcp__ prefix)
// to an icon + progress phrases. Unknown names fall back to a generic wrench.
const getToolMeta = (rawName: string): ToolMeta => {
  const name = rawName.replace(/^mcp__/, "").toLowerCase();
  if (name.includes("websearch") || name.includes("search"))
    return { icon: Search, running: "正在搜索资料", done: "已搜索资料" };
  if (name.includes("webfetch") || name.includes("fetch") || name.includes("browser"))
    return { icon: Globe, running: "正在读取网页", done: "已读取网页" };
  if (name.startsWith("read") || name.includes("read_file"))
    return { icon: FileText, running: "正在阅读文件", done: "已阅读文件" };
  if (name.startsWith("write") || name.includes("write_file"))
    return { icon: Pencil, running: "正在写入文件", done: "已写入文件" };
  if (name.startsWith("edit") || name.includes("str_replace") || name.includes("edit_file"))
    return { icon: Pencil, running: "正在编辑文件", done: "已编辑文件" };
  if (name.startsWith("glob") || name.startsWith("grep") || name.includes("find"))
    return { icon: FolderSearch, running: "正在检索代码", done: "已检索代码" };
  if (name.startsWith("bash") || name.includes("shell") || name.includes("terminal"))
    return { icon: Terminal, running: "正在执行命令", done: "已执行命令" };
  if (name.startsWith("todo") || name.includes("task"))
    return { icon: ListTodo, running: "正在规划任务", done: "已更新任务" };
  if (name.startsWith("skill") || name.includes("load"))
    return { icon: Sparkles, running: "正在加载技能", done: "已加载技能" };
  return { icon: Wrench, running: "正在调用工具", done: "已调用工具" };
};

// Best-effort chips from the tool input JSON. Never throws; returns [] on any
// parse miss so the status row still renders cleanly.
const extractToolChips = (argsText: string): string[] => {
  if (!argsText) return [];
  let obj: Record<string, unknown>;
  try {
    obj = JSON.parse(argsText) as Record<string, unknown>;
  } catch {
    return [];
  }
  const chips: string[] = [];
  const push = (v: unknown) => {
    if (typeof v === "string" && v.trim()) chips.push(v.trim());
  };
  push(obj.query);
  push(obj.pattern);
  if (typeof obj.url === "string") {
    try {
      chips.push(new URL(obj.url).host);
    } catch {
      push(obj.url);
    }
  }
  const file = obj.file_path ?? obj.path ?? obj.filename;
  if (typeof file === "string" && file.trim()) {
    const parts = file.trim().split("/");
    chips.push(parts[parts.length - 1] || file.trim());
  }
  if (typeof obj.command === "string" && obj.command.trim()) {
    const firstLine = obj.command.trim().split("\n")[0] ?? "";
    chips.push(firstLine.slice(0, 48));
  }
  if (typeof obj.description === "string" && obj.description.trim() && chips.length === 0) {
    chips.push(obj.description.trim().slice(0, 48));
  }
  return Array.from(new Set(chips)).slice(0, 4);
};

type SearchResult = { title: string; url: string; host: string };

// Detect the tools whose result content IS the thing to show (a list of web
// resources). Only these get the rich favicon-card treatment; everything else
// keeps the lightweight chip/label row.
const isSearchTool = (rawName: string): boolean => {
  const name = rawName.replace(/^mcp__/, "").toLowerCase();
  return name.includes("search") || name.includes("webfetch") || name.includes("fetch");
};

// Walk an arbitrary parsed tool_result payload and collect every {title,url}.
// WebSearch returns nested content blocks whose exact shape varies by provider,
// so we recurse and pick up any object exposing a usable url. Never throws.
const collectResults = (node: unknown, out: SearchResult[], seen: Set<string>): void => {
  if (out.length >= 12 || node === null || typeof node !== "object") return;
  if (Array.isArray(node)) {
    for (const item of node) collectResults(item, out, seen);
    return;
  }
  const obj = node as Record<string, unknown>;
  const rawUrl = typeof obj.url === "string" ? obj.url : "";
  if (rawUrl.startsWith("http")) {
    let host = "";
    try {
      host = new URL(rawUrl).host.replace(/^www\./, "");
    } catch {
      host = "";
    }
    if (host && !seen.has(rawUrl)) {
      seen.add(rawUrl);
      const title =
        (typeof obj.title === "string" && obj.title.trim()) ||
        (typeof obj.page_title === "string" && obj.page_title.trim()) ||
        host;
      out.push({ title, url: rawUrl, host });
    }
  }
  for (const v of Object.values(obj)) {
    if (v && typeof v === "object") collectResults(v, out, seen);
  }
};

const parseSearchResults = (result: unknown): SearchResult[] => {
  if (result === undefined || result === null) return [];
  let payload: unknown = result;
  if (typeof result === "string") {
    const trimmed = result.trim();
    if (!trimmed) return [];
    try {
      payload = JSON.parse(trimmed);
    } catch {
      return [];
    }
  }
  const out: SearchResult[] = [];
  collectResults(payload, out, new Set<string>());
  return out;
};

// A single web resource pill: favicon + title, links out in a new tab.
const SearchResultCard: FC<{ item: SearchResult }> = ({ item }) => (
  <a
    href={item.url}
    target="_blank"
    rel="noopener noreferrer"
    title={item.url}
    className="inline-flex max-w-[20rem] items-center gap-1.5 rounded-full border border-border/70 bg-background px-2 py-1 text-xs text-foreground transition-colors hover:border-border hover:bg-muted"
  >
    <Image
      src={`https://www.google.com/s2/favicons?domain=${item.host}&sz=64`}
      alt=""
      width={16}
      height={16}
      unoptimized
      className="size-4 shrink-0 rounded-sm"
      aria-hidden="true"
    />
    <span className="truncate">{item.title}</span>
    <ExternalLink className="size-3 shrink-0 text-muted-foreground/60" />
  </a>
);

const ToolFallback: FC<ToolCallMessagePartProps> = ({
  toolName,
  argsText,
  result,
  isError,
  status,
}) => {
  const meta = getToolMeta(toolName);
  const running = status.type === "running" || status.type === "requires-action";
  const Icon = meta.icon;
  const chips = extractToolChips(argsText ?? "");
  const label = isError ? "工具调用失败" : running ? meta.running : meta.done;
  const hasArgs = Boolean((argsText ?? "").trim());
  // Rich result cards only for web-search/fetch tools once their result lands.
  const searchResults = !isError && isSearchTool(toolName) ? parseSearchResults(result) : [];

  return (
    <RailRow
      icon={Icon}
      label={label}
      running={running}
      tone={isError ? "error" : "default"}
    >
      {chips.length ? (
        <div className="flex flex-wrap gap-1.5">
          {chips.map((chip, i) => (
            <span
              key={i}
              className="inline-block max-w-full break-words rounded-md bg-muted px-2 py-1 align-top font-mono text-[11px] leading-5 text-muted-foreground"
            >
              {chip}
            </span>
          ))}
        </div>
      ) : null}
      {searchResults.length ? (
        <div className="mt-1.5 flex flex-wrap gap-1.5">
          {searchResults.map((item) => (
            <SearchResultCard key={item.url} item={item} />
          ))}
        </div>
      ) : null}
      {hasArgs ? (
        <details className="aui-details group mt-1.5 text-sm">
          <summary className="flex w-fit cursor-pointer select-none items-center gap-1 py-0.5 text-xs text-muted-foreground/70 transition-colors hover:text-foreground [&::-webkit-details-marker]:hidden">
            <ChevronRight className="size-3 shrink-0 transition-transform group-open:rotate-90" />
            <span>查看调用参数</span>
          </summary>
          <div className="aui-details-body mt-1 border-l-2 border-border/70 pl-3">
            <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words py-1 font-mono text-[11px] leading-5 text-muted-foreground">
              {formatPayload(argsText)}
            </pre>
          </div>
        </details>
      ) : null}
    </RailRow>
  );
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
