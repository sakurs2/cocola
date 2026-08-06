"use client";

import { Layers as SandboxesPageIcon } from "lucide-react";
import {
  AdminConfirmDialog,
  AdminEmptyState,
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
  AdminStatusBadge,
  AdminTruncatedValue,
} from "@/components/admin/admin-ui";
import { Button, Card } from "@heroui/react";
import { DataGrid, type DataGridColumn } from "@cocola/ui-compat/data-grid";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { CircleDot, LoaderCircle, Server, Trash2 } from "lucide-react";
import { signOut } from "next-auth/react";
import { useCallback, useEffect, useState } from "react";

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

  const columns: DataGridColumn<SandboxRuntime>[] = [
    {
      id: "sandbox",
      header: "Sandbox",
      isRowHeader: true,
      minWidth: 250,
      cell: (sandbox) => (
        <span className="block min-w-0">
          <AdminTruncatedValue
            className="font-mono text-xs font-medium"
            copyLabel="sandbox ID"
            value={sandbox.sandbox_id}
          />
          <span className="text-muted block truncate text-xs">
            {sandbox.lifecycle_state || "unknown"}
          </span>
        </span>
      ),
    },
    {
      id: "status",
      header: "Status",
      width: 150,
      cell: (sandbox) => (
        <AdminStatusBadge tone={STATUS_TONES[sandbox.status] ?? "neutral"} dot>
          {STATUS_LABELS[sandbox.status] ?? sandbox.status}
        </AdminStatusBadge>
      ),
    },
    {
      id: "session",
      header: "Session",
      minWidth: 220,
      cell: (sandbox) => (
        <AdminTruncatedValue
          className="font-mono text-xs"
          copyLabel="session ID"
          value={sandbox.session_id || "—"}
        />
      ),
    },
    {
      id: "user",
      header: "User",
      minWidth: 200,
      cell: (sandbox) => (
        <span className="block min-w-0">
          <AdminTruncatedValue
            className="text-sm font-medium"
            copyLabel="username"
            value={sandbox.username || sandbox.user_id || "—"}
          />
          {sandbox.username ? (
            <AdminTruncatedValue
              className="text-muted font-mono text-xs"
              copyLabel="user ID"
              value={sandbox.user_id}
            />
          ) : null}
        </span>
      ),
    },
    {
      id: "runtime",
      header: "Runtime",
      minWidth: 220,
      cell: (sandbox) => (
        <AdminTruncatedValue
          className="font-mono text-xs"
          copyLabel="runtime image"
          value={sandbox.image || "—"}
        />
      ),
    },
    {
      id: "created",
      header: "Created",
      minWidth: 150,
      cell: (sandbox) => (
        <span className="text-muted text-xs tabular-nums">{formatDate(sandbox.created_at)}</span>
      ),
    },
    {
      id: "placement",
      header: "Node / Pod",
      minWidth: 240,
      cell: (sandbox) => (
        <span className="block min-w-0">
          <AdminTruncatedValue
            className="text-xs"
            copyLabel="node name"
            value={sandbox.node_name || "—"}
          />
          <AdminTruncatedValue
            className="text-muted font-mono text-xs"
            copyLabel="pod ID"
            value={`${sandbox.pod_name || "—"}${sandbox.pod_phase ? ` / ${sandbox.pod_phase}` : ""}`}
          />
        </span>
      ),
    },
    {
      id: "actions",
      header: "Actions",
      align: "center",
      pinned: "end",
      width: 100,
      cell: (sandbox) =>
        sandbox.status === "ready" || sandbox.status === "orphan" ? (
          <Button
            isIconOnly
            aria-label={`Delete sandbox ${sandbox.sandbox_id}`}
            isDisabled={Boolean(deletingId)}
            isPending={deletingId === sandbox.sandbox_id}
            size="sm"
            variant="danger-soft"
            onPress={() => setPendingDeleteId(sandbox.sandbox_id)}
          >
            {deletingId === sandbox.sandbox_id ? (
              <LoaderCircle className="size-4 animate-spin" />
            ) : (
              <Trash2 className="size-4" />
            )}
          </Button>
        ) : (
          <span className="text-muted">—</span>
        ),
    },
  ];

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
        <div className="rounded-xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
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
        <DataGrid
          aria-label="Sandboxes"
          columns={columns}
          contentClassName="min-w-[1380px]"
          data={sandboxes}
          getRowId={(sandbox) => sandbox.sandbox_id}
          selectionMode="none"
          variant="primary"
          renderEmptyState={() => (
            <EmptyState>
              <EmptyState.Header>
                <EmptyState.Media variant="icon">
                  <SandboxesPageIcon className="text-teal-500" />
                </EmptyState.Media>
                <EmptyState.Title>
                  {loading ? "Loading sandboxes" : "No sandboxes found"}
                </EmptyState.Title>
                <EmptyState.Description>
                  {loading
                    ? "Fetching sandbox runtime state…"
                    : "Session-bound sandboxes will appear here once they are provisioned."}
                </EmptyState.Description>
              </EmptyState.Header>
            </EmptyState>
          )}
        />
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

function UnsupportedState() {
  return (
    <Card className="p-8">
      <AdminEmptyState
        icon={<Server className="text-teal-500" />}
        title="Sandbox runtime monitoring is not configured"
        description="Start admin-api with shared Redis access to read sandbox-manager binding metadata."
      />
    </Card>
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
