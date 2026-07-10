"use client";

import { ShieldCheck as AuditPageIcon } from "@phosphor-icons/react";
import {
  AlertTriangle,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Loader2,
  Search,
} from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import { AdminRefreshButton } from "@/components/admin/admin-ui";

type AuditEvent = {
  id: number;
  at: string;
  actor_type: string;
  actor_user_id?: string;
  actor_email?: string;
  action: string;
  resource_type?: string;
  resource_id?: string;
  result: "success" | "failure" | "denied" | string;
  http_method?: string;
  route?: string;
  status_code?: number;
  request_id?: string;
  trace_id?: string;
  client_ip?: string;
  user_agent?: string;
  metadata_json?: Record<string, unknown>;
  error_code?: string;
};

type Filters = {
  actor: string;
  action: string;
  resourceType: string;
  result: string;
  requestID: string;
  traceID: string;
};

const input =
  "h-9 min-w-0 rounded-md border border-input bg-background px-3 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-ring focus:ring-1 focus:ring-ring";
const iconBtn =
  "inline-flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-40";
const PAGE_SIZE = 50;

export default function AdminAuditPage() {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [filters, setFilters] = useState<Filters>({
    actor: "",
    action: "",
    resourceType: "conversation",
    result: "",
    requestID: "",
    traceID: "",
  });
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
    if (filters.actor.trim()) params.set("actor_user_id", filters.actor.trim());
    if (filters.action.trim()) params.set("action", filters.action.trim());
    if (filters.resourceType.trim()) params.set("resource_type", filters.resourceType.trim());
    if (filters.result.trim()) params.set("result", filters.result.trim());
    if (filters.requestID.trim()) params.set("request_id", filters.requestID.trim());
    if (filters.traceID.trim()) params.set("trace_id", filters.traceID.trim());
    try {
      const res = await fetch(`/api/admin/audit-events?${params.toString()}`, {
        cache: "no-store",
      });
      if (!res.ok) throw new Error(await errorText(res));
      const body = (await res.json()) as { events?: AuditEvent[] };
      setEvents(body.events ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [filters, page]);

  useEffect(() => {
    void load();
  }, [load]);

  const stats = useMemo(
    () => ({
      total: events.length,
      failures: events.filter((event) => event.result === "failure").length,
      denied: events.filter((event) => event.result === "denied").length,
    }),
    [events],
  );

  return (
    <main className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border">
        <div className="mx-auto flex h-16 max-w-7xl items-center gap-3 px-6">
          <div className="admin-page-icon">
            <AuditPageIcon className="size-[18px]" weight="duotone" />
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-base font-semibold">Audit Logs</h1>
            <p className="truncate text-xs text-muted-foreground">
              Server-side user behavior events without chat content
            </p>
          </div>
          <AdminRefreshButton
            className={iconBtn}
            title="Refresh audit logs"
            aria-label="Refresh audit logs"
            onClick={() => void load()}
            disabled={loading}
            refreshing={loading}
            variant="ghost"
            size="icon"
          />
        </div>
      </header>

      <div className="mx-auto max-w-7xl space-y-5 px-6 py-6">
        <section className="grid gap-3 md:grid-cols-3">
          <Metric label="Loaded Events" value={String(stats.total)} />
          <Metric label="Failures" value={String(stats.failures)} />
          <Metric label="Denied" value={String(stats.denied)} />
        </section>

        <section className="rounded-lg border border-border bg-card">
          <div className="flex items-center gap-2 border-b border-border px-4 py-3">
            <Search className="size-4 text-muted-foreground" />
            <h2 className="text-sm font-semibold">Filters</h2>
          </div>
          <div className="grid gap-3 p-4 md:grid-cols-3 xl:grid-cols-6">
            <input
              className={input}
              placeholder="actor"
              value={filters.actor}
              onChange={(event) => {
                setPage(0);
                setFilters((prev) => ({ ...prev, actor: event.target.value }));
              }}
            />
            <input
              className={input}
              placeholder="action"
              value={filters.action}
              onChange={(event) => {
                setPage(0);
                setFilters((prev) => ({ ...prev, action: event.target.value }));
              }}
            />
            <select
              className={input}
              value={filters.resourceType}
              onChange={(event) => {
                setPage(0);
                setFilters((prev) => ({ ...prev, resourceType: event.target.value }));
              }}
            >
              <option value="">any resource</option>
              <option value="conversation">conversation</option>
              <option value="artifact">artifact</option>
              <option value="users">users</option>
              <option value="tokens">tokens</option>
              <option value="settings">settings</option>
              <option value="quotas">quotas</option>
              <option value="skills">skills</option>
              <option value="models">models</option>
              <option value="model_providers">model_providers</option>
              <option value="scheduled_tasks">scheduled_tasks</option>
              <option value="scheduled_task_runs">scheduled_task_runs</option>
              <option value="sandbox_nodes">sandbox_nodes</option>
              <option value="sandboxes">sandboxes</option>
              <option value="runtime_token">runtime_token</option>
            </select>
            <select
              className={input}
              value={filters.result}
              onChange={(event) => {
                setPage(0);
                setFilters((prev) => ({ ...prev, result: event.target.value }));
              }}
            >
              <option value="">any result</option>
              <option value="success">success</option>
              <option value="failure">failure</option>
              <option value="denied">denied</option>
            </select>
            <input
              className={input}
              placeholder="request_id"
              value={filters.requestID}
              onChange={(event) => {
                setPage(0);
                setFilters((prev) => ({ ...prev, requestID: event.target.value }));
              }}
            />
            <input
              className={input}
              placeholder="trace_id"
              value={filters.traceID}
              onChange={(event) => {
                setPage(0);
                setFilters((prev) => ({ ...prev, traceID: event.target.value }));
              }}
            />
          </div>
        </section>

        {error ? (
          <div className="flex items-center gap-2 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            <AlertTriangle className="size-4 shrink-0" />
            <span className="min-w-0">{error}</span>
          </div>
        ) : null}

        <section className="overflow-hidden rounded-lg border border-border bg-card">
          <div className="flex items-center justify-between border-b border-border px-4 py-3">
            <h2 className="text-sm font-semibold">Recent Events</h2>
            <div className="flex items-center gap-2">
              {loading ? (
                <span className="inline-flex items-center text-xs text-muted-foreground">
                  <Loader2 className="mr-2 size-3 animate-spin" />
                  Loading
                </span>
              ) : (
                <span className="inline-flex items-center text-xs text-muted-foreground">
                  <CheckCircle2 className="mr-2 size-3" />
                  Updated
                </span>
              )}
              <button
                className={iconBtn}
                title="Previous page"
                disabled={page === 0 || loading}
                onClick={() => setPage((prev) => Math.max(0, prev - 1))}
              >
                <ChevronLeft className="size-4" />
              </button>
              <span className="min-w-12 text-center text-xs text-muted-foreground">{page + 1}</span>
              <button
                className={iconBtn}
                title="Next page"
                disabled={events.length < PAGE_SIZE || loading}
                onClick={() => setPage((prev) => prev + 1)}
              >
                <ChevronRight className="size-4" />
              </button>
            </div>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full min-w-[980px] text-left text-sm">
              <thead className="border-b border-border bg-muted/50 text-xs text-muted-foreground">
                <tr>
                  <th className="px-4 py-2 font-medium">Time</th>
                  <th className="px-4 py-2 font-medium">Actor</th>
                  <th className="px-4 py-2 font-medium">Action</th>
                  <th className="px-4 py-2 font-medium">Resource</th>
                  <th className="px-4 py-2 font-medium">Result</th>
                  <th className="px-4 py-2 font-medium">Route</th>
                  <th className="px-4 py-2 font-medium">Trace</th>
                  <th className="px-4 py-2 font-medium">Metadata</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {events.map((event) => {
                  const conversationID = conversationIDForEvent(event);
                  return (
                    <tr key={event.id} className="align-top">
                      <td className="whitespace-nowrap px-4 py-3 text-xs text-muted-foreground">
                        {formatDate(event.at)}
                      </td>
                      <td className="px-4 py-3">
                        <div className="font-medium">
                          {event.actor_email || event.actor_user_id || "-"}
                        </div>
                        <div className="text-xs text-muted-foreground">
                          {event.actor_type || "-"}
                        </div>
                      </td>
                      <td className="px-4 py-3 font-mono text-xs">{event.action}</td>
                      <td className="px-4 py-3">
                        <div>{event.resource_type || "-"}</div>
                        {conversationID ? (
                          <Link
                            href={`/conversations/${encodeURIComponent(conversationID)}`}
                            className="block max-w-[160px] truncate font-mono text-xs text-primary underline-offset-2 hover:underline"
                            title={`Open conversation ${conversationID}`}
                          >
                            {conversationID}
                          </Link>
                        ) : (
                          <div className="max-w-[160px] truncate font-mono text-xs text-muted-foreground">
                            {event.resource_id || "-"}
                          </div>
                        )}
                      </td>
                      <td className="px-4 py-3">
                        <Badge tone={resultTone(event.result)}>{event.result || "-"}</Badge>
                        {event.error_code ? (
                          <div className="mt-1 font-mono text-xs text-muted-foreground">
                            {event.error_code}
                          </div>
                        ) : null}
                      </td>
                      <td className="px-4 py-3">
                        <div className="font-mono text-xs">
                          {[event.http_method, event.route].filter(Boolean).join(" ") || "-"}
                        </div>
                        {event.status_code ? (
                          <div className="mt-1 text-xs text-muted-foreground">
                            {event.status_code}
                          </div>
                        ) : null}
                      </td>
                      <td className="px-4 py-3">
                        <TraceLink traceID={event.trace_id} requestID={event.request_id} />
                      </td>
                      <td className="px-4 py-3">
                        <MetadataCell event={event} conversationID={conversationID} />
                      </td>
                    </tr>
                  );
                })}
                {!loading && events.length === 0 ? (
                  <tr>
                    <td
                      className="px-4 py-10 text-center text-sm text-muted-foreground"
                      colSpan={8}
                    >
                      No audit events
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

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-card px-4 py-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-2xl font-semibold">{value}</div>
    </div>
  );
}

function MetadataCell({ event, conversationID }: { event: AuditEvent; conversationID: string }) {
  const metadata = formatMetadata(event.metadata_json);
  if (!conversationID) {
    return (
      <div className="max-w-[240px] truncate font-mono text-xs text-muted-foreground">
        {metadata}
      </div>
    );
  }
  return (
    <div className="max-w-[260px] space-y-1">
      <Link
        href={`/conversations/${encodeURIComponent(conversationID)}`}
        className="block truncate font-mono text-xs text-primary underline-offset-2 hover:underline"
        title={`Open conversation ${conversationID}`}
      >
        conversation_id={conversationID}
      </Link>
      <div className="truncate font-mono text-xs text-muted-foreground">{metadata}</div>
    </div>
  );
}

function TraceLink({ traceID, requestID }: { traceID?: string; requestID?: string }) {
  if (traceID) {
    return (
      <Link
        href={`/admin/traces/${encodeURIComponent(traceID)}`}
        className="block max-w-[180px] truncate font-mono text-xs text-primary underline-offset-2 hover:underline"
        title={`Open trace ${traceID}`}
      >
        {traceID}
      </Link>
    );
  }
  return (
    <div className="max-w-[180px] truncate font-mono text-xs text-muted-foreground">
      {requestID || "-"}
    </div>
  );
}

function Badge({ children, tone }: { children: string; tone: "green" | "amber" | "red" }) {
  const cls =
    tone === "green"
      ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
      : tone === "amber"
        ? "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300"
        : "border-destructive/30 bg-destructive/10 text-destructive";
  return <span className={`rounded-md border px-2 py-0.5 text-xs ${cls}`}>{children}</span>;
}

function resultTone(result: string) {
  if (result === "success") return "green";
  if (result === "denied") return "amber";
  return "red";
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

function conversationIDForEvent(event: AuditEvent): string {
  const fromMetadata = event.metadata_json?.conversation_id;
  if (typeof fromMetadata === "string" && fromMetadata.trim()) {
    return fromMetadata.trim();
  }
  if (event.resource_type === "conversation" && event.resource_id?.trim()) {
    return event.resource_id.trim();
  }
  return "";
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
