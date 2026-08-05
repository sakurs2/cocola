"use client";

import {
  ActionBarPrimitive,
  AttachmentPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  type DataMessagePartProps,
  type FileMessagePartProps,
  type ReasoningMessagePartProps,
  type TextMessagePartProps,
  ThreadPrimitive,
  type ToolCallMessagePartProps,
  unstable_useMentionAdapter,
  unstable_useSlashCommandAdapter,
  unstable_useTriggerPopoverScopeContext,
  useComposer,
  useComposerRuntime,
  useMessage,
  useThread,
  useThreadComposerAttachment,
} from "@assistant-ui/react";
import { ChatMessage } from "@heroui-pro/react/chat-message";
import { ChatConversation } from "@heroui-pro/react/chat-conversation";
import { PromptInput } from "@heroui-pro/react/prompt-input";
import { PromptSuggestion } from "@heroui-pro/react/prompt-suggestion";
import { Button, Dropdown, Label } from "@heroui/react";
import {
  BookOpen as GravityBookOpen,
  Bulb,
  ChartColumn,
  ChevronDown as GravityChevronDown,
  Code,
  FaceRobot,
  Paperclip as GravityPaperclip,
  Pencil,
  Xmark,
} from "@gravity-ui/icons";
import { motion } from "framer-motion";
import {
  ArrowDownIcon,
  Check,
  CopyIcon,
  PaperclipIcon,
  SendHorizontalIcon,
  XIcon,
  ArrowUp as ArrowUpIcon,
  Map as PlanModeIcon,
  Sparkles,
} from "lucide-react";
import {
  createContext,
  forwardRef,
  Fragment,
  useCallback,
  useContext,
  useEffect,
  useId,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  type ComponentProps,
  type FC,
  type ReactNode,
} from "react";
import { useCocola, type UiMessageMetadata } from "@/app/runtime-provider";
import { AgentAvatar } from "@/components/agents/agent-avatar";
import { CocolaTagline } from "@/components/assistant-ui/cocola-tagline";
import { CocolaWordmark } from "@/components/assistant-ui/cocola-wordmark";
import { PlanCardPart } from "@/components/assistant-ui/plan-card";
import {
  QuestionCardPart,
  RunSummaryPart,
  StructuredResultCardPart,
} from "@/components/assistant-ui/rich-message-parts";
import { TooltipIconButton } from "@/components/assistant-ui/tooltip-icon-button";
import { MarkdownText } from "@/components/assistant-ui/markdown-text";
import { CocolaLogo } from "@/components/cocola-logo";
import { ModelIcon } from "@/components/ui/model-icon";
import {
  RailEnvironment,
  RailFile,
  RailMemoryRecall,
  RailProgress,
  RailProcessSummary,
  RailReasoning,
  RailResponsePending,
  RailSCMApproval,
  RailText,
  RailTool,
} from "@/components/assistant-ui/rail";
import { type EnvironmentPreparationSnapshot } from "@/lib/environment";
import {
  buildAgentTurnRenderPlan,
  finalAgentOutputText,
  splitAgentTurnParts,
} from "@/lib/agent-turn-summary.mjs";
import { toolOutcomeFromArtifact } from "@/lib/tool-failure.mjs";
import {
  createWikiComposerAttachment,
  isWikiComposerAttachment,
  layoutWikiComposerMentions,
  wikiComposerMentionText,
  wikiReferencesFromAttachments,
} from "@/lib/wiki-composer-reference";
import {
  COMPOSER_SLASH_COPY,
  PLAN_MODE_COMMAND,
  PLAN_MODE_COPY,
  isPlanModeCommandAvailable,
  planComposerContext,
} from "@/lib/plan-mode.mjs";
import {
  fileMatchesPromptStarterSlot,
  firstMissingPromptStarterSlot,
  layoutPromptStarterSlots,
  promptStarterSlotMarker,
  replacePromptStarterSlotValue,
  restorePromptStarterSlotValue,
  type PromptStarterFileSlot,
  type PromptStarterSlotBinding,
  type PromptStarterSlotBindings,
} from "@/lib/prompt-starter";
import { findLatestProgressItems, normalizeProgressItems } from "@/lib/progress-items.mjs";
import { cn } from "@/lib/utils";
import { SkillIcon } from "@/components/ui/skill-icon";
import { useProjectComposerBranchControl } from "@/components/assistant-ui/project-branch-control";

// HeroUI Demo owns the presentation; assistant-ui and Cocola retain the live
// message semantics, streaming lifecycle, attachments, Skills, Wiki, and Plan Mode.

export const Thread: FC = () => {
  return (
    <WikiMentionCatalogProvider>
      <ThreadPrimitive.Root
        className="relative flex h-full flex-col overflow-hidden bg-transparent"
        style={{ ["--thread-max-width" as string]: "58rem" }}
      >
        <ThreadPrimitive.If empty>
          <ThreadPrimitive.Viewport className="h-full overflow-y-auto">
            <ThreadWelcome />
          </ThreadPrimitive.Viewport>
        </ThreadPrimitive.If>
        <ThreadPrimitive.If empty={false}>
          <ChatConversation className="relative z-10 min-h-0 flex-1 overflow-hidden">
            <ThreadPrimitive.Viewport className="flex h-full flex-1 flex-col items-center overflow-y-auto scroll-smooth px-4 sm:px-6 [scrollbar-gutter:stable_both-edges]">
              <ChatConversation.Content className="flex min-h-full w-full max-w-[var(--thread-max-width)] flex-col gap-0 px-0 pb-0 pt-4">
                <div className="h-6 w-full shrink-0" aria-hidden="true" />

                <ActiveExecutionDock />

                <ThreadPrimitive.Messages
                  components={{
                    UserMessage,
                    AssistantMessage,
                  }}
                />

                <div className="min-h-8 flex-grow" />

                <div className="sticky bottom-0 z-30 mt-3 flex w-full flex-col items-center justify-end bg-gradient-to-t from-background via-background to-transparent pb-5 pt-4">
                  <ScrollToBottom />
                  <ConversationComposer />
                </div>
              </ChatConversation.Content>
            </ThreadPrimitive.Viewport>
          </ChatConversation>
        </ThreadPrimitive.If>
      </ThreadPrimitive.Root>
    </WikiMentionCatalogProvider>
  );
};

