"use client";

import { Graph as TracePageIcon } from "@phosphor-icons/react";
import {
  AlertTriangle,
  ArrowLeft,
  CheckCircle2,
  ChevronRight,
  Clock3,
  FileClock,
  Loader2,
  TimerReset,
} from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { AdminRefreshButton } from "@/components/admin/admin-ui";
import { cn } from "@/lib/utils";

type TraceEvent = {
  id: number;
  trace_id: string;
  service: string;
  name: string;
  category?: string;
  started_at: string;
  duration_ms: number;
  status: string;
  metadata_json?: Record<string, unknown>;
};

type AuditEvent = {
  id: number;
  at: string;
  actor_type: string;
  actor_email?: string;
  actor_user_id?: string;
  action: string;
  resource_type?: string;
  resource_id?: string;
  result: string;
  route?: string;
  status_code?: number;
  trace_id?: string;
  metadata_json?: Record<string, unknown>;
  error_code?: string;
};

type TraceResponse = {
  trace_id: string;
  events?: TraceEvent[];
  audit_events?: AuditEvent[];
};

type TraceModule = {
  key: string;
  label: string;
  category?: string;
  service: string;
  events: TraceEvent[];
  startMs: number;
  endMs: number;
  durationMs: number;
  spanMs: number;
  errorCount: number;
};

type LoadState =
  | { status: "loading"; data: TraceResponse | null; error: "" }
  | { status: "ready"; data: TraceResponse; error: "" }
  | { status: "error"; data: TraceResponse | null; error: string };

const iconBtn =
  "inline-flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-40";

