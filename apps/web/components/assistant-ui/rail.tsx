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
  BrainCircuit,
  MessageCircle as ChatCircle,
  Box as Cube,
  FilePlus,
  FileText as PhFileText,
  FolderOpen,
  Globe as PhGlobe,
  ListChecks,
  Search as MagnifyingGlass,
  ShieldAlert,
  Pencil as PencilSimple,
  Sparkles as Sparkle,
  Loader2 as SpinnerGap,
  Terminal as TerminalWindow,
  Wrench as PhWrench,
  type LucideIcon as PhosphorIcon,
} from "lucide-react";
import { CheckCircle2, ChevronRight, CircleX, Download, ExternalLink, Eye } from "lucide-react";
import Image from "next/image";
import { Button, Card, Tooltip } from "@heroui/react";
import { useTranslations } from "next-intl";
import { useEffect, useState, type FC, type ReactNode } from "react";
import { TooltipIconButton } from "@/components/assistant-ui/tooltip-icon-button";
import { formatAgentDuration } from "@/lib/agent-turn-summary.mjs";
import { cn } from "@/lib/utils";
import { type EnvironmentPreparationSnapshot } from "@/lib/environment";
import { resolveFileType } from "@/lib/file-type";
import { MaterialFileIcon } from "@/lib/material-file-icons";
import { normalizeProgressItems } from "@/lib/progress-items.mjs";
import { isCommandTool, toolOutcomeLabel } from "@/lib/tool-failure.mjs";
import { CodeBlock } from "@/components/assistant-ui/markdown-text";

// All rail action icons come from Phosphor; reuse its component type so the
// `weight` prop (duotone/bold/...) type-checks.
export type RailIcon = PhosphorIcon;

export const RailProcessSummary: FC<{
  durationMs?: number;
  children?: ReactNode;
  expanded?: boolean;
  onExpandedChange?: (expanded: boolean) => void;
}> = ({ durationMs, children, expanded: controlledExpanded, onExpandedChange }) => {
  const t = useTranslations("chat.rail");
  const [localExpanded, setLocalExpanded] = useState(false);
  const expanded = controlledExpanded ?? localExpanded;
  const duration = formatAgentDuration(durationMs);

  const toggle = () => {
    const next = !expanded;
    if (controlledExpanded === undefined) setLocalExpanded(next);
    onExpandedChange?.(next);
  };

  return (
    <div className="mb-2">
      <button
        type="button"
        aria-expanded={expanded}
        onClick={toggle}
        className="group grid min-h-10 w-full grid-cols-[1.75rem_minmax(0,1fr)_auto] items-center gap-x-2.5 rounded-xl border border-border/60 bg-surface-secondary/35 py-2 pr-3.5 text-left text-sm font-medium text-muted transition-colors hover:bg-surface-secondary/60 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2"
      >
        <span className="flex items-center justify-center">
          <CheckCircle2 className="size-4 shrink-0 text-emerald-500" aria-hidden="true" />
        </span>
        <span className="truncate">
          {duration ? t("processedIn", { duration }) : t("processed")}
        </span>
        <ChevronRight
          className={cn("size-4 shrink-0 transition-transform", expanded && "rotate-90")}
          aria-hidden="true"
        />
      </button>
      {expanded && children ? <div className="mt-2">{children}</div> : null}
    </div>
  );
};

