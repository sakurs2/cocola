"use client";

import { Button, Card, ProgressBar } from "@heroui/react";
import { ListView } from "@cocola/ui-compat/list-view";
import { LoaderCircle, RefreshCw } from "lucide-react";
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
const fmtInt = (value: number | undefined | null) =>
  value == null || Number.isNaN(value) ? "-" : nf.format(value);
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
                <StatTile label="Calls" value={fmtInt(aggregate?.calls)} />
                <StatTile label="Prompt tokens" value={fmtInt(aggregate?.prompt_tokens)} />
                <StatTile label="Completion" value={fmtInt(aggregate?.completion_tokens)} />
                <StatTile label="Total tokens" value={fmtInt(aggregate?.total_tokens)} />
              </div>
            </div>
            <div>
              <h3 className="text-sm font-semibold">Recent activity</h3>
              {(usage?.recent.length ?? 0) === 0 ? (
                <p className="text-muted mt-3 text-sm">No usage recorded yet.</p>
              ) : (
                <ListView
                  aria-label="Recent model activity"
                  className="mt-3"
                  items={usage?.recent ?? []}
                  selectionMode="none"
                  variant="secondary"
                >
                  {(record) => (
                    <ListView.Item
                      id={
                        record.request_id ??
                        record.id ??
                        `${recordModel(record)}-${formatTs(record)}`
                      }
                      textValue={`${recordModel(record)} ${formatTs(record)}`}
                    >
                      <ListView.ItemContent>
                        <span className="bg-surface-secondary flex size-9 shrink-0 items-center justify-center rounded-2xl">
                          <ModelIcon
                            bare
                            className="size-6"
                            icon={{ type: "lobe-icons", slug: modelSlug(record) }}
                          />
                        </span>
                        <span className="flex min-w-0 flex-col">
                          <ListView.Title>{recordModel(record)}</ListView.Title>
                          <ListView.Description>{formatTs(record)}</ListView.Description>
                        </span>
                      </ListView.ItemContent>
                      <ListView.ItemAction className="gap-4 text-xs tabular-nums">
                        <span className="text-muted hidden sm:block">
                          {fmtInt(record.prompt_tokens)} prompt
                        </span>
                        <span>{fmtInt(record.completion_tokens)} output</span>
                        <span className="font-medium">{fmtInt(recordTotal(record))} total</span>
                      </ListView.ItemAction>
                    </ListView.Item>
                  )}
                </ListView>
              )}
            </div>
          </>
        )}
      </Card.Content>
    </Card>
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

function StatTile({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-surface-secondary rounded-2xl p-4">
      <span className="text-muted text-xs">{label}</span>
      <span className="mt-1 block text-xl font-semibold tabular-nums">{value}</span>
    </div>
  );
}