export default function AdminTracePage({ params }: { params: { traceId: string } }) {
  const traceId = params.traceId;
  const [state, setState] = useState<LoadState>({ status: "loading", data: null, error: "" });

  const load = useCallback(async () => {
    setState((prev) => ({ status: "loading", data: prev.data, error: "" }));
    try {
      const res = await fetch(`/api/admin/traces/${encodeURIComponent(traceId)}`, {
        cache: "no-store",
      });
      if (!res.ok) throw new Error(await errorText(res));
      const data = (await res.json()) as TraceResponse;
      setState({ status: "ready", data, error: "" });
    } catch (err) {
      setState((prev) => ({
        status: "error",
        data: prev.data,
        error: err instanceof Error ? err.message : String(err),
      }));
    }
  }, [traceId]);

  useEffect(() => {
    void load();
  }, [load]);

  const events = useMemo(() => state.data?.events ?? [], [state.data]);
  const auditEvents = useMemo(() => state.data?.audit_events ?? [], [state.data]);
  const timelineEvents = useMemo(
    () => (events.length ? events : auditEvents.map(auditEventToTraceEvent)),
    [auditEvents, events],
  );
  const stats = useMemo(() => traceStats(timelineEvents), [timelineEvents]);
  const modules = useMemo(() => traceModules(timelineEvents), [timelineEvents]);

  return (
    <main className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border">
        <div className="mx-auto flex h-16 max-w-7xl items-center gap-3 px-6">
          <Link href="/admin/audit" className={iconBtn} title="Back to audit logs">
            <ArrowLeft className="size-4" />
          </Link>
          <div className="admin-page-icon">
            <TracePageIcon className="size-[18px]" weight="duotone" />
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-base font-semibold">Trace Detail</h1>
            <p className="truncate font-mono text-xs text-muted-foreground">{traceId}</p>
          </div>
          <AdminRefreshButton
            className={iconBtn}
            title="Refresh trace"
            aria-label="Refresh trace"
            onClick={() => void load()}
            disabled={state.status === "loading"}
            refreshing={state.status === "loading"}
            variant="ghost"
            size="icon"
          />
        </div>
      </header>

      <div className="mx-auto max-w-7xl space-y-5 px-6 py-6">
        {state.status === "error" ? (
          <div className="flex items-center gap-2 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            <AlertTriangle className="size-4 shrink-0" />
            <span className="min-w-0">{state.error}</span>
          </div>
        ) : null}

        <section className="grid gap-3 md:grid-cols-4">
          <Metric
            icon={<TimerReset className="size-4" />}
            label="Trace Duration"
            value={timelineEvents.length ? formatDuration(stats.totalMs) : "-"}
          />
          <Metric
            icon={<Clock3 className="size-4" />}
            label="Slowest Stage"
            value={stats.slowest ? formatDuration(stats.slowest.duration_ms) : "-"}
            sub={stats.slowest?.name}
          />
          <Metric
            icon={<AlertTriangle className="size-4" />}
            label="Errors"
            value={String(stats.errorCount)}
          />
          <Metric
            icon={<FileClock className="size-4" />}
            label="Audit Events"
            value={String(auditEvents.length)}
          />
        </section>

        <section className="overflow-hidden rounded-lg border border-border bg-card">
          <div className="flex items-center justify-between border-b border-border px-4 py-3">
            <div>
              <h2 className="text-sm font-semibold">Modules</h2>
              <p className="text-xs text-muted-foreground">
                Expand a module to inspect its internal stages
              </p>
            </div>
            {state.status === "loading" ? (
              <span className="inline-flex items-center text-xs text-muted-foreground">
                <Loader2 className="mr-2 size-3 animate-spin" />
                Loading
              </span>
            ) : (
              <span className="inline-flex items-center text-xs text-muted-foreground">
                <CheckCircle2 className="mr-2 size-3" />
                {modules.length} modules · {timelineEvents.length} stages
              </span>
            )}
          </div>
          {!events.length && timelineEvents.length ? (
            <div className="border-b border-border bg-amber-500/10 px-4 py-2 text-xs text-amber-700 dark:text-amber-300">
              Detailed stage events were not recorded for this trace; showing audit duration as a
              fallback.
            </div>
          ) : null}
          {timelineEvents.length ? (
            <div className="divide-y divide-border">
              {modules.map((module) => (
                <TraceModuleRow key={module.key} module={module} stats={stats} />
              ))}
            </div>
          ) : (
            <div className="px-4 py-12 text-center text-sm text-muted-foreground">
              No trace timing events found for this trace id.
            </div>
          )}
        </section>

        <section className="overflow-hidden rounded-lg border border-border bg-card">
          <div className="border-b border-border px-4 py-3">
            <h2 className="text-sm font-semibold">Related Audit Events</h2>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full min-w-[880px] text-left text-sm">
              <thead className="border-b border-border bg-muted/50 text-xs text-muted-foreground">
                <tr>
                  <th className="px-4 py-2 font-medium">Time</th>
                  <th className="px-4 py-2 font-medium">Actor</th>
                  <th className="px-4 py-2 font-medium">Action</th>
                  <th className="px-4 py-2 font-medium">Resource</th>
                  <th className="px-4 py-2 font-medium">Result</th>
                  <th className="px-4 py-2 font-medium">Route</th>
                  <th className="px-4 py-2 font-medium">Metadata</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {auditEvents.map((event) => (
                  <tr key={event.id} className="align-top">
                    <td className="whitespace-nowrap px-4 py-3 text-xs text-muted-foreground">
                      {formatDate(event.at)}
                    </td>
                    <td className="px-4 py-3">
                      <div className="font-medium">
                        {event.actor_email || event.actor_user_id || "-"}
                      </div>
                      <div className="text-xs text-muted-foreground">{event.actor_type || "-"}</div>
                    </td>
                    <td className="px-4 py-3 font-mono text-xs">{event.action}</td>
                    <td className="px-4 py-3">
                      <div>{event.resource_type || "-"}</div>
                      <div className="max-w-[180px] truncate font-mono text-xs text-muted-foreground">
                        {event.resource_id || "-"}
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <Badge tone={event.result === "success" ? "green" : "red"}>
                        {event.result || "-"}
                      </Badge>
                      {event.error_code ? (
                        <div className="mt-1 font-mono text-xs text-muted-foreground">
                          {event.error_code}
                        </div>
                      ) : null}
                    </td>
                    <td className="px-4 py-3 font-mono text-xs">
                      {event.route || "-"}
                      {event.status_code ? (
                        <div className="mt-1 text-xs text-muted-foreground">
                          {event.status_code}
                        </div>
                      ) : null}
                    </td>
                    <td className="px-4 py-3">
                      <div className="max-w-[260px] truncate font-mono text-xs text-muted-foreground">
                        {formatMetadata(event.metadata_json)}
                      </div>
                    </td>
                  </tr>
                ))}
                {!auditEvents.length ? (
                  <tr>
                    <td
                      className="px-4 py-10 text-center text-sm text-muted-foreground"
                      colSpan={7}
                    >
                      No related audit events
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </section>
      </div>
    </main>
  );
}

function TraceModuleRow({
  module,
  stats,
}: {
  module: TraceModule;
  stats: ReturnType<typeof traceStats>;
}) {
  const total = Math.max(stats.totalMs, 1);
  const offset = Math.max(module.startMs - stats.startMs, 0);
  const spanLeftPct = Math.min((offset / total) * 100, 98);
  const spanWidthPct = Math.max((module.spanMs / total) * 100, 1.5);
  const tone = module.errorCount > 0 ? "red" : categoryTone(module.category);
  const showSpan = module.spanMs > module.durationMs + 25;

  return (
    <details className="group">
      <summary className="grid cursor-pointer list-none gap-3 px-4 py-4 hover:bg-muted/30 lg:grid-cols-[260px_1fr_120px] [&::-webkit-details-marker]:hidden">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <ChevronRight className="size-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-90" />
            <span className={cn("size-2 rounded-full", toneDot(tone))} />
            <span className="truncate text-sm font-semibold">{module.label}</span>
          </div>
          <div className="mt-1 flex min-w-0 items-center gap-2 pl-6 text-xs text-muted-foreground">
            <span className="truncate font-mono">{module.service}</span>
            <span>{module.events.length} stages</span>
          </div>
        </div>
        <div className="min-w-0">
          <div className="relative h-8 overflow-hidden rounded-md border border-border bg-background">
            <div
              className="absolute top-1/2 h-5 -translate-y-1/2 rounded-sm bg-muted"
              style={{
                left: `${spanLeftPct}%`,
                width: `${Math.min(spanWidthPct, 100 - spanLeftPct)}%`,
              }}
            />
            {module.events.map((event) => (
              <div
                key={event.id}
                className={cn("absolute top-1/2 h-3 -translate-y-1/2 rounded-full", toneBar(tone))}
                style={eventBarStyle(event, stats)}
                title={`${event.name}: ${formatDuration(event.duration_ms)}`}
              />
            ))}
          </div>
          <div className="mt-1 flex justify-between text-[11px] text-muted-foreground">
            <span>+{formatDuration(offset)}</span>
            <span>
              {showSpan
                ? `span ${formatDuration(module.spanMs)}`
                : formatDate(new Date(module.startMs).toISOString())}
            </span>
          </div>
        </div>
        <div className="text-right">
          <div className="font-mono text-sm font-semibold">{formatDuration(module.durationMs)}</div>
          <div className="mt-0.5 text-[11px] text-muted-foreground">active</div>
          <div className="mt-1">
            <Badge tone={module.errorCount > 0 ? "red" : "green"}>
              {module.errorCount > 0 ? `${module.errorCount} errors` : "ok"}
            </Badge>
          </div>
        </div>
      </summary>
      <div className="border-t border-border bg-background/40">
        {module.events.map((event) => (
          <TraceRow key={event.id} event={event} stats={stats} />
        ))}
      </div>
    </details>
  );
}

function TraceRow({ event, stats }: { event: TraceEvent; stats: ReturnType<typeof traceStats> }) {
  const started = Date.parse(event.started_at);
  const offset = Number.isFinite(started) ? Math.max(started - stats.startMs, 0) : 0;
  const tone =
    event.status === "error" || event.status === "failure" ? "red" : categoryTone(event.category);

  return (
    <div className="grid gap-3 px-4 py-3 lg:grid-cols-[260px_1fr_120px]">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span className={cn("size-2 rounded-full", toneDot(tone))} />
          <span className="truncate text-sm font-medium">{event.name}</span>
        </div>
        <div className="mt-1 flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
          <span className="truncate font-mono">{event.service}</span>
          {event.category ? <span className="truncate">{event.category}</span> : null}
        </div>
      </div>
      <div className="min-w-0">
        <div className="relative h-8 overflow-hidden rounded-md border border-border bg-background">
          <div
            className={cn("absolute top-1/2 h-3 -translate-y-1/2 rounded-full", toneBar(tone))}
            style={eventBarStyle(event, stats)}
          />
        </div>
        <div className="mt-1 flex justify-between text-[11px] text-muted-foreground">
          <span>+{formatDuration(offset)}</span>
          <span>{formatDate(event.started_at)}</span>
        </div>
      </div>
      <div className="text-right">
        <div className="font-mono text-sm font-semibold">{formatDuration(event.duration_ms)}</div>
        <div className="mt-1">
          <Badge tone={tone === "red" ? "red" : "green"}>{event.status || "ok"}</Badge>
        </div>
      </div>
      {event.metadata_json && Object.keys(event.metadata_json).length > 0 ? (
        <details className="lg:col-start-2 lg:col-span-2">
          <summary className="cursor-pointer text-xs text-muted-foreground">metadata</summary>
          <pre className="mt-2 max-h-48 overflow-auto rounded-md border border-border bg-background p-3 font-mono text-xs text-muted-foreground">
            {JSON.stringify(event.metadata_json, null, 2)}
          </pre>
        </details>
      ) : null}
    </div>
  );
}

function Metric({
  icon,
  label,
  value,
  sub,
}: {
  icon: ReactNode;
  label: string;
  value: string;
  sub?: string;
}) {
  return (
    <div className="rounded-lg border border-border bg-card px-4 py-3">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        {icon}
        <span>{label}</span>
      </div>
      <div className="mt-2 truncate text-2xl font-semibold">{value}</div>
      {sub ? <div className="mt-1 truncate text-xs text-muted-foreground">{sub}</div> : null}
    </div>
  );
}

function Badge({ children, tone }: { children: string; tone: "green" | "red" }) {
  const cls =
    tone === "green"
      ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
      : "border-destructive/30 bg-destructive/10 text-destructive";
  return <span className={`rounded-md border px-2 py-0.5 text-xs ${cls}`}>{children}</span>;
}

function traceStats(events: TraceEvent[]) {
  const starts = events
    .map((event) => Date.parse(event.started_at))
    .filter((value) => Number.isFinite(value));
  const startMs = starts.length ? Math.min(...starts) : Date.now();
  const endMs = events.reduce((max, event) => {
    const started = Date.parse(event.started_at);
    if (!Number.isFinite(started)) return max;
    return Math.max(max, started + Math.max(event.duration_ms, 0));
  }, startMs);
  return {
    startMs,
    totalMs: Math.max(endMs - startMs, 0),
    slowest: [...events].sort((a, b) => b.duration_ms - a.duration_ms)[0],
    errorCount: events.filter((event) => event.status === "error" || event.status === "failure")
      .length,
  };
}

function traceModules(events: TraceEvent[]): TraceModule[] {
  const groups = new Map<string, TraceEvent[]>();
  for (const event of events) {
    const key = moduleKey(event);
    groups.set(key, [...(groups.get(key) ?? []), event]);
  }
  return [...groups.entries()]
    .map(([key, rows]) => {
      const sorted = [...rows].sort((a, b) => Date.parse(a.started_at) - Date.parse(b.started_at));
      const starts = sorted
        .map((event) => Date.parse(event.started_at))
        .filter((value) => Number.isFinite(value));
      const startMs = starts.length ? Math.min(...starts) : Date.now();
      const endMs = sorted.reduce((max, event) => {
        const started = Date.parse(event.started_at);
        if (!Number.isFinite(started)) return max;
        return Math.max(max, started + Math.max(event.duration_ms, 0));
      }, startMs);
      const durationMs = activeDuration(sorted);
      return {
        key,
        label: moduleLabel(key),
        category: key,
        service: moduleService(sorted),
        events: sorted,
        startMs,
        endMs,
        durationMs,
        spanMs: Math.max(endMs - startMs, 0),
        errorCount: sorted.filter((event) => event.status === "error" || event.status === "failure")
          .length,
      };
    })
    .sort((a, b) => a.startMs - b.startMs);
}

function moduleKey(event: TraceEvent): string {
  if (event.category === "sandbox") return "agent";
  return event.category || event.service || "unknown";
}

function moduleLabel(key: string): string {
  const labels: Record<string, string> = {
    gateway: "Gateway",
    agent: "Agent Runtime",
    artifact: "Artifacts",
    persistence: "Persistence",
    audit: "Audit",
  };
  return labels[key] ?? key.replaceAll("_", " ");
}

function moduleService(events: TraceEvent[]): string {
  const services = [...new Set(events.map((event) => event.service).filter(Boolean))];
  if (!services.length) return "-";
  if (services.length <= 2) return services.join(" / ");
  return `${services.slice(0, 2).join(" / ")} +${services.length - 2}`;
}

function activeDuration(events: TraceEvent[]): number {
  const intervals = events
    .map((event) => {
      const started = Date.parse(event.started_at);
      if (!Number.isFinite(started)) return null;
      return {
        start: started,
        end: started + Math.max(event.duration_ms, 0),
      };
    })
    .filter((value): value is { start: number; end: number } => value !== null)
    .sort((a, b) => a.start - b.start);

  let total = 0;
  let currentStart: number | null = null;
  let currentEnd: number | null = null;
  for (const interval of intervals) {
    if (currentStart === null || currentEnd === null) {
      currentStart = interval.start;
      currentEnd = interval.end;
      continue;
    }
    if (interval.start <= currentEnd) {
      currentEnd = Math.max(currentEnd, interval.end);
      continue;
    }
    total += Math.max(currentEnd - currentStart, 0);
    currentStart = interval.start;
    currentEnd = interval.end;
  }
  if (currentStart !== null && currentEnd !== null) {
    total += Math.max(currentEnd - currentStart, 0);
  }
  return total;
}

function eventBarStyle(event: TraceEvent, stats: ReturnType<typeof traceStats>) {
  const started = Date.parse(event.started_at);
  const total = Math.max(stats.totalMs, 1);
  const offset = Number.isFinite(started) ? Math.max(started - stats.startMs, 0) : 0;
  const leftPct = Math.min((offset / total) * 100, 98);
  const widthPct = Math.max((event.duration_ms / total) * 100, 1.5);
  return {
    left: `${leftPct}%`,
    width: `${Math.min(widthPct, 100 - leftPct)}%`,
  };
}

function auditEventToTraceEvent(event: AuditEvent, index: number): TraceEvent {
  return {
    id: -event.id || -(index + 1),
    trace_id: event.trace_id || "",
    service: event.actor_type || "audit",
    name: event.action || "audit.event",
    category: "audit",
    started_at: event.at,
    duration_ms: metadataDuration(event.metadata_json),
    status: event.result || "success",
    metadata_json: {
      ...(event.metadata_json ?? {}),
      route: event.route,
      resource_type: event.resource_type,
      resource_id: event.resource_id,
      status_code: event.status_code,
      error_code: event.error_code,
    },
  };
}

function metadataDuration(metadata: Record<string, unknown> | undefined): number {
  const raw = metadata?.duration_ms;
  if (typeof raw === "number" && Number.isFinite(raw)) return Math.max(raw, 0);
  if (typeof raw === "string") {
    const parsed = Number.parseInt(raw, 10);
    if (Number.isFinite(parsed)) return Math.max(parsed, 0);
  }
  return 0;
}

function categoryTone(category?: string): "blue" | "green" | "amber" {
  if (category === "agent") return "blue";
  if (category === "persistence") return "green";
  if (category === "sandbox") return "amber";
  return "amber";
}

function toneDot(tone: "blue" | "green" | "amber" | "red") {
  if (tone === "blue") return "bg-sky-500";
  if (tone === "green") return "bg-emerald-500";
  if (tone === "red") return "bg-destructive";
  return "bg-amber-500";
}

function toneBar(tone: "blue" | "green" | "amber" | "red") {
  if (tone === "blue") return "bg-sky-500/80";
  if (tone === "green") return "bg-emerald-500/80";
  if (tone === "red") return "bg-destructive/80";
  return "bg-amber-500/80";
}

function formatDuration(ms: number) {
  if (!Number.isFinite(ms)) return "-";
  if (ms < 1000) return `${Math.max(ms, 0).toFixed(0)} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 2 : 1)} s`;
  return `${(ms / 60_000).toFixed(1)} min`;
}

function formatDate(value: string) {
  if (!value) return "-";
  return new Intl.DateTimeFormat(undefined, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(new Date(value));
}

function formatMetadata(value: Record<string, unknown> | undefined) {
  if (!value || Object.keys(value).length === 0) return "-";
  return JSON.stringify(value);
}

async function errorText(res: Response) {
  try {
    const body = (await res.json()) as { error?: string | { message?: string } };
    if (typeof body.error === "string") return body.error;
    if (body.error?.message) return body.error.message;
  } catch {
    // fall through
  }
  return `${res.status} ${res.statusText}`;
}
