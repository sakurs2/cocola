"use client";

import { Layers as SandboxesPageIcon } from "lucide-react";
import {
  AdminConfirmDialog,
  AdminEmptyState,
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
  AdminStatusBadge,
} from "@/components/admin/admin-ui";
import { cn } from "@/lib/utils";
import { AlertTriangle, CircleDot, Flame, LoaderCircle, Play, Server, Trash2 } from "lucide-react";
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
  stale_metadata: "Stale metadata",
  stopped: "Stopped",
  orphan: "Orphan",
  unknown: "Unknown",
};

type BadgeTone = "neutral" | "sky" | "green" | "amber" | "red";

const STATUS_TONES: Record<string, BadgeTone> = {
  running: "green",
  ready: "sky",
  starting: "sky",
  pending_reclaim: "amber",
  stale_metadata: "neutral",
  stopped: "neutral",
  orphan: "red",
  unknown: "neutral",
};

const LIST_COLS = "1.6fr 0.9fr 1.4fr 1fr 0.9fr 1fr 1.4fr 0.8fr";

export default function SandboxesPage() {
  const [sandboxes, setSandboxes] = useState<SandboxRuntime[]>([]);
  const [loading, setLoading] = useState(true);
  const [unsupported, setUnsupported] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [deletingId, setDeletingId] = useState("");
  const [pendingDeleteId, setPendingDeleteId] = useState("");

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
      setPendingDeleteId("");
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
      reclaiming: sandboxes.filter((s) => s.status === "pending_reclaim").length,
    }),
    [sandboxes],
  );

  return (
    <AdminPage className="admin-theme-teal">
      <AdminPageHeader
        icon={<SandboxesPageIcon className="size-5" />}
        title="Sandboxes"
        description="Runtime state for session-bound sandboxes"
        actions={
          <AdminRefreshButton
            variant="outline"
            refreshing={loading}
            disabled={loading}
            onClick={() => void refresh()}
          >
            Refresh
          </AdminRefreshButton>
        }
      />

      {error ? (
        <div className="rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
          {error}
        </div>
      ) : null}
      {notice && !loading && !error && !unsupported ? (
        <div className="flex items-center gap-2 rounded-xl border border-emerald-500/30 bg-emerald-500/10 px-4 py-3 text-sm text-emerald-700">
          <CircleDot className="size-4 shrink-0" />
          <span>{notice}</span>
        </div>
      ) : null}

      {unsupported ? (
        <UnsupportedState />
      ) : (
        <>
          <section className="grid gap-4 md:grid-cols-2 xl:grid-cols-5">
            <Metric label="Sandboxes" value={String(totals.total)} tone="teal" icon={<Server />} />
            <Metric label="Running" value={String(totals.running)} tone="green" icon={<Play />} />
            <Metric
              label="Ready (warm)"
              value={String(totals.ready)}
              tone="sky"
              icon={<CircleDot />}
            />
            <Metric
              label="Orphan"
              value={String(totals.orphan)}
              tone="amber"
              icon={<AlertTriangle />}
            />
            <Metric
              label="To Reclaim"
              value={String(totals.reclaiming)}
              tone="rose"
              icon={<Flame />}
            />
          </section>

          <div className="admin-list">
            {loading && sandboxes.length === 0 ? (
              <div className="admin-list-empty">Loading sandboxes…</div>
            ) : sandboxes.length === 0 ? (
              <AdminEmptyState
                icon={<SandboxesPageIcon className="size-6" />}
                title="No sandboxes found"
                description="Session-bound sandboxes will appear here once they are provisioned."
              />
            ) : (
              <div className="admin-list-scroll">
                <div className="min-w-[1180px]">
                  <div className="admin-list-cols" style={{ gridTemplateColumns: LIST_COLS }}>
                    <div>Sandbox ID</div>
                    <div>Status</div>
                    <div>Session ID</div>
                    <div>User</div>
                    <div>Runtime</div>
                    <div>Created</div>
                    <div>Node / Pod ID</div>
                    <div className="text-right">Actions</div>
                  </div>
                  {sandboxes.map((sandbox) => (
                    <div
                      key={sandbox.sandbox_id}
                      className="admin-list-row"
                      style={{ gridTemplateColumns: LIST_COLS }}
                    >
                      <div className="min-w-0">
                        <TruncatedValue
                          value={sandbox.sandbox_id}
                          className="admin-list-primary admin-list-mono"
                        />
                        <TruncatedValue
                          value={sandbox.lifecycle_state || "unknown"}
                          className="admin-list-sub"
                        />
                      </div>
                      <div className="admin-list-cell">
                        <AdminStatusBadge tone={STATUS_TONES[sandbox.status] ?? "neutral"} dot>
                          {STATUS_LABELS[sandbox.status] ?? sandbox.status}
                        </AdminStatusBadge>
                      </div>
                      <div className="admin-list-cell">
                        <TruncatedValue
                          value={sandbox.session_id || "—"}
                          tooltip={sandbox.session_id}
                          className="admin-list-mono"
                        />
                      </div>
                      <div className="admin-list-cell">
                        <TruncatedValue
                          value={sandbox.username || sandbox.user_id || "—"}
                          tooltip={userTitle(sandbox)}
                          className="admin-list-primary"
                        />
                        {sandbox.username ? (
                          <TruncatedValue
                            value={sandbox.user_id}
                            className="admin-list-sub admin-list-mono"
                          />
                        ) : null}
                      </div>
                      <div className="admin-list-cell">
                        <TruncatedValue
                          value={sandbox.image || "—"}
                          tooltip={sandbox.image}
                          className="admin-list-mono"
                        />
                      </div>
                      <div className="admin-list-cell admin-list-muted">
                        {formatDate(sandbox.created_at)}
                      </div>
                      <div className="admin-list-cell">
                        <TruncatedValue
                          value={sandbox.node_name || "—"}
                          tooltip={sandbox.node_name}
                        />
                        <TruncatedValue
                          value={`${sandbox.pod_name || "—"}${
                            sandbox.pod_phase ? ` / ${sandbox.pod_phase}` : ""
                          }`}
                          tooltip={podTitle(sandbox)}
                          className="admin-list-sub admin-list-mono"
                        />
                      </div>
                      <div className="flex justify-end">
                        {sandbox.status === "ready" || sandbox.status === "orphan" ? (
                          <button
                            type="button"
                            className="admin-card-btn admin-card-btn--danger"
                            disabled={Boolean(deletingId)}
                            onClick={() => setPendingDeleteId(sandbox.sandbox_id)}
                          >
                            {deletingId === sandbox.sandbox_id ? (
                              <LoaderCircle className="size-3.5 animate-spin" />
                            ) : (
                              <Trash2 className="size-3.5" />
                            )}
                            Delete
                          </button>
                        ) : (
                          <span className="admin-list-muted">—</span>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </>
      )}

      <AdminConfirmDialog
        open={Boolean(pendingDeleteId)}
        onOpenChange={(open) => {
          if (!open) setPendingDeleteId("");
        }}
        title="Delete sandbox?"
        description={`This removes ${pendingDeleteId || "this sandbox"} and its runtime metadata. This action cannot be undone.`}
        confirmLabel="Delete sandbox"
        busy={Boolean(deletingId)}
        destructive
        onConfirm={() => void handleDelete(pendingDeleteId)}
      />
    </AdminPage>
  );
}

function Metric({
  label,
  value,
  tone,
  icon,
}: {
  label: string;
  value: string;
  tone: string;
  icon: React.ReactNode;
}) {
  return (
    <div className="admin-metric-card" data-tone={tone}>
      <div className="admin-metric-head">
        <span className="admin-metric-glyph">{icon}</span>
        <span className="admin-metric-key">{label}</span>
      </div>
      <div className="admin-metric-val truncate">{value}</div>
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
  const hasTooltip = Boolean(fullValue && fullValue !== "—" && fullValue !== "-");

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
    <section className="admin-surface px-4 py-10 text-center">
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

function formatDate(value?: string) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.getTime() <= 0) return "—";
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
