"use client";

import { Graph as TracePageIcon } from "@phosphor-icons/react";
import {
  AlertTriangle,
  ArrowLeft,
  Bot,
  Box,
  BrainCircuit,
  CheckCircle2,
  ChevronRight,
  Clock3,
  Database,
  Hammer,
  Loader2,
  TimerReset,
} from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import {
  AdminAlert,
  AdminDrawer,
  AdminMetric,
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
  AdminStatusBadge,
} from "@/components/admin/admin-ui";
import { cn } from "@/lib/utils";

type ConversationRun = {
  trace_id: string;
  root_span_id: string;
  conversation_id: string;
  conversation_title?: string;
  user_id: string;
  user_email: string;
  source: string;
  model_alias: string;
  status: string;
  started_at: string;
  completed_at?: string;
  duration_ms: number;
  ttft_ms: number;
  llm_call_count: number;
  tool_call_count: number;
  input_tokens: number;
  output_tokens: number;
  cache_tokens: number;
  error_code?: string;
  safe_error_summary?: string;
  detail_status: string;
};

type TraceSpan = {
  id: number;
  trace_id: string;
  span_id: string;
  parent_span_id?: string;
  schema_version: number;
  service: string;
  name: string;
  category: string;
  started_at: string;
  duration_us: number;
  status: string;
  attributes_json?: Record<string, unknown>;
};

