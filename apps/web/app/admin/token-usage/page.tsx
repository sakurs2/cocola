"use client";

import { ChartLineUp as TokenUsagePageIcon } from "@phosphor-icons/react";
import {
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  Filler,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
  type ChartOptions,
} from "chart.js";
import { Download, Loader2, RefreshCw, Search, UserRound } from "lucide-react";
import { Line } from "react-chartjs-2";
import { useCallback, useEffect, useMemo, useState } from "react";

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  BarElement,
  Tooltip,
  Legend,
  Filler,
);

type TokenUsageSummary = {
  calls: number;
  user_count: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_usd: number;
};

type TokenUsagePoint = {
  bucket_start: string;
  calls: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_usd: number;
};

type TokenUsageUser = {
  user_id: string;
  username?: string;
  email?: string;
  name?: string;
  role?: string;
  enabled: boolean;
  known_user: boolean;
  calls: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_usd: number;
  last_used_at?: string;
};

type TokenUsageReport = {
  from: string;
  to: string;
  bucket: "hour" | "day";
  summary: TokenUsageSummary;
  trend: TokenUsagePoint[];
  users?: TokenUsageUser[];
  limit?: number;
  offset?: number;
};

type RangePreset = "24h" | "7d" | "30d" | "90d" | "custom";

const input =
  "h-9 min-w-0 rounded-md border border-input bg-background px-3 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-ring focus:ring-1 focus:ring-ring";
const iconBtn =
  "inline-flex h-9 items-center justify-center gap-2 rounded-md border border-border bg-background px-3 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-50";
const PAGE_LIMIT = 100;

const chartOptions = {
  responsive: true,
  maintainAspectRatio: false,
  animation: false,
  interaction: { mode: "index", intersect: false },
  plugins: {
    legend: {
      labels: {
        color: "#516174",
        boxWidth: 10,
        usePointStyle: true,
      },
    },
    tooltip: {
      callbacks: {
        label(context) {
          const value = Number(context.raw ?? 0);
          return `${context.dataset.label}: ${formatNumber(value)}`;
        },
      },
    },
  },
  scales: {
    x: {
      grid: { color: "rgba(37, 99, 235, 0.08)" },
      ticks: { color: "#64748b", maxRotation: 0 },
    },
    y: {
      beginAtZero: true,
      grid: { color: "rgba(37, 99, 235, 0.09)" },
      ticks: {
        color: "#64748b",
        callback(value) {
          return compactNumber(Number(value));
        },
      },
    },
  },
} satisfies ChartOptions<"line">;

