"use client";

// Shared vertical-rail presentation layer.
//
// Both the live chat thread (thread.tsx) and the read-only shared-conversation
// page (conversation-readonly.tsx) render assistant responses as a continuous
// vertical timeline: each step (reasoning / tool call / answer / generated file)
// hangs off one line as a "rail node". Keeping that rendering in ONE place is
// what keeps the two surfaces visually identical -- change it here and both
// update. These components are presentation-only: they take plain props and
// hold no assistant-ui runtime dependency, so the read-only page (which has no
// runtime) can reuse them verbatim.

import {
  Brain,
  MessageCircle as ChatCircle,
  Box as Cube,
  FilePlus,
  FileText as PhFileText,
  FolderOpen,
  Globe as PhGlobe,
  ListChecks,
  Search as MagnifyingGlass,
  Pencil as PencilSimple,
  Sparkles as Sparkle,
  Loader2 as SpinnerGap,
  Terminal as TerminalWindow,
  Wrench as PhWrench,
  type LucideIcon as PhosphorIcon,
} from "lucide-react";
import { ChevronRight, Download, ExternalLink, Eye } from "lucide-react";
import Image from "next/image";
import { type FC, type ReactNode } from "react";
import { TooltipIconButton } from "@/components/assistant-ui/tooltip-icon-button";
import { cn } from "@/lib/utils";
import { type EnvironmentPreparationSnapshot } from "@/lib/environment";
import { resolveFileType } from "@/lib/file-type";
import { MaterialFileIcon } from "@/lib/material-file-icons";

// All rail action icons come from Phosphor; reuse its component type so the
// `weight` prop (duotone/bold/...) type-checks.
export type RailIcon = PhosphorIcon;

// Shared vertical-rail row. Every response node hangs off one continuous line
// (drawn by the icon column's `after:` pseudo): an icon badge sits on the line,
// an action label + type-specific content sit to its right.
export const RailRow: FC<{
  icon: RailIcon;
  label: string;
  running?: boolean;
  tone?: "default" | "error";
  color?: string;
  children?: ReactNode;
}> = ({ icon: Icon, label, running, tone = "default", color, children }) => (
  // The `after:` pseudo on the icon column paints the continuous vertical rail.
  // The last node in a message must NOT trail a line below it, so when this
  // RailRow is the final sibling we hide its connector via :last-child (scoped
  // to the `.aui-rail-streaming` ancestor the caller toggles while streaming).
  <div className="grid grid-cols-[1.75rem_1fr] gap-x-2.5 [.aui-rail-streaming_&:last-child_.rail-connector]:after:hidden">
    <div className="rail-connector relative flex items-start justify-center after:absolute after:left-1/2 after:top-8 after:bottom-0 after:w-0.5 after:-translate-x-1/2 after:rounded-full after:bg-border/50">
      <span
        className={cn(
          "relative z-[1] flex size-7 items-center justify-center",
          tone === "error" ? "text-destructive" : (color ?? "text-muted-foreground"),
        )}
      >
        {running ? (
          <SpinnerGap className="size-5 animate-spin" />
        ) : (
          <Icon className="size-5" />
        )}
      </span>
    </div>
    <div className="min-w-0 pb-4">
      {label ? (
        <div
          className={cn(
            "mb-1 flex min-h-7 items-center text-[13px] font-medium leading-none",
            tone === "error" ? "text-destructive" : "text-foreground",
          )}
        >
          {label}
        </div>
      ) : null}
      {children}
    </div>
  </div>
);

// Plain assistant text answer node. The markdown body is passed as children so
// each surface can supply its own source: the live thread renders the streaming
// <MarkdownText/> (reads the part from context), while the read-only page passes
// <MarkdownContent value={...}/>. While streaming, the icon spins in place.
export const RailText: FC<{ running?: boolean; children: ReactNode }> = ({ running, children }) => (
  <RailRow icon={ChatCircle} label="Answer" running={running} color="text-indigo-500">
    {children}
  </RailRow>
);

export const RailEnvironment: FC<{
  environment: EnvironmentPreparationSnapshot;
}> = ({ environment }) => {
  const running = environment.state === "preparing";
  const degraded = environment.state === "degraded";
  const label = running
    ? "Preparing environment"
    : degraded
      ? "Environment ready with limits"
      : environment.state === "ready"
        ? "Environment ready"
        : "Environment updated";
  const summaries = environment.components.map((component) =>
    component.summary ? `${component.label}: ${component.summary}` : component.label,
  );

  return (
    <RailRow
      icon={Cube}
      label={label}
      running={running}
      color={degraded ? "text-amber-500" : "text-sky-500"}
    >
      {summaries.length > 0 ? (
        <p className="text-[13px] leading-5 text-muted-foreground">{summaries.join(" · ")}</p>
      ) : null}
    </RailRow>
  );
};