export default function AdminTracePage({ params }: { params: { traceId: string } }) {
  const traceId = params.traceId;
  const [run, setRun] = useState<ConversationRun | null>(null);
  const [spans, setSpans] = useState<TraceSpan[]>([]);
  const [selected, setSelected] = useState<TraceSpan | null>(null);
  const [inspectorOpen, setInspectorOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setError("");
    try {
      const base = `/api/admin/conversation-runs/${encodeURIComponent(traceId)}`;
      const [runResponse, spansResponse] = await Promise.all([
        fetch(base, { cache: "no-store" }),
        fetch(`${base}/spans`, { cache: "no-store" }),
      ]);
      if (!runResponse.ok) throw new Error(await errorText(runResponse));
      if (!spansResponse.ok) throw new Error(await errorText(spansResponse));
      const runBody = (await runResponse.json()) as { run?: ConversationRun };
      const spansBody = (await spansResponse.json()) as { spans?: TraceSpan[] };
      const nextSpans = spansBody.spans ?? [];
      setRun(runBody.run ?? null);
      setSpans(nextSpans);
      setSelected((current) => {
        if (current) return nextSpans.find((span) => span.span_id === current.span_id) ?? current;
        return nextSpans.find((span) => span.name === "conversation.run") ?? nextSpans[0] ?? null;
      });
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : String(loadError));
    } finally {
      setLoading(false);
    }
  }, [traceId]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (run?.status !== "running") return;
    const timer = window.setInterval(() => void load(), 2000);
    return () => window.clearInterval(timer);
  }, [load, run?.status]);

  const timeline = useMemo(() => timelineStats(run, spans), [run, spans]);
  const traceTree = useMemo(() => buildSpanTree(spans), [spans]);

  return (
    <AdminPage>
      <AdminPageHeader
        icon={<TracePageIcon className="size-5" weight="duotone" />}
        eyebrow="Conversation trace"
        title={run?.conversation_title || "Agent run"}
        description={traceId}
        actions={
          <div className="flex items-center gap-2">
            {run ? <RunBadge status={run.status} /> : null}
            <AdminRefreshButton onClick={() => void load()} refreshing={loading} disabled={loading}>
              Refresh
            </AdminRefreshButton>
          </div>
        }
      />

      <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-sm text-muted-foreground">
        <Link
          href="/admin/audit"
          className="inline-flex items-center gap-1.5 hover:text-foreground"
        >
          <ArrowLeft className="size-4" /> Agent Runs
        </Link>
        <span>{run?.user_email || run?.user_id || "—"}</span>
        <span>{run?.source === "scheduled_task" ? "Scheduled task" : "Interactive"}</span>
        <span>{run?.model_alias || "Default model"}</span>
        {run?.conversation_id ? (
          <Link
            href={`/conversations/${encodeURIComponent(run.conversation_id)}`}
            className="font-mono text-xs text-primary hover:underline"
          >
            Open conversation
          </Link>
        ) : null}
      </div>

      {error ? (
        <AdminAlert tone="error" icon={<AlertTriangle className="size-4" />}>
          {error}
        </AdminAlert>
      ) : null}
      {run?.detail_status === "expired" ? (
        <AdminAlert tone="warning" icon={<Clock3 className="size-4" />}>
          Trace details expired. The conversation audit summary remains available.
        </AdminAlert>
      ) : null}
      {run?.detail_status === "partial" ? (
        <AdminAlert tone="warning" icon={<AlertTriangle className="size-4" />}>
          Some trace spans could not be recorded. The agent run summary is complete.
        </AdminAlert>
      ) : null}

      <section className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <AdminMetric
          icon={<TimerReset className="size-4" />}
          label="Total"
          value={formatDurationMS(run?.duration_ms ?? timeline.totalMs)}
          tone="sky"
        />
        <AdminMetric
          icon={<Clock3 className="size-4" />}
          label="Time to first token"
          value={formatDurationMS(run?.ttft_ms ?? 0)}
        />
        <AdminMetric
          icon={<BrainCircuit className="size-4" />}
          label="Model calls"
          value={run?.llm_call_count ?? countCategory(spans, "model")}
          tone="violet"
        />
        <AdminMetric
          icon={<Hammer className="size-4" />}
          label="Tool calls"
          value={run?.tool_call_count ?? countCategory(spans, "tool")}
          tone="amber"
        />
        <AdminMetric
          icon={<Database className="size-4" />}
          label="Tokens"
          value={formatNumber((run?.input_tokens ?? 0) + (run?.output_tokens ?? 0))}
          detail={`${formatNumber(run?.input_tokens ?? 0)} in · ${formatNumber(run?.output_tokens ?? 0)} out`}
          tone="green"
        />
      </section>

      <section className="grid min-h-[38rem] overflow-hidden rounded-2xl border border-border/80 bg-card/80 shadow-[0_18px_55px_-42px_rgba(37,99,235,0.45)] lg:grid-cols-[21rem_minmax(0,1fr)]">
        <div className="min-w-0 border-border/70 lg:border-r">
          <div className="flex min-h-14 items-center justify-between border-b border-border/70 px-4 sm:px-5">
            <div>
              <h2 className="text-xs font-semibold uppercase tracking-[0.16em]">Trace</h2>
              <p className="text-xs text-muted-foreground">{uniqueSpanCount(spans)} spans</p>
            </div>
            {run?.status === "running" ? (
              <span className="inline-flex items-center gap-2 text-xs text-primary">
                <Loader2 className="size-3.5 animate-spin" /> Live
              </span>
            ) : (
              <span className="inline-flex items-center gap-2 text-xs text-muted-foreground">
                <CheckCircle2 className="size-3.5" /> Complete
              </span>
            )}
          </div>

          {traceTree.length ? (
            <div className="max-h-[42rem] divide-y divide-border/60 overflow-y-auto">
              {traceTree.map((node) => (
                <TraceTreeNode
                  key={node.span.span_id}
                  node={node}
                  selectedID={selected?.span_id}
                  onSelect={(span) => {
                    setSelected(span);
                    if (!window.matchMedia("(min-width: 1024px)").matches) setInspectorOpen(true);
                  }}
                />
              ))}
            </div>
          ) : (
            <div className="flex min-h-72 items-center justify-center px-6 text-sm text-muted-foreground">
              {loading ? "Loading trace…" : "No detailed spans were recorded."}
            </div>
          )}
        </div>

        <aside className="hidden min-w-0 bg-background/35 lg:block">
          <SpanInspector span={selected} run={run} timeline={timeline} />
        </aside>
      </section>

      <AdminDrawer
        open={inspectorOpen}
        onOpenChange={setInspectorOpen}
        title={selected?.name || "Span details"}
        description="Safe execution metadata"
      >
        <SpanInspector span={selected} run={run} timeline={timeline} embedded />
      </AdminDrawer>
    </AdminPage>
  );
}