const ActiveExecutionDock: FC = () => {
  const messages = useThread((thread) => thread.messages);
  const isRunning = useThread((thread) => thread.isRunning);
  const items = useMemo(() => {
    if (!isRunning) return undefined;
    const message = messages[messages.length - 1];
    if (message?.role !== "assistant") return undefined;
    const metadata = message.metadata?.custom as UiMessageMetadata | undefined;
    if (metadata?.interaction_mode === "plan") return undefined;
    return findLatestProgressItems(message.content);
  }, [isRunning, messages]);

  if (normalizeProgressItems(items).length === 0) return null;

  return (
    <div className="pointer-events-none sticky top-0 z-30 flex w-full shrink-0 justify-center bg-gradient-to-b from-background via-background/95 to-transparent pb-3 pt-1">
      <div className="pointer-events-auto w-full max-w-[var(--thread-max-width)]">
        <RailProgress items={items} pinned />
      </div>
    </div>
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

type PromptStarter = {
  icon: typeof ChartColumn;
  label: string;
  iconClassName: string;
  prompt: string;
  fileSlots?: readonly PromptStarterFileSlot[];
};

const SPREADSHEET_FILE_SLOT: PromptStarterFileSlot = {
  key: "spreadsheet",
  label: "Choose spreadsheet",
  accept: [".xlsx", ".csv"],
  required: true,
};

const PROMPT_STARTERS: PromptStarter[] = [
  {
    icon: ChartColumn,
    label: "Excel analysis",
    iconClassName: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-300",
    prompt: `Analyze ${promptStarterSlotMarker(
      SPREADSHEET_FILE_SLOT,
    )} and summarize key trends, anomalies, and actionable insights.`,
    fileSlots: [SPREADSHEET_FILE_SLOT],
  },
  {
    icon: Pencil,
    label: "Write a draft",
    iconClassName: "bg-blue-500/10 text-blue-600 dark:text-blue-300",
    prompt: "Draft a project plan for a new product.",
  },
  {
    icon: Code,
    label: "Write code",
    iconClassName: "bg-violet-500/10 text-violet-600 dark:text-violet-300",
    prompt: "Write a Python script to automate this task.",
  },
  {
    icon: Bulb,
    label: "Brainstorm",
    iconClassName: "bg-pink-500/10 text-pink-600 dark:text-pink-300",
    prompt: "Brainstorm creative ideas for a campaign.",
  },
];

const EMPTY_PROMPT_SLOT_BINDINGS: PromptStarterSlotBindings = {};
const EMPTY_PROMPT_FILE_SLOTS: readonly PromptStarterFileSlot[] = [];

type ComposerWikiInputHandle = {
  focus: () => void;
  openFileSlot: (slotKey: string) => void;
};

const ThreadWelcome: FC = () => {
  const composer = useComposerRuntime();
  const composerIsEmpty = useComposer((state) => state.isEmpty);
  const { selectedAgent } = useCocola();
  const visiblePromptStarters = selectedAgent ? [] : PROMPT_STARTERS;
  const [activePromptStarter, setActivePromptStarter] = useState<PromptStarter | null>(null);
  const [promptSlotBindings, setPromptSlotBindings] = useState<
    Record<string, PromptStarterSlotBinding | undefined>
  >({});
  useEffect(() => {
    if (!composerIsEmpty) return;
    setActivePromptStarter(null);
    setPromptSlotBindings({});
  }, [composerIsEmpty]);

  const handlePromptStarterClick = useCallback(
    (starter: PromptStarter) => {
      setPromptSlotBindings({});
      composer.setText(starter.prompt);
      setActivePromptStarter(starter);
    },
    [composer],
  );

  const handlePromptSlotBindingChange = useCallback((binding: PromptStarterSlotBinding) => {
    setPromptSlotBindings((current) => ({
      ...current,
      [binding.slotKey]: binding,
    }));
  }, []);

  const handlePromptSlotBindingRemove = useCallback((slotKey: string) => {
    setPromptSlotBindings((current) => {
      const next = { ...current };
      delete next[slotKey];
      return next;
    });
  }, []);

  const handlePromptStarterDetach = useCallback(() => {
    setActivePromptStarter(null);
    setPromptSlotBindings({});
  }, []);

  return (
    <ThreadPrimitive.Empty>
      <div className="mx-auto flex h-[calc(100svh-3.5rem)] min-h-0 w-full max-w-5xl flex-col px-4 py-4 sm:px-6 lg:px-8">
        <div className="flex flex-1 flex-col items-center justify-center py-10 text-center">
          <h1 className="sr-only">cocola — Your trusty and powerful agent platform</h1>
          <div className="flex flex-col items-center gap-2 sm:flex-row sm:items-center sm:justify-center sm:gap-3">
            <CocolaLogo className="h-28 w-28 shrink-0 sm:h-32 sm:w-32" />
            <div className="flex flex-col items-center text-center sm:-ml-6">
              <CocolaWordmark className="cocola-wordmark -my-4 h-32 w-auto max-w-[min(90vw,460px)] sm:h-36" />
              <CocolaTagline />
            </div>
          </div>
          <div className="mt-7 w-full max-w-3xl">
            <ConversationComposer
              promptStarter={activePromptStarter}
              promptSlotBindings={promptSlotBindings}
              onPromptSlotBindingChange={handlePromptSlotBindingChange}
              onPromptSlotBindingRemove={handlePromptSlotBindingRemove}
              onPromptStarterDetach={handlePromptStarterDetach}
            />
          </div>
          <div className="mt-5 min-h-10 w-full">
            <PromptSuggestion variant="pill">
              <PromptSuggestion.Items className="mx-auto flex w-full max-w-3xl flex-wrap justify-center gap-2.5">
                {visiblePromptStarters.map((starter) => {
                  const { icon: Icon, label, iconClassName } = starter;
                  return (
                    <PromptSuggestion.Item
                      key={label}
                      className="cocola-web-prompt-starter group !min-h-11 !w-auto !items-center !justify-start !rounded-full !border !border-border !px-3.5 !py-2 whitespace-nowrap"
                      showEndIcon={false}
                      onPress={() => handlePromptStarterClick(starter)}
                    >
                      <span
                        className={cn(
                          "cocola-web-prompt-starter-icon flex size-8 shrink-0 items-center justify-center rounded-full",
                          iconClassName,
                        )}
                      >
                        <Icon className="size-4" />
                      </span>
                      <span className="text-sm font-medium leading-tight">{label}</span>
                    </PromptSuggestion.Item>
                  );
                })}
              </PromptSuggestion.Items>
            </PromptSuggestion>
          </div>
        </div>
      </div>
    </ThreadPrimitive.Empty>
  );
};

const ConversationComposerInner: FC<{
  placeholder?: string;
  branchControl?: ReactNode;
  promptStarter?: PromptStarter | null;
  promptSlotBindings?: PromptStarterSlotBindings;
  onPromptSlotBindingChange?: (binding: PromptStarterSlotBinding) => void;
  onPromptSlotBindingRemove?: (slotKey: string) => void;
  onPromptStarterDetach?: () => void;
}> = ({
  placeholder,
  branchControl,
  promptStarter = null,
  promptSlotBindings = EMPTY_PROMPT_SLOT_BINDINGS,
  onPromptSlotBindingChange,
  onPromptSlotBindingRemove,
  onPromptStarterDetach,
}) => {
  const {
    selectedModel,
    selectedRuntime,
    selectedSkill,
    modelsLoaded,
    runtimeConfigError,
    interactionMode,
    revisingPlanId,
    pendingQuestion,
    questionInputLocked,
  } = useCocola();
  const hasCurrentPlan = useThread((thread) =>
    thread.messages.some((message) =>
      message.content.some(
        (part) =>
          part.type === "data" &&
          part.name === "plan" &&
          ["ready", "stopped"].includes(String(part.data.status)),
      ),
    ),
  );
  const contextualBranchControl = useProjectComposerBranchControl();
  const revisingPlanVersion = useThread((thread) => {
    if (!revisingPlanId) return null;
    for (let messageIndex = thread.messages.length - 1; messageIndex >= 0; messageIndex -= 1) {
      const content = thread.messages[messageIndex]?.content ?? [];
      for (let partIndex = content.length - 1; partIndex >= 0; partIndex -= 1) {
        const part = content[partIndex];
        if (
          part?.type === "data" &&
          part.name === "plan" &&
          String(part.data.planId) === revisingPlanId
        ) {
          const version = Number(part.data.version);
          return Number.isInteger(version) && version > 0 ? version : null;
        }
      }
    }
    return null;
  });
  const noModel = !modelsLoaded || !selectedModel;
  const effectiveBranchControl = branchControl ?? contextualBranchControl;
  const promptInputRef = useRef<ComposerWikiInputHandle>(null);
  const composerAttachments = useComposer((state) => state.attachments);
  const composerText = useComposer((state) => state.text);
  const isRunning = useThread((thread) => thread.isRunning);
  const promptFileSlots = promptStarter?.fileSlots ?? EMPTY_PROMPT_FILE_SLOTS;
  const missingPromptFileSlot = firstMissingPromptStarterSlot(
    promptFileSlots,
    promptSlotBindings,
    (attachmentId) => composerAttachments.some((attachment) => attachment.id === attachmentId),
  );

  useEffect(() => {
    if (!promptStarter) return;
    requestAnimationFrame(() => promptInputRef.current?.focus());
  }, [promptStarter]);

  return (
    <div className="relative w-full">
      <ComposerPrimitive.Unstable_TriggerPopoverRoot>
        <ComposerSlashMenu />
        <ComposerWikiMentionMenu />
        <PromptInput
          className={cn(
            "cocola-web-composer w-full",
            interactionMode === "plan" &&
              "[&_[data-slot=prompt-input-shell]]:border-indigo-500/30 [&_[data-slot=prompt-input-shell]]:bg-indigo-500/[0.025] [&_[data-slot=prompt-input-shell]]:shadow-[0_12px_36px_-24px_rgba(79,70,229,0.65)]",
          )}
          maxHeight={220}
          status={isRunning ? "streaming" : "ready"}
          value={composerText}
          variant="primary"
        >
          <PromptInput.Shell>
            <ComposerPrimitive.Root className="contents">
              {selectedRuntime?.id === "claude-code" && interactionMode === "plan" ? (
                <PlanModeContextStrip revisingPlanVersion={revisingPlanVersion} />
              ) : null}
              {selectedSkill ||
              composerAttachments.some((attachment) => !isWikiComposerAttachment(attachment)) ? (
                <PromptInput.Attachments className="flex items-center gap-2 px-4 pt-3">
                  <SelectedSkillChip />
                  <ComposerAttachments />
                </PromptInput.Attachments>
              ) : null}
              <PromptInput.Content>
                <div className="relative min-w-0">
                  <ComposerWikiInput
                    ref={promptInputRef}
                    autoFocus={!noModel}
                    disabled={noModel}
                    placeholder={
                      runtimeConfigError
                        ? runtimeConfigError
                        : noModel
                          ? selectedRuntime
                            ? "No compatible model configured"
                            : "No Agent Runtime available"
                          : pendingQuestion
                            ? "Reply to Cocola…"
                            : interactionMode === "plan"
                              ? hasCurrentPlan
                                ? PLAN_MODE_COPY.revisionPlaceholder
                                : PLAN_MODE_COPY.initialPlaceholder
                              : placeholder || COMPOSER_SLASH_COPY.defaultPlaceholder
                    }
                    promptStarter={promptStarter}
                    promptSlotBindings={promptSlotBindings}
                    onPromptSlotBindingChange={onPromptSlotBindingChange}
                    onPromptSlotBindingRemove={onPromptSlotBindingRemove}
                    onPromptStarterDetach={onPromptStarterDetach}
                  />
                </div>
              </PromptInput.Content>
              <PromptInput.Toolbar>
                <PromptInput.ToolbarStart className="min-w-0 flex-1 overflow-x-auto">
                  <ComposerPrimitive.AddAttachment asChild>
                    <PromptInput.Action
                      isIconOnly
                      aria-label="Attach file"
                      tooltip={noModel ? "No model configured" : "Attach file"}
                      variant="ghost"
                      isDisabled={noModel || questionInputLocked}
                      className="cocola-web-composer-action shrink-0 rounded-xl"
                    >
                      <GravityPaperclip className="size-4" />
                    </PromptInput.Action>
                  </ComposerPrimitive.AddAttachment>
                  <ModelPicker />
                  <AgentPicker />
                  {selectedRuntime?.id === "claude-code" && interactionMode === "plan" ? (
                    <PlanModeIndicator />
                  ) : null}
                  {effectiveBranchControl}
                </PromptInput.ToolbarStart>
                <PromptInput.ToolbarEnd>
                  <ComposerAction
                    missingPromptFileSlot={missingPromptFileSlot}
                    onResolveMissingPromptFileSlot={() =>
                      missingPromptFileSlot
                        ? promptInputRef.current?.openFileSlot(missingPromptFileSlot.key)
                        : undefined
                    }
                  />
                </PromptInput.ToolbarEnd>
              </PromptInput.Toolbar>
            </ComposerPrimitive.Root>
          </PromptInput.Shell>
        </PromptInput>
      </ComposerPrimitive.Unstable_TriggerPopoverRoot>
    </div>
  );
};

// Public entry: guarantees a Wiki mention catalog context is present so the
// composer can be mounted standalone (project/folder pages) as well as inside
// <Thread/>. The provider is idempotent, so nesting is safe.
export const ConversationComposer: FC<ComponentProps<typeof ConversationComposerInner>> = (
  props,
) => (
  <WikiMentionCatalogProvider>
    <ConversationComposerInner {...props} />
  </WikiMentionCatalogProvider>
);

type WikiMentionNode = {
  id: string;
  kind: "folder" | "file";
  name: string;
  logical_path?: string;
  extension?: string;
};

type WikiMentionCatalog = {
  nodes: WikiMentionNode[];
  loading: boolean;
  revision: number;
  ensureLoaded: () => Promise<void>;
};

const WikiMentionCatalogContext = createContext<WikiMentionCatalog | null>(null);

const WikiMentionCatalogProvider: FC<{ children: ReactNode }> = ({ children }) => {
  // Idempotent: if a catalog provider already exists above (e.g. inside
  // <Thread/>), reuse it so standalone <ConversationComposer/> mounts don't
  // create a second catalog with duplicate fetches/listeners.
  const parent = useContext(WikiMentionCatalogContext);
  if (parent) return <>{children}</>;
  return <WikiMentionCatalogProviderInner>{children}</WikiMentionCatalogProviderInner>;
};

const WikiMentionCatalogProviderInner: FC<{ children: ReactNode }> = ({ children }) => {
  const [nodes, setNodes] = useState<WikiMentionNode[]>([]);
  const [loading, setLoading] = useState(false);
  const [revision, setRevision] = useState(0);
  const staleRef = useRef(true);
  const requestRef = useRef<Promise<void> | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  const ensureLoaded = useCallback(() => {
    if (!staleRef.current) return Promise.resolve();
    if (requestRef.current) return requestRef.current;
    const controller = new AbortController();
    abortRef.current = controller;
    setLoading(true);
    const request = fetch("/api/wiki/tree", {
      cache: "no-store",
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) return;
        const body = (await response.json()) as { nodes?: WikiMentionNode[] };
        if (controller.signal.aborted) return;
        setNodes(Array.isArray(body.nodes) ? body.nodes : []);
        staleRef.current = false;
      })
      .catch(() => {})
      .finally(() => {
        if (requestRef.current !== request) return;
        requestRef.current = null;
        abortRef.current = null;
        setLoading(false);
      });
    requestRef.current = request;
    return request;
  }, []);

  useEffect(() => {
    const invalidate = () => {
      staleRef.current = true;
      abortRef.current?.abort();
      abortRef.current = null;
      requestRef.current = null;
      setLoading(false);
      setRevision((value) => value + 1);
    };
    window.addEventListener("cocola:wiki-changed", invalidate);
    return () => {
      window.removeEventListener("cocola:wiki-changed", invalidate);
      abortRef.current?.abort();
    };
  }, []);

  const value = useMemo(
    () => ({ nodes, loading, revision, ensureLoaded }),
    [ensureLoaded, loading, nodes, revision],
  );
  return (
    <WikiMentionCatalogContext.Provider value={value}>
      {children}
    </WikiMentionCatalogContext.Provider>
  );
};

function useWikiMentionCatalog(): WikiMentionCatalog {
  const catalog = useContext(WikiMentionCatalogContext);
  if (!catalog) throw new Error("Wiki mention catalog provider is missing");
  return catalog;
}

const WIKI_MENTION_FORMATTER = {
  serialize: (item: { label: string }) => wikiComposerMentionText(item.label),
  parse: (text: string) => [{ kind: "text" as const, text }],
};

const WikiMentionLoadOnOpen: FC = () => {
  const popover = unstable_useTriggerPopoverScopeContext();
  const { ensureLoaded, revision } = useWikiMentionCatalog();
  useEffect(() => {
    if (popover.open) void ensureLoaded();
  }, [ensureLoaded, popover.open, revision]);
  return null;
};

const ComposerWikiMentionMenu: FC = () => {
  const { questionInputLocked } = useCocola();
  const { loading, nodes } = useWikiMentionCatalog();
  const composer = useComposerRuntime();

  const items = useMemo(
    () =>
      nodes
        .filter((node) => node.kind === "file")
        .map((node) => ({
          id: node.id,
          type: "wiki-file",
          label: node.name.replace(/[\]\n]/g, ""),
          description: node.logical_path || node.name,
          metadata: { extension: node.extension || "" },
        })),
    [nodes],
  );
  const mention = unstable_useMentionAdapter({
    items,
    includeModelContextTools: false,
  });

  if (questionInputLocked) return null;
  return (
    <ComposerPrimitive.Unstable_TriggerPopover
      char="@"
      adapter={mention.adapter}
      isLoading={loading}
      aria-label="Reference a Wiki file"
      className="absolute bottom-[calc(100%+0.625rem)] left-0 z-50 w-full max-w-2xl overflow-hidden rounded-2xl border border-border bg-overlay text-overlay-foreground shadow-xl"
    >
      <WikiMentionLoadOnOpen />
      <ComposerPrimitive.Unstable_TriggerPopover.Action
        formatter={WIKI_MENTION_FORMATTER}
        onExecute={(item) => {
          void composer.addAttachment(
            createWikiComposerAttachment(
              {
                nodeId: item.id,
                filename: item.label,
                logicalPath: item.description || item.label,
              },
              crypto.randomUUID(),
            ),
          );
        }}
      />
      <ComposerPrimitive.Unstable_TriggerPopoverItems>
        {(results) => (
          <div className="p-1.5">
            <div className="flex items-center gap-2 border-b border-border px-2.5 pb-2 pt-1 text-xs font-medium text-muted">
              <GravityBookOpen className="size-3.5 text-blue-600" />
              Wiki files
            </div>
            <div className="max-h-72 overflow-y-auto pt-1">
              {results.length === 0 ? (
                <div className="px-3 py-8 text-center text-xs text-muted">
                  No Wiki files found. Create or upload one from the Wiki tab.
                </div>
              ) : (
                results.map((item, index) => (
                  <ComposerPrimitive.Unstable_TriggerPopoverItem
                    key={item.id}
                    item={item}
                    index={index}
                    className="flex w-full items-center gap-2.5 rounded-xl px-2.5 py-2 text-left outline-none transition-colors hover:bg-surface-secondary/80 data-[highlighted]:bg-surface-secondary"
                  >
                    <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-blue-50 text-blue-600">
                      <GravityBookOpen className="size-4" />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm font-medium text-foreground">
                        {item.label}
                      </span>
                    </span>
                  </ComposerPrimitive.Unstable_TriggerPopoverItem>
                ))
              )}
            </div>
          </div>
        )}
      </ComposerPrimitive.Unstable_TriggerPopoverItems>
    </ComposerPrimitive.Unstable_TriggerPopover>
  );
};