// Ephemeral hand-off between environment preparation and the first real
// response part. It is derived from the live thread state and is never stored.
export const RailResponsePending: FC = () => (
  <RailRow icon={ChatCircle} label="Starting response" running color="text-indigo-500" />
);

// Reasoning / chain-of-thought node with a collapsible body.
export const RailReasoning: FC<{ text: string; running?: boolean }> = ({ text, running }) => (
  <RailRow
    icon={Brain}
    label={running ? "Thinking" : "Reasoning"}
    running={running}
    color="text-purple-500"
  >
    <details className="aui-details group text-sm">
      <summary className="flex w-fit cursor-pointer select-none items-center gap-1 py-0.5 text-[13px] text-muted-foreground transition-colors hover:text-foreground [&::-webkit-details-marker]:hidden">
        <ChevronRight className="size-3 shrink-0 transition-transform group-open:rotate-90" />
        <span>Show reasoning</span>
      </summary>
      <div className="aui-details-body mt-1 border-l-2 border-border/70 pl-3 text-sm leading-6 text-muted-foreground">
        {text}
      </div>
    </details>
  </RailRow>
);

type ToolMeta = { icon: RailIcon; running: string; done: string; color: string };

// Map SDK tool names (Claude Agent SDK: Bash/Read/Write/Edit/Glob/Grep/
// WebSearch/WebFetch/Task/TodoWrite/Skill; MCP tools carry an mcp__ prefix)
// to an icon + progress phrases. Unknown names fall back to a generic wrench.
const getToolMeta = (rawName: string): ToolMeta => {
  const name = rawName.replace(/^mcp__/, "").toLowerCase();
  if (name.includes("websearch") || name.includes("search"))
    return {
      icon: MagnifyingGlass,
      running: "Searching",
      done: "Searched",
      color: "text-violet-500",
    };
  if (name.includes("webfetch") || name.includes("fetch") || name.includes("browser"))
    return { icon: PhGlobe, running: "Reading page", done: "Read page", color: "text-sky-500" };
  if (name.startsWith("read") || name.includes("read_file"))
    return { icon: PhFileText, running: "Reading file", done: "Read file", color: "text-blue-500" };
  if (name.startsWith("write") || name.includes("write_file"))
    return {
      icon: PencilSimple,
      running: "Writing file",
      done: "Wrote file",
      color: "text-emerald-500",
    };
  if (name.startsWith("edit") || name.includes("str_replace") || name.includes("edit_file"))
    return {
      icon: PencilSimple,
      running: "Editing file",
      done: "Edited file",
      color: "text-amber-500",
    };
  if (name.startsWith("glob") || name.startsWith("grep") || name.includes("find"))
    return {
      icon: FolderOpen,
      running: "Searching code",
      done: "Searched code",
      color: "text-cyan-600",
    };
  if (name.startsWith("bash") || name.includes("shell") || name.includes("terminal"))
    return {
      icon: TerminalWindow,
      running: "Running command",
      done: "Ran command",
      color: "text-orange-500",
    };
  if (name.startsWith("todo") || name.includes("task"))
    return {
      icon: ListChecks,
      running: "Planning tasks",
      done: "Updated tasks",
      color: "text-fuchsia-500",
    };
  if (name.startsWith("skill") || name.includes("load"))
    return {
      icon: Sparkle,
      running: "Loading skill",
      done: "Loaded skill",
      color: "text-yellow-500",
    };
  return { icon: PhWrench, running: "Calling tool", done: "Called tool", color: "text-slate-400" };
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
  if (typeof obj.description === "string" && obj.description.trim() && chips.length === 0) {
    chips.push(obj.description.trim().slice(0, 48));
  }
  return Array.from(new Set(chips)).slice(0, 4);
};

// Pull the shell command out of the tool input, if any. Bash-like tools carry
// their command under `command`; it gets its own terminal-style preview rather
// than a plain chip. Returns the trimmed command (multi-line preserved) or null.
const extractCommand = (argsText: string): string | null => {
  if (!argsText) return null;
  try {
    const obj = JSON.parse(argsText) as Record<string, unknown>;
    if (typeof obj.command === "string" && obj.command.trim()) {
      return obj.command.trim();
    }
  } catch {
    return null;
  }
  return null;
};

