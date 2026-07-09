"use client";

import { Button } from "@/components/ui/button";
import { Gauge, LoaderCircle, Receipt, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useState } from "react";

// Self-service usage + remaining quota. Reads are keyed to the caller's own
// runtime token via the /api/me/* BFF routes, which proxy llm-gateway
// /v1/quota + /v1/usage. A user can only ever see their own numbers.

type QuotaScope = {
  scope: string;
  subject: string;
  period: string;
  used: number;
  limit: number;
  remaining: number;
  exceeded: boolean;
};

type QuotaResponse = {
  user_id: string;
  tenant_id: string;
  scopes: QuotaScope[];
};

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
  // Backend serializes UsageRecord via asdict(): timestamp is `ts_unix`
  // (epoch seconds), model comes as `alias` / `real_model`, and there is no
  // `total_tokens` field (it is a computed @property). Older shapes kept as
  // optional fallbacks.
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

function fmtInt(n: number | undefined | null): string {
  if (n === undefined || n === null || Number.isNaN(n)) return "-";
  return nf.format(n);
}

function scopeLabel(scope: string): string {
  if (scope === "user") return "Personal (daily)";
  if (scope === "tenant") return "Team (monthly)";
  return scope;
}

function formatTs(rec: UsageRecord): string {
  // Prefer epoch seconds (ts_unix); fall back to string timestamps.
  if (typeof rec.ts_unix === "number" && !Number.isNaN(rec.ts_unix)) {
    const d = new Date(rec.ts_unix * 1000);
    if (!Number.isNaN(d.getTime())) return d.toLocaleString();
  }
  const raw = rec.ts ?? rec.created_at;
  if (!raw) return "-";
  const d = new Date(raw);
  if (Number.isNaN(d.getTime())) return raw;
  return d.toLocaleString();
}

function recordModel(rec: UsageRecord): string {
  return rec.alias || rec.model || rec.real_model || "-";
}