// Shared vertical-rail row. Every response node hangs off one continuous line
// (drawn by the icon column's `after:` pseudo): an icon badge sits on the line,
// an action label + type-specific content sit to its right.
export const RailRow: FC<{
  icon: RailIcon;
  label: ReactNode;
  running?: boolean;
  tone?: "default" | "error";
  color?: string;
  children?: ReactNode;
}> = ({ icon: Icon, label, running, tone = "default", color, children }) => (
  // The `after:` pseudo on the icon column paints the continuous vertical rail.
  // The last node in a message must NOT trail a line below it, so when this
  // RailRow is the final sibling we hide its connector via :last-child (scoped
  // to the `.aui-rail-streaming` ancestor the caller toggles while streaming).
  <div className="rail-row grid grid-cols-[1.75rem_1fr] gap-x-2.5">
    <div className="rail-connector relative flex items-start justify-center after:absolute after:left-1/2 after:top-8 after:bottom-0 after:w-0.5 after:-translate-x-1/2 after:rounded-full after:bg-border/50">
      <span
        className={cn(
          "relative z-[1] flex size-7 items-center justify-center",
          tone === "error" ? "text-danger" : (color ?? "text-muted"),
        )}
      >
        {running ? <SpinnerGap className="size-5 animate-spin" /> : <Icon className="size-5" />}
      </span>
    </div>
    <div className="min-w-0 pb-4">
      {label ? (
        <div
          className={cn(
            "mb-1 flex min-h-7 items-center text-[13px] font-medium leading-none",
            tone === "error" ? "text-danger" : "text-foreground",
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
export const RailText: FC<{ running?: boolean; children: ReactNode }> = ({ running, children }) => {
  const t = useTranslations("chat.rail");
  return (
    <RailRow icon={ChatCircle} label={t("answer")} running={running} color="text-indigo-500">
      {children}
    </RailRow>
  );
};

export const RailEnvironment: FC<{
  environment: EnvironmentPreparationSnapshot;
}> = ({ environment }) => {
  const t = useTranslations("chat.rail");
  const running = environment.state === "preparing";
  const degraded = environment.state === "degraded";
  const label = running
    ? t("environment.preparing")
    : degraded
      ? t("environment.limited")
      : environment.state === "ready"
        ? t("environment.ready")
        : t("environment.updated");
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
        <p className="text-[13px] leading-5 text-muted">{summaries.join(" · ")}</p>
      ) : null}
    </RailRow>
  );
};

// Ephemeral hand-off between environment preparation and the first real
// response part. It is derived from the live thread state and is never stored.
export const RailResponsePending: FC = () => {
  const t = useTranslations("chat.rail");
  return (
    <RailRow icon={ChatCircle} label={t("startingResponse")} running color="text-indigo-500" />
  );
};

export const RailProgress: FC<{ items?: unknown[]; pinned?: boolean }> = ({ items, pinned }) => {
  const t = useTranslations("chat.rail");
  const normalized = normalizeProgressItems(items);
  const completed = normalized.filter((item) => item.status === "completed").length;
  const label = normalized.length ? (
    <span className="flex items-baseline gap-1.5">
      <span>{t("plan")}</span>
      <span className="text-xs font-normal tabular-nums text-muted">
        {completed}/{normalized.length}
      </span>
    </span>
  ) : (
    t("plan")
  );

  const content = normalized.length ? (
    <ol className="space-y-1.5 py-0.5">
      {normalized.map((item) => {
        const done = item.status === "completed";
        const active = item.status === "in_progress";
        return (
          <li key={item.id} className="grid grid-cols-[1rem_minmax(0,1fr)] items-start gap-2">
            <span className="flex h-5 items-center justify-end" aria-hidden="true">
              {done ? (
                <CheckCircle2 className="size-3.5 text-emerald-500" />
              ) : active ? (
                <SpinnerGap className="text-accent size-3.5 animate-spin motion-reduce:animate-none" />
              ) : (
                <span className="bg-surface-secondary size-3 rounded-full border-2 border-foreground/25" />
              )}
            </span>
            <span
              className={cn(
                "text-[13px] leading-5",
                done && "text-muted/70 line-through decoration-muted-foreground/50",
                active && "font-medium text-foreground",
                !done && !active && "text-muted",
              )}
            >
              {item.text}
            </span>
          </li>
        );
      })}
    </ol>
  ) : (
    <p className="text-[13px] leading-5 text-muted">{t("noPlan")}</p>
  );

  if (pinned) {
    return (
      <section
        aria-label={t("currentPlan")}
        aria-live="polite"
        className="rounded-2xl border border-border/80 bg-surface px-4 py-3 shadow-surface"
      >
        <div className="grid min-w-0 grid-cols-[1rem_minmax(0,1fr)] items-center gap-2">
          <span className="text-accent flex h-7 items-center justify-end" aria-hidden="true">
            <ListChecks className="size-4" />
          </span>
          <div className="flex min-h-7 items-center text-[13px] font-medium leading-none text-foreground">
            {label}
          </div>
        </div>
        <div className="max-h-64 overflow-y-auto overscroll-contain pr-1 pt-1 [scrollbar-gutter:stable]">
          {content}
        </div>
      </section>
    );
  }

  return (
    <RailRow icon={ListChecks} label={label} color="text-violet-500">
      {content}
    </RailRow>
  );
};

export const RailMemoryRecall: FC<{
  status: "running" | "hit" | "degraded" | "unavailable";
  count?: number;
  content?: string;
}> = ({ status, count = 0, content = "" }) => {
  const t = useTranslations("chat.rail");
  const [expanded, setExpanded] = useState(false);
  const recalled = Math.max(0, Math.floor(count));
  const usedLabel = t("memory.used", { count: recalled });
  const unavailable = status === "unavailable";
  const degraded = status === "degraded";
  const label =
    status === "running"
      ? t("memory.recalling")
      : unavailable
        ? t("memory.unavailable")
        : degraded
          ? t("memory.limited", { label: usedLabel })
          : usedLabel;
  const expandable = content.length > 0;
  const labelContent = expandable ? (
    <button
      type="button"
      aria-expanded={expanded}
      onClick={() => setExpanded((value) => !value)}
      className="group flex min-h-7 w-full items-center justify-between gap-2 rounded-md text-left outline-none hover:text-accent focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2"
    >
      <span>{label}</span>
      <ChevronRight
        className={cn(
          "mr-1 size-3.5 shrink-0 text-muted transition-transform group-hover:text-foreground",
          expanded && "rotate-90",
        )}
        aria-hidden="true"
      />
    </button>
  ) : (
    label
  );

  return (
    <RailRow
      icon={BrainCircuit}
      label={labelContent}
      running={status === "running"}
      tone={unavailable ? "error" : "default"}
      color={degraded ? "text-amber-500" : "text-emerald-500"}
    >
      {unavailable || degraded ? (
        <p className="text-[13px] leading-5 text-muted">
          {unavailable ? t("memory.continued") : t("memory.partial")}
        </p>
      ) : null}
      {expanded && expandable ? (
        <pre className="mt-1 max-h-72 overflow-auto whitespace-pre-wrap break-words border-l-2 border-emerald-500/30 bg-surface-secondary/25 py-2 pl-3 pr-2 font-mono text-[11px] leading-5 text-muted">
          {content}
        </pre>
      ) : null}
    </RailRow>
  );
};

export const RailSCMApproval: FC<{
  status: "pending" | "approved" | "denied" | "expired";
  category?: string;
  commandLabel?: string;
  busy?: boolean;
  error?: string;
  onDecision?: (decision: "approved" | "denied") => void;
}> = ({ status, category, commandLabel, busy, error, onDecision }) => {
  const t = useTranslations("chat.rail");
  const label =
    status === "pending"
      ? t("approval.pending")
      : status === "approved"
        ? t("approval.approved")
        : status === "denied"
          ? t("approval.denied")
          : t("approval.expired");
  return (
    <RailRow
      icon={ShieldAlert}
      label={label}
      color={status === "pending" ? "text-amber-500" : "text-muted"}
    >
      <div className="space-y-2">
        <p className="text-[13px] leading-5 text-muted">
          {commandLabel ? `${commandLabel}. ` : ""}
          {category ? `${t("approval.category", { category })} ` : ""}
          {t("approval.description")}
        </p>
        {status === "pending" && onDecision ? (
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={busy}
              onClick={() => onDecision("approved")}
              className="rounded-lg bg-foreground px-3 py-1.5 text-xs font-medium text-background disabled:opacity-50"
            >
              {t("approval.approve")}
            </button>
            <button
              type="button"
              disabled={busy}
              onClick={() => onDecision("denied")}
              className="rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-foreground disabled:opacity-50"
            >
              {t("approval.deny")}
            </button>
          </div>
        ) : null}
        {error ? <p className="text-xs text-danger">{error}</p> : null}
      </div>
    </RailRow>
  );
};

// Reasoning / chain-of-thought node with a collapsible body.
export const RailReasoning: FC<{ text: string; running?: boolean }> = ({ text, running }) => {
  const t = useTranslations("chat.rail");
  return (
    <RailRow
      icon={Brain}
      label={running ? t("reasoning.thinking") : t("reasoning.reasoning")}
      running={running}
      color="text-purple-500"
    >
      <details className="aui-details group text-sm">
        <summary className="flex w-fit cursor-pointer select-none items-center gap-1 py-0.5 text-[13px] text-muted transition-colors hover:text-foreground [&::-webkit-details-marker]:hidden">
          <ChevronRight className="size-3 shrink-0 transition-transform group-open:rotate-90" />
          <span>{t("reasoning.show")}</span>
        </summary>
        <div className="aui-details-body mt-1 border-l-2 border-border/70 pl-3 text-sm leading-6 text-muted">
          {text}
        </div>
      </details>
    </RailRow>
  );
};

type ToolMeta = { icon: RailIcon; running: string; done: string; color: string };

// Map SDK tool names (Claude Agent SDK: Bash/Read/Write/Edit/Glob/Grep/
// WebSearch/WebFetch/Task/TodoWrite/Skill; MCP tools carry an mcp__ prefix)
// to an icon + progress phrases. Unknown names fall back to a generic wrench.
type RailTranslations = ReturnType<typeof useTranslations<"chat.rail">>;

const getToolMeta = (rawName: string, t: RailTranslations): ToolMeta => {
  const name = rawName.replace(/^mcp__/, "").toLowerCase();
  if (name.includes("websearch") || name.includes("search"))
    return {
      icon: MagnifyingGlass,
      running: t("tools.searching"),
      done: t("tools.searched"),
      color: "text-violet-500",
    };
  if (name.includes("webfetch") || name.includes("fetch") || name.includes("browser"))
    return {
      icon: PhGlobe,
      running: t("tools.readingPage"),
      done: t("tools.readPage"),
      color: "text-sky-500",
    };
  if (name.startsWith("read") || name.includes("read_file"))
    return {
      icon: PhFileText,
      running: t("tools.readingFile"),
      done: t("tools.readFile"),
      color: "text-blue-500",
    };
  if (name.startsWith("write") || name.includes("write_file"))
    return {
      icon: PencilSimple,
      running: t("tools.writingFile"),
      done: t("tools.wroteFile"),
      color: "text-emerald-500",
    };
  if (name.startsWith("edit") || name.includes("str_replace") || name.includes("edit_file"))
    return {
      icon: PencilSimple,
      running: t("tools.editingFile"),
      done: t("tools.editedFile"),
      color: "text-amber-500",
    };
  if (name.startsWith("glob") || name.startsWith("grep") || name.includes("find"))
    return {
      icon: FolderOpen,
      running: t("tools.searchingCode"),
      done: t("tools.searchedCode"),
      color: "text-cyan-600",
    };
  if (isCommandTool(rawName))
    return {
      icon: TerminalWindow,
      running: t("tools.runningCommand"),
      done: t("tools.ranCommand"),
      color: "text-orange-500",
    };
  if (name.startsWith("todo") || name.includes("task"))
    return {
      icon: ListChecks,
      running: t("tools.planningTasks"),
      done: t("tools.updatedTasks"),
      color: "text-fuchsia-500",
    };
  if (name.startsWith("skill") || name.includes("load"))
    return {
      icon: Sparkle,
      running: t("tools.loadingSkill"),
      done: t("tools.loadedSkill"),
      color: "text-yellow-500",
    };
  return {
    icon: PhWrench,
    running: t("tools.calling"),
    done: t("tools.called"),
    color: "text-slate-400",
  };
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

const CommandExecutionCard: FC<{
  command: string;
  output?: string;
  running?: boolean;
  isError?: boolean;
}> = ({ command, output = "", running, isError }) => {
  const t = useTranslations("chat.rail");
  const [expanded, setExpanded] = useState(false);
  const [elapsedSeconds, setElapsedSeconds] = useState(0);

  useEffect(() => {
    if (!running) return;
    const startedAt = Date.now();
    const update = () =>
      setElapsedSeconds(Math.max(0, Math.floor((Date.now() - startedAt) / 1000)));
    update();
    const timer = window.setInterval(update, 1000);
    return () => window.clearInterval(timer);
  }, [running]);

  const status = running
    ? t("command.running")
    : isError
      ? t("command.failed")
      : t("command.finished");
  const duration = running ? formatAgentDuration(elapsedSeconds * 1000) : "";
  const statusLabel = duration ? `${status} · ${duration}` : status;
  const latestOutput = output.trimEnd().split("\n").at(-1) ?? "";
  const statusTooltip = running && latestOutput ? `${statusLabel}\n${latestOutput}` : statusLabel;

  return (
    <Card
      className={cn(
        "w-full overflow-hidden border border-border/70 bg-surface/80 p-0 shadow-none",
        isError && "border-danger/30",
      )}
    >
      <Card.Header className="grid min-h-9 grid-cols-[0.2rem_auto_minmax(0,1fr)_auto_auto] items-center gap-2 px-2.5 py-1">
        <span
          className={cn(
            "h-7 w-0.5 rounded-full",
            running && "bg-sky-500 animate-pulse motion-reduce:animate-none",
            !running && !isError && "bg-emerald-500/70",
            isError && "bg-danger/80",
          )}
          aria-hidden="true"
        />
        <span className="grid size-6 place-items-center rounded-md bg-surface-secondary text-muted">
          <TerminalWindow className="size-3.5" />
        </span>
        <code className="block min-w-0 truncate font-mono text-[11.5px] font-medium text-foreground/85">
          {command}
        </code>
        <span
          aria-label={statusLabel}
          className={cn(
            "grid size-7 place-items-center rounded-full",
            running && "bg-sky-500/10 text-sky-600 dark:text-sky-300",
            !running && !isError && "bg-emerald-500/10 text-emerald-600 dark:text-emerald-300",
            isError && "bg-danger/10 text-danger",
          )}
          role="status"
          title={statusTooltip}
        >
          {running ? (
            <SpinnerGap className="size-4 animate-spin motion-reduce:animate-none" />
          ) : isError ? (
            <CircleX className="size-4" />
          ) : (
            <CheckCircle2 className="size-4" />
          )}
        </span>
        <Tooltip>
          <Button
            isIconOnly
            aria-label={expanded ? t("command.hide") : t("command.show")}
            className="size-7 min-h-7 min-w-7 rounded-full"
            size="sm"
            variant="ghost"
            onPress={() => setExpanded((value) => !value)}
          >
            <ChevronRight
              className={cn("size-3.5 transition-transform", expanded && "rotate-90")}
              aria-hidden="true"
            />
          </Button>
          <Tooltip.Content>
            {expanded ? t("command.hideShort") : t("command.showShort")}
          </Tooltip.Content>
        </Tooltip>
      </Card.Header>
      {expanded ? (
        <Card.Content className="grid gap-2 border-t border-border/60 bg-surface-secondary/20 p-2">
          <CodeBlock
            className="[&>div:first-child]:!mt-0 [&>pre:last-child]:!mb-0"
            code={command}
            language="shell"
          />
          {output ? (
            <section className="overflow-hidden rounded-lg border border-border/70 bg-background">
              <header className="flex h-7 items-center border-b border-border/60 px-3 font-mono text-[10px] uppercase tracking-[0.12em] text-muted">
                {t("command.output")}
              </header>
              <div className="max-h-72 overflow-auto">
                <pre className="min-w-max whitespace-pre px-3 py-2 font-mono text-[11.5px] leading-5 text-foreground">
                  {output}
                </pre>
              </div>
            </section>
          ) : null}
        </Card.Content>
      ) : null}
    </Card>
  );
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
    className="inline-flex max-w-[20rem] items-center gap-1.5 rounded-full border border-border/70 bg-background px-2 py-1 text-xs text-foreground transition-colors hover:border-border hover:bg-surface-secondary"
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
    <ExternalLink className="size-3 shrink-0 text-muted/60" />
  </a>
);

// Tool call node -- content-flow style (no card chrome): an icon + an English
// progress phrase, a terminal-style command preview, small chips from the
// and a collapsible raw-arguments block.
export const RailTool: FC<{
  toolName: string;
  argsText?: string;
  liveOutput?: string;
  result?: unknown;
  isError?: boolean;
  outcome?: string;
  running?: boolean;
}> = ({ toolName, argsText, liveOutput, result, isError, outcome, running }) => {
  const t = useTranslations("chat.rail");
  const meta = getToolMeta(toolName, t);
  const Icon = meta.icon;
  const chips = extractToolChips(argsText ?? "");
  const command = extractCommand(argsText ?? "");
  const commandFailure = Boolean(isError && isCommandTool(toolName));
  const failureOutput = isError ? formatPayload(result) : undefined;
  const commandOutput = command
    ? running && liveOutput
      ? liveOutput
      : result !== undefined
        ? formatPayload(result)
        : liveOutput
    : undefined;
  const label = running
    ? meta.running
    : isError
      ? toolOutcomeLabel(toolName, outcome, true)
      : meta.done;
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
          <CommandExecutionCard
            command={command}
            output={commandOutput}
            running={running}
            isError={isError}
          />
        </div>
      ) : null}
      {chips.length ? (
        <div className="flex flex-wrap gap-1.5">
          {chips.map((chip, i) => (
            <span
              key={i}
              className="inline-block max-w-full break-words rounded-md bg-surface-secondary px-2 py-1 align-top font-mono text-[11px] leading-5 text-muted"
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
      {failureOutput && !command ? (
        <details className="aui-details group mt-1.5 text-sm">
          <summary className="flex w-fit cursor-pointer select-none items-center gap-1 py-0.5 text-xs text-muted/70 transition-colors hover:text-foreground [&::-webkit-details-marker]:hidden">
            <ChevronRight className="size-3 shrink-0 transition-transform group-open:rotate-90" />
            <span>{commandFailure ? t("command.viewOutput") : t("command.viewError")}</span>
          </summary>
          <div className="aui-details-body mt-1 border-l-2 border-danger/30 pl-3">
            <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words py-1 font-mono text-[11px] leading-5 text-muted">
              {failureOutput}
            </pre>
          </div>
        </details>
      ) : null}
      {hasArgs ? (
        <details className="aui-details group mt-1.5 text-sm">
          <summary className="flex w-fit cursor-pointer select-none items-center gap-1 py-0.5 text-xs text-muted/70 transition-colors hover:text-foreground [&::-webkit-details-marker]:hidden">
            <ChevronRight className="size-3 shrink-0 transition-transform group-open:rotate-90" />
            <span>{t("command.viewArguments")}</span>
          </summary>
          <div className="aui-details-body mt-1 border-l-2 border-border/70 pl-3">
            <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words py-1 font-mono text-[11px] leading-5 text-muted">
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
  const t = useTranslations("chat.rail");
  const kind = resolveFileType(filename, mimeType);
  const showThumbnail = kind.isImage && Boolean(downloadUrl);

  return (
    <RailRow icon={FilePlus} label={t("file.generated")} color="text-teal-500">
      <div className="inline-flex w-fit max-w-full items-center gap-3 rounded-xl border border-border/60 bg-surface-secondary/40 p-3 text-sm">
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
            <MaterialFileIcon
              name={kind.icon}
              className="flex size-6 items-center justify-center"
            />
          )}
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate font-medium text-foreground">{filename}</div>
          <div className="mt-0.5 flex items-center gap-1.5 truncate text-xs text-muted">
            <span className="rounded bg-surface-secondary px-1.5 py-px font-medium tracking-wide text-muted/90">
              {kind.badge}
            </span>
            <span aria-hidden>·</span>
            <span className="truncate">{formatBytes(size, t("file.unknownSize"))}</span>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {onPreview && kind.previewable ? (
            <TooltipIconButton
              tooltip={t("file.preview")}
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
              title={t("file.download")}
              aria-label={t("file.downloadNamed", { name: filename })}
              className="inline-flex size-8 items-center justify-center rounded-full text-muted transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-focus"
            >
              <Download className="size-4" />
            </a>
          ) : null}
        </div>
      </div>
    </RailRow>
  );
};

export const formatBytes = (bytes: number, unknownSize = "Unknown size"): string => {
  if (!bytes) return unknownSize;
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