type ComposerInlineTextSegment = ReturnType<
  typeof layoutWikiComposerMentions
>["segments"][number] & {
  promptSlot?: PromptStarterFileSlot;
  promptSlotBinding?: PromptStarterSlotBinding;
};

const ComposerWikiInput = forwardRef<
  ComposerWikiInputHandle,
  {
    autoFocus: boolean;
    disabled: boolean;
    placeholder: string;
    textIndent?: string;
    promptStarter?: PromptStarter | null;
    promptSlotBindings?: PromptStarterSlotBindings;
    onPromptSlotBindingChange?: (binding: PromptStarterSlotBinding) => void;
    onPromptSlotBindingRemove?: (slotKey: string) => void;
    onPromptStarterDetach?: () => void;
  }
>(
  (
    {
      autoFocus,
      disabled,
      placeholder,
      textIndent,
      promptStarter = null,
      promptSlotBindings = EMPTY_PROMPT_SLOT_BINDINGS,
      onPromptSlotBindingChange,
      onPromptSlotBindingRemove,
      onPromptStarterDetach,
    },
    forwardedRef,
  ) => {
    const composer = useComposerRuntime();
    const text = useComposer((state) => state.text);
    const attachments = useComposer((state) => state.attachments);
    const overlayRef = useRef<HTMLDivElement>(null);
    const inputRef = useRef<HTMLTextAreaElement>(null);
    const fileInputRef = useRef<HTMLInputElement>(null);
    const pickerSlotKeyRef = useRef("");
    const [slotError, setSlotError] = useState("");
    const promptFileSlots = promptStarter?.fileSlots ?? EMPTY_PROMPT_FILE_SLOTS;
    const missingPromptFileSlot = firstMissingPromptStarterSlot(
      promptFileSlots,
      promptSlotBindings,
      (attachmentId) => attachments.some((attachment) => attachment.id === attachmentId),
    );

    const wikiAttachments = useMemo(
      () =>
        attachments.flatMap((attachment, attachmentIndex) => {
          const reference = wikiReferencesFromAttachments([attachment])[0];
          return reference ? [{ attachmentIndex, reference }] : [];
        }),
      [attachments],
    );
    const mentionLayout = useMemo(
      () =>
        layoutWikiComposerMentions(
          text,
          wikiAttachments.map(({ reference }) => reference),
        ),
      [text, wikiAttachments],
    );
    const unmatchedAttachmentIndexes = useMemo(() => {
      const matched = new Set(mentionLayout.matchedReferenceIndexes);
      return wikiAttachments
        .filter((_, referenceIndex) => !matched.has(referenceIndex))
        .map(({ attachmentIndex }) => attachmentIndex);
    }, [mentionLayout.matchedReferenceIndexes, wikiAttachments]);
    const inlineLayout = useMemo(() => {
      const matchedSlotKeys = new Set<string>();
      const segments = mentionLayout.segments.flatMap((segment): ComposerInlineTextSegment[] => {
        if (segment.reference || promptFileSlots.length === 0) return [segment];
        const slotLayout = layoutPromptStarterSlots(
          segment.text,
          promptFileSlots,
          promptSlotBindings,
        );
        slotLayout.matchedSlotKeys.forEach((key) => matchedSlotKeys.add(key));
        return slotLayout.segments.map((slotSegment) => ({
          text: slotSegment.text,
          promptSlot: slotSegment.slot,
          promptSlotBinding: slotSegment.binding,
        }));
      });
      return { segments, matchedSlotKeys };
    }, [mentionLayout.segments, promptFileSlots, promptSlotBindings]);
    const hasInlineMentions =
      mentionLayout.matchedReferenceIndexes.length > 0 || inlineLayout.matchedSlotKeys.size > 0;

    const removeAttachmentByID = useCallback(
      async (attachmentID: string) => {
        const index = composer
          .getState()
          .attachments.findIndex((attachment) => attachment.id === attachmentID);
        if (index < 0) return;
        await composer.getAttachmentByIndex(index).remove();
      },
      [composer],
    );

    const openFileSlot = useCallback(
      (slotKey: string) => {
        const slot = promptFileSlots.find((candidate) => candidate.key === slotKey);
        if (!slot || disabled || !fileInputRef.current) return;
        pickerSlotKeyRef.current = slot.key;
        fileInputRef.current.accept = slot.accept.join(",");
        fileInputRef.current.click();
      },
      [disabled, promptFileSlots],
    );

    useImperativeHandle(
      forwardedRef,
      () => ({
        focus: () => inputRef.current?.focus(),
        openFileSlot,
      }),
      [openFileSlot],
    );

    useEffect(() => {
      if (unmatchedAttachmentIndexes.length === 0) return;
      const staleAttachments = unmatchedAttachmentIndexes.map((index) =>
        composer.getAttachmentByIndex(index),
      );
      void Promise.allSettled(staleAttachments.map((attachment) => attachment.remove()));
    }, [composer, unmatchedAttachmentIndexes]);

    useEffect(() => {
      if (!promptStarter) return;
      const attachmentIDs = new Set(attachments.map((attachment) => attachment.id));
      let restoredText = composer.getState().text;
      let textChanged = false;

      for (const slot of promptFileSlots) {
        const binding = promptSlotBindings[slot.key];
        if (!binding || attachmentIDs.has(binding.attachmentId)) continue;
        const nextText = restorePromptStarterSlotValue(restoredText, slot, binding);
        if (nextText === null) {
          onPromptStarterDetach?.();
          return;
        }
        restoredText = nextText;
        textChanged = true;
        onPromptSlotBindingRemove?.(slot.key);
      }

      if (textChanged) composer.setText(restoredText);
    }, [
      attachments,
      composer,
      onPromptSlotBindingRemove,
      onPromptStarterDetach,
      promptFileSlots,
      promptSlotBindings,
      promptStarter,
    ]);

    useEffect(() => {
      setSlotError("");
    }, [promptStarter]);

    const handleFileChange = useCallback(
      async (event: React.ChangeEvent<HTMLInputElement>) => {
        const file = event.currentTarget.files?.[0];
        event.currentTarget.value = "";
        const slot = promptFileSlots.find(
          (candidate) => candidate.key === pickerSlotKeyRef.current,
        );
        if (!file || !slot) return;
        if (!fileMatchesPromptStarterSlot(file, slot)) {
          setSlotError(`Choose a ${slot.accept.join(" or ")} file.`);
          return;
        }

        setSlotError("");
        const previousAttachmentIDs = new Set(
          composer.getState().attachments.map((attachment) => attachment.id),
        );
        try {
          await composer.addAttachment(file);
        } catch (error) {
          setSlotError(error instanceof Error ? error.message : "The file could not be attached.");
          return;
        }

        const addedAttachment = [...composer.getState().attachments]
          .reverse()
          .find((attachment) => !previousAttachmentIDs.has(attachment.id));
        if (!addedAttachment) {
          setSlotError("The file could not be attached.");
          return;
        }

        const previousBinding = promptSlotBindings[slot.key];
        const nextText = replacePromptStarterSlotValue(
          composer.getState().text,
          slot,
          previousBinding,
          file.name,
        );
        if (nextText === null) {
          await removeAttachmentByID(addedAttachment.id);
          onPromptStarterDetach?.();
          return;
        }

        const binding: PromptStarterSlotBinding = {
          slotKey: slot.key,
          attachmentId: addedAttachment.id,
          filename: file.name.trim().replace(/[\r\n]+/g, " "),
        };
        onPromptSlotBindingChange?.(binding);
        composer.setText(nextText);
        if (previousBinding?.attachmentId && previousBinding.attachmentId !== addedAttachment.id) {
          await removeAttachmentByID(previousBinding.attachmentId);
        }
        requestAnimationFrame(() => {
          inputRef.current?.focus();
        });
      },
      [
        composer,
        onPromptSlotBindingChange,
        onPromptStarterDetach,
        promptFileSlots,
        promptSlotBindings,
        removeAttachmentByID,
      ],
    );

    return (
      <>
        {hasInlineMentions ? (
          <ComposerWikiMentionOverlay
            ref={overlayRef}
            segments={inlineLayout.segments}
            textIndent={textIndent}
            onPromptSlotClick={(slot) => openFileSlot(slot.key)}
          />
        ) : null}
        <ComposerPrimitive.Input
          ref={inputRef}
          rows={1}
          autoFocus={autoFocus}
          disabled={disabled}
          style={textIndent ? { textIndent } : undefined}
          placeholder={placeholder}
          onKeyDown={(event) => {
            if (
              event.key === "Enter" &&
              !event.shiftKey &&
              !event.nativeEvent.isComposing &&
              missingPromptFileSlot
            ) {
              event.preventDefault();
              event.stopPropagation();
              openFileSlot(missingPromptFileSlot.key);
            }
          }}
          onScroll={(event) => {
            if (!overlayRef.current) return;
            overlayRef.current.scrollTop = event.currentTarget.scrollTop;
            overlayRef.current.scrollLeft = event.currentTarget.scrollLeft;
          }}
          className={cn(
            "prompt-input__textarea relative z-[1] block max-h-40 w-full resize-none border-none bg-transparent pb-2 text-[15px] leading-6 outline-none placeholder:text-muted focus:ring-0 disabled:cursor-not-allowed",
            hasInlineMentions &&
              "text-transparent caret-foreground selection:bg-blue-200/60 selection:text-transparent",
          )}
        />
        <input
          ref={fileInputRef}
          type="file"
          tabIndex={-1}
          aria-hidden="true"
          className="hidden"
          onChange={handleFileChange}
        />
        {slotError ? (
          <p className="px-2 pb-1 text-xs text-danger" role="alert">
            {slotError}
          </p>
        ) : null}
      </>
    );
  },
);