function recordTotal(rec: UsageRecord): number | undefined {
  if (typeof rec.total_tokens === "number") return rec.total_tokens;
  const p = rec.prompt_tokens;
  const c = rec.completion_tokens;
  if (typeof p === "number" || typeof c === "number") {
    return (p ?? 0) + (c ?? 0);
  }
  return undefined;
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
    // Local fetches resolve almost instantly, so the spinner would only flash
    // for a frame. Hold the "refreshing" state for at least one full rotation
    // (600ms) so the refresh gesture reads as a deliberate, complete animation.
    const startedAt = Date.now();
    const MIN_SPIN_MS = 600;
    try {
      const [qRes, uRes] = await Promise.all([
        fetch("/api/me/quota", { cache: "no-store" }),
        fetch("/api/me/usage?limit=20", { cache: "no-store" }),
      ]);
      if (!qRes.ok) {
        const t = await qRes.text();
        throw new Error(`quota ${qRes.status}: ${t}`);
      }
      if (!uRes.ok) {
        const t = await uRes.text();
        throw new Error(`usage ${uRes.status}: ${t}`);
      }
      setQuota((await qRes.json()) as QuotaResponse);
      setUsage((await uRes.json()) as UsageResponse);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      if (!showLoading) {
        const elapsed = Date.now() - startedAt;
        if (elapsed < MIN_SPIN_MS) {
          await new Promise((r) => setTimeout(r, MIN_SPIN_MS - elapsed));
        }
      }
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const agg = usage?.user_aggregate;

  return (
    <div className="space-y-5">
      {error && (
        <div className="rounded-md border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive">
          {error}
        </div>
      )}

      <section className="rounded-lg border border-border bg-card">
        <div className="flex items-center gap-3 border-b border-border px-4 py-3">
          <div className="grid size-8 place-items-center rounded-md bg-muted">
            <Gauge className="size-4 text-muted-foreground" />
          </div>
          <div className="min-w-0 flex-1">
            <h2 className="text-sm font-semibold">Remaining Quota</h2>
            <p className="truncate text-xs text-muted-foreground">
              Token allowance across your active scopes
            </p>
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => void load(false)}
            disabled={loading || refreshing}
          >
            <RefreshCw className={refreshing ? "mr-2 size-4 animate-spin" : "mr-2 size-4"} />
            Refresh
          </Button>
        </div>
        {loading ? (
          <div className="flex items-center gap-2 px-4 py-8 text-sm text-muted-foreground">
            <LoaderCircle className="size-4 animate-spin" />
            Loading usage…
          </div>
        ) : (
          <div className="grid gap-3 p-4 sm:grid-cols-2">
            {(quota?.scopes?.length ?? 0) === 0 && (
              <div className="text-sm text-muted-foreground">
                No quota policy applies — usage is unlimited.
              </div>
            )}
            {quota?.scopes?.map((s) => (
              <QuotaTile key={`${s.scope}:${s.subject}:${s.period}`} scope={s} />
            ))}
          </div>
        )}
      </section>

      {!loading && (
        <>
          <section className="rounded-lg border border-border bg-card">
            <div className="flex items-center gap-3 border-b border-border px-4 py-3">
              <div className="grid size-8 place-items-center rounded-md bg-muted">
                <Receipt className="size-4 text-muted-foreground" />
              </div>
              <h2 className="text-sm font-semibold">Lifetime Totals</h2>
            </div>
            <div className="grid gap-3 p-4 sm:grid-cols-2 lg:grid-cols-4">
              <StatTile label="Calls" value={fmtInt(agg?.calls)} />
              <StatTile label="Total tokens" value={fmtInt(agg?.total_tokens)} />
              <StatTile label="Prompt tokens" value={fmtInt(agg?.prompt_tokens)} />
              <StatTile label="Completion tokens" value={fmtInt(agg?.completion_tokens)} />
            </div>
          </section>

          <section className="rounded-lg border border-border bg-card">
            <div className="flex items-center gap-3 border-b border-border px-4 py-3">
              <div className="grid size-8 place-items-center rounded-md bg-muted">
                <Receipt className="size-4 text-muted-foreground" />
              </div>
              <h2 className="text-sm font-semibold">Recent Activity</h2>
            </div>
            {(usage?.recent?.length ?? 0) === 0 ? (
              <div className="px-4 py-8 text-sm text-muted-foreground">No usage recorded yet.</div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border text-left text-xs text-muted-foreground">
                      <th className="px-4 py-2 font-medium">Time</th>
                      <th className="px-4 py-2 font-medium">Model</th>
                      <th className="px-4 py-2 text-right font-medium">Prompt</th>
                      <th className="px-4 py-2 text-right font-medium">Completion</th>
                      <th className="px-4 py-2 text-right font-medium">Total</th>
                    </tr>
                  </thead>
                  <tbody>
                    {usage?.recent?.map((r, i) => (
                      <tr
                        key={r.request_id ?? r.id ?? i}
                        className="border-b border-border/60 last:border-0"
                      >
                        <td className="whitespace-nowrap px-4 py-2 text-muted-foreground">
                          {formatTs(r)}
                        </td>
                        <td className="px-4 py-2">{recordModel(r)}</td>
                        <td className="px-4 py-2 text-right tabular-nums">
                          {fmtInt(r.prompt_tokens)}
                        </td>
                        <td className="px-4 py-2 text-right tabular-nums">
                          {fmtInt(r.completion_tokens)}
                        </td>
                        <td className="px-4 py-2 text-right font-medium tabular-nums">
                          {fmtInt(recordTotal(r))}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        </>
      )}
    </div>
  );
}

function QuotaTile({ scope }: { scope: QuotaScope }) {
  const unlimited = scope.limit <= 0;
  const pct = unlimited
    ? 0
    : Math.min(100, Math.round((scope.used / Math.max(1, scope.limit)) * 100));
  const barColor = scope.exceeded
    ? "bg-destructive"
    : pct >= 80
      ? "bg-amber-500"
      : "bg-emerald-500";
  return (
    <div className="rounded-md border border-border bg-background p-3">
      <div className="flex items-center justify-between">
        <div className="text-sm font-medium">{scopeLabel(scope.scope)}</div>
        <div className="text-xs text-muted-foreground">{scope.period}</div>
      </div>
      <div className="mt-2 flex items-baseline gap-1">
        <span className="text-lg font-semibold tabular-nums">{fmtInt(scope.used)}</span>
        <span className="text-xs text-muted-foreground">
          / {unlimited ? "∞" : fmtInt(scope.limit)} tokens
        </span>
      </div>
      {!unlimited && (
        <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-muted">
          <div className={`h-full ${barColor}`} style={{ width: `${pct}%` }} />
        </div>
      )}
      <div className="mt-2 text-xs">
        {unlimited ? (
          <span className="text-muted-foreground">Unlimited</span>
        ) : scope.exceeded ? (
          <span className="font-medium text-destructive">Limit reached</span>
        ) : (
          <span className="text-muted-foreground">
            {fmtInt(scope.remaining)} tokens remaining
          </span>
        )}
      </div>
    </div>
  );
}

function StatTile({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-border bg-background px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-sm font-medium tabular-nums">{value}</div>
    </div>
  );
}
