"use client";

import {
  ActionBarPrimitive,
  AttachmentPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
  type ToolCallMessagePartProps,
} from "@assistant-ui/react";
import {
  ArrowDownIcon,
  CopyIcon,
  MessagesSquare,
  PaperclipIcon,
  SendHorizontalIcon,
  Square,
  XIcon,
  Zap,
} from "lucide-react";
import type { FC } from "react";
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
          <div className="sticky bottom-0 mt-3 flex w-full max-w-[var(--thread-max-width)] flex-col items-center justify-end rounded-t-lg bg-background pb-4">
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

const ThreadWelcome: FC = () => (
  <ThreadPrimitive.Empty>
    <div className="flex w-full max-w-[var(--thread-max-width)] flex-grow flex-col items-center justify-center">
      <div className="flex items-center gap-3">
        <div className="flex size-9 items-center justify-center rounded-full bg-foreground text-background">
          <MessagesSquare className="size-5" />
        </div>
        <p className="text-3xl font-semibold text-foreground">gpt-4.1-nano</p>
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

const Composer: FC = () => (
  <ComposerPrimitive.Root className="flex w-full flex-col rounded-[1.5rem] border border-input bg-card px-3 py-1.5 shadow-sm transition-colors focus-within:border-ring">
    <ComposerAttachments />
    <div className="flex w-full items-end">
      <ComposerPrimitive.AddAttachment asChild>
        <TooltipIconButton
          tooltip="Attach file"
          variant="ghost"
          className="my-1 size-8 shrink-0 rounded-full p-2 text-muted-foreground"
        >
          <PaperclipIcon className="h-4 w-4" />
        </TooltipIconButton>
      </ComposerPrimitive.AddAttachment>
      <ComposerPrimitive.Input
        rows={1}
        autoFocus
        placeholder="How can I help you today?"
        className="max-h-40 flex-grow resize-none border-none bg-transparent px-2 py-3 text-sm outline-none placeholder:text-muted-foreground focus:ring-0 disabled:cursor-not-allowed"
      />
      <ComposerAction />
    </div>
  </ComposerPrimitive.Root>
);

// Pending attachment chips shown inside the composer before send. Each chip
// carries the file name plus a remove control; the runtime holds the File until
// send(), when Base64AttachmentAdapter turns it into a base64 FileMessagePart.
const ComposerAttachments: FC = () => (
  <ComposerPrimitive.Attachments
    components={{
      Attachment: () => (
        <AttachmentPrimitive.Root className="relative flex items-center gap-2 rounded-lg border border-border bg-muted px-3 py-1.5 text-xs text-foreground">
          <PaperclipIcon className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="max-w-[12rem] truncate">
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
      <MessagePrimitive.Attachments
        components={{
          Attachment: () => (
            <AttachmentPrimitive.Root className="flex items-center gap-2 rounded-lg border border-border bg-muted/60 px-3 py-1.5 text-xs text-foreground">
              <PaperclipIcon className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="max-w-[12rem] truncate">
                <AttachmentPrimitive.Name />
              </span>
            </AttachmentPrimitive.Root>
          ),
        }}
      />
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
      <MessagePrimitive.Parts
        components={{
          Text: MarkdownText,
          Reasoning: ReasoningPart,
          tools: { Fallback: ToolFallback },
        }}
      />
    </div>
    <AssistantActionBar />
  </MessagePrimitive.Root>
);

const ReasoningPart: FC<{ text: string }> = ({ text }) => (
  <div className="my-2 border-l-2 border-border pl-3 text-sm italic text-muted-foreground">
    {text}
  </div>
);

const ToolFallback: FC<ToolCallMessagePartProps> = ({ toolName, argsText, result, isError }) => (
  <ToolCard
    title={toolName}
    args={argsText}
    result={result === undefined ? undefined : String(result)}
    isError={isError}
  />
);

const ToolCard: FC<{
  title: string;
  args?: string;
  result?: string;
  isError?: boolean;
}> = ({ title, args, result, isError }) => (
  <details className="my-2 overflow-hidden rounded-lg border border-border bg-muted/50 text-sm">
    <summary className="cursor-pointer select-none px-3 py-2 font-mono text-xs text-muted-foreground">
      <span className="mr-2 rounded bg-muted px-1.5 py-0.5">tool</span>
      {title}
    </summary>
    <div className="border-t border-border px-3 py-2">
      {args ? (
        <pre className="overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs text-muted-foreground">
          {args}
        </pre>
      ) : null}
      {result !== undefined ? (
        <pre
          className={cn(
            "mt-2 overflow-x-auto whitespace-pre-wrap break-words border-t border-border pt-2 font-mono text-xs",
            isError ? "text-destructive" : "text-muted-foreground",
          )}
        >
          {result}
        </pre>
      ) : null}
    </div>
  </details>
);

const AssistantActionBar: FC = () => (
  <ActionBarPrimitive.Root
    hideWhenRunning
    autohide="not-last"
    className="col-start-1 row-start-2 -ml-1 flex gap-1 text-muted-foreground"
  >
    <ActionBarPrimitive.Copy asChild>
      <TooltipIconButton tooltip="Copy">
        <CopyIcon className="h-4 w-4" />
      </TooltipIconButton>
    </ActionBarPrimitive.Copy>
  </ActionBarPrimitive.Root>
);
