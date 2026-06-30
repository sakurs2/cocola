"use client";

import {
  ActionBarPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
  type ToolCallMessagePartProps,
} from "@assistant-ui/react";
import { ArrowDown, CopyIcon, SendHorizontalIcon, Square } from "lucide-react";
import type { FC } from "react";
import { Button } from "@/components/ui/button";
import { MarkdownText } from "@/components/assistant-ui/markdown-text";
import { cn } from "@/lib/utils";

// shadcn-style product Thread for cocola.
//
// Wires assistant-ui primitives to our local styling. Renders text as Markdown,
// reasoning as a dimmed block, and tool calls as collapsible cards (name + args
// + result), matching the backend AgentEvent vocabulary. Uses ExternalStore
// capabilities only for what the adapter implements (send / cancel).

export const Thread: FC = () => {
  return (
    <ThreadPrimitive.Root
      className="flex h-full flex-col bg-white"
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

        <div className="sticky bottom-0 mt-3 flex w-full max-w-[var(--thread-max-width)] flex-col items-center justify-end rounded-t-lg bg-white pb-4">
          <ScrollToBottom />
          <Composer />
        </div>
      </ThreadPrimitive.Viewport>
    </ThreadPrimitive.Root>
  );
};

const ScrollToBottom: FC = () => (
  <ThreadPrimitive.ScrollToBottom asChild>
    <Button
      variant="outline"
      size="icon"
      className="absolute -top-10 rounded-full disabled:invisible"
    >
      <ArrowDown className="h-4 w-4" />
    </Button>
  </ThreadPrimitive.ScrollToBottom>
);

const ThreadWelcome: FC = () => (
  <ThreadPrimitive.Empty>
    <div className="flex w-full max-w-[var(--thread-max-width)] flex-grow flex-col items-center justify-center">
      <p className="mt-4 text-lg font-medium text-neutral-700">How can I help you today?</p>
      <p className="mt-1 text-sm text-neutral-400">
        Ask anything — the agent runs in your own sandbox.
      </p>
    </div>
  </ThreadPrimitive.Empty>
);

const Composer: FC = () => (
  <ComposerPrimitive.Root className="flex w-full flex-wrap items-end rounded-xl border border-neutral-300 bg-white px-3 py-2 shadow-sm transition-colors focus-within:border-neutral-400">
    <ComposerPrimitive.Input
      rows={1}
      autoFocus
      placeholder="Send a message…"
      className="max-h-40 flex-grow resize-none border-none bg-transparent px-1 py-2 text-sm outline-none placeholder:text-neutral-400 focus:ring-0 disabled:cursor-not-allowed"
    />
    <ComposerAction />
  </ComposerPrimitive.Root>
);

const ComposerAction: FC = () => (
  <>
    <ThreadPrimitive.If running={false}>
      <ComposerPrimitive.Send asChild>
        <Button size="icon" className="my-1 size-8 rounded-lg p-2" aria-label="Send">
          <SendHorizontalIcon className="h-4 w-4" />
        </Button>
      </ComposerPrimitive.Send>
    </ThreadPrimitive.If>
    <ThreadPrimitive.If running>
      <ComposerPrimitive.Cancel asChild>
        <Button
          size="icon"
          variant="outline"
          className="my-1 size-8 rounded-lg p-2"
          aria-label="Stop"
        >
          <Square className="h-3.5 w-3.5 fill-current" />
        </Button>
      </ComposerPrimitive.Cancel>
    </ThreadPrimitive.If>
  </>
);

const UserMessage: FC = () => (
  <MessagePrimitive.Root className="grid w-full max-w-[var(--thread-max-width)] auto-rows-auto grid-cols-[minmax(72px,1fr)_auto] gap-y-1 py-3">
    <div className="col-start-2 row-start-1 max-w-[80%] break-words rounded-2xl bg-neutral-100 px-4 py-2 text-sm text-neutral-900">
      <MessagePrimitive.Parts />
    </div>
  </MessagePrimitive.Root>
);

const AssistantMessage: FC = () => (
  <MessagePrimitive.Root className="relative grid w-full max-w-[var(--thread-max-width)] grid-cols-[auto_1fr] grid-rows-[auto_1fr] py-3">
    <div className="col-span-2 col-start-1 row-start-1 my-1.5 max-w-full break-words leading-7 text-neutral-900">
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
  <div className="my-2 border-l-2 border-neutral-200 pl-3 text-sm italic text-neutral-500">
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
  <details className="my-2 overflow-hidden rounded-lg border border-neutral-200 bg-neutral-50 text-sm">
    <summary className="cursor-pointer select-none px-3 py-2 font-mono text-xs text-neutral-700">
      <span className="mr-2 rounded bg-neutral-200 px-1.5 py-0.5">tool</span>
      {title}
    </summary>
    <div className="border-t border-neutral-200 px-3 py-2">
      {args ? (
        <pre className="overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs text-neutral-600">
          {args}
        </pre>
      ) : null}
      {result !== undefined ? (
        <pre
          className={cn(
            "mt-2 overflow-x-auto whitespace-pre-wrap break-words border-t border-neutral-200 pt-2 font-mono text-xs",
            isError ? "text-red-600" : "text-neutral-700",
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
    className="col-start-1 row-start-2 -ml-1 flex gap-1 text-neutral-400"
  >
    <ActionBarPrimitive.Copy asChild>
      <Button variant="ghost" size="icon" className="size-7" aria-label="Copy">
        <CopyIcon className="h-4 w-4" />
      </Button>
    </ActionBarPrimitive.Copy>
  </ActionBarPrimitive.Root>
);
