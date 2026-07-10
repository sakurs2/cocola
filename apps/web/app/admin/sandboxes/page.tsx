"use client";

import { Stack as SandboxesPageIcon } from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { CheckCircle2, Clock3, LoaderCircle, RefreshCw, Server, Trash2 } from "lucide-react";
import { signOut } from "next-auth/react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

type SandboxRuntime = {
  sandbox_id: string;
  session_id: string;
  user_id: string;
  username?: string;
  status:
    | "running"
    | "ready"
    | "starting"
    | "pending_reclaim"
    | "reclaiming"
    | "stale_metadata"
    | "stopped"
    | "orphan"
    | "unknown"
    | string;
  lifecycle_state: string;
  image?: string;
  created_at?: string;
  paused_at?: string;
  pod_name?: string;
  pod_phase?: string;
  node_name?: string;
};

type SandboxListResponse = { sandboxes: SandboxRuntime[] };

const STATUS_LABELS: Record<string, string> = {
  running: "Running",
  ready: "Ready",
  starting: "Starting",
  pending_reclaim: "Pending reclaim",
  reclaiming: "Reclaiming",
  stale_metadata: "Stale metadata",
  stopped: "Stopped",
  orphan: "Orphan",
  unknown: "Unknown",
};

export default function SandboxesPage() {
  const [sandboxes, setSandboxes] = useState<SandboxRuntime[]>([]);
  const [loading, setLoading] = useState(true);
  const [unsupported, setUnsupported] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [deletingId, setDeletingId] = useState("");

  const refresh = useCallback(async () => {
    setError("");
    setLoading(true);
    try {
      const res = await fetch("/api/admin/sandboxes", { cache: "no-store" });
      if (isAccountDisabledResponse(res)) return redirectAccountDisabled();
      if (await isUnsupportedResponse(res)) {
        setUnsupported(true);
        setSandboxes([]);
        return;
      }
      if (!res.ok) throw new Error(await responseError(res));
      const body = (await res.json()) as SandboxListResponse;
      setUnsupported(false);
      setSandboxes(Array.isArray(body.sandboxes) ? body.sandboxes : []);
      setNotice("Sandbox runtime state refreshed");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  const handleDelete = useCallback(async (sandboxID: string) => {
    if (!sandboxID) return;
    if (!window.confirm(`Delete sandbox ${sandboxID}? This removes the pod and its metadata.`)) {
      return;
    }
    setError("");
    setDeletingId(sandboxID);
    try {
      const res = await fetch(`/api/admin/sandboxes/${encodeURIComponent(sandboxID)}`, {
        method: "DELETE",
        cache: "no-store",
      });
      if (isAccountDisabledResponse(res)) return redirectAccountDisabled();
      if (!res.ok && res.status !== 204) throw new Error(await responseError(res));
      setSandboxes((prev) => prev.filter((s) => s.sandbox_id !== sandboxID));
      setNotice(`Sandbox ${sandboxID} deleted`);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setDeletingId("");
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const totals = useMemo(
    () => ({
      total: sandboxes.length,
      running: sandboxes.filter((s) => s.status === "running").length,
      ready: sandboxes.filter((s) => s.status === "ready").length,
      orphan: sandboxes.filter((s) => s.status === "orphan").length,
      reclaiming: sandboxes.filter((s) => ["pending_reclaim", "reclaiming"].includes(s.status))
        .length,
    }),
    [sandboxes],
  );

  return (
    <main className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border">
        <div className="mx-auto flex h-16 max-w-7xl items-center gap-3 px-6">
          <div className="admin-page-icon">
            <SandboxesPageIcon className="size-[18px]" weight="duotone" />
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-base font-semibold">Sandbox Runtime</h1>
            <p className="truncate text-xs text-muted-foreground">
              Runtime state for session-bound sandboxes
            </p>
          </div>
          <Button variant="outline" size="sm" onClick={() => void refresh()} disabled={loading}>
            {loading ? (
              <LoaderCircle className="mr-2 size-4 animate-spin" />
            ) : (
              <RefreshCw className="mr-2 size-4" />
            )}
            Refresh
          </Button>
        </div>
      </header>

      <div className="mx-auto max-w-7xl space-y-6 px-6 py-6">
        {error && (
          <div className="rounded-md border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
            {error}
          </div>
        )}
        {notice && !loading && !error && !unsupported && (
          <div className="flex items-center gap-2 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-4 py-3 text-sm text-emerald-700 dark:text-emerald-300">
            <CheckCircle2 className="size-4 shrink-0" />
            <span>{notice}</span>
          </div>
        )}

        {unsupported ? (
          <UnsupportedState />
        ) : (
          <>
            <section className="grid gap-3 md:grid-cols-5">
              <Metric label="Sandboxes" value={String(totals.total)} />
              <Metric label="Running" value={String(totals.running)} />
              <Metric label="Ready (warm)" value={String(totals.ready)} />
              <Metric label="Orphan" value={String(totals.orphan)} />
              <Metric label="To Reclaim" value={String(totals.reclaiming)} />
            </section>

            <section className="overflow-hidden rounded-lg border border-border bg-card">
              <div className="overflow-x-auto">
                <table className="w-full min-w-[1280px] text-sm">
                  <thead className="border-b border-border bg-muted/50 text-xs text-muted-foreground">
                    <tr>
                      <th className="px-4 py-3 text-left font-medium">Sandbox ID</th>
                      <th className="px-4 py-3 text-left font-medium">Status</th>
                      <th className="px-4 py-3 text-left font-medium">Session ID</th>
                      <th className="px-4 py-3 text-left font-medium">User</th>
                      <th className="px-4 py-3 text-left font-medium">Runtime</th>
                      <th className="px-4 py-3 text-left font-medium">Created</th>
                      <th className="px-4 py-3 text-left font-medium">Node / Pod ID</th>
                      <th className="px-4 py-3 text-right font-medium">Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {loading && sandboxes.length === 0 ? (
                      <tr>
                        <td colSpan={8} className="px-4 py-10 text-center text-muted-foreground">
                          Loading sandboxes...
                        </td>
                      </tr>
                    ) : sandboxes.length === 0 ? (
                      <tr>
                        <td colSpan={8} className="px-4 py-10 text-center text-muted-foreground">
                          No sandboxes found
                        </td>
                      </tr>
                    ) : (
                      sandboxes.map((sandbox) => (
                        <tr
                          key={sandbox.sandbox_id}
                          className="border-b border-border/70 last:border-0"
                        >
                          <td className="px-4 py-3">
                            <TruncatedValue
                              value={sandbox.sandbox_id}
                              className="max-w-[210px] font-mono text-xs"
                            />
                            <TruncatedValue
                              value={sandbox.lifecycle_state || "unknown"}
                              className="mt-1 max-w-[210px] text-xs text-muted-foreground"
                            />
                          </td>
                          <td className="px-4 py-3">
                            <StatusPill status={sandbox.status} />
                          </td>
                          <td className="px-4 py-3">
                            <TruncatedValue
                              value={sandbox.session_id || "-"}
                              tooltip={sandbox.session_id}
                              className="max-w-[190px] font-mono text-xs"
                            />
                          </td>
                          <td className="px-4 py-3">
                            <TruncatedValue
                              value={sandbox.username || sandbox.user_id || "-"}
                              tooltip={userTitle(sandbox)}
                              className="max-w-[190px]"
                            />
                            {sandbox.username && (
                              <TruncatedValue
                                value={sandbox.user_id}
                                className="mt-1 max-w-[190px] font-mono text-xs text-muted-foreground"
                              />
                            )}
                          </td>
                          <td className="px-4 py-3">
                            <TruncatedValue
                              value={sandbox.image || "-"}
                              tooltip={sandbox.image}
                              className="max-w-[210px] font-mono text-xs"
                            />
                          </td>
                          <td className="px-4 py-3 text-xs text-muted-foreground">
                            {formatDate(sandbox.created_at)}
                          </td>
                          <td className="px-4 py-3">
                            <TruncatedValue
                              value={sandbox.node_name || "-"}
                              tooltip={sandbox.node_name}
                              className="max-w-[210px]"
                            />
                            <TruncatedValue
                              value={`${sandbox.pod_name || "-"}${
                                sandbox.pod_phase ? ` / ${sandbox.pod_phase}` : ""
                              }`}
                              tooltip={podTitle(sandbox)}
                              className="mt-1 max-w-[210px] font-mono text-xs text-muted-foreground"
                            />
                          </td>
                          <td className="px-4 py-3 text-right">
                            {sandbox.status === "ready" || sandbox.status === "orphan" ? (
                              <Button
                                variant="outline"
                                size="sm"
                                className="text-destructive hover:text-destructive"
                                disabled={deletingId === sandbox.sandbox_id}
                                onClick={() => void handleDelete(sandbox.sandbox_id)}
                              >
                                {deletingId === sandbox.sandbox_id ? (
                                  <LoaderCircle className="mr-1 size-3.5 animate-spin" />
                                ) : (
                                  <Trash2 className="mr-1 size-3.5" />
                                )}
                                Delete
                              </Button>
                            ) : (
                              <span className="text-xs text-muted-foreground">-</span>
                            )}
                          </td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>
            </section>
          </>
        )}
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

function TruncatedValue({
  value,
  tooltip,
  className,
}: {
  value: string;
  tooltip?: string;
  className?: string;
}) {
  const ref = useRef<HTMLDivElement | null>(null);
  const hideTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [position, setPosition] = useState<{ left: number; top: number } | null>(null);
  const fullValue = tooltip || value;
  const hasTooltip = Boolean(fullValue && fullValue !== "-");

  const clearHideTimer = useCallback(() => {
    if (hideTimer.current) {
      clearTimeout(hideTimer.current);
      hideTimer.current = null;
    }
  }, []);

  const showTooltip = useCallback(() => {
    if (!hasTooltip || !ref.current) return;
    clearHideTimer();
    const rect = ref.current.getBoundingClientRect();
    const maxWidth = Math.min(520, window.innerWidth - 24);
    setPosition({
      left: Math.min(Math.max(rect.left, 12), Math.max(12, window.innerWidth - maxWidth - 12)),
      top: rect.bottom + 8,
    });
  }, [clearHideTimer, hasTooltip]);

  const scheduleHide = useCallback(() => {
    clearHideTimer();
    hideTimer.current = setTimeout(() => setPosition(null), 140);
  }, [clearHideTimer]);

  useEffect(() => {
    return () => clearHideTimer();
  }, [clearHideTimer]);

  return (
    <>
      <div
        ref={ref}
        tabIndex={hasTooltip ? 0 : undefined}
        className={cn("truncate outline-none", hasTooltip && "cursor-default", className)}
        onFocus={showTooltip}
        onBlur={scheduleHide}
        onMouseEnter={showTooltip}
        onMouseLeave={scheduleHide}
      >
        {value}
      </div>
      {position && hasTooltip ? (
        <div
          className="fixed z-50 select-text break-all rounded-md border border-border bg-popover px-3 py-2 font-mono text-xs leading-relaxed text-popover-foreground shadow-lg"
          style={{
            left: position.left,
            top: position.top,
            maxWidth: "min(520px, calc(100vw - 24px))",
          }}
          onMouseEnter={clearHideTimer}
          onMouseLeave={scheduleHide}
        >
          {fullValue}
        </div>
      ) : null}
    </>
  );
}

function UnsupportedState() {
  return (
    <section className="rounded-lg border border-border bg-card px-4 py-10 text-center">
      <div className="mx-auto grid size-10 place-items-center rounded-md bg-muted">
        <Server className="size-5 text-muted-foreground" />
      </div>
      <h2 className="mt-4 text-sm font-semibold">Sandbox runtime monitoring is not configured.</h2>
      <p className="mx-auto mt-2 max-w-xl text-sm text-muted-foreground">
        Start admin-api with shared Redis access to read sandbox-manager binding metadata.
      </p>
    </section>
  );
}

function StatusPill({ status }: { status: string }) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium",
        status === "running" && "bg-emerald-500/15 text-emerald-400",
        status === "ready" && "bg-teal-500/15 text-teal-400",
        status === "starting" && "bg-sky-500/15 text-sky-400",
        status === "pending_reclaim" && "bg-amber-500/15 text-amber-400",
        status === "reclaiming" && "bg-amber-500/15 text-amber-400",
        status === "stale_metadata" && "bg-muted text-muted-foreground",
        status === "stopped" && "bg-muted text-muted-foreground",
        status === "orphan" && "bg-rose-500/15 text-rose-400",
        status === "unknown" && "bg-muted text-muted-foreground",
      )}
    >
      {status === "pending_reclaim" || status === "reclaiming" ? (
        <Clock3 className="mr-1 size-3" />
      ) : null}
      {STATUS_LABELS[status] ?? status}
    </span>
  );
}

function formatDate(value?: string) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.getTime() <= 0) return "-";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(
    date.getHours(),
  )}:${pad(date.getMinutes())}`;
}

function userTitle(sandbox: SandboxRuntime) {
  if (sandbox.username && sandbox.user_id) return `${sandbox.username} / ${sandbox.user_id}`;
  return sandbox.username || sandbox.user_id || undefined;
}

function podTitle(sandbox: SandboxRuntime) {
  if (sandbox.pod_name && sandbox.pod_phase) return `${sandbox.pod_name} / ${sandbox.pod_phase}`;
  return sandbox.pod_name || sandbox.pod_phase || undefined;
}

async function responseError(res: Response) {
  const text = await res.text();
  try {
    const body = JSON.parse(text) as {
      error?: { code?: string; message?: string };
      message?: string;
      error_description?: string;
    };
    if (body.error?.code === "NOT_CONFIGURED") {
      return "Sandbox runtime monitoring is not configured.";
    }
    return (
      body.error?.message ??
      body.message ??
      body.error_description ??
      `${res.status} ${res.statusText}`
    );
  } catch {
    return text || `${res.status} ${res.statusText}`;
  }
}

async function isUnsupportedResponse(res: Response) {
  if (res.status !== 501) return false;
  try {
    const body = (await res.clone().json()) as { error?: { code?: string } };
    return body.error?.code === "NOT_CONFIGURED";
  } catch {
    return true;
  }
}

function isAccountDisabledResponse(res: Response) {
  return res.headers.get("x-cocola-auth") === "account-disabled";
}

function redirectAccountDisabled() {
  void signOut({ callbackUrl: "/login?reason=account_disabled" });
}