export default function AdminTokenUsagePage() {
  const [preset, setPreset] = useState<RangePreset>("30d");
  const [customFrom, setCustomFrom] = useState(dateInput(daysAgo(30)));
  const [customTo, setCustomTo] = useState(dateInput(new Date()));
  const [report, setReport] = useState<TokenUsageReport | null>(null);
  const [selectedUser, setSelectedUser] = useState<TokenUsageUser | null>(null);
  const [userReport, setUserReport] = useState<TokenUsageReport | null>(null);
  const [loading, setLoading] = useState(true);
  const [userLoading, setUserLoading] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState("");

  const queryString = useMemo(() => {
    const params = new URLSearchParams({ limit: String(PAGE_LIMIT), bucket: "auto" });
    if (preset === "custom") {
      if (customFrom) params.set("from", customFrom);
      if (customTo) params.set("to", customTo);
      return params.toString();
    }
    const to = new Date();
    const from = new Date(to);
    if (preset === "24h") from.setHours(from.getHours() - 24);
    if (preset === "7d") from.setDate(from.getDate() - 7);
    if (preset === "30d") from.setDate(from.getDate() - 30);
    if (preset === "90d") from.setDate(from.getDate() - 90);
    params.set("from", from.toISOString());
    params.set("to", to.toISOString());
    return params.toString();
  }, [customFrom, customTo, preset]);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await fetch(`/api/admin/token-usage?${queryString}`, { cache: "no-store" });
      if (!res.ok) throw new Error(await errorText(res));
      const body = (await res.json()) as TokenUsageReport;
      setReport(body);
      setSelectedUser((prev) => {
        if (!prev) return body.users?.[0] ?? null;
        return body.users?.find((user) => user.user_id === prev.user_id) ?? body.users?.[0] ?? null;
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setReport(null);
      setSelectedUser(null);
    } finally {
      setLoading(false);
    }
  }, [queryString]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!selectedUser) {
      setUserReport(null);
      return;
    }
    let cancelled = false;
    const run = async () => {
      setUserLoading(true);
      try {
        const res = await fetch(
          `/api/admin/token-usage/users/${encodeURIComponent(selectedUser.user_id)}?${queryString}`,
          { cache: "no-store" },
        );
        if (!res.ok) throw new Error(await errorText(res));
        const body = (await res.json()) as TokenUsageReport;
        if (!cancelled) setUserReport(body);
      } catch {
        if (!cancelled) setUserReport(null);
      } finally {
        if (!cancelled) setUserLoading(false);
      }
    };
    void run();
    return () => {
      cancelled = true;
    };
  }, [queryString, selectedUser]);

  const users = useMemo(() => {
    const source = report?.users ?? [];
    const needle = filter.trim().toLowerCase();
    if (!needle) return source;
    return source.filter((user) =>
      [user.user_id, user.email, user.username, user.name]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(needle)),
    );
  }, [filter, report?.users]);

  const exportExcel = async () => {
    setExporting(true);
    try {
      const res = await fetch(`/api/admin/token-usage/export?${queryString}`, {
        cache: "no-store",
      });
      if (!res.ok) throw new Error(await errorText(res));
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = filenameFromDisposition(res.headers.get("content-disposition"));
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setExporting(false);
    }
  };

  const summary = report?.summary ?? emptySummary();
  const activeUser = selectedUser ?? users[0] ?? null;

  return (
    <main className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border">
        <div className="mx-auto flex h-16 max-w-7xl items-center gap-3 px-6">
          <div className="admin-page-icon">
            <TokenUsagePageIcon className="size-[18px]" weight="duotone" />
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-base font-semibold">Token Usage</h1>
            <p className="truncate text-xs text-muted-foreground">
              User token consumption from the LLM usage ledger
            </p>
          </div>
          <button
            className={iconBtn}
            title="Refresh"
            onClick={() => void load()}
            disabled={loading}
          >
            <RefreshCw className={loading ? "size-4 animate-spin" : "size-4"} />
            Refresh
          </button>
          <button
            className={iconBtn}
            title="Export Excel"
            onClick={() => void exportExcel()}
            disabled={exporting || !report}
          >
            {exporting ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Download className="size-4" />
            )}
            Export
          </button>
        </div>
      </header>

      <div className="mx-auto max-w-7xl space-y-5 px-6 py-6">
        <section className="flex flex-wrap items-end gap-3 rounded-lg border border-border bg-card p-4">
          <label className="space-y-1">
            <span className="text-xs text-muted-foreground">Range</span>
            <select
              className={input}
              value={preset}
              onChange={(event) => setPreset(event.target.value as RangePreset)}
            >
              <option value="24h">Last 24 hours</option>
              <option value="7d">Last 7 days</option>
              <option value="30d">Last 30 days</option>
              <option value="90d">Last 90 days</option>
              <option value="custom">Custom</option>
            </select>
          </label>
          {preset === "custom" ? (
            <>
              <label className="space-y-1">
                <span className="text-xs text-muted-foreground">From</span>
                <input
                  className={input}
                  type="date"
                  value={customFrom}
                  onChange={(event) => setCustomFrom(event.target.value)}
                />
              </label>
              <label className="space-y-1">
                <span className="text-xs text-muted-foreground">To</span>
                <input
                  className={input}
                  type="date"
                  value={customTo}
                  onChange={(event) => setCustomTo(event.target.value)}
                />
              </label>
            </>
          ) : null}
          <div className="ml-auto text-xs text-muted-foreground">
            {report ? `${formatDateTime(report.from)} - ${formatDateTime(report.to)}` : ""}
          </div>
        </section>

        {error ? (
          <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {error}
          </div>
        ) : null}

        <section className="grid gap-3 md:grid-cols-5">
          <Metric label="Total Tokens" value={formatNumber(summary.total_tokens)} />
          <Metric label="Input Tokens" value={formatNumber(summary.prompt_tokens)} />
          <Metric label="Output Tokens" value={formatNumber(summary.completion_tokens)} />
          <Metric label="Calls" value={formatNumber(summary.calls)} />
          <Metric label="Users" value={formatNumber(summary.user_count)} />
        </section>

        <section className="rounded-lg border border-border bg-card">
          <div className="flex items-center justify-between border-b border-border px-4 py-3">
            <div>
              <h2 className="text-sm font-semibold">Usage Trend</h2>
              <p className="text-xs text-muted-foreground">Bucket: {report?.bucket ?? "auto"}</p>
            </div>
            {loading ? <Loader2 className="size-4 animate-spin text-muted-foreground" /> : null}
          </div>
          <div className="h-[340px] p-4">
            {report && report.trend.length > 0 ? (
              <Line data={chartData(report.trend, report.bucket)} options={chartOptions} />
            ) : (
              <EmptyState label={loading ? "Loading usage trend" : "No usage in this range"} />
            )}
          </div>
        </section>

        <section className="grid gap-5 xl:grid-cols-[minmax(0,1.15fr)_minmax(360px,0.85fr)]">
          <div className="rounded-lg border border-border bg-card">
            <div className="flex flex-col gap-3 border-b border-border px-4 py-3 md:flex-row md:items-center">
              <div className="min-w-0 flex-1">
                <h2 className="text-sm font-semibold">Users</h2>
                <p className="text-xs text-muted-foreground">Sorted by total token usage</p>
              </div>
              <label className="relative block w-full md:w-72">
                <Search className="pointer-events-none absolute left-2 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <input
                  className={`${input} w-full pl-8`}
                  placeholder="Filter users"
                  value={filter}
                  onChange={(event) => setFilter(event.target.value)}
                />
              </label>
            </div>
            <div className="overflow-x-auto">
              <table className="w-full min-w-[760px] text-left text-sm">
                <thead className="border-b border-border text-xs text-muted-foreground">
                  <tr>
                    <th className="px-4 py-3 font-medium">User</th>
                    <th className="px-4 py-3 text-right font-medium">Total</th>
                    <th className="px-4 py-3 text-right font-medium">Input</th>
                    <th className="px-4 py-3 text-right font-medium">Output</th>
                    <th className="px-4 py-3 text-right font-medium">Calls</th>
                    <th className="px-4 py-3 font-medium">Last Used</th>
                  </tr>
                </thead>
                <tbody>
                  {users.map((user) => {
                    const active = activeUser?.user_id === user.user_id;
                    return (
                      <tr
                        key={user.user_id}
                        onClick={() => setSelectedUser(user)}
                        className={
                          active
                            ? "cursor-pointer border-b border-border bg-accent/70"
                            : "cursor-pointer border-b border-border transition-colors hover:bg-accent/50"
                        }
                      >
                        <td className="px-4 py-3">
                          <div className="font-medium">{displayUser(user)}</div>
                          <div className="text-xs text-muted-foreground">{user.user_id}</div>
                        </td>
                        <td className="px-4 py-3 text-right tabular-nums">
                          {formatNumber(user.total_tokens)}
                        </td>
                        <td className="px-4 py-3 text-right tabular-nums">
                          {formatNumber(user.prompt_tokens)}
                        </td>
                        <td className="px-4 py-3 text-right tabular-nums">
                          {formatNumber(user.completion_tokens)}
                        </td>
                        <td className="px-4 py-3 text-right tabular-nums">
                          {formatNumber(user.calls)}
                        </td>
                        <td className="px-4 py-3 text-xs text-muted-foreground">
                          {user.last_used_at ? formatDateTime(user.last_used_at) : "-"}
                        </td>
                      </tr>
                    );
                  })}
                  {users.length === 0 ? (
                    <tr>
                      <td
                        className="px-4 py-8 text-center text-sm text-muted-foreground"
                        colSpan={6}
                      >
                        {loading ? "Loading users" : "No users in this range"}
                      </td>
                    </tr>
                  ) : null}
                </tbody>
              </table>
            </div>
          </div>

          <aside className="rounded-lg border border-border bg-card">
            <div className="flex items-center gap-3 border-b border-border px-4 py-3">
              <div className="grid size-9 place-items-center rounded-md bg-muted">
                <UserRound className="size-4 text-muted-foreground" />
              </div>
              <div className="min-w-0">
                <h2 className="truncate text-sm font-semibold">
                  {activeUser ? displayUser(activeUser) : "No user selected"}
                </h2>
                <p className="truncate text-xs text-muted-foreground">
                  {activeUser?.email || activeUser?.user_id || "Select a user"}
                </p>
              </div>
              {userLoading ? (
                <Loader2 className="ml-auto size-4 animate-spin text-muted-foreground" />
              ) : null}
            </div>
            <div className="space-y-4 p-4">
              <div className="grid grid-cols-2 gap-3">
                <Metric
                  label="User Tokens"
                  value={formatNumber(userReport?.summary.total_tokens ?? 0)}
                />
                <Metric label="User Calls" value={formatNumber(userReport?.summary.calls ?? 0)} />
              </div>
              <div className="h-[260px]">
                {userReport && userReport.trend.length > 0 ? (
                  <Line
                    data={chartData(userReport.trend, userReport.bucket)}
                    options={chartOptions}
                  />
                ) : (
                  <EmptyState label={activeUser ? "No user trend data" : "Select a user"} />
                )}
              </div>
            </div>
          </aside>
        </section>
      </div>
    </main>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-card px-4 py-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 truncate text-2xl font-semibold tabular-nums">{value}</div>
    </div>
  );
}