type TraceNode = { span: TraceSpan; children: TraceNode[] };

function TraceTreeNode({
  node,
  selectedID,
  onSelect,
  depth = 0,
}: {
  node: TraceNode;
  selectedID?: string;
  onSelect: (span: TraceSpan) => void;
  depth?: number;
}) {
  const key = moduleKey(node.span.category);
  const Icon = moduleIcon(key);
  const hasChildren = node.children.length > 0;
  const [expanded, setExpanded] = useState(true);
  return (
    <div
      className={cn(
        depth > 0 &&
          "relative before:absolute before:bottom-0 before:left-0 before:top-0 before:w-px before:bg-border/70",
      )}
      style={depth > 0 ? { paddingLeft: 18, marginLeft: 18 } : undefined}
    >
      <div
        className={cn(
          "flex items-center py-2 pr-3 transition-colors hover:bg-muted/35",
          selectedID === node.span.span_id && "bg-primary/[0.07]",
        )}
        style={{ paddingLeft: depth === 0 ? 12 : 2 }}
      >
        <button
          type="button"
          className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-background disabled:opacity-25"
          onClick={() => setExpanded((current) => !current)}
          aria-label={
            expanded ? `Collapse ${humanize(node.span.name)}` : `Expand ${humanize(node.span.name)}`
          }
          aria-expanded={expanded}
          disabled={!hasChildren}
        >
          <ChevronRight className={cn("size-3.5 transition-transform", expanded && "rotate-90")} />
        </button>
        <button
          type="button"
          className="flex min-w-0 flex-1 items-center gap-2.5 text-left"
          onClick={() => onSelect(node.span)}
        >
          <span
            className={cn(
              "flex size-8 shrink-0 items-center justify-center rounded-lg",
              moduleTone(key),
            )}
          >
            <Icon className="size-3.5" />
          </span>
          <span className="min-w-0 flex-1">
            <span className="flex items-center gap-2">
              <span className={cn("size-1.5 shrink-0 rounded-full", statusDot(node.span.status))} />
              <span className="truncate text-sm font-medium">{humanize(node.span.name)}</span>
            </span>
            <span className="mt-0.5 block truncate font-mono text-[10px] text-muted-foreground">
              {node.span.service}
            </span>
          </span>
          <span className="ml-2 shrink-0 font-mono text-[11px] tabular-nums text-muted-foreground">
            {formatDurationUS(node.span.duration_us)}
          </span>
        </button>
      </div>
      {expanded && hasChildren ? (
        <div>
          {node.children.map((child) => (
            <TraceTreeNode
              key={child.span.span_id}
              node={child}
              selectedID={selectedID}
              onSelect={onSelect}
              depth={depth + 1}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}

function SpanInspector({
  span,
  run,
  timeline,
  embedded = false,
}: {
  span: TraceSpan | null;
  run: ConversationRun | null;
  timeline: ReturnType<typeof timelineStats>;
  embedded?: boolean;
}) {
  const [tab, setTab] = useState<"run" | "metadata">("run");
  if (!span) {
    return (
      <div
        className={cn(
          "flex min-h-72 items-center justify-center p-6 text-center text-sm text-muted-foreground",
          !embedded && "sticky top-0",
        )}
      >
        Select a span to inspect its timing and safe metadata.
      </div>
    );
  }
  return (
    <div className={cn(!embedded && "sticky top-0")}>
      {!embedded ? (
        <div className="border-b border-border/70 px-5 pt-5">
          <div className="flex items-start justify-between gap-4">
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <span className={cn("size-2 rounded-full", statusDot(span.status))} />
                <h2 className="truncate text-lg font-semibold tracking-tight">
                  {humanize(span.name)}
                </h2>
              </div>
              <p className="mt-1 truncate font-mono text-[11px] text-muted-foreground">
                {span.span_id}
              </p>
            </div>
            <RunBadge status={span.status} />
          </div>
          <div className="mt-5 flex gap-6">
            <InspectorTab active={tab === "run"} onClick={() => setTab("run")}>
              Run
            </InspectorTab>
            <InspectorTab active={tab === "metadata"} onClick={() => setTab("metadata")}>
              Metadata
            </InspectorTab>
          </div>
        </div>
      ) : null}
      <div className="space-y-6 p-5 sm:p-6">
        {embedded || tab === "run" ? (
          <>
            <div className="grid gap-3 sm:grid-cols-3">
              <InspectorMetric label="Duration" value={formatDurationUS(span.duration_us)} />
              <InspectorMetric label="Started" value={formatTime(span.started_at)} />
              <InspectorMetric label="Run position" value={formatRunPosition(span, timeline)} />
            </div>
            <dl className="grid grid-cols-[7rem_1fr] gap-x-4 gap-y-3 border-t border-border/70 pt-5 text-sm">
              <InspectorRow label="Service" value={span.service} mono />
              <InspectorRow label="Module" value={moduleKey(span.category)} />
              <InspectorRow label="Started" value={formatDate(span.started_at)} />
              <InspectorRow label="Parent span" value={span.parent_span_id || "Root"} mono />
              <InspectorRow label="Trace ID" value={span.trace_id} mono />
            </dl>
            <div className="flex flex-wrap gap-4 border-t border-border/70 pt-5">
              {run?.conversation_id ? (
                <Link
                  href={`/conversations/${encodeURIComponent(run.conversation_id)}`}
                  className="text-sm font-medium text-primary hover:underline"
                >
                  Open conversation
                </Link>
              ) : null}
              <Link
                href={`/admin/logs?trace_id=${encodeURIComponent(span.trace_id)}`}
                className="text-sm font-medium text-primary hover:underline"
              >
                View related component logs
              </Link>
            </div>
            {embedded ? <SafeAttributes attributes={span.attributes_json} /> : null}
          </>
        ) : (
          <SafeAttributes attributes={span.attributes_json} />
        )}
      </div>
    </div>
  );
}

function InspectorTab({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "relative pb-3 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground",
        active &&
          "text-primary after:absolute after:inset-x-0 after:-bottom-px after:h-0.5 after:bg-primary",
      )}
    >
      {children}
    </button>
  );
}

function InspectorMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-border/70 bg-card/75 px-4 py-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 font-mono text-sm font-medium tabular-nums">{value}</div>
    </div>
  );
}

function SafeAttributes({ attributes }: { attributes?: Record<string, unknown> }) {
  const entries = Object.entries(attributes ?? {});
  if (!entries.length) {
    return (
      <div className="rounded-xl border border-dashed border-border px-5 py-10 text-center text-sm text-muted-foreground">
        This span has no additional safe metadata.
      </div>
    );
  }
  return (
    <div>
      <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        Safe metadata
      </h3>
      <dl className="mt-3 grid gap-3 xl:grid-cols-2">
        {entries.map(([key, value]) => (
          <div key={key} className="rounded-xl border border-border/70 bg-card/75 px-4 py-3">
            <dt className="font-mono text-[11px] text-muted-foreground">{key}</dt>
            <dd className="mt-1 break-words text-sm">{formatAttribute(value)}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

function InspectorRow({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={cn("min-w-0 break-all", mono && "font-mono text-xs")}>{value || "—"}</dd>
    </>
  );
}

function RunBadge({ status }: { status: string }) {
  const tone =
    status === "success"
      ? "green"
      : status === "running"
        ? "sky"
        : status === "cancelled" || status === "interrupted"
          ? "amber"
          : "red";
  return (
    <AdminStatusBadge tone={tone} dot>
      {status}
    </AdminStatusBadge>
  );
}

function buildSpanTree(spans: TraceSpan[]) {
  const latest = new Map<string, TraceSpan>();
  for (const span of spans) latest.set(span.span_id, span);
  const nodes = new Map<string, TraceNode>();
  for (const span of latest.values()) nodes.set(span.span_id, { span, children: [] });

  const roots: TraceNode[] = [];
  for (const node of nodes.values()) {
    const parent = node.span.parent_span_id ? nodes.get(node.span.parent_span_id) : undefined;
    if (parent && parent !== node) parent.children.push(node);
    else roots.push(node);
  }
  const sortNodes = (rows: TraceNode[]) => {
    rows.sort(
      (left, right) => Date.parse(left.span.started_at) - Date.parse(right.span.started_at),
    );
    for (const row of rows) sortNodes(row.children);
  };
  sortNodes(roots);
  roots.sort((left, right) => {
    if (left.span.name === "conversation.run") return -1;
    if (right.span.name === "conversation.run") return 1;
    return Date.parse(left.span.started_at) - Date.parse(right.span.started_at);
  });
  return roots;
}

function uniqueSpanCount(spans: TraceSpan[]) {
  return new Set(spans.map((span) => span.span_id)).size;
}

function moduleKey(category: string) {
  if (category === "sandbox") return "environment";
  if (category === "persistence" || category === "artifact") return "finalization";
  if (["request", "environment", "agent", "model", "tool", "finalization"].includes(category))
    return category;
  return "agent";
}

function moduleIcon(key: string) {
  return (
    (
      {
        request: Clock3,
        environment: Box,
        agent: Bot,
        model: BrainCircuit,
        tool: Hammer,
        finalization: Database,
      } as Record<string, typeof Clock3>
    )[key] ?? Bot
  );
}

function moduleTone(key: string) {
  return {
    request: "bg-sky-500/10 text-sky-700",
    environment: "bg-amber-500/10 text-amber-700",
    agent: "bg-blue-500/10 text-blue-700",
    model: "bg-violet-500/10 text-violet-700",
    tool: "bg-orange-500/10 text-orange-700",
    finalization: "bg-emerald-500/10 text-emerald-700",
  }[key];
}

function timelineStats(run: ConversationRun | null, spans: TraceSpan[]) {
  const starts = spans.map((span) => Date.parse(span.started_at)).filter(Number.isFinite);
  const startMs = starts.length
    ? Math.min(...starts)
    : Date.parse(run?.started_at ?? "") || Date.now();
  const endMs = spans.reduce(
    (latest, span) => Math.max(latest, Date.parse(span.started_at) + span.duration_us / 1000),
    startMs,
  );
  return { startMs, totalMs: Math.max(run?.duration_ms ?? 0, endMs - startMs, 1) };
}

function countCategory(spans: TraceSpan[], category: string) {
  return spans.filter((span) => moduleKey(span.category) === category).length;
}

function statusDot(status: string) {
  if (status === "error" || status === "interrupted") return "bg-destructive";
  if (status === "cancelled") return "bg-amber-500";
  if (status === "running") return "animate-pulse bg-primary";
  return "bg-emerald-500";
}

function humanize(value: string) {
  return value.replaceAll(".", " · ").replaceAll("_", " ");
}

function formatDurationUS(us: number) {
  return formatDurationMS(us / 1000);
}

function formatDurationMS(ms: number) {
  if (!Number.isFinite(ms) || ms <= 0) return "—";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 2 : 1)} s`;
  return `${(ms / 60_000).toFixed(1)} min`;
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    fractionalSecondDigits: 3,
  }).format(new Date(value));
}

function formatTime(value: string) {
  return new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    fractionalSecondDigits: 3,
  }).format(new Date(value));
}

function formatRunPosition(span: TraceSpan, timeline: ReturnType<typeof timelineStats>) {
  const offset = Math.max(Date.parse(span.started_at) - timeline.startMs, 0);
  return offset > 0 ? `+${formatDurationMS(offset)}` : "+0 ms";
}

function formatNumber(value: number) {
  return new Intl.NumberFormat(undefined, {
    notation: value >= 10_000 ? "compact" : "standard",
  }).format(value);
}

function formatAttribute(value: unknown) {
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean")
    return String(value);
  return JSON.stringify(value);
}

async function errorText(response: Response) {
  try {
    const body = (await response.json()) as { error?: string | { message?: string } };
    if (typeof body.error === "string") return body.error;
    if (body.error?.message) return body.error.message;
  } catch {
    // Fall through to the status line.
  }
  return `${response.status} ${response.statusText}`;
}