ComposerWikiInput.displayName = "ComposerWikiInput";

const ComposerWikiMentionOverlay = forwardRef<
  HTMLDivElement,
  {
    segments: ComposerInlineTextSegment[];
    textIndent?: string;
    onPromptSlotClick: (slot: PromptStarterFileSlot) => void;
  }
>(({ segments, textIndent, onPromptSlotClick }, ref) => (
  <div
    ref={ref}
    style={textIndent ? { textIndent } : undefined}
    className="pointer-events-none absolute inset-0 z-[2] max-h-40 min-h-[5.75rem] overflow-hidden whitespace-pre-wrap break-words px-4 pb-2 pt-4 text-[15px] leading-6 text-foreground"
  >
    {segments.map((segment, index) => {
      if (segment.promptSlot) {
        return (
          <button
            key={`prompt-slot-${segment.promptSlot.key}-${index}`}
            type="button"
            tabIndex={-1}
            aria-label={
              segment.promptSlotBinding
                ? `Replace ${segment.promptSlotBinding.filename}`
                : segment.promptSlot.label
            }
            className={cn(
              "pointer-events-auto cursor-pointer rounded-[4px] shadow-[0_0_0_1px_rgba(37,99,235,0.2)] [box-decoration-break:clone]",
              segment.promptSlotBinding
                ? "bg-emerald-100 text-emerald-700"
                : "bg-blue-100 text-blue-700",
            )}
            onClick={() => onPromptSlotClick(segment.promptSlot!)}
          >
            {segment.text}
          </button>
        );
      }
      return segment.reference ? (
        <span
          key={`${segment.reference.nodeId}-${index}`}
          aria-hidden="true"
          className="rounded-[4px] bg-blue-100 text-blue-700 shadow-[0_0_0_1px_rgba(37,99,235,0.18)] [box-decoration-break:clone]"
        >
          {segment.text}
        </span>
      ) : (
        <span key={index} aria-hidden="true">
          {segment.text}
        </span>
      );
    })}
  </div>
));