function EmptyState({ label }: { label: string }) {
  return (
    <div className="grid h-full place-items-center rounded-md border border-dashed border-border text-sm text-muted-foreground">
      {label}
    </div>
  );
}

function chartData(points: TokenUsagePoint[], bucket: "hour" | "day") {
  return {
    labels: points.map((point) => formatBucket(point.bucket_start, bucket)),
    datasets: [
      {
        label: "Total",
        data: points.map((point) => point.total_tokens),
        borderColor: "#2563eb",
        backgroundColor: "rgba(37, 99, 235, 0.12)",
        fill: true,
        tension: 0.35,
        pointRadius: 2,
      },
      {
        label: "Input",
        data: points.map((point) => point.prompt_tokens),
        borderColor: "#10b981",
        backgroundColor: "rgba(16, 185, 129, 0.08)",
        tension: 0.35,
        pointRadius: 2,
      },
      {
        label: "Output",
        data: points.map((point) => point.completion_tokens),
        borderColor: "#7c3aed",
        backgroundColor: "rgba(124, 58, 237, 0.08)",
        tension: 0.35,
        pointRadius: 2,
      },
    ],
  };
}

function emptySummary(): TokenUsageSummary {
  return {
    calls: 0,
    user_count: 0,
    prompt_tokens: 0,
    completion_tokens: 0,
    total_tokens: 0,
    cost_usd: 0,
  };
}

