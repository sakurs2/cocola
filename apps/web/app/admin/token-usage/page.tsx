"use client";

import { BarChart3 as TokenUsagePageIcon } from "lucide-react";
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
import { Download, Loader2, UserRound } from "lucide-react";
import { Button, Card, Input, Label, SearchField, TextField } from "@heroui/react";
import { DataGrid, type DataGridColumn } from "@heroui-pro/react/data-grid";
import { EmptyState } from "@heroui-pro/react/empty-state";
import { Segment } from "@heroui-pro/react/segment";
import { Line } from "react-chartjs-2";
import { useCallback, useEffect, useMemo, useState } from "react";
import { AdminPage, AdminPageHeader, AdminRefreshButton } from "@/components/admin/admin-ui";

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
      grid: { color: "rgba(225, 29, 72, 0.08)" },
      ticks: { color: "#64748b", maxRotation: 0 },
    },
    y: {
      beginAtZero: true,
      grid: { color: "rgba(225, 29, 72, 0.09)" },
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

  const activeUser = selectedUser ?? users[0] ?? null;

  const columns: DataGridColumn<TokenUsageUser>[] = [
    { id: "user", header: "User", isRowHeader: true, minWidth: 260, cell: (user) => <Button className="h-auto min-w-0 justify-start px-0 py-1 text-left" variant="ghost" onPress={() => setSelectedUser(user)}><span className="min-w-0"><span className="block truncate font-semibold">{displayUser(user)}</span><span className="text-muted block truncate font-mono text-xs">{user.user_id}</span></span></Button> },
    { id: "total", header: "Total", width: 120, align: "end", cell: (user) => <span className="tabular-nums" title={formatNumber(user.total_tokens)}>{compactNumber(user.total_tokens)}</span> },
    { id: "input", header: "Input", width: 120, align: "end", cell: (user) => <span className="tabular-nums" title={formatNumber(user.prompt_tokens)}>{compactNumber(user.prompt_tokens)}</span> },
    { id: "output", header: "Output", width: 120, align: "end", cell: (user) => <span className="tabular-nums" title={formatNumber(user.completion_tokens)}>{compactNumber(user.completion_tokens)}</span> },
    { id: "calls", header: "Calls", width: 100, align: "end", cell: (user) => <span className="tabular-nums" title={formatNumber(user.calls)}>{compactNumber(user.calls)}</span> },
    { id: "last", header: "Last used", minWidth: 160, cell: (user) => <span className="text-muted text-xs tabular-nums">{user.last_used_at ? formatDateTime(user.last_used_at) : "—"}</span> },
  ];

  return (
    <AdminPage className="admin-theme-rose">
      <AdminPageHeader
        icon={<TokenUsagePageIcon className="size-5" />}
        title="Token Usage"
        description="User token consumption from the LLM usage ledger"
        actions={
          <>
            <AdminRefreshButton
              variant="outline"
              refreshing={loading}
              disabled={loading}
              onClick={() => void load()}
            >
              Refresh
            </AdminRefreshButton>
            <Button
              aria-label="Export Excel"
              isDisabled={exporting || !report}
              isPending={exporting}
              variant="outline"
              onPress={() => void exportExcel()}
            >
              {exporting ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Download className="size-4" />
              )}
              Export
            </Button>
          </>
        }
      />

      <Card className="p-4"><Card.Content className="flex flex-wrap items-end gap-3 p-0">
        <div><Label className="mb-2 block text-sm">Range</Label><Segment aria-label="Usage range" selectedKey={preset} onSelectionChange={(key) => setPreset(String(key) as RangePreset)}><Segment.Item id="24h">24h</Segment.Item><Segment.Item id="7d">7d</Segment.Item><Segment.Item id="30d">30d</Segment.Item><Segment.Item id="90d">90d</Segment.Item><Segment.Item id="custom">Custom</Segment.Item></Segment></div>
        {preset === "custom" ? (
          <>
            <TextField value={customFrom} variant="secondary" onChange={setCustomFrom}><Label>From</Label><Input type="date" /></TextField>
            <TextField value={customTo} variant="secondary" onChange={setCustomTo}><Label>To</Label><Input type="date" /></TextField>
          </>
        ) : null}
        <div className="text-muted ml-auto text-xs">
          {report ? `${formatDateTime(report.from)} - ${formatDateTime(report.to)}` : ""}
        </div>
      </Card.Content></Card>

      {error ? (
        <div className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">
          {error}
        </div>
      ) : null}

      <Card className="p-0">
        <Card.Header className="flex-row items-center justify-between p-5">
          <span><Card.Title>Usage trend</Card.Title><Card.Description>Bucket: {report?.bucket ?? "auto"}</Card.Description></span>
          {loading ? <Loader2 className="size-4 animate-spin text-muted" /> : null}
        </Card.Header>
        <div className="h-[340px] p-4">
          {report && report.trend.length > 0 ? (
            <Line data={chartData(report.trend, report.bucket)} options={chartOptions} />
          ) : (
            <ChartEmptyState label={loading ? "Loading usage trend" : "No usage in this range"} />
          )}
        </div>
      </Card>

      <section className="grid gap-5 xl:grid-cols-[minmax(0,1.15fr)_minmax(360px,0.85fr)]">
        <Card className="min-w-0 overflow-hidden p-0"><Card.Header className="flex-row items-center justify-between gap-4 p-5"><span><Card.Title>Users</Card.Title><Card.Description>Sorted by total token usage</Card.Description></span><SearchField aria-label="Filter users" className="w-full max-w-72" value={filter} onChange={setFilter}><SearchField.Group><SearchField.SearchIcon /><SearchField.Input placeholder="Filter users" /><SearchField.ClearButton /></SearchField.Group></SearchField></Card.Header><Card.Content className="p-0"><DataGrid aria-label="Token usage by user" columns={columns} contentClassName="min-w-[820px]" data={users} getRowId={(user) => user.user_id} selectionMode="none" variant="primary" renderEmptyState={() => <EmptyState><EmptyState.Header><EmptyState.Media variant="icon"><UserRound className="text-rose-500" /></EmptyState.Media><EmptyState.Title>{loading ? "Loading users" : "No users in this range"}</EmptyState.Title><EmptyState.Description>Usage will appear after users complete model calls.</EmptyState.Description></EmptyState.Header></EmptyState>} /></Card.Content></Card>

        <Card className="p-0">
          <Card.Header className="flex-row items-center justify-between p-5">
            <div className="flex min-w-0 items-center gap-3">
              <div className="admin-page-icon">
                <UserRound className="size-4" />
              </div>
              <div className="min-w-0">
                <Card.Title className="truncate">
                  {activeUser ? displayUser(activeUser) : "No user selected"}
                </Card.Title>
                <Card.Description className="truncate">
                  {activeUser?.email || activeUser?.user_id || "Select a user"}
                </Card.Description>
              </div>
            </div>
            {userLoading ? <Loader2 className="size-4 animate-spin text-muted" /> : null}
          </Card.Header>
          <Card.Content className="space-y-4 p-5 pt-0">
            <div className="bg-surface-secondary flex items-center justify-between rounded-2xl px-4 py-3 text-sm"><span><span className="text-muted block text-xs">Tokens</span><strong className="tabular-nums" title={formatNumber(userReport?.summary.total_tokens ?? 0)}>{compactNumber(userReport?.summary.total_tokens ?? 0)}</strong></span><span className="text-right"><span className="text-muted block text-xs">Calls</span><strong className="tabular-nums" title={formatNumber(userReport?.summary.calls ?? 0)}>{compactNumber(userReport?.summary.calls ?? 0)}</strong></span></div>
            <div className="h-[260px]">
              {userReport && userReport.trend.length > 0 ? (
                <Line
                  data={chartData(userReport.trend, userReport.bucket)}
                  options={chartOptions}
                />
              ) : (
                <ChartEmptyState label={activeUser ? "No user trend data" : "Select a user"} />
              )}
            </div>
          </Card.Content>
        </Card>
      </section>
    </AdminPage>
  );
}

function ChartEmptyState({ label }: { label: string }) {
  return (
    <div className="grid h-full place-items-center rounded-md border border-dashed border-border text-sm text-muted">
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
        borderColor: "#e11d48",
        backgroundColor: "rgba(225, 29, 72, 0.12)",
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
