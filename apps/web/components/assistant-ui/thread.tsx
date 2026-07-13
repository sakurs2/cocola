"use client";

import {
  ActionBarPrimitive,
  AttachmentPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  type FileMessagePartProps,
  type ReasoningMessagePartProps,
  type TextMessagePartProps,
  ThreadPrimitive,
  type ToolCallMessagePartProps,
  useMessage,
  useThread,
} from "@assistant-ui/react";
import * as Popover from "@radix-ui/react-popover";
import { Command } from "cmdk";
import { motion } from "framer-motion";
import {
  ArrowDownIcon,
  BrainCircuit,
  Check,
  ChevronDown,
  CopyIcon,
  PaperclipIcon,
  Search,
  SendHorizontalIcon,
  Square,
  XIcon,
  ArrowRight,
  ArrowUp as ArrowUpIcon,
  BarChart3,
  Code2,
  Lightbulb,
  Pencil,
  Sparkles,
} from "lucide-react";
import Image from "next/image";
import { useEffect, useState, type FC } from "react";
import { useCocola, type ModelIconConfig, type UiMessageMetadata } from "@/app/runtime-provider";
import { CocolaWordmark } from "@/components/assistant-ui/cocola-wordmark";
import { CocolaLogo } from "@/components/cocola-logo";
import { CocolaTagline } from "@/components/assistant-ui/cocola-tagline";
import { TooltipIconButton } from "@/components/assistant-ui/tooltip-icon-button";
import { MarkdownText } from "@/components/assistant-ui/markdown-text";
import {
  RailEnvironment,
  RailFile,
  RailReasoning,
  RailResponsePending,
  RailText,
  RailTool,
} from "@/components/assistant-ui/rail";
import { type EnvironmentPreparationSnapshot } from "@/lib/environment";
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
      style={{ ["--thread-max-width" as string]: "52rem" }}
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

const RUNTIME_ICONS: Record<string, ModelIconConfig> = {
  "claude-code": { type: "lobe-icons", slug: "claudecode" },
  codex: { type: "lobe-icons", slug: "codex" },
};

const ThreadWelcome: FC = () => {
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
        <div className="flex flex-col items-center gap-2 sm:flex-row sm:items-center sm:justify-center sm:gap-3">
          <h1 className="sr-only">{greeting}</h1>
          <CocolaLogo className="h-28 w-28 shrink-0 sm:h-32 sm:w-32" />
          <div className="flex flex-col items-center text-center sm:-ml-6">
            <CocolaWordmark className="cocola-wordmark -my-4 h-32 w-auto max-w-[min(90vw,460px)] sm:h-36" />
            <CocolaTagline />
          </div>
        </div>
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
  const { selectedModel, selectedRuntime, modelsLoaded } = useCocola();
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
            noModel
              ? selectedRuntime?.model_protocol === "openai-responses"
                ? "Codex requires an OpenAI Responses model"
                : selectedRuntime
                  ? `No ${selectedRuntime.label} compatible model configured`
                  : "No Agent Runtime available"
              : "Send a message... (@ to mention, / for commands)"
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
            <RuntimePicker />
            <ModelPicker />
          </div>
          <ComposerAction />
        </div>
      </ComposerPrimitive.Root>
    </motion.div>
  );
};

const RuntimePicker: FC = () => {
  const { runtimes, selectedRuntime, runtimeLocked, setSelectedRuntimeId } = useCocola();
  const [open, setOpen] = useState(false);

  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <button
          type="button"
          className="flex max-w-[11rem] min-w-0 items-center gap-1.5 rounded-full border border-transparent px-2 py-1.5 text-sm font-medium text-foreground transition-colors hover:border-border hover:bg-muted disabled:cursor-not-allowed disabled:opacity-70"
          aria-label="Select Agent Runtime"
          disabled={runtimeLocked || runtimes.length === 0}
          title={runtimeLocked ? "Runtime is fixed for this conversation" : "Select Agent Runtime"}
        >
          <ModelIcon icon={RUNTIME_ICONS[selectedRuntime?.id ?? ""]} className="size-5" />
          <span className="truncate">{selectedRuntime?.label ?? "No runtime"}</span>
          {runtimeLocked || runtimes.length === 0 ? null : (
            <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
          )}
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          side="top"
          align="start"
          sideOffset={10}
          className="cocola-user-ui z-50 w-56 overflow-hidden rounded-2xl border border-border bg-popover p-1.5 text-popover-foreground shadow-xl"
        >
          {runtimes.map((runtime) => (
            <button
              key={runtime.id}
              type="button"
              className="flex w-full items-center gap-2 rounded-xl px-2 py-2 text-left text-sm hover:bg-accent"
              onClick={() => {
                setSelectedRuntimeId(runtime.id);
                setOpen(false);
              }}
            >
              <ModelIcon icon={RUNTIME_ICONS[runtime.id]} className="size-6" />
              <span className="min-w-0 flex-1 truncate font-medium">{runtime.label}</span>
              {runtime.id === selectedRuntime?.id ? <Check className="size-4" /> : null}
            </button>
          ))}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
};