// Terminal-style command preview. A slim dark "window" with a leading prompt
// glyph makes a shell command read as a command — monospace, syntax-neutral,
// wraps instead of truncating, and stays legible on both light and dark pages.
const CommandPreview: FC<{ command: string }> = ({ command }) => (
  <div className="overflow-hidden rounded-lg border border-border/60 bg-zinc-950 text-zinc-100 shadow-sm dark:bg-zinc-900">
    <div className="flex items-center gap-1.5 border-b border-white/10 px-3 py-1.5">
      <span className="size-2 rounded-full bg-red-400/80" />
      <span className="size-2 rounded-full bg-yellow-400/80" />
      <span className="size-2 rounded-full bg-green-400/80" />
      <span className="ml-1.5 flex items-center gap-1 text-[10px] font-medium uppercase tracking-wide text-zinc-400">
        <TerminalWindow className="size-3" />
        Terminal
      </span>
    </div>
    <pre className="max-h-48 overflow-auto px-3 py-2 font-mono text-[11.5px] leading-5">
      <code>
        <span className="mr-2 select-none text-emerald-400">$</span>
        <span className="whitespace-pre-wrap break-words text-zinc-100">{command}</span>
      </code>
    </pre>
  </div>
);

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

// Tool call node -- content-flow style (no card chrome): an icon + an English
// progress phrase, a terminal-style command preview, small chips from the
// and a collapsible raw-arguments block.
export const RailTool: FC<{
  toolName: string;
  argsText?: string;
  result?: unknown;
  isError?: boolean;
  running?: boolean;
}> = ({ toolName, argsText, result, isError, running }) => {
  const meta = getToolMeta(toolName);
  const Icon = meta.icon;
  const chips = extractToolChips(argsText ?? "");
  const command = extractCommand(argsText ?? "");
  const label = isError ? "Tool call failed" : running ? meta.running : meta.done;
  const hasArgs = Boolean((argsText ?? "").trim());
  // Rich result cards only for web-search/fetch tools once their result lands.
  const searchResults = !isError && isSearchTool(toolName) ? parseSearchResults(result) : [];

  return (
    <RailRow
      icon={Icon}
      label={label}
      running={running}
      tone={isError ? "error" : "default"}
      color={meta.color}
    >
      {command ? (
        <div className="mb-1.5">
          <CommandPreview command={command} />
        </div>
      ) : null}
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
            <span>View arguments</span>
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

// Generated-file node. `onPreview` is optional: the live thread passes it to
// open the Artifacts side panel; the read-only shared page omits it and offers
// download only (there is no side panel there).
export const RailFile: FC<{
  filename: string;
  mimeType: string;
  size: number;
  downloadUrl: string;
  onPreview?: () => void;
}> = ({ filename, mimeType, size, downloadUrl, onPreview }) => {
  const kind = resolveFileType(filename, mimeType);
  const showThumbnail = kind.isImage && Boolean(downloadUrl);

  return (
    <RailRow icon={FilePlus} label="Generated file" color="text-teal-500">
      <div className="inline-flex w-fit max-w-full items-center gap-3 rounded-xl border border-border/60 bg-muted/40 p-3 text-sm">
        <span className="flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-lg bg-background">
          {showThumbnail ? (
            <Image
              src={downloadUrl}
              alt=""
              width={36}
              height={36}
              unoptimized
              className="size-9 rounded-lg object-cover"
              aria-hidden="true"
            />
          ) : (
            <MaterialFileIcon name={kind.icon} className="flex size-6 items-center justify-center" />
          )}
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate font-medium text-foreground">{filename}</div>
          <div className="mt-0.5 flex items-center gap-1.5 truncate text-xs text-muted-foreground">
            <span className="rounded bg-muted px-1.5 py-px font-medium tracking-wide text-muted-foreground/90">
              {kind.badge}
            </span>
            <span aria-hidden>·</span>
            <span className="truncate">{formatBytes(size)}</span>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {onPreview && kind.previewable ? (
            <TooltipIconButton
              tooltip="Preview"
              variant="ghost"
              className="size-8 rounded-full p-2"
              onClick={onPreview}
            >
              <Eye className="size-4" />
            </TooltipIconButton>
          ) : null}
          {downloadUrl ? (
            <a
              href={downloadUrl}
              download={filename}
              title="Download"
              aria-label={`Download ${filename}`}
              className="inline-flex size-8 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
            >
              <Download className="size-4" />
            </a>
          ) : null}
        </div>
      </div>
    </RailRow>
  );
};

export const formatBytes = (bytes: number): string => {
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

export const formatPayload = (value: unknown): string | undefined => {
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