ComposerWikiMentionOverlay.displayName = "ComposerWikiMentionOverlay";

const PlanModeContextStrip: FC<{ revisingPlanVersion: number | null }> = ({
  revisingPlanVersion,
}) => {
  const context = planComposerContext(revisingPlanVersion);
  return (
    <div className="-mx-3 -mt-3 mb-1 flex min-w-0 items-center gap-2.5 rounded-t-[15px] border-b border-indigo-500/15 bg-indigo-500/[0.065] px-3 py-2.5">
      <span className="grid size-7 shrink-0 place-items-center rounded-lg bg-indigo-600 text-white shadow-sm">
        <PlanModeIcon className="size-3.5" aria-hidden="true" />
      </span>
      <div className="min-w-0">
        <div className="truncate text-xs font-semibold text-indigo-700">{context.label}</div>
        <div className="truncate text-[11px] leading-4 text-indigo-700/70">
          {context.description}
        </div>
      </div>
    </div>
  );
};

const ComposerSlashMenu: FC = () => {
  const {
    skills,
    skillsLoaded,
    selectedRuntime,
    selectedSkill,
    interactionMode,
    setInteractionMode,
    setSelectedSkillId,
    questionInputLocked,
  } = useCocola();
  const isRunning = useThread((thread) => thread.isRunning);
  const [activeTab, setActiveTab] = useState<"commands" | "skills">("commands");
  const tabsId = useId();
  const skillByID = useMemo(() => new Map(skills.map((skill) => [skill.id, skill])), [skills]);
  const canSelectPlanMode = isPlanModeCommandAvailable(
    selectedRuntime?.id,
    interactionMode,
    isRunning,
  );
  const planCommands = useMemo(
    () =>
      canSelectPlanMode
        ? [
            {
              ...PLAN_MODE_COMMAND,
              execute: () => setInteractionMode("plan"),
            },
          ]
        : [],
    [canSelectPlanMode, setInteractionMode],
  );
  const skillCommands = useMemo(
    () =>
      [
        ...skills.filter((skill) => skill.scope === "user"),
        ...skills.filter((skill) => skill.scope !== "user"),
      ].map((skill) => ({
        id: skill.id,
        label: skill.name,
        description: skill.description,
        execute: () => setSelectedSkillId(skill.id),
      })),
    [setSelectedSkillId, skills],
  );
  const planSlash = unstable_useSlashCommandAdapter({
    commands: planCommands,
    removeOnExecute: true,
  });
  const skillSlash = unstable_useSlashCommandAdapter({
    commands: skillCommands,
    removeOnExecute: true,
  });
  const slash = activeTab === "commands" ? planSlash : skillSlash;

  if (selectedSkill || questionInputLocked) return null;

  return (
    <ComposerPrimitive.Unstable_TriggerPopover
      char="/"
      adapter={slash.adapter}
      aria-label={COMPOSER_SLASH_COPY.menuAriaLabel}
      className="absolute bottom-[calc(100%+0.625rem)] left-0 z-50 w-full max-w-2xl overflow-hidden rounded-2xl border border-border bg-overlay text-overlay-foreground shadow-xl"
    >
      <ComposerPrimitive.Unstable_TriggerPopover.Action {...slash.action} />
      {/* Items stays unmounted while the trigger is closed, so the always-registered
          popover never leaks menu chrome into the composer's document flow. */}
      <ComposerPrimitive.Unstable_TriggerPopoverItems>
        {(items) => {
          const groups =
            activeTab === "commands"
              ? [{ label: "", items }]
              : [
                  {
                    label: "Personal",
                    items: items.filter((item) => skillByID.get(item.id)?.scope === "user"),
                  },
                  {
                    label: "Shared",
                    items: items.filter((item) => skillByID.get(item.id)?.scope !== "user"),
                  },
                ].filter((group) => group.items.length > 0);
          const visibleItemCount = groups.reduce((count, group) => count + group.items.length, 0);

          const rows = groups.flatMap((group) =>
            group.items.map((item, index) => ({
              groupLabel: index === 0 ? group.label : "",
              item,
            })),
          );
          return (
            <>
              <div
                role="tablist"
                aria-label={COMPOSER_SLASH_COPY.menuAriaLabel}
                className="flex items-center gap-1 border-b border-border/70 px-2 pt-2"
              >
                {(
                  [
                    { id: "commands", label: COMPOSER_SLASH_COPY.commandsTab },
                    { id: "skills", label: COMPOSER_SLASH_COPY.skillsTab },
                  ] as const
                ).map((tab) => (
                  <button
                    key={tab.id}
                    type="button"
                    role="tab"
                    id={`${tabsId}-${tab.id}-tab`}
                    aria-controls={`${tabsId}-${tab.id}-panel`}
                    aria-selected={activeTab === tab.id}
                    onClick={() => setActiveTab(tab.id)}
                    className={cn(
                      "relative px-3 py-2 text-sm font-medium text-muted outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-1",
                      activeTab === tab.id &&
                        "text-foreground after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded-full after:bg-accent",
                    )}
                  >
                    {tab.label}
                  </button>
                ))}
              </div>
              <div
                role="tabpanel"
                id={`${tabsId}-${activeTab}-panel`}
                aria-labelledby={`${tabsId}-${activeTab}-tab`}
                className="max-h-72 overflow-y-auto p-1.5"
              >
                {visibleItemCount === 0 ? (
                  <div className="px-3 py-8 text-center text-xs text-muted">
                    {activeTab === "commands"
                      ? COMPOSER_SLASH_COPY.noCommands
                      : skillsLoaded
                        ? COMPOSER_SLASH_COPY.noSkills
                        : COMPOSER_SLASH_COPY.loadingSkills}
                  </div>
                ) : (
                  rows.map(({ groupLabel, item }, index) => {
                    const skill = skillByID.get(item.id);
                    const isPlanModeCommand = item.id === PLAN_MODE_COMMAND.id;
                    return (
                      <Fragment key={item.id}>
                        {groupLabel ? (
                          <div className="px-2.5 pt-2 pb-1 text-xs font-medium text-muted">
                            {groupLabel}
                          </div>
                        ) : null}
                        <ComposerPrimitive.Unstable_TriggerPopoverItem
                          item={item}
                          index={index}
                          className="flex w-full items-center gap-2.5 rounded-xl px-2.5 py-2 text-left outline-none transition-colors hover:bg-surface-secondary/80 data-[highlighted]:bg-surface-secondary"
                        >
                          {isPlanModeCommand ? (
                            <span className="grid size-7 shrink-0 place-items-center rounded-lg bg-indigo-500/10 text-indigo-600">
                              <PlanModeIcon className="size-4" />
                            </span>
                          ) : (
                            <SkillIcon name={skill?.name || item.label} size="sm" />
                          )}
                          <span
                            className="max-w-[45%] shrink-0 truncate text-sm font-medium text-foreground"
                            title={skill?.name || item.label}
                          >
                            {skill?.name || item.label}
                          </span>
                          <span
                            className="min-w-0 flex-1 truncate text-sm text-muted"
                            title={
                              isPlanModeCommand ? PLAN_MODE_COMMAND.description : skill?.description
                            }
                          >
                            {isPlanModeCommand
                              ? PLAN_MODE_COMMAND.description
                              : skill?.description || `/${item.id}`}
                          </span>
                        </ComposerPrimitive.Unstable_TriggerPopoverItem>
                      </Fragment>
                    );
                  })
                )}
              </div>
            </>
          );
        }}
      </ComposerPrimitive.Unstable_TriggerPopoverItems>
    </ComposerPrimitive.Unstable_TriggerPopover>
  );
};

