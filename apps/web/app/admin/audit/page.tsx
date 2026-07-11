"use client";

import { ChatCircleDots } from "@phosphor-icons/react";
import * as Popover from "@radix-ui/react-popover";
import {
  AlertTriangle,
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  Clock3,
  Search,
} from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import {
  AdminAlert,
  AdminEmptyState,
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
  AdminStatusBadge,
  AdminTable,
  AdminToolbar,
} from "@/components/admin/admin-ui";
import { cn } from "@/lib/utils";

type AuditEvent = {
  id: number;
  at: string;
  actor_user_id?: string;
  actor_email?: string;
  resource_id?: string;
  result: string;
  trace_id?: string;
  error_code?: string;
  metadata_json?: Record<string, unknown>;
};

const PAGE_SIZE = 50;
const control =
  "h-10 min-w-0 rounded-xl border border-border/80 bg-background/80 px-3 text-sm text-foreground outline-none transition focus:border-primary/40 focus:ring-2 focus:ring-primary/10 placeholder:text-muted-foreground";

export default function AdminAuditPage() {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [source, setSource] = useState("");
  const [from, setFrom] = useState("");
  const [until, setUntil] = useState("");
  const [page, setPage] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    const params = new URLSearchParams({
      limit: String(PAGE_SIZE),
      offset: String(page * PAGE_SIZE),
    });
    if (search.trim()) params.set("search", search.trim());
    if (status) params.set("status", status);
    if (source) params.set("source", source);
    if (from) params.set("since", new Date(`${from}T00:00:00`).toISOString());
    if (until) params.set("until", new Date(`${until}T23:59:59.999`).toISOString());
    try {
      const response = await fetch(`/api/admin/audit-events?${params}`, { cache: "no-store" });
      if (!response.ok) throw new Error(await errorText(response));
      const body = (await response.json()) as { events?: AuditEvent[] };
      setEvents(body.events ?? []);
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : String(loadError));
    } finally {
      setLoading(false);
    }
  }, [from, page, search, source, status, until]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <AdminPage>
      <AdminPageHeader
        icon={<ChatCircleDots className="size-5" weight="duotone" />}
        eyebrow="Operations"
        title="Agent Runs"
        description="One safe metadata record for every user–agent run. Chat content stays in its conversation."
        actions={
          <AdminRefreshButton onClick={() => void load()} refreshing={loading} disabled={loading}>
            Refresh
          </AdminRefreshButton>
        }
      />

      <AdminToolbar>
        <label className="min-w-[16rem] flex-1">
          <span className="sr-only">Search conversation runs</span>
          <span className="relative block">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <input
              className={`${control} w-full pl-9`}
              placeholder="Search user, conversation, or trace ID"
              value={search}
              onChange={(event) => {
                setPage(0);
                setSearch(event.target.value);
              }}
            />
          </span>
        </label>
        <select
          aria-label="Result"
          className={`${control} sm:w-40`}
          value={status}
          onChange={(event) => {
            setPage(0);
            setStatus(event.target.value);
          }}
        >
          <option value="">All results</option>
          <option value="running">Running</option>
          <option value="success">Success</option>
          <option value="error">Error</option>
          <option value="cancelled">Cancelled</option>
          <option value="interrupted">Interrupted</option>
        </select>
        <select
          aria-label="Source"
          className={`${control} sm:w-44`}
          value={source}
          onChange={(event) => {
            setPage(0);
            setSource(event.target.value);
          }}
        >
          <option value="">All sources</option>
          <option value="interactive">Interactive</option>
          <option value="scheduled_task">Scheduled task</option>
        </select>
        <DateRangeFilter
          from={from}
          until={until}
          onChange={(nextFrom, nextUntil) => {
            setPage(0);
            setFrom(nextFrom);
            setUntil(nextUntil);
          }}
        />
      </AdminToolbar>

      {error ? (
        <AdminAlert tone="error" icon={<AlertTriangle className="size-4" />}>
          {error}
        </AdminAlert>
      ) : null}

      <AdminTable>
        <div className="flex min-h-12 items-center justify-between border-b border-border/70 px-4">
          <div className="text-sm font-semibold">Agent runs</div>
          <div className="flex items-center gap-1">
            <button
              className="inline-flex size-9 items-center justify-center rounded-xl text-muted-foreground hover:bg-muted disabled:opacity-35"
              aria-label="Previous page"
              disabled={page === 0 || loading}
              onClick={() => setPage((value) => Math.max(0, value - 1))}
            >
              <ChevronLeft className="size-4" />
            </button>
            <span className="min-w-10 text-center font-mono text-xs text-muted-foreground">
              {page + 1}
            </span>
            <button
              className="inline-flex size-9 items-center justify-center rounded-xl text-muted-foreground hover:bg-muted disabled:opacity-35"
              aria-label="Next page"
              disabled={events.length < PAGE_SIZE || loading}
              onClick={() => setPage((value) => value + 1)}
            >
              <ChevronRight className="size-4" />
            </button>
          </div>
        </div>
        <table className="w-full min-w-[1120px] text-left text-sm">
          <thead className="sticky top-0 border-b border-border/70 bg-muted/45 text-xs text-muted-foreground">
            <tr>
              <th className="px-4 py-3 font-medium">Started</th>
              <th className="px-4 py-3 font-medium">User</th>
              <th className="px-4 py-3 font-medium">Conversation</th>
              <th className="px-4 py-3 font-medium">Source</th>
              <th className="px-4 py-3 font-medium">Model</th>
              <th className="px-4 py-3 text-right font-medium">Total</th>
              <th className="px-4 py-3 text-right font-medium">TTFT</th>
              <th className="px-4 py-3 font-medium">Result</th>
              <th className="px-4 py-3 font-medium">Trace ID</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border/60">
            {events.map((event) => {
              const metadata = event.metadata_json ?? {};
              const traceID = event.trace_id ?? "";
              const conversationID =
                stringValue(metadata.conversation_id) || event.resource_id || "";
              const runStatus = stringValue(metadata.status) || event.result;
              return (
                <tr
                  key={traceID || event.id}
                  className="cursor-pointer transition-colors hover:bg-primary/[0.035] focus-within:bg-primary/[0.035]"
                  onClick={() =>
                    traceID &&
                    (window.location.href = `/admin/traces/${encodeURIComponent(traceID)}`)
                  }
                >
                  <td className="whitespace-nowrap px-4 py-3 text-xs text-muted-foreground">
                    {formatDate(event.at)}
                  </td>
                  <td className="px-4 py-3 font-medium">
                    {event.actor_email || event.actor_user_id || "—"}
                  </td>
                  <td className="max-w-[220px] px-4 py-3">
                    <div className="truncate font-medium">
                      {stringValue(metadata.conversation_title) || "Untitled conversation"}
                    </div>
                    {conversationID ? (
                      <Link
                        href={`/conversations/${encodeURIComponent(conversationID)}`}
                        onClick={(clickEvent) => clickEvent.stopPropagation()}
                        className="mt-0.5 block truncate font-mono text-xs text-primary hover:underline"
                      >
                        {conversationID}
                      </Link>
                    ) : null}
                  </td>
                  <td className="px-4 py-3 text-muted-foreground">
                    {stringValue(metadata.chat_type) === "scheduled_task"
                      ? "Scheduled task"
                      : "Interactive"}
                  </td>
                  <td className="px-4 py-3">{stringValue(metadata.model_alias) || "Default"}</td>
                  <td className="px-4 py-3 text-right font-mono tabular-nums">
                    {formatDuration(numberValue(metadata.duration_ms))}
                  </td>
                  <td className="px-4 py-3 text-right font-mono tabular-nums">
                    {formatDuration(numberValue(metadata.ttft_ms))}
                  </td>
                  <td className="px-4 py-3">
                    <RunStatus status={runStatus} />
                    {event.error_code ? (
                      <div className="mt-1 font-mono text-[11px] text-muted-foreground">
                        {event.error_code}
                      </div>
                    ) : null}
                  </td>
                  <td className="max-w-[190px] px-4 py-3">
                    <Link
                      href={`/admin/traces/${encodeURIComponent(traceID)}`}
                      onClick={(clickEvent) => clickEvent.stopPropagation()}
                      className="block truncate font-mono text-xs text-primary hover:underline"
                    >
                      {traceID || "—"}
                    </Link>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
        {!loading && events.length === 0 ? (
          <AdminEmptyState
            icon={<Clock3 className="size-5" />}
            title="No agent runs found"
            description="Conversation runs will appear here after a user sends a message or a scheduled task executes."
          />
        ) : null}
      </AdminTable>
    </AdminPage>
  );
}

function DateRangeFilter({
  from,
  until,
  onChange,
}: {
  from: string;
  until: string;
  onChange: (from: string, until: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [month, setMonth] = useState(() =>
    startOfMonth(from ? new Date(`${from}T12:00:00`) : new Date()),
  );
  const days = calendarDays(month);

  const selectDay = (day: Date) => {
    const value = localDateValue(day);
    if (!from || until) {
      onChange(value, "");
      return;
    }
    if (value < from) onChange(value, from);
    else onChange(from, value);
    setOpen(false);
  };

  const applyRecentDays = (count: number) => {
    const end = startOfDay(new Date());
    const start = new Date(end);
    start.setDate(start.getDate() - (count - 1));
    onChange(localDateValue(start), localDateValue(end));
    setMonth(startOfMonth(start));
    setOpen(false);
  };

  return (
    <Popover.Root
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (nextOpen) setMonth(startOfMonth(from ? new Date(`${from}T12:00:00`) : new Date()));
      }}
    >
      <Popover.Trigger asChild>
        <button
          type="button"
          className={`${control} inline-flex min-w-[12rem] items-center justify-between gap-3 text-left`}
          aria-label="Filter by date range"
        >
          <span className="inline-flex min-w-0 items-center gap-2">
            <CalendarDays className="size-4 shrink-0 text-primary" />
            <span className="truncate">{dateRangeLabel(from, until)}</span>
          </span>
          <ChevronRight className="size-3.5 rotate-90 text-muted-foreground" />
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          align="end"
          sideOffset={8}
          className="cocola-admin-ui z-50 w-[20rem] rounded-2xl border border-border/80 bg-popover p-3 text-popover-foreground shadow-[0_24px_70px_-28px_rgba(20,32,51,0.45)] outline-none"
        >
          <div className="flex items-center justify-between px-1 pb-3">
            <button
              type="button"
              className="flex size-8 items-center justify-center rounded-lg text-muted-foreground hover:bg-muted hover:text-foreground"
              onClick={() => setMonth(addMonths(month, -1))}
              aria-label="Previous month"
            >
              <ChevronLeft className="size-4" />
            </button>
            <div className="text-sm font-semibold">{formatMonth(month)}</div>
            <button
              type="button"
              className="flex size-8 items-center justify-center rounded-lg text-muted-foreground hover:bg-muted hover:text-foreground"
              onClick={() => setMonth(addMonths(month, 1))}
              aria-label="Next month"
            >
              <ChevronRight className="size-4" />
            </button>
          </div>
          <div className="grid grid-cols-7 px-1 text-center text-[11px] font-medium text-muted-foreground">
            {["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"].map((label) => (
              <div key={label} className="py-1.5">
                {label}
              </div>
            ))}
          </div>
          <div className="grid grid-cols-7 gap-y-1 px-1">
            {days.map((day) => {
              const value = localDateValue(day);
              const endpoint = value === from || value === until;
              const inRange = Boolean(from && until && value > from && value < until);
              const outside = day.getMonth() !== month.getMonth();
              const today = value === localDateValue(new Date());
              return (
                <button
                  type="button"
                  key={value}
                  onClick={() => selectDay(day)}
                  className={cn(
                    "relative flex h-9 items-center justify-center rounded-lg text-sm tabular-nums transition-colors hover:bg-primary/10 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/30",
                    outside && "text-muted-foreground/45",
                    inRange && "rounded-none bg-primary/[0.08] text-primary",
                    endpoint &&
                      "bg-primary font-semibold text-primary-foreground hover:bg-primary hover:text-primary-foreground",
                    today &&
                      !endpoint &&
                      "font-semibold text-primary ring-1 ring-inset ring-primary/30",
                  )}
                >
                  {day.getDate()}
                </button>
              );
            })}
          </div>
          <div className="mt-3 border-t border-border/70 pt-3">
            <div className="mb-2 flex items-center justify-between px-1 text-xs text-muted-foreground">
              <span>{from && !until ? "Select an end date" : "Quick ranges"}</span>
              {from ? (
                <button
                  type="button"
                  className="font-medium text-primary hover:underline"
                  onClick={() => onChange("", "")}
                >
                  Clear
                </button>
              ) : null}
            </div>
            <div className="grid grid-cols-3 gap-2">
              <RangePreset onClick={() => applyRecentDays(1)}>Today</RangePreset>
              <RangePreset onClick={() => applyRecentDays(7)}>Last 7 days</RangePreset>
              <RangePreset onClick={() => applyRecentDays(30)}>Last 30 days</RangePreset>
            </div>
          </div>
          <Popover.Arrow className="fill-border" />
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

function RangePreset({ children, onClick }: { children: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="rounded-lg border border-border/70 bg-background/70 px-2 py-2 text-xs font-medium text-muted-foreground hover:border-primary/30 hover:bg-primary/[0.05] hover:text-primary"
    >
      {children}
    </button>
  );
}

function startOfDay(date: Date) {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate());
}

function startOfMonth(date: Date) {
  return new Date(date.getFullYear(), date.getMonth(), 1);
}

function addMonths(date: Date, amount: number) {
  return new Date(date.getFullYear(), date.getMonth() + amount, 1);
}

function calendarDays(month: Date) {
  const first = startOfMonth(month);
  const mondayOffset = (first.getDay() + 6) % 7;
  const cursor = new Date(first);
  cursor.setDate(cursor.getDate() - mondayOffset);
  return Array.from({ length: 42 }, (_, index) => {
    const day = new Date(cursor);
    day.setDate(cursor.getDate() + index);
    return day;
  });
}

function localDateValue(date: Date) {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function formatMonth(date: Date) {
  return new Intl.DateTimeFormat(undefined, { month: "long", year: "numeric" }).format(date);
}

function dateRangeLabel(from: string, until: string) {
  if (!from) return "Any date";
  const format = (value: string) =>
    new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", year: "numeric" }).format(
      new Date(`${value}T12:00:00`),
    );
  return until ? `${format(from)} – ${format(until)}` : `${format(from)} – Select end`;
}

function RunStatus({ status }: { status: string }) {
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
      {status || "unknown"}
    </AdminStatusBadge>
  );
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value : "";
}

function numberValue(value: unknown) {
  if (typeof value === "number") return value;
  if (typeof value === "string") return Number.parseInt(value, 10) || 0;
  return 0;
}

function formatDuration(ms: number) {
  if (!ms) return "—";
  if (ms < 1000) return `${ms} ms`;
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
  }).format(new Date(value));
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
