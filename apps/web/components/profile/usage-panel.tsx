"use client";

import { Button, Card, ProgressBar } from "@heroui/react";
import { ChevronLeft, ChevronRight, LoaderCircle, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ModelIcon } from "@/components/ui/model-icon";

type QuotaScope = {
  scope: string;
  subject: string;
  period: string;
  used: number;
  limit: number;
  remaining: number;
  exceeded: boolean;
};
type QuotaResponse = { user_id: string; tenant_id: string; scopes: QuotaScope[] };
type UsageAggregate = {
  calls: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_usd: number;
};
type UsageRecord = {
  request_id?: string;
  id?: string;
  ts_unix?: number;
  ts?: string;
  created_at?: string;
  alias?: string;
  real_model?: string;
  model?: string;
  session_id?: string;
  prompt_tokens?: number;
  completion_tokens?: number;
  total_tokens?: number;
  cost_usd?: number;
};
type UsageResponse = {
  recent: UsageRecord[];
  user_aggregate?: UsageAggregate;
  session_aggregate?: UsageAggregate;
};

const nf = new Intl.NumberFormat();
const compactNf = new Intl.NumberFormat("en-US", {
  notation: "compact",
  maximumFractionDigits: 1,
});
const ACTIVITY_PAGE_SIZE = 5;
const fmtInt = (value: number | undefined | null) =>
  value == null || Number.isNaN(value) ? "-" : nf.format(value);
const fmtCompact = (value: number | undefined | null) =>
  value == null || Number.isNaN(value) ? "-" : compactNf.format(value);
const scopeLabel = (scope: string) =>
  scope === "user" ? "Personal" : scope === "tenant" ? "Team" : scope;

function formatTs(record: UsageRecord) {
  const raw =
    typeof record.ts_unix === "number" ? record.ts_unix * 1000 : (record.ts ?? record.created_at);
  if (!raw) return "-";
  const value = new Date(raw);
  return Number.isNaN(value.getTime()) ? String(raw) : value.toLocaleString();
}

function recordModel(record: UsageRecord) {
  return record.alias || record.model || record.real_model || "Unknown model";
}
function recordTotal(record: UsageRecord) {
  return record.total_tokens ?? (record.prompt_tokens ?? 0) + (record.completion_tokens ?? 0);
}
function modelSlug(record: UsageRecord) {
  const value = recordModel(record).toLowerCase();
  if (value.includes("deepseek")) return "deepseek";
  if (value.includes("qwen")) return "qwen";
  if (value.includes("claude")) return "claude";
  if (value.includes("gemini")) return "gemini";
  return "openai";
}