const SelectedSkillChip: FC = () => {
  const { selectedSkill, setSelectedSkillId, questionInputLocked } = useCocola();

  if (!selectedSkill) return null;
  return (
    <span className="bg-accent-soft text-accent inline-flex h-7 max-w-52 items-center gap-1 rounded-full pr-1 pl-2.5 text-xs font-medium">
      <Sparkles className="size-3 shrink-0" />
      <span className="truncate">{selectedSkill.name}</span>
      <Button
        isIconOnly
        aria-label={`Remove ${selectedSkill.name} skill`}
        className="size-5 min-h-5 min-w-5 rounded-full"
        isDisabled={questionInputLocked}
        size="sm"
        variant="ghost"
        onPress={() => setSelectedSkillId(null)}
      >
        <Xmark className="size-3" />
      </Button>
    </span>
  );
};

const PlanModeIndicator: FC = () => {
  const { setInteractionMode, questionInputLocked } = useCocola();
  const isRunning = useThread((thread) => thread.isRunning);

  return (
    <Button
      aria-label="Exit Plan mode"
      className="cocola-web-plan-indicator h-9 shrink-0 rounded-full px-3"
      isDisabled={isRunning || questionInputLocked}
      size="sm"
      onPress={() => setInteractionMode("execute")}
    >
      <GravityBookOpen className="size-3.5" />
      {PLAN_MODE_COPY.activeLabel}
      <Xmark className="size-3" />
    </Button>
  );
};

const ModelPicker: FC = () => {
  const {
    models,
    selectedModel,
    selectedModelID,
    setSelectedModelID,
    modelsLoaded,
    questionInputLocked,
    selectedAgent,
  } = useCocola();
  const noModel = !modelsLoaded || !selectedModel;
  const pickerDisabled = noModel || questionInputLocked || Boolean(selectedAgent);

  return (
    <Dropdown>
      <Dropdown.Trigger
        aria-label={
          selectedAgent ? "Agent model" : noModel ? "No model configured" : "Select model"
        }
        className="cocola-web-composer-selector cocola-web-select-trigger inline-flex h-9 max-w-[14rem] min-w-0 items-center gap-2 rounded-xl border border-transparent px-2.5 text-xs font-medium"
        isDisabled={pickerDisabled}
      >
        <ModelIcon icon={selectedModel?.icon} className="size-4" bare />
        <span className="truncate">{selectedModel?.label ?? "No model"}</span>
        {pickerDisabled ? null : <GravityChevronDown className="text-muted size-3 shrink-0" />}
      </Dropdown.Trigger>
      <Dropdown.Popover className="min-w-60" placement="top start">
        <Dropdown.Menu
          aria-label="Select model"
          selectedKeys={selectedModelID ? [selectedModelID] : []}
          selectionMode="single"
          onAction={(key) => setSelectedModelID(String(key))}
        >
          {models.map((model) => (
            <Dropdown.Item key={model.id} id={model.id} textValue={model.label}>
              <Label className="flex min-w-0 items-center gap-2">
                <ModelIcon icon={model.icon} className="size-5" bare />
                <span className="truncate">{model.label}</span>
              </Label>
            </Dropdown.Item>
          ))}
        </Dropdown.Menu>
      </Dropdown.Popover>
    </Dropdown>
  );
};

