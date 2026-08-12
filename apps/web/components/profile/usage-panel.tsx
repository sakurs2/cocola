"use client";

import { Button, Card, ProgressBar } from "@heroui/react";
import { ChevronLeft, ChevronRight, LoaderCircle, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useFormatter, useTranslations } from "next-intl";
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

const ACTIVITY_PAGE_SIZE = 5;

function recordModel(record: UsageRecord, fallback: string) {
  return record.alias || record.model || record.real_model || fallback;
}
function recordTotal(record: UsageRecord) {
  return record.total_tokens ?? (record.prompt_tokens ?? 0) + (record.completion_tokens ?? 0);
}
function modelSlug(record: UsageRecord) {
  const value = recordModel(record, "").toLowerCase();
  if (value.includes("deepseek")) return "deepseek";
  if (value.includes("qwen")) return "qwen";
  if (value.includes("claude")) return "claude";
  if (value.includes("gemini")) return "gemini";
  return "openai";
}

export function UsagePanel() {
  const t = useTranslations("profile.usage");
  const format = useFormatter();
  const fmtInt = (value: number | undefined | null) =>
    value == null || Number.isNaN(value) ? "-" : format.number(value);
  const fmtCompact = (value: number | undefined | null) =>
    value == null || Number.isNaN(value)
      ? "-"
      : format.number(value, { notation: "compact", maximumFractionDigits: 1 });
  const formatTs = (record: UsageRecord) => {
    const raw =
      typeof record.ts_unix === "number" ? record.ts_unix * 1000 : (record.ts ?? record.created_at);
    if (!raw) return "-";
    const value = new Date(raw);
    return Number.isNaN(value.getTime())
      ? String(raw)
      : format.dateTime(value, { dateStyle: "medium", timeStyle: "short" });
  };
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
          <Card.Title>{t("title")}</Card.Title>
          <Card.Description>{t("description")}</Card.Description>
        </span>
        <Button
          isDisabled={loading || refreshing}
          size="sm"
          variant="outline"
          onPress={() => void load(false)}
        >
          <RefreshCw className={`size-3.5 ${refreshing ? "animate-spin" : ""}`} />
          {t("refresh")}
        </Button>
      </Card.Header>
      <Card.Content className="mt-5 grid gap-5 p-0">
        {error ? (
          <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">{error}</div>
        ) : null}
        {loading ? (
          <div className="text-muted flex min-h-32 items-center justify-center gap-2 text-sm">
            <LoaderCircle className="size-4 animate-spin" />
            {t("loading")}
          </div>
        ) : (
          <>
            <div className="grid gap-3 md:grid-cols-2">
              {(quota?.scopes.length ?? 0) === 0 ? (
                <div className="bg-surface-secondary text-muted rounded-2xl p-4 text-sm">
                  {t("unlimitedPolicy")}
                </div>
              ) : (
                quota?.scopes.map((scope) => (
                  <QuotaTile
                    key={`${scope.scope}:${scope.subject}:${scope.period}`}
                    scope={scope}
                    t={t}
                    fmtInt={fmtInt}
                  />
                ))
              )}
            </div>
            <div>
              <h3 className="text-sm font-semibold">{t("lifetime")}</h3>
              <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                <StatTile
                  exactValue={fmtInt(aggregate?.calls)}
                  label={t("calls")}
                  value={fmtCompact(aggregate?.calls)}
                />
                <StatTile
                  exactValue={fmtInt(aggregate?.prompt_tokens)}
                  label={t("promptTokens")}
                  value={fmtCompact(aggregate?.prompt_tokens)}
                />
                <StatTile
                  exactValue={fmtInt(aggregate?.completion_tokens)}
                  label={t("completion")}
                  value={fmtCompact(aggregate?.completion_tokens)}
                />
                <StatTile
                  exactValue={fmtInt(aggregate?.total_tokens)}
                  label={t("totalTokens")}
                  value={fmtCompact(aggregate?.total_tokens)}
                />
              </div>
            </div>
            <div>
              <h3 className="text-sm font-semibold">{t("recent")}</h3>
              {recentActivity.length === 0 ? (
                <p className="text-muted mt-3 text-sm">{t("noActivity")}</p>
              ) : (
                <div
                  aria-label={t("activityAria")}
                  className="border-border mt-3 overflow-hidden rounded-2xl border"
                  role="table"
                >
                  <div
                    className="bg-surface-secondary/70 text-muted hidden min-h-10 items-center gap-4 px-4 text-xs font-medium md:grid md:grid-cols-[minmax(13rem,1fr)_6.5rem_6.5rem_6.5rem_11rem]"
                    role="row"
                  >
                    <span role="columnheader">{t("model")}</span>
                    <span className="text-right" role="columnheader">
                      {t("prompt")}
                    </span>
                    <span className="text-right" role="columnheader">
                      {t("output")}
                    </span>
                    <span className="text-right" role="columnheader">
                      {t("total")}
                    </span>
                    <span className="text-right" role="columnheader">
                      {t("time")}
                    </span>
                  </div>
                  <div role="rowgroup">
                    {visibleActivity.map((record) => (
                      <ActivityRow
                        key={
                          record.request_id ??
                          record.id ??
                          `${recordModel(record, t("unknownModel"))}-${formatTs(record)}`
                        }
                        record={record}
                        t={t}
                        fmtInt={fmtInt}
                        formatTs={formatTs}
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
                    t={t}
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

function ActivityRow({
  record,
  t,
  fmtInt,
  formatTs,
}: {
  record: UsageRecord;
  t: ReturnType<typeof useTranslations<"profile.usage">>;
  fmtInt: (value: number | undefined | null) => string;
  formatTs: (record: UsageRecord) => string;
}) {
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
          <span className="block truncate text-sm font-semibold">
            {recordModel(record, t("unknownModel"))}
          </span>
          <span className="text-muted mt-0.5 block text-xs md:hidden">{formatTs(record)}</span>
        </span>
      </span>
      <span className="grid grid-cols-3 gap-3 text-right text-xs tabular-nums md:contents">
        <span role="cell">
          <span className="text-muted block md:hidden">{t("prompt")}</span>
          {fmtInt(record.prompt_tokens)}
        </span>
        <span role="cell">
          <span className="text-muted block md:hidden">{t("output")}</span>
          {fmtInt(record.completion_tokens)}
        </span>
        <span className="font-medium" role="cell">
          <span className="text-muted block font-normal md:hidden">{t("total")}</span>
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
  t,
}: {
  end: number;
  page: number;
  pageCount: number;
  start: number;
  total: number;
  onPageChange: (page: number) => void;
  t: ReturnType<typeof useTranslations<"profile.usage">>;
}) {
  return (
    <nav
      aria-label={t("pagination")}
      className="border-border bg-surface-secondary/30 flex min-h-14 flex-wrap items-center justify-between gap-3 border-t px-4 py-2.5"
    >
      <span className="text-muted text-xs tabular-nums">
        {t("showing", { start: start + 1, end, total })}
      </span>
      <span className="flex items-center gap-2">
        <Button
          isIconOnly
          aria-label={t("previousPage")}
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
          aria-label={t("nextPage")}
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

function QuotaTile({
  scope,
  t,
  fmtInt,
}: {
  scope: QuotaScope;
  t: ReturnType<typeof useTranslations<"profile.usage">>;
  fmtInt: (value: number | undefined | null) => string;
}) {
  const scopeName =
    scope.scope === "user" ? t("personal") : scope.scope === "tenant" ? t("team") : scope.scope;
  const unlimited = scope.limit <= 0;
  const value = unlimited
    ? 0
    : Math.min(100, Math.round((scope.used / Math.max(1, scope.limit)) * 100));
  return (
    <div className="bg-surface-secondary rounded-2xl p-4">
      <div className="flex items-start justify-between gap-3">
        <span>
          <span className="text-sm font-semibold">{scopeName}</span>
          <span className="text-muted mt-0.5 block text-xs">
            {t("allowance", { period: scope.period })}
          </span>
        </span>
        <span className="text-sm font-semibold tabular-nums">
          {unlimited ? t("unlimited") : t("left", { count: fmtInt(scope.remaining) })}
        </span>
      </div>
      {!unlimited ? (
        <ProgressBar
          aria-label={t("quotaUsed", { scope: scopeName })}
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
          ? t("limitReached")
          : unlimited
            ? t("noLimit")
            : t("used", { used: fmtInt(scope.used), limit: fmtInt(scope.limit) })}
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
