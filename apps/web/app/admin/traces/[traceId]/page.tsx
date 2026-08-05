"use client";

import { Workflow as TracePageIcon } from "lucide-react";
import {
  AlertTriangle,
  ArrowLeft,
  Bot,
  Box,
  BrainCircuit,
  CheckCircle2,
  Clock3,
  Database,
  Hammer,
  Loader2,
  TimerReset,
} from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Button, Card } from "@heroui/react";
import {
  AdminAlert,
  AdminDrawer,
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

export default function AdminTracePage() {
  const { traceId } = useParams<{ traceId: string }>();
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
  const orderedSpans = useMemo(() => {
    const latest = new Map<string, TraceSpan>();
    for (const span of spans) latest.set(span.span_id, span);
    return [...latest.values()].sort(
      (left, right) => Date.parse(left.started_at) - Date.parse(right.started_at),
    );
  }, [spans]);

  return (
    <AdminPage className="admin-theme-indigo">
      <AdminPageHeader
        icon={<TracePageIcon className="size-5" />}
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

      <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-sm text-muted">
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
            className="font-mono text-xs text-accent hover:underline"
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

      <section className="grid items-start gap-4 lg:grid-cols-[minmax(0,1.65fr)_minmax(18rem,0.75fr)]">
        <Card className="p-5 sm:p-6">
          <Card.Header className="flex items-start justify-between gap-4 p-0">
            <div>
              <Card.Title>Trace timeline</Card.Title>
              <Card.Description>Ordered runtime events for this run.</Card.Description>
            </div>
            {run?.status === "running" ? (
              <span className="text-accent inline-flex items-center gap-2 text-xs">
                <Loader2 className="size-3.5 animate-spin" /> Live
              </span>
            ) : (
              <span className="text-muted inline-flex items-center gap-2 text-xs">
                <CheckCircle2 className="size-3.5" /> {orderedSpans.length} events
              </span>
            )}
          </Card.Header>
          <Card.Content className="mt-6 p-0">
            {orderedSpans.length ? (
              <ol>
                {orderedSpans.map((span, index) => {
                  const key = moduleKey(span.category);
                  const Icon = moduleIcon(key);
                  return (
                    <li key={span.span_id} className="relative flex gap-3 pb-1 last:pb-0">
                      {index < orderedSpans.length - 1 ? (
                        <span className="bg-separator absolute bottom-0 left-[1.25rem] top-10 w-px" />
                      ) : null}
                      <Button
                        variant="ghost"
                        className="hover:bg-surface-secondary relative z-10 h-auto w-full justify-start gap-3 rounded-2xl px-2 py-3 text-left"
                        onPress={() => {
                          setSelected(span);
                          setInspectorOpen(true);
                        }}
                      >
                        <span className={cn("grid size-9 shrink-0 place-items-center rounded-xl", moduleTone(key))}>
                          <Icon className="size-4" />
                        </span>
                        <span className="flex min-w-0 flex-1 items-start justify-between gap-4">
                          <span className="min-w-0">
                            <span className="flex items-center gap-2">
                              <span className={cn("size-1.5 shrink-0 rounded-full", statusDot(span.status))} />
                              <span className="truncate text-sm font-medium">{humanize(span.name)}</span>
                            </span>
                            <span className="text-muted mt-1 block truncate font-mono text-[11px]">
                              {span.service} · {formatDurationUS(span.duration_us)}
                            </span>
                          </span>
                          <span className="text-muted shrink-0 text-xs tabular-nums">
                            {formatTime(span.started_at)}
                          </span>
                        </span>
                      </Button>
                    </li>
                  );
                })}
              </ol>
            ) : (
              <div className="text-muted flex min-h-52 items-center justify-center text-sm">
                {loading ? "Loading trace…" : "No detailed spans were recorded."}
              </div>
            )}
          </Card.Content>
        </Card>

        <Card className="p-5 sm:p-6">
          <Card.Header className="p-0">
            <Card.Title>Run context</Card.Title>
            <Card.Description>Execution summary from Cocola.</Card.Description>
          </Card.Header>
          <Card.Content className="mt-5 space-y-3 p-0">
            <TraceContextRow label="Source" value={run?.source === "scheduled_task" ? "Scheduled task" : "Interactive"} />
            <TraceContextRow label="Model" value={run?.model_alias || "Default model"} />
            <TraceContextRow label="Duration" value={formatDurationMS(run?.duration_ms ?? timeline.totalMs)} />
            <TraceContextRow label="First token" value={formatDurationMS(run?.ttft_ms ?? 0)} />
            <TraceContextRow label="Model calls" value={String(run?.llm_call_count ?? countCategory(spans, "model"))} />
            <TraceContextRow label="Tool calls" value={String(run?.tool_call_count ?? countCategory(spans, "tool"))} />
            <TraceContextRow label="Tokens" value={`${formatNumber(run?.input_tokens ?? 0)} in · ${formatNumber(run?.output_tokens ?? 0)} out`} />
            <TraceContextRow label="Trace ID" value={traceId} mono />
          </Card.Content>
        </Card>
      </section>

      <AdminDrawer
        open={inspectorOpen}
        onOpenChange={setInspectorOpen}
        className="admin-theme-indigo"
        title={selected?.name || "Span details"}
        description="Safe execution metadata"
      >
        <SpanInspector span={selected} run={run} timeline={timeline} embedded />
      </AdminDrawer>
    </AdminPage>
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
          "flex min-h-72 items-center justify-center p-6 text-center text-sm text-muted",
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
              <p className="mt-1 truncate font-mono text-[11px] text-muted">
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
                  className="text-sm font-medium text-accent hover:underline"
                >
                  Open conversation
                </Link>
              ) : null}
              <Link
                href={`/admin/logs?trace_id=${encodeURIComponent(span.trace_id)}`}
                className="text-sm font-medium text-accent hover:underline"
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

function InspectorTab({ active, onClick, children }: { active: boolean; onClick: () => void; children: string }) {
  return (
    <Button
      variant="ghost"
      onPress={onClick}
      className={cn(
        "relative h-auto min-w-0 rounded-none px-0 pb-3 text-sm font-medium text-muted hover:bg-transparent hover:text-foreground",
        active &&
          "text-accent after:absolute after:inset-x-0 after:-bottom-px after:h-0.5 after:bg-accent",
      )}
    >
      {children}
    </Button>
  );
}

function TraceContextRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="bg-surface-secondary flex items-start justify-between gap-4 rounded-2xl px-4 py-3">
      <span className="text-muted text-xs">{label}</span>
      <span className={cn("min-w-0 break-all text-right text-sm font-medium", mono && "font-mono text-xs")}>
        {value || "—"}
      </span>
    </div>
  );
}

function InspectorMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-border/70 bg-surface/75 px-4 py-3">
      <div className="text-xs text-muted">{label}</div>
      <div className="mt-1 font-mono text-sm font-medium tabular-nums">{value}</div>
    </div>
  );
}

function SafeAttributes({ attributes }: { attributes?: Record<string, unknown> }) {
  const entries = Object.entries(attributes ?? {});
  if (!entries.length) {
    return (
      <div className="rounded-xl border border-dashed border-border px-5 py-10 text-center text-sm text-muted">
        This span has no additional safe metadata.
      </div>
    );
  }
  return (
    <div>
      <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-muted">
        Safe metadata
      </h3>
      <dl className="mt-3 grid gap-3 xl:grid-cols-2">
        {entries.map(([key, value]) => (
          <div key={key} className="rounded-xl border border-border/70 bg-surface/75 px-4 py-3">
            <dt className="font-mono text-[11px] text-muted">{key}</dt>
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
      <dt className="text-muted">{label}</dt>
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
  if (status === "error" || status === "interrupted") return "bg-danger";
  if (status === "cancelled") return "bg-amber-500";
  if (status === "running") return "animate-pulse bg-accent";
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