const AgentPicker: FC = () => {
  const { agents, agentsLoaded, selectedAgent, selectedAgentID, agentLocked, setSelectedAgentID } =
    useCocola();
  return (
    <Dropdown>
      <Dropdown.Trigger
        aria-label="Select agent"
        className="cocola-web-composer-selector cocola-web-select-trigger inline-flex h-9 max-w-[12rem] min-w-0 shrink-0 items-center gap-2 rounded-xl border border-transparent px-2.5 text-xs font-medium"
        isDisabled={!agentsLoaded || agentLocked}
      >
        {selectedAgent ? (
          <AgentAvatar
            avatarKey={selectedAgent.avatar_key}
            avatarColor={selectedAgent.avatar_color}
            className="size-4 rounded-md ring-0"
            iconClassName="size-2.5"
          />
        ) : (
          <FaceRobot className="size-3.5 text-cyan-600" />
        )}
        <span className="truncate">{selectedAgent?.name ?? "None"}</span>
        {!agentsLoaded || agentLocked ? null : (
          <GravityChevronDown className="text-muted size-3 shrink-0" />
        )}
      </Dropdown.Trigger>
      <Dropdown.Popover placement="top start">
        <Dropdown.Menu
          aria-label="Select agent"
          selectedKeys={selectedAgentID ? [selectedAgentID] : ["none"]}
          selectionMode="single"
          onAction={(key) => setSelectedAgentID(String(key) === "none" ? null : String(key))}
        >
          <Dropdown.Item id="none" textValue="None">
            <Label className="flex min-w-0 items-center gap-2">
              <span className="bg-surface-secondary text-muted grid size-5 shrink-0 place-items-center rounded-md">
                <FaceRobot className="size-3" />
              </span>
              <span>None</span>
            </Label>
          </Dropdown.Item>
          {agents.map((agent) => (
            <Dropdown.Item key={agent.id} id={agent.id} textValue={agent.name}>
              <Label className="flex min-w-0 items-center gap-2">
                <AgentAvatar
                  avatarKey={agent.avatar_key}
                  avatarColor={agent.avatar_color}
                  className="size-5 rounded-md ring-0"
                  iconClassName="size-3"
                />
                <span className="truncate">{agent.name}</span>
              </Label>
            </Dropdown.Item>
          ))}
        </Dropdown.Menu>
      </Dropdown.Popover>
    </Dropdown>
  );
};

// Regular pending files stay below the text input. Wiki references are rendered
// in place by ComposerWikiInput and therefore have no second attachment chip.
const ComposerAttachments: FC = () => (
  <div className="flex flex-wrap gap-1.5 empty:hidden [&:not(:empty)]:pb-1.5">
    <ComposerPrimitive.Attachments
      components={{
        Attachment: ComposerAttachmentChip,
      }}
    />
  </div>
);

const ComposerAttachmentChip: FC = () => {
  const isWiki = useThreadComposerAttachment((attachment) => isWikiComposerAttachment(attachment));
  const name = useThreadComposerAttachment((attachment) => attachment.name);

  if (isWiki) return null;

  return (
    <AttachmentPrimitive.Root className="relative flex w-fit max-w-full self-start items-center gap-2 rounded-lg border border-border bg-surface-secondary px-3 py-1.5 text-xs text-foreground">
      <PaperclipIcon className="size-3.5 shrink-0 text-muted" />
      <span className="max-w-[16rem] truncate">
        <AttachmentPrimitive.Name />
      </span>
      <AttachmentPrimitive.Remove asChild>
        <button
          type="button"
          aria-label={`Remove attachment ${name}`}
          className="ml-1 rounded-full p-0.5 text-muted transition-colors hover:bg-background hover:text-foreground"
        >
          <XIcon className="size-3.5" />
        </button>
      </AttachmentPrimitive.Remove>
    </AttachmentPrimitive.Root>
  );
};

const ComposerAction: FC<{
  missingPromptFileSlot?: PromptStarterFileSlot;
  onResolveMissingPromptFileSlot?: () => void;
}> = ({ missingPromptFileSlot, onResolveMissingPromptFileSlot }) => {
  const { selectedModel, modelsLoaded } = useCocola();
  const noModel = !modelsLoaded || !selectedModel;

  return (
    <>
      <ThreadPrimitive.If running={false}>
        {missingPromptFileSlot ? (
          <Button
            isIconOnly
            aria-label={`${missingPromptFileSlot.label} before sending`}
            className="cocola-web-composer-send"
            isDisabled={noModel}
            onPress={onResolveMissingPromptFileSlot}
          >
            <ArrowUpIcon className="h-4 w-4" />
          </Button>
        ) : (
          <ComposerPrimitive.Send asChild>
            <PromptInput.Send
              aria-label="Send"
              className="cocola-web-composer-send"
              isDisabled={noModel}
              status="ready"
            />
          </ComposerPrimitive.Send>
        )}
      </ThreadPrimitive.If>
      <ThreadPrimitive.If running>
        <ComposerPrimitive.Cancel asChild>
          <PromptInput.Send
            aria-label="Stop"
            className="cocola-web-composer-send"
            status="streaming"
          />
        </ComposerPrimitive.Cancel>
      </ThreadPrimitive.If>
    </>
  );
};

const UserMessage: FC = () => {
  const id = useMessage((m) => m.id);
  return (
    <MessagePrimitive.Root
      data-message-id={id}
      className="message-enter w-full max-w-[var(--thread-max-width)] pb-1 pt-3"
    >
      <ChatMessage.User className="gap-1.5">
        <UserSkillBadge />
        <div className="flex flex-wrap justify-end gap-1.5 empty:hidden">
          <MessagePrimitive.Attachments
            components={{
              Attachment: () => (
                <AttachmentPrimitive.Root className="flex w-fit max-w-full items-center gap-2 rounded-lg border border-border bg-surface-secondary/60 px-3 py-1.5 text-xs text-foreground">
                  <PaperclipIcon className="size-3.5 shrink-0 text-muted" />
                  <span className="max-w-[16rem] truncate">
                    <AttachmentPrimitive.Name />
                  </span>
                </AttachmentPrimitive.Root>
              ),
            }}
          />
        </div>
        <MessagePrimitive.If hasContent>
          <ChatMessage.Bubble className="cocola-chat-user-bubble max-w-[min(72%,42rem)] whitespace-pre-wrap break-words rounded-xl px-3 py-1.5 text-[15px] leading-6 text-foreground">
            <MessagePrimitive.Parts components={USER_PART_COMPONENTS} />
          </ChatMessage.Bubble>
        </MessagePrimitive.If>
      </ChatMessage.User>
    </MessagePrimitive.Root>
  );
};

const WikiFilePart: FC<
  DataMessagePartProps<{
    wikiNodeId: string;
    wikiVersionId?: string;
    filename: string;
    logicalPath: string;
    mimeType?: string;
    size?: number;
    downloadUrl: string;
  }>
> = ({ data }) => (
  <a
    href={data.downloadUrl}
    className="my-1 flex max-w-sm items-center gap-2 rounded-xl border border-blue-200 bg-white/75 px-3 py-2 text-left shadow-sm transition hover:border-blue-300"
  >
    <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-blue-50 text-blue-600">
      <GravityBookOpen className="size-4" />
    </span>
    <span className="min-w-0">
      <span className="block truncate text-xs font-semibold">{data.filename}</span>
      <span className="block truncate text-[10px] text-muted">{data.logicalPath}</span>
    </span>
  </a>
);

const USER_PART_COMPONENTS = {
  data: {
    by_name: {
      "wiki-file": WikiFilePart,
    },
  },
};

const UserSkillBadge: FC = () => {
  const metadata = useMessage((message) => message.metadata.custom) as
    | UiMessageMetadata
    | undefined;
  const { skills } = useCocola();
  const skillID = metadata?.skill_id;
  if (!skillID) return null;
  const label = skills.find((skill) => skill.id === skillID)?.name || skillID;
  return (
    <span className="inline-flex max-w-full items-center gap-1.5 rounded-full border border-accent/15 bg-accent/10 px-2.5 py-1 text-[11px] font-medium text-accent">
      <Sparkles className="size-3 shrink-0" />
      <span className="truncate">{label}</span>
    </span>
  );
};

