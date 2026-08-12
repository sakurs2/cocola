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
import { type DataGridColumn } from "@cocola/ui-compat/data-grid";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { Segment } from "@cocola/ui-compat/segment";
import { Line } from "react-chartjs-2";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useLocale, useTranslations } from "next-intl";
import {
  AdminDataGrid,
  AdminErrorDialog,
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
  AdminTruncatedValue,
} from "@/components/admin/admin-ui";

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

function createChartOptions(locale: string) {
  return {
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
            return `${context.dataset.label}: ${formatNumber(value, locale)}`;
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
            return compactNumber(Number(value), locale);
          },
        },
      },
    },
  } satisfies ChartOptions<"line">;
}

export default function AdminTokenUsagePage() {
  const t = useTranslations("admin.tokenUsagePage");
  const locale = useLocale();
  const chartOptions = useMemo(() => createChartOptions(locale), [locale]);
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
    {
      id: "user",
      header: t("columns.user"),
      isRowHeader: true,
      minWidth: 180,
      cell: (user) => (
        <AdminTruncatedValue
          className="text-sm font-semibold"
          copyLabel={t("copyUser")}
          value={`${displayUser(user, t("unknownUser"))} · ${user.user_id}`}
        />
      ),
    },
    {
      id: "total",
      header: t("columns.total"),
      width: 90,
      align: "end",
      cell: (user) => (
        <span className="tabular-nums" title={formatNumber(user.total_tokens, locale)}>
          {compactNumber(user.total_tokens, locale)}
        </span>
      ),
    },
    {
      id: "input",
      header: t("columns.input"),
      width: 90,
      align: "end",
      cell: (user) => (
        <span className="tabular-nums" title={formatNumber(user.prompt_tokens, locale)}>
          {compactNumber(user.prompt_tokens, locale)}
        </span>
      ),
    },
    {
      id: "output",
      header: t("columns.output"),
      width: 90,
      align: "end",
      cell: (user) => (
        <span className="tabular-nums" title={formatNumber(user.completion_tokens, locale)}>
          {compactNumber(user.completion_tokens, locale)}
        </span>
      ),
    },
    {
      id: "calls",
      header: t("columns.calls"),
      width: 80,
      align: "end",
      cell: (user) => (
        <span className="tabular-nums" title={formatNumber(user.calls, locale)}>
          {compactNumber(user.calls, locale)}
        </span>
      ),
    },
    {
      id: "last",
      header: t("columns.lastUsed"),
      minWidth: 135,
      cell: (user) => (
        <span className="text-muted text-xs tabular-nums">
          {user.last_used_at ? formatDateTime(user.last_used_at, locale) : "—"}
        </span>
      ),
    },
  ];

  return (
    <AdminPage className="admin-theme-rose">
      <AdminPageHeader
        icon={<TokenUsagePageIcon className="size-5" />}
        title={t("title")}
        description={t("description")}
        actions={
          <>
            <AdminRefreshButton
              variant="outline"
              refreshing={loading}
              disabled={loading}
              onClick={() => void load()}
            >
              {t("refresh")}
            </AdminRefreshButton>
            <Button
              aria-label={t("exportExcel")}
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
              {t("export")}
            </Button>
          </>
        }
      />

      <div className="admin-token-usage-range flex flex-wrap items-center gap-2">
        <span className="text-sm font-medium text-muted">{t("range")}</span>
        <Segment
          aria-label={t("rangeAria")}
          selectedKey={preset}
          onSelectionChange={(key) => setPreset(String(key) as RangePreset)}
        >
          <Segment.Item id="24h">24h</Segment.Item>
          <Segment.Item id="7d">7d</Segment.Item>
          <Segment.Item id="30d">30d</Segment.Item>
          <Segment.Item id="90d">90d</Segment.Item>
          <Segment.Item id="custom">{t("custom")}</Segment.Item>
        </Segment>
        {preset === "custom" ? (
          <>
            <TextField
              className="w-40"
              value={customFrom}
              variant="secondary"
              onChange={setCustomFrom}
            >
              <Label className="sr-only">{t("from")}</Label>
              <Input type="date" />
            </TextField>
            <TextField className="w-40" value={customTo} variant="secondary" onChange={setCustomTo}>
              <Label className="sr-only">{t("to")}</Label>
              <Input type="date" />
            </TextField>
          </>
        ) : null}
        <div className="ml-auto rounded-xl bg-surface-secondary px-3 py-2 text-xs text-muted tabular-nums">
          {report
            ? `${formatDateTime(report.from, locale)} – ${formatDateTime(report.to, locale)}`
            : ""}
        </div>
      </div>

      <AdminErrorDialog
        error={error}
        title={t("operationFailed")}
        onDismiss={() => setError("")}
        onRetry={() => void load()}
      />

      <Card className="p-0">
        <Card.Header className="flex-row items-center justify-between px-5 pb-0 pt-5">
          <span className="flex items-center gap-2">
            <Card.Title>{t("trend")}</Card.Title>
            <span className="rounded-full bg-surface-secondary px-2 py-1 text-xs text-muted">
              {report?.bucket === "hour"
                ? t("bucket.hour")
                : report?.bucket === "day"
                  ? t("bucket.day")
                  : t("bucket.auto")}
            </span>
          </span>
          {loading ? <Loader2 className="size-4 animate-spin text-muted" /> : null}
        </Card.Header>
        <div className="h-[300px] px-4 pb-4 pt-1">
          {report && report.trend.length > 0 ? (
            <Line
              data={chartData(report.trend, report.bucket, locale, {
                total: t("dataset.total"),
                input: t("dataset.input"),
                output: t("dataset.output"),
              })}
              options={chartOptions}
            />
          ) : (
            <ChartEmptyState label={loading ? t("loadingTrend") : t("noTrend")} />
          )}
        </div>
      </Card>

      <section className="grid items-start gap-5 xl:grid-cols-[minmax(0,1.15fr)_minmax(360px,0.85fr)]">
        <Card className="min-w-0 overflow-hidden p-0">
          <Card.Header className="flex-row items-center justify-between gap-4 p-4">
            <span>
              <Card.Title>{t("users")}</Card.Title>
              <Card.Description>{t("usersDescription")}</Card.Description>
            </span>
            <SearchField
              aria-label={t("filterUsers")}
              className="w-full max-w-60"
              value={filter}
              onChange={setFilter}
            >
              <SearchField.Group>
                <SearchField.SearchIcon />
                <SearchField.Input placeholder={t("filterUsers")} />
                <SearchField.ClearButton />
              </SearchField.Group>
            </SearchField>
          </Card.Header>
          <Card.Content className="p-0">
            <AdminDataGrid
              aria-label={t("tableAria")}
              columns={columns}
              contentClassName="admin-token-usage-grid min-w-[700px]"
              data={users}
              getRowId={(user) => user.user_id}
              selectionMode="none"
              variant="primary"
              onRowAction={(key) => {
                const user = users.find((item) => item.user_id === String(key));
                if (user) setSelectedUser(user);
              }}
              renderEmptyState={() => (
                <EmptyState>
                  <EmptyState.Header>
                    <EmptyState.Media variant="icon">
                      <UserRound className="text-rose-500" />
                    </EmptyState.Media>
                    <EmptyState.Title>
                      {loading ? t("loadingUsers") : t("noUsers")}
                    </EmptyState.Title>
                    <EmptyState.Description>{t("noUsersDescription")}</EmptyState.Description>
                  </EmptyState.Header>
                </EmptyState>
              )}
            />
          </Card.Content>
        </Card>

        <Card className="p-0">
          <Card.Header className="flex-row items-center justify-between p-5">
            <div className="flex min-w-0 items-center gap-3">
              <div className="admin-page-icon">
                <UserRound className="size-4" />
              </div>
              <div className="min-w-0">
                <Card.Title className="truncate">
                  {activeUser ? displayUser(activeUser, t("unknownUser")) : t("noUser")}
                </Card.Title>
                <Card.Description className="truncate">
                  {activeUser?.email || activeUser?.user_id || t("selectUser")}
                </Card.Description>
              </div>
            </div>
            {userLoading ? <Loader2 className="size-4 animate-spin text-muted" /> : null}
          </Card.Header>
          <Card.Content className="space-y-4 p-5 pt-0">
            <div className="bg-surface-secondary flex items-center justify-between rounded-2xl px-4 py-3 text-sm">
              <span>
                <span className="text-muted block text-xs">{t("tokens")}</span>
                <strong
                  className="tabular-nums"
                  title={formatNumber(userReport?.summary.total_tokens ?? 0, locale)}
                >
                  {compactNumber(userReport?.summary.total_tokens ?? 0, locale)}
                </strong>
              </span>
              <span className="text-right">
                <span className="text-muted block text-xs">{t("calls")}</span>
                <strong
                  className="tabular-nums"
                  title={formatNumber(userReport?.summary.calls ?? 0, locale)}
                >
                  {compactNumber(userReport?.summary.calls ?? 0, locale)}
                </strong>
              </span>
            </div>
            <div className="h-[260px]">
              {userReport && userReport.trend.length > 0 ? (
                <Line
                  data={chartData(userReport.trend, userReport.bucket, locale, {
                    total: t("dataset.total"),
                    input: t("dataset.input"),
                    output: t("dataset.output"),
                  })}
                  options={chartOptions}
                />
              ) : (
                <ChartEmptyState label={activeUser ? t("noUserTrend") : t("selectUser")} />
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

function chartData(
  points: TokenUsagePoint[],
  bucket: "hour" | "day",
  locale: string,
  labels: { total: string; input: string; output: string },
) {
  return {
    labels: points.map((point) => formatBucket(point.bucket_start, bucket, locale)),
    datasets: [
      {
        label: labels.total,
        data: points.map((point) => point.total_tokens),
        borderColor: "#e11d48",
        backgroundColor: "rgba(225, 29, 72, 0.12)",
        fill: true,
        tension: 0.35,
        pointRadius: 2,
      },
      {
        label: labels.input,
        data: points.map((point) => point.prompt_tokens),
        borderColor: "#10b981",
        backgroundColor: "rgba(16, 185, 129, 0.08)",
        tension: 0.35,
        pointRadius: 2,
      },
      {
        label: labels.output,
        data: points.map((point) => point.completion_tokens),
        borderColor: "#7c3aed",
        backgroundColor: "rgba(124, 58, 237, 0.08)",
        tension: 0.35,
        pointRadius: 2,
      },
    ],
  };
}

function displayUser(user: TokenUsageUser, fallback: string) {
  return user.name || user.email || user.username || user.user_id || fallback;
}

function formatNumber(value: number, locale: string) {
  return new Intl.NumberFormat(locale).format(Math.round(value));
}

function compactNumber(value: number, locale: string) {
  return new Intl.NumberFormat(locale, { notation: "compact", maximumFractionDigits: 1 }).format(
    value,
  );
}

function formatDateTime(value: string, locale: string) {
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

function formatBucket(value: string, bucket: "hour" | "day", locale: string) {
  const date = new Date(value);
  if (bucket === "hour") {
    return new Intl.DateTimeFormat(locale, {
      month: "short",
      day: "2-digit",
      hour: "2-digit",
    }).format(date);
  }
  return new Intl.DateTimeFormat(locale, {
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