export function UsagePanel() {
  const [quota, setQuota] = useState<QuotaResponse | null>(null);
  const [usage, setUsage] = useState<UsageResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [activityPage, setActivityPage] = useState(1);

  const load = useCallback(async (showLoading = true) => {
    if (showLoading) setLoading(true);
    else setRefreshing(true);
    setError(null);
    const startedAt = Date.now();
    try {
      const [quotaResponse, usageResponse] = await Promise.all([
        fetch("/api/me/quota", { cache: "no-store" }),
        fetch("/api/me/usage?limit=20", { cache: "no-store" }),
      ]);
      if (!quotaResponse.ok)
        throw new Error(`quota ${quotaResponse.status}: ${await quotaResponse.text()}`);
      if (!usageResponse.ok)
        throw new Error(`usage ${usageResponse.status}: ${await usageResponse.text()}`);
      setQuota((await quotaResponse.json()) as QuotaResponse);
      setUsage((await usageResponse.json()) as UsageResponse);
      setActivityPage(1);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      if (!showLoading) {
        const remaining = 600 - (Date.now() - startedAt);
        if (remaining > 0) await new Promise((resolve) => window.setTimeout(resolve, remaining));
      }
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);
  const aggregate = usage?.user_aggregate;
  const recentActivity = usage?.recent ?? [];
  const activityPageCount = Math.max(1, Math.ceil(recentActivity.length / ACTIVITY_PAGE_SIZE));
  const currentActivityPage = Math.min(activityPage, activityPageCount);
  const activityStart = (currentActivityPage - 1) * ACTIVITY_PAGE_SIZE;
  const visibleActivity = recentActivity.slice(activityStart, activityStart + ACTIVITY_PAGE_SIZE);

  return (
    <Card className="p-5">
      <Card.Header className="flex-row items-start justify-between gap-4 p-0">
        <span>
          <Card.Title>Usage & quota</Card.Title>
          <Card.Description>Token allowances and recent model activity.</Card.Description>
        </span>
        <Button
          isDisabled={loading || refreshing}
          size="sm"
          variant="outline"
          onPress={() => void load(false)}
        >
          <RefreshCw className={`size-3.5 ${refreshing ? "animate-spin" : ""}`} />
          Refresh
        </Button>
      </Card.Header>
      <Card.Content className="mt-5 grid gap-5 p-0">
        {error ? (
          <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">{error}</div>
        ) : null}
        {loading ? (
          <div className="text-muted flex min-h-32 items-center justify-center gap-2 text-sm">
            <LoaderCircle className="size-4 animate-spin" />
            Loading usage…
          </div>
        ) : (
          <>
            <div className="grid gap-3 md:grid-cols-2">
              {(quota?.scopes.length ?? 0) === 0 ? (
                <div className="bg-surface-secondary text-muted rounded-2xl p-4 text-sm">
                  No quota policy applies — usage is unlimited.
                </div>
              ) : (
                quota?.scopes.map((scope) => (
                  <QuotaTile
                    key={`${scope.scope}:${scope.subject}:${scope.period}`}
                    scope={scope}
                  />
                ))
              )}
            </div>
            <div>
              <h3 className="text-sm font-semibold">Lifetime totals</h3>
              <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                <StatTile
                  exactValue={fmtInt(aggregate?.calls)}
                  label="Calls"
                  value={fmtCompact(aggregate?.calls)}
                />
                <StatTile
                  exactValue={fmtInt(aggregate?.prompt_tokens)}
                  label="Prompt tokens"
                  value={fmtCompact(aggregate?.prompt_tokens)}
                />
                <StatTile
                  exactValue={fmtInt(aggregate?.completion_tokens)}
                  label="Completion"
                  value={fmtCompact(aggregate?.completion_tokens)}
                />
                <StatTile
                  exactValue={fmtInt(aggregate?.total_tokens)}
                  label="Total tokens"
                  value={fmtCompact(aggregate?.total_tokens)}
                />
              </div>
            </div>
            <div>
              <h3 className="text-sm font-semibold">Recent activity</h3>
              {recentActivity.length === 0 ? (
                <p className="text-muted mt-3 text-sm">No usage recorded yet.</p>
              ) : (
                <div
                  aria-label="Recent model activity"
                  className="border-border mt-3 overflow-hidden rounded-2xl border"
                  role="table"
                >
                  <div
                    className="bg-surface-secondary/70 text-muted hidden min-h-10 items-center gap-4 px-4 text-xs font-medium md:grid md:grid-cols-[minmax(13rem,1fr)_6.5rem_6.5rem_6.5rem_11rem]"
                    role="row"
                  >
                    <span role="columnheader">Model</span>
                    <span className="text-right" role="columnheader">
                      Prompt
                    </span>
                    <span className="text-right" role="columnheader">
                      Output
                    </span>
                    <span className="text-right" role="columnheader">
                      Total
                    </span>
                    <span className="text-right" role="columnheader">
                      Time
                    </span>
                  </div>
                  <div role="rowgroup">
                    {visibleActivity.map((record) => (
                      <ActivityRow
                        key={
                          record.request_id ??
                          record.id ??
                          `${recordModel(record)}-${formatTs(record)}`
                        }
                        record={record}
                      />
                    ))}
                  </div>
                  <ActivityPagination
                    end={Math.min(activityStart + ACTIVITY_PAGE_SIZE, recentActivity.length)}
                    page={currentActivityPage}
                    pageCount={activityPageCount}
                    start={activityStart}
                    total={recentActivity.length}
                    onPageChange={setActivityPage}
                  />
                </div>
              )}
            </div>
          </>
        )}
      </Card.Content>
    </Card>
  );
}

function ActivityRow({ record }: { record: UsageRecord }) {
  return (
    <div
      className="border-border grid min-h-16 grid-cols-[minmax(0,1fr)_auto] items-center gap-3 border-t px-4 py-3 first:border-t-0 md:grid-cols-[minmax(13rem,1fr)_6.5rem_6.5rem_6.5rem_11rem] md:gap-4"
      role="row"
    >
      <span className="flex min-w-0 items-center gap-3" role="cell">
        <span className="bg-surface-secondary flex size-9 shrink-0 items-center justify-center rounded-xl">
          <ModelIcon
            bare
            className="size-6"
            icon={{ type: "lobe-icons", slug: modelSlug(record) }}
          />
        </span>
        <span className="min-w-0">
          <span className="block truncate text-sm font-semibold">{recordModel(record)}</span>
          <span className="text-muted mt-0.5 block text-xs md:hidden">{formatTs(record)}</span>
        </span>
      </span>
      <span className="grid grid-cols-3 gap-3 text-right text-xs tabular-nums md:contents">
        <span role="cell">
          <span className="text-muted block md:hidden">Prompt</span>
          {fmtInt(record.prompt_tokens)}
        </span>
        <span role="cell">
          <span className="text-muted block md:hidden">Output</span>
          {fmtInt(record.completion_tokens)}
        </span>
        <span className="font-medium" role="cell">
          <span className="text-muted block font-normal md:hidden">Total</span>
          {fmtInt(recordTotal(record))}
        </span>
      </span>
      <span className="text-muted hidden text-right text-xs tabular-nums md:block" role="cell">
        {formatTs(record)}
      </span>
    </div>
  );
}

function ActivityPagination({
  end,
  page,
  pageCount,
  start,
  total,
  onPageChange,
}: {
  end: number;
  page: number;
  pageCount: number;
  start: number;
  total: number;
  onPageChange: (page: number) => void;
}) {
  return (
    <nav
      aria-label="Recent activity pagination"
      className="border-border bg-surface-secondary/30 flex min-h-14 flex-wrap items-center justify-between gap-3 border-t px-4 py-2.5"
    >
      <span className="text-muted text-xs tabular-nums">
        Showing {start + 1}–{end} of {total}
      </span>
      <span className="flex items-center gap-2">
        <Button
          isIconOnly
          aria-label="Previous activity page"
          isDisabled={page === 1}
          size="sm"
          variant="outline"
          onPress={() => onPageChange(Math.max(1, page - 1))}
        >
          <ChevronLeft className="size-4" />
        </Button>
        <span className="text-muted min-w-14 text-center text-xs tabular-nums">
          {page} / {pageCount}
        </span>
        <Button
          isIconOnly
          aria-label="Next activity page"
          isDisabled={page === pageCount}
          size="sm"
          variant="outline"
          onPress={() => onPageChange(Math.min(pageCount, page + 1))}
        >
          <ChevronRight className="size-4" />
        </Button>
      </span>
    </nav>
  );
}

function QuotaTile({ scope }: { scope: QuotaScope }) {
  const unlimited = scope.limit <= 0;
  const value = unlimited
    ? 0
    : Math.min(100, Math.round((scope.used / Math.max(1, scope.limit)) * 100));
  return (
    <div className="bg-surface-secondary rounded-2xl p-4">
      <div className="flex items-start justify-between gap-3">
        <span>
          <span className="text-sm font-semibold">{scopeLabel(scope.scope)}</span>
          <span className="text-muted mt-0.5 block text-xs">{scope.period} allowance</span>
        </span>
        <span className="text-sm font-semibold tabular-nums">
          {unlimited ? "Unlimited" : `${fmtInt(scope.remaining)} left`}
        </span>
      </div>
      {!unlimited ? (
        <ProgressBar
          aria-label={`${scopeLabel(scope.scope)} quota used`}
          className="mt-4"
          value={value}
        >
          <ProgressBar.Track>
            <ProgressBar.Fill />
          </ProgressBar.Track>
        </ProgressBar>
      ) : null}
      <p className={`mt-2 text-xs tabular-nums ${scope.exceeded ? "text-danger" : "text-muted"}`}>
        {scope.exceeded
          ? "Limit reached"
          : unlimited
            ? "No quota limit"
            : `${fmtInt(scope.used)} of ${fmtInt(scope.limit)} used`}
      </p>
    </div>
  );
}

function StatTile({
  exactValue,
  label,
  value,
}: {
  exactValue: string;
  label: string;
  value: string;
}) {
  return (
    <div className="bg-surface-secondary rounded-2xl p-4">
      <span className="text-muted text-xs">{label}</span>
      <span
        aria-label={`${label}: ${exactValue}`}
        className="mt-1 block text-xl font-semibold tabular-nums"
        title={exactValue}
      >
        {value}
      </span>
    </div>
  );
}
