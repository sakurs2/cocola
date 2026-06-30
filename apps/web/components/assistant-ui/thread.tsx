"use client";

import {
  ActionBarPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
  type ToolCallMessagePartProps,
} from "@assistant-ui/react";
import { ArrowDownIcon, CopyIcon, SendHorizontalIcon, Square } from "lucide-react";
import type { FC } from "react";
import { TooltipIconButton } from "@/components/assistant-ui/tooltip-icon-button";
import { MarkdownText } from "@/components/assistant-ui/markdown-text";
import { cn } from "@/lib/utils";

// shadcn-style product Thread for cocola, authored against the design tokens in
// app/globals.css so it matches the official assistant-ui look on Tailwind v3.
//
// Wires assistant-ui primitives to local styling: text renders as Markdown,
// reasoning as a dimmed block, and tool calls as collapsible cards (name + args
// + result) matching the backend AgentEvent vocabulary. Only the ExternalStore
// capabilities the adapter implements are surfaced (send / cancel) — no
// attachment or branch controls, since the runtime does not support them.

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

        <div className="sticky bottom-0 mt-3 flex w-full max-w-[var(--thread-max-width)] flex-col items-center justify-end rounded-t-lg bg-background pb-4">
          <ScrollToBottom />
          <Composer />
        </div>
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

const SUGGESTIONS = [
  "What can you help me with?",
  "Summarize the latest changes in this repo",
  "Write a unit test for the runtime adapter",
  "Explain how the SSE proxy works",
];

const ThreadWelcome: FC = () => (
  <ThreadPrimitive.Empty>
    <div className="flex w-full max-w-[var(--thread-max-width)] flex-grow flex-col">
      <div className="flex w-full flex-grow flex-col items-center justify-center">
        <p className="mt-4 text-2xl font-semibold text-foreground">How can I help you today?</p>
        <p className="mt-2 text-sm text-muted-foreground">
          Ask anything — the agent runs in your own sandbox.
        </p>
      </div>
      <div className="mt-6 grid w-full gap-2 sm:grid-cols-2">
        {SUGGESTIONS.map((prompt) => (
          <ThreadPrimitive.Suggestion
            key={prompt}
            prompt={prompt}
            send
            className="flex items-center justify-start rounded-lg border border-border bg-card p-3 text-left text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
          >
            {prompt}
          </ThreadPrimitive.Suggestion>
        ))}
      </div>
    </div>
  </ThreadPrimitive.Empty>
);

const Composer: FC = () => (
  <ComposerPrimitive.Root className="flex w-full flex-wrap items-end rounded-3xl border border-input bg-background px-3 py-1.5 shadow-sm transition-colors focus-within:border-ring">
    <ComposerPrimitive.Input
      rows={1}
      autoFocus
      placeholder="Send a message…"
      className="max-h-40 flex-grow resize-none border-none bg-transparent px-2 py-3 text-sm outline-none placeholder:text-muted-foreground focus:ring-0 disabled:cursor-not-allowed"
    />
    <ComposerAction />
  </ComposerPrimitive.Root>
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
    <div className="col-start-2 row-start-1 max-w-[80%] break-words rounded-2xl bg-muted px-4 py-2 text-sm text-foreground">
      <MessagePrimitive.Parts />
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