const AssistantMessage: FC = () => (
  <MessagePrimitive.Root className="message-enter relative w-full max-w-[var(--thread-max-width)] pb-4 pt-1">
    <ChatMessage.Assistant className="cocola-chat-assistant gap-0 py-0">
      <ChatMessage.Body className="gap-0 pe-0">
        <div className="relative max-w-full break-words px-0.5 py-1 leading-7 text-foreground">
          <div className="relative z-[1]">
            <AssistantMessageHeader />
            {/* Vertical timeline rail: one continuous line (the ::before pseudo)
              runs at x=0.875rem — exactly the center of each RailRow icon column
              (1.75rem wide) — so every node's badge sits centered on the line.
              Badges carry bg-background + z-[1] to punch through it. */}
            <AssistantMessageParts />
          </div>
        </div>
        <AssistantActionBar />
      </ChatMessage.Body>
    </ChatMessage.Assistant>
  </MessagePrimitive.Root>
);

// Renders the message's parts. The vertical rail connector under the FINAL node
// is hidden only while this (last) message is still streaming — so the trailing
// line does not dangle mid-generation. Once the turn completes the connector is
// restored, keeping the rail continuous with whatever renders below.
const AssistantMessageParts: FC = () => {
  const isLast = useMessage((m) => m.isLast);
  const isRunning = useThread((t) => t.isRunning);
  const parts = useMessage((m) => m.content);
  const [processExpanded, setProcessExpanded] = useState(false);
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
  const renderPlan = buildAgentTurnRenderPlan(
    parts,
    custom.environmentPreparation != null,
    streaming,
  );
  const activePlanItems = streaming ? findLatestProgressItems(parts) : undefined;
  const hasActivePlan = normalizeProgressItems(activePlanItems).length > 0;

  return (
    <div className={streaming ? "aui-rail-streaming" : undefined}>
      {!streaming && renderPlan.showProcessSummary ? (
        <RailProcessSummary
          key="process-summary"
          durationMs={custom.duration_ms}
          expanded={processExpanded}
          onExpandedChange={setProcessExpanded}
        />
      ) : null}
      {custom.environmentPreparation && (streaming || processExpanded) ? (
        <RailEnvironment key="environment" environment={custom.environmentPreparation} />
      ) : null}
      {awaitingFirstResponsePart ? <RailResponsePending key="response-pending" /> : null}
      {!custom.environmentOnly ? (
        <div
          key="message-parts"
          className={cn(
            "agent-turn-parts",
            hasActivePlan && "agent-turn-plan-pinned",
            !streaming && renderPlan.showProcessSummary && !processExpanded
              ? "agent-turn-collapsed"
              : "",
          )}
        >
          <MessagePrimitive.Parts components={ASSISTANT_PART_COMPONENTS} />
        </div>
      ) : null}
    </div>
  );
};

const AssistantMessageHeader: FC = () => {
  const { selectedModel } = useCocola();
  const metadata = useMessage((m) => m.metadata.custom) as UiMessageMetadata | undefined;
  const label = metadata?.model_label || selectedModel?.label || "Model";
  const icon = metadata?.model_icon || selectedModel?.icon;
  const isPlanMode = metadata?.interaction_mode === "plan";

  return (
    <div className="cocola-chat-model-header mb-3 grid grid-cols-[1.75rem_minmax(0,1fr)] items-center gap-x-2.5">
      <ModelIcon icon={icon} className="size-7 shrink-0" bare />
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <span className="min-w-0 truncate text-[15px] font-semibold leading-none text-foreground">
          {label}
        </span>
        {isPlanMode ? (
          <span className="inline-flex items-center gap-1.5 rounded-full bg-indigo-600 px-2.5 py-1 text-[11px] font-semibold leading-none text-white">
            <PlanModeIcon className="size-3.5 shrink-0" aria-hidden="true" />
            {PLAN_MODE_COPY.responseLabel}
          </span>
        ) : null}
      </div>
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
    <AgentTurnPart kind="file">
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
    </AgentTurnPart>
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
const AgentTurnPart: FC<{
  children: ReactNode;
  kind?: "file" | "process" | "progress";
}> = ({ children, kind }) => (
  <div
    className={cn(
      "agent-turn-part",
      (kind === "process" || kind === "progress") && "agent-turn-process-part",
      kind === "progress" && "agent-turn-progress-part",
      kind === "file" && "agent-turn-file-part",
    )}
  >
    {children}
  </div>
);

const TextPart: FC<TextMessagePartProps> = ({ status }) => (
  <AgentTurnPart>
    <RailText running={status.type === "running"}>
      <MarkdownText />
    </RailText>
  </AgentTurnPart>
);

const ReasoningPart: FC<ReasoningMessagePartProps> = ({ text, status }) => (
  <AgentTurnPart kind="process">
    <RailReasoning text={text} running={status.type === "running"} />
  </AgentTurnPart>
);

// Tool call rendering delegates to the shared rail layer. The gateway streams
// tool_use (name + input) and a bare tool_result (id + is_error); RailTool turns
// that into a light status row with input-derived chips and web-result cards.
const ToolFallback: FC<ToolCallMessagePartProps> = ({
  toolName,
  argsText,
  result,
  isError,
  artifact,
  status,
}) => (
  <AgentTurnPart kind="process">
    <RailTool
      toolName={toolName}
      argsText={argsText}
      result={result}
      isError={isError}
      outcome={toolOutcomeFromArtifact(artifact, isError)}
      running={status.type === "running" || status.type === "requires-action"}
    />
  </AgentTurnPart>
);

const MemoryRecallPart: FC<
  DataMessagePartProps<{
    status: "running" | "hit" | "miss" | "degraded" | "unavailable";
    count: number;
    content?: string;
  }>
> = ({ data }) =>
  data.status === "miss" ? null : (
    <AgentTurnPart kind="process">
      <RailMemoryRecall status={data.status} count={data.count} content={data.content} />
    </AgentTurnPart>
  );

const ProgressPart: FC<DataMessagePartProps<{ items: unknown[] }>> = ({ data }) => (
  <AgentTurnPart kind="progress">
    <RailProgress items={data.items} />
  </AgentTurnPart>
);

const SCMApprovalPart: FC<
  DataMessagePartProps<{
    approvalId: string;
    status: "pending" | "approved" | "denied" | "expired";
    category?: string;
    label?: string;
  }>
> = ({ data }) => {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [resolvedStatus, setResolvedStatus] = useState(data.status);

  useEffect(() => setResolvedStatus(data.status), [data.status]);

  const decide = async (decision: "approved" | "denied") => {
    setBusy(true);
    setError("");
    try {
      const response = await fetch(
        `/api/scm/approvals/${encodeURIComponent(data.approvalId)}/decision`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ decision }),
        },
      );
      if (!response.ok) {
        throw new Error("The approval could not be saved. It may have expired.");
      }
      setResolvedStatus(decision);
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : "Approval failed.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <AgentTurnPart kind="process">
      <RailSCMApproval
        status={resolvedStatus}
        category={data.category}
        commandLabel={data.label}
        busy={busy}
        error={error}
        onDecision={decide}
      />
    </AgentTurnPart>
  );
};

const ASSISTANT_PART_COMPONENTS = {
  Text: TextPart,
  Reasoning: ReasoningPart,
  File: ArtifactFilePart,
  tools: { Fallback: ToolFallback },
  data: {
    by_name: {
      progress: ProgressPart,
      "memory-recall": MemoryRecallPart,
      "scm-approval": SCMApprovalPart,
      plan: PlanCardPart,
      question: QuestionCardPart,
      "run-summary": RunSummaryPart,
      "structured-result": StructuredResultCardPart,
    },
  },
};

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
  const parts = useMessage((m) => m.content);
  const [copied, setCopied] = useState(false);
  const { outputIndices } = splitAgentTurnParts(parts);
  const text = finalAgentOutputText(parts, outputIndices);
  if (isLast && isRunning) return null;

  const copy = async () => {
    if (!text) return;
    await navigator.clipboard.writeText(text);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1_400);
  };

  return (
    <ActionBarPrimitive.Root autohide="never" className="ml-8.5 flex gap-1 text-muted">
      <TooltipIconButton tooltip={copied ? "Copied" : "Copy"} disabled={!text} onClick={copy}>
        {copied ? <Check className="h-4 w-4 text-emerald-500" /> : <CopyIcon className="h-4 w-4" />}
      </TooltipIconButton>
    </ActionBarPrimitive.Root>
  );
};