const ModelPicker: FC = () => {
  const { models, selectedModel, selectedModelID, setSelectedModelID, modelsLoaded } = useCocola();
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
          // Popover.Portal mounts to <body>, which sits under <html class="dark">
          // and outside the .cocola-user-ui wrapper — so it would inherit the dark
          // --popover token (near-black). Re-declare the user theme on the content
          // itself so its tokens (bg-popover, border, accent, ...) resolve light.
          className="cocola-user-ui z-50 w-72 overflow-hidden rounded-2xl border border-border bg-popover text-popover-foreground shadow-xl"
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
                  key={model.id}
                  value={`${model.label} ${model.alias} ${model.provider ?? ""}`}
                  className="flex cursor-pointer items-center gap-2 rounded-xl px-2 py-2 text-sm outline-none data-[selected=true]:bg-accent data-[selected=true]:text-accent-foreground"
                  onSelect={() => {
                    setSelectedModelID(model.id);
                    setOpen(false);
                  }}
                >
                  <ModelIcon icon={model.icon} className="size-6" />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium">{model.label}</span>
                    <span className="block truncate text-xs text-muted-foreground">
                      {model.alias}
                      {model.provider ? ` · ${model.provider}` : ""}
                    </span>
                  </span>
                  {model.id === selectedModelID ? <Check className="size-4" /> : null}
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
          <AssistantMessageParts />
        </div>
      </div>
    </div>
    <AssistantActionBar />
  </MessagePrimitive.Root>
);

// Renders the message's parts. The vertical rail connector under the FINAL node
// is hidden only while this (last) message is still streaming — so the trailing
// line does not dangle mid-generation. Once the turn completes the connector is
// restored, keeping the rail continuous with whatever renders below.
const AssistantMessageParts: FC = () => {
  const isLast = useMessage((m) => m.isLast);
  const isRunning = useThread((t) => t.isRunning);
  const custom = useMessage((m) => m.metadata.custom) as UiMessageMetadata & {
    environmentPreparation?: EnvironmentPreparationSnapshot;
    environmentOnly?: boolean;
  };
  const streaming = isLast && isRunning;
  const awaitingFirstResponsePart =
    streaming &&
    custom.environmentOnly === true &&
    custom.environmentPreparation != null &&
    custom.environmentPreparation.state !== "preparing";
  return (
    <div className={streaming ? "aui-rail-streaming" : undefined}>
      {custom.environmentPreparation ? (
        <RailEnvironment environment={custom.environmentPreparation} />
      ) : null}
      {awaitingFirstResponsePart ? <RailResponsePending /> : null}
      {!custom.environmentOnly ? (
        <MessagePrimitive.Parts
          components={{
            Text: TextPart,
            Reasoning: ReasoningPart,
            File: ArtifactFilePart,
            tools: { Fallback: ToolFallback },
          }}
        />
      ) : null}
    </div>
  );
};

const AssistantMessageHeader: FC = () => {
  const { selectedModel } = useCocola();
  const metadata = useMessage((m) => m.metadata.custom) as UiMessageMetadata | undefined;
  const label = metadata?.model_label || selectedModel?.label || "Model";
  const icon = metadata?.model_icon || selectedModel?.icon;

  return (
    <div className="mb-2 grid grid-cols-[1.75rem_1fr] gap-x-2.5 text-sm text-muted-foreground">
      <div className="flex items-center justify-center">
        <ModelIcon icon={icon} className="size-5 opacity-90" />
      </div>
      <span className="min-w-0 self-center truncate font-medium">{label}</span>
    </div>
  );
};

const ArtifactFilePart: FC<FileMessagePartProps> = ({ filename, mimeType, data }) => {
  const { activeSessionId, openArtifact } = useCocola();
  const meta = parseArtifactData(data);
  const name = filename || "file";
  const type = mimeType || "application/octet-stream";
  const downloadUrl = meta.url || data;

  return (
    <RailFile
      filename={name}
      mimeType={type}
      size={meta.size}
      downloadUrl={downloadUrl}
      onPreview={() =>
        openArtifact({
          id: meta.id || name,
          sessionId: activeSessionId,
          filename: name,
          mimeType: type,
          size: meta.size,
          downloadUrl,
        })
      }
    />
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

// Plain assistant text answer, rendered as a rail node via the shared layer.
// While the text part is still streaming (status "running") the node icon spins
// in place — the single, localized "answering" affordance.
const TextPart: FC<TextMessagePartProps> = ({ status }) => (
  <RailText running={status.type === "running"}>
    <MarkdownText />
  </RailText>
);

const ReasoningPart: FC<ReasoningMessagePartProps> = ({ text, status }) => (
  <RailReasoning text={text} running={status.type === "running"} />
);

// Tool call rendering delegates to the shared rail layer. The gateway streams
// tool_use (name + input) and a bare tool_result (id + is_error); RailTool turns
// that into a light status row with input-derived chips and web-result cards.
const ToolFallback: FC<ToolCallMessagePartProps> = ({
  toolName,
  argsText,
  result,
  isError,
  status,
}) => (
  <RailTool
    toolName={toolName}
    argsText={argsText}
    result={result}
    isError={isError}
    running={status.type === "running" || status.type === "requires-action"}
  />
);

const AssistantActionBar: FC = () => {
  // Copy control stays resident: autohide="never" so every completed assistant
  // turn keeps its copy button, not just on hover.
  //
  // We deliberately do NOT use the library's `hideWhenRunning`: it keys off the
  // THREAD-level isRunning, so a new turn streaming would hide the copy button
  // on EVERY prior assistant message. Instead we hide the bar only for the one
  // message that is actively streaming (the last one while the thread runs).
  const isLast = useMessage((m) => m.isLast);
  const isRunning = useThread((t) => t.isRunning);
  if (isLast && isRunning) return null;
  return (
    <ActionBarPrimitive.Root
      autohide="never"
      className="col-start-1 row-start-2 ml-1 flex gap-1 text-muted-foreground"
    >
      <ActionBarPrimitive.Copy asChild>
        <TooltipIconButton tooltip="Copy">
          <CopyIcon className="h-4 w-4" />
        </TooltipIconButton>
      </ActionBarPrimitive.Copy>
    </ActionBarPrimitive.Root>
  );
};
