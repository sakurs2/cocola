"use client";

import { MessageCircle as ChatCircleDots } from "lucide-react";
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
  AdminPagination,
  AdminRefreshButton,
  AdminStatusBadge,
} from "@/components/admin/admin-ui";
import { SelectControl } from "@/components/ui/select-control";
import { cn } from "@/lib/utils";

type ConversationRun = {
  trace_id: string;
  conversation_id: string;
  conversation_title?: string;
  user_id: string;
  user_email: string;
  source: string;
  model_alias: string;
  status: string;
  started_at: string;
  duration_ms: number;
  ttft_ms: number;
  error_code?: string;
};

const PAGE_SIZE = 50;
const control =
  "h-10 min-w-0 rounded-xl border border-border/80 bg-background/80 px-3 text-sm text-foreground outline-none transition focus:border-primary/40 focus:ring-2 focus:ring-primary/10 placeholder:text-muted-foreground";

export default function AdminAuditPage() {
  const [runs, setRuns] = useState<ConversationRun[]>([]);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [source, setSource] = useState("");
  const [from, setFrom] = useState("");
  const [until, setUntil] = useState("");
  const [page, setPage] = useState(0);
  const [hasNext, setHasNext] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    const params = new URLSearchParams({
      limit: String(PAGE_SIZE + 1),
      offset: String(page * PAGE_SIZE),
    });
    if (search.trim()) params.set("search", search.trim());
    if (status) params.set("status", status);
    if (source) params.set("source", source);
    if (from) params.set("from", new Date(`${from}T00:00:00`).toISOString());
    if (until) params.set("until", new Date(`${until}T23:59:59.999`).toISOString());
    try {
      const response = await fetch(`/api/admin/conversation-runs?${params}`, {
        cache: "no-store",
      });
      if (!response.ok) throw new Error(await errorText(response));
      const body = (await response.json()) as { runs?: ConversationRun[] };
      const loadedRuns = Array.isArray(body.runs) ? body.runs : [];
      setHasNext(loadedRuns.length > PAGE_SIZE);
      setRuns(loadedRuns.slice(0, PAGE_SIZE));
    } catch (loadError) {
      setHasNext(false);
      setError(loadError instanceof Error ? loadError.message : String(loadError));
    } finally {
      setLoading(false);
    }
  }, [from, page, search, source, status, until]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <AdminPage className="admin-theme-indigo">
      <AdminPageHeader
        icon={<ChatCircleDots className="size-5" />}
        title="Agent Runs"
        description="One safe metadata record for every user–agent run. Chat content stays in its conversation."
        actions={
          <AdminRefreshButton onClick={() => void load()} refreshing={loading} disabled={loading}>
            Refresh
          </AdminRefreshButton>
        }
      />

      <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center">
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
        <SelectControl
          ariaLabel="Result"
          className={`${control} sm:w-40`}
          value={status}
          onValueChange={(value) => {
            setPage(0);
            setStatus(value);
          }}
          options={[
            { value: "", label: "All results" },
            { value: "running", label: "Running" },
            { value: "success", label: "Success" },
            { value: "error", label: "Error" },
            { value: "cancelled", label: "Cancelled" },
            { value: "interrupted", label: "Interrupted" },
          ]}
          contentClassName="cocola-admin-ui"
        />
        <SelectControl
          ariaLabel="Source"
          className={`${control} sm:w-44`}
          value={source}
          onValueChange={(value) => {
            setPage(0);
            setSource(value);
          }}
          options={[
            { value: "", label: "All sources" },
            { value: "interactive", label: "Interactive" },
            { value: "scheduled_task", label: "Scheduled task" },
          ]}
          contentClassName="cocola-admin-ui"
        />
        <DateRangeFilter
          from={from}
          until={until}
          onChange={(nextFrom, nextUntil) => {
            setPage(0);
            setFrom(nextFrom);
            setUntil(nextUntil);
          }}
        />
      </div>

      {error ? (
        <AdminAlert tone="error" icon={<AlertTriangle className="size-4" />}>
          {error}
        </AdminAlert>
      ) : null}

      <div className="admin-list">
        <div className="admin-list-scroll">
          <div className="min-w-[1120px]">
            <div className="admin-list-cols" style={{ gridTemplateColumns: "1.4fr 1.5fr 2fr 1.1fr 1.2fr 0.9fr 0.9fr 1.1fr 1.6fr" }}>
              <div>Started</div>
              <div>User</div>
              <div>Conversation</div>
              <div>Source</div>
              <div>Model</div>
              <div className="text-right">Total</div>
              <div className="text-right">TTFT</div>
              <div>Result</div>
              <div>Trace ID</div>
            </div>
            {runs.map((run) => {
              const traceID = run.trace_id;
              return (
                <div
                  key={traceID}
                  role="button"
                  tabIndex={0}
                  className="admin-list-row"
                  style={{ gridTemplateColumns: "1.4fr 1.5fr 2fr 1.1fr 1.2fr 0.9fr 0.9fr 1.1fr 1.6fr" }}
                  onClick={() =>
                    traceID &&
                    (window.location.href = `/admin/traces/${encodeURIComponent(traceID)}`)
                  }
                  onKeyDown={(event) =>
                    event.target === event.currentTarget &&
                    event.key === "Enter" &&
                    traceID &&
                    (window.location.href = `/admin/traces/${encodeURIComponent(traceID)}`)
                  }
                >
                  <div className="admin-list-cell admin-list-muted" style={{ fontSize: "12px" }}>
                    {formatDate(run.started_at)}
                  </div>
                  <div className="admin-list-cell admin-list-primary" style={{ fontSize: "13.5px" }}>
                    {run.user_email || run.user_id || "—"}
                  </div>
                  <div className="min-w-0">
                    <div className="admin-list-primary" style={{ fontSize: "13.5px" }}>
                      {run.conversation_title || "Untitled conversation"}
                    </div>
                    {run.conversation_id ? (
                      <Link
                        href={`/conversations/${encodeURIComponent(run.conversation_id)}`}
                        onClick={(clickEvent) => clickEvent.stopPropagation()}
                        className="admin-list-sub block font-mono text-primary hover:underline"
                      >
                        {run.conversation_id}
                      </Link>
                    ) : null}
                  </div>
                  <div className="admin-list-cell admin-list-muted">
                    {run.source === "scheduled_task" ? "Scheduled task" : "Interactive"}
                  </div>
                  <div className="admin-list-cell">{run.model_alias || "Default"}</div>
                  <div className="admin-list-cell admin-list-mono text-right">
                    {formatDuration(run.duration_ms)}
                  </div>
                  <div className="admin-list-cell admin-list-mono text-right">
                    {formatDuration(run.ttft_ms)}
                  </div>
                  <div className="admin-list-cell">
                    <RunStatus status={run.status} />
                    {run.error_code ? (
                      <div className="admin-list-sub font-mono">{run.error_code}</div>
                    ) : null}
                  </div>
                  <div className="min-w-0">
                    <Link
                      href={`/admin/traces/${encodeURIComponent(traceID)}`}
                      onClick={(clickEvent) => clickEvent.stopPropagation()}
                      className="block truncate font-mono text-xs text-primary hover:underline"
                    >
                      {traceID || "—"}
                    </Link>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
        {!loading && runs.length === 0 ? (
          <AdminEmptyState
            icon={<Clock3 className="size-5" />}
            title="No agent runs found"
            description="Conversation runs will appear here after a user sends a message or a scheduled task executes."
          />
        ) : null}
      </div>
      <AdminPagination
        page={page}
        pageSize={PAGE_SIZE}
        count={runs.length}
        hasNext={hasNext}
        loading={loading}
        label="runs"
        onPageChange={setPage}
      />
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
          className="cocola-admin-ui admin-theme-indigo z-50 w-[20rem] rounded-2xl border border-border/80 bg-popover p-3 text-popover-foreground shadow-[0_24px_70px_-28px_rgba(20,32,51,0.45)] outline-none"
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