function displayUser(user: TokenUsageUser) {
  return user.name || user.email || user.username || user.user_id || "Unknown user";
}

function formatNumber(value: number) {
  return new Intl.NumberFormat("en-US").format(Math.round(value));
}

function compactNumber(value: number) {
  return new Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 1 }).format(
    value,
  );
}

function formatDateTime(value: string) {
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

function formatBucket(value: string, bucket: "hour" | "day") {
  const date = new Date(value);
  if (bucket === "hour") {
    return new Intl.DateTimeFormat("en-US", {
      month: "short",
      day: "2-digit",
      hour: "2-digit",
    }).format(date);
  }
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "2-digit",
  }).format(date);
}

function daysAgo(days: number) {
  const date = new Date();
  date.setDate(date.getDate() - days);
  return date;
}

function dateInput(date: Date) {
  return date.toISOString().slice(0, 10);
}

async function errorText(res: Response) {
  try {
    const body = (await res.json()) as {
      error?: string | { message?: string };
      error_description?: string;
      message?: string;
    };
    return (
      (typeof body.error === "object" ? body.error.message : body.error) ||
      body.message ||
      body.error_description ||
      `${res.status} ${res.statusText}`
    );
  } catch {
    return `${res.status} ${res.statusText}`;
  }
}

function filenameFromDisposition(header: string | null) {
  const match = header?.match(/filename="?([^";]+)"?/i);
  return match?.[1] || "cocola-token-usage.xlsx";
}
