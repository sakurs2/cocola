"use client";

import { Layers as SandboxesPageIcon } from "lucide-react";
import {
  AdminConfirmDialog,
  AdminDataGrid,
  AdminEmptyState,
  AdminErrorDialog,
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
  AdminRowActions,
  AdminStatusBadge,
  AdminToast,
  AdminTruncatedValue,
} from "@/components/admin/admin-ui";
import { Card, Modal } from "@heroui/react";
import { type DataGridColumn } from "@cocola/ui-compat/data-grid";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { Eye, Server, Trash2 } from "lucide-react";
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
  const [detailSandbox, setDetailSandbox] = useState<SandboxRuntime | null>(null);

  const refresh = useCallback(async (notify = false) => {
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
      if (notify) setNotice("Sandbox runtime state refreshed");
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
      width: 220,
      cell: (sandbox) => (
        <AdminTruncatedValue
          className="font-mono text-xs font-medium"
          copyLabel="sandbox ID"
          value={sandbox.sandbox_id}
        />
      ),
    },
    {
      id: "status",
      header: "Status",
      width: 135,
      cell: (sandbox) => (
        <AdminStatusBadge tone={STATUS_TONES[sandbox.status] ?? "neutral"} dot>
          {STATUS_LABELS[sandbox.status] ?? sandbox.status}
        </AdminStatusBadge>
      ),
    },
    {
      id: "owner",
      header: "Owner",
      width: 190,
      cell: (sandbox) => (
        <AdminTruncatedValue
          className="text-sm"
          copyLabel={sandbox.username ? "username" : "user ID"}
          value={sandbox.username || sandbox.user_id || "—"}
        />
      ),
    },
    {
      id: "created",
      header: "Created",
      width: 145,
      cell: (sandbox) => (
        <span className="text-muted text-xs tabular-nums">{formatDate(sandbox.created_at)}</span>
      ),
    },
    {
      id: "node",
      header: "Node",
      width: 190,
      cell: (sandbox) => (
        <AdminTruncatedValue
          className="text-xs"
          copyLabel="node name"
          value={sandbox.node_name || "Unassigned"}
        />
      ),
    },
    {
      id: "actions",
      header: "Actions",
      align: "center",
      width: 72,
      cell: (sandbox) => (
        <AdminRowActions
          label={`Actions for sandbox ${sandbox.sandbox_id}`}
          busy={deletingId === sandbox.sandbox_id}
          actions={[
            {
              id: "details",
              label: "View details",
              icon: <Eye className="size-4" />,
            },
            {
              id: "delete",
              label: sandbox.status === "running" ? "Delete running sandbox" : "Delete sandbox",
              icon: <Trash2 className="size-4" />,
              destructive: true,
            },
          ]}
          onAction={(action) => {
            if (action === "details") setDetailSandbox(sandbox);
            if (action === "delete") setPendingDeleteId(sandbox.sandbox_id);
          }}
        />
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
            onClick={() => void refresh(true)}
          >
            Refresh
          </AdminRefreshButton>
        }
      />

      <AdminErrorDialog
        error={error}
        title="Sandbox operation failed"
        onDismiss={() => setError("")}
        onRetry={() => void refresh()}
      />
      <AdminToast
        message={!loading && !error && !unsupported ? notice : undefined}
        tone="success"
        onDismiss={() => setNotice("")}
      />

      {unsupported ? (
        <UnsupportedState />
      ) : (
        <AdminDataGrid
          aria-label="Sandboxes"
          columns={columns}
          contentClassName="min-w-[860px]"
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

      <SandboxDetailsDialog
        sandbox={detailSandbox}
        onOpenChange={(open) => {
          if (!open) setDetailSandbox(null);
        }}
      />

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

function SandboxDetailsDialog({
  sandbox,
  onOpenChange,
}: {
  sandbox: SandboxRuntime | null;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Modal isOpen={Boolean(sandbox)} onOpenChange={onOpenChange}>
      <Modal.Backdrop>
        <Modal.Container placement="center" scroll="inside" size="md">
          <Modal.Dialog>
            <Modal.CloseTrigger aria-label="Close sandbox details" />
            <Modal.Header className="items-start">
              <Modal.Icon className="bg-teal-500/10 text-teal-600">
                <SandboxesPageIcon className="size-5" />
              </Modal.Icon>
              <div className="min-w-0">
                <Modal.Heading>Sandbox details</Modal.Heading>
                <p className="mt-1 text-sm text-muted">Runtime diagnostics for this sandbox.</p>
              </div>
            </Modal.Header>
            <Modal.Body>
              {sandbox ? (
                <dl className="divide-y divide-border/70 rounded-2xl border border-border/70 px-4">
                  <SandboxDetailValue
                    label="Sandbox ID"
                    value={sandbox.sandbox_id}
                    copyLabel="sandbox ID"
                  />
                  <SandboxDetailValue
                    label="Session ID"
                    value={sandbox.session_id || "—"}
                    copyLabel="session ID"
                  />
                  <SandboxDetailValue
                    label="User ID"
                    value={sandbox.user_id || "—"}
                    copyLabel="user ID"
                  />
                  <SandboxDetailValue
                    label="Lifecycle"
                    value={sandbox.lifecycle_state || "unknown"}
                  />
                  <SandboxDetailValue
                    label="Runtime image"
                    value={sandbox.image || "—"}
                    copyLabel="runtime image"
                  />
                  <SandboxDetailValue
                    label="Node"
                    value={sandbox.node_name || "Unassigned"}
                    copyLabel="node name"
                  />
                  <SandboxDetailValue
                    label="Pod"
                    value={sandbox.pod_name || "—"}
                    copyLabel="pod name"
                  />
                  <SandboxDetailValue label="Pod phase" value={sandbox.pod_phase || "—"} />
                  <SandboxDetailValue label="Created" value={formatDate(sandbox.created_at)} />
                  <SandboxDetailValue label="Paused" value={formatDate(sandbox.paused_at)} />
                </dl>
              ) : null}
            </Modal.Body>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}

function SandboxDetailValue({
  label,
  value,
  copyLabel,
}: {
  label: string;
  value: string;
  copyLabel?: string;
}) {
  return (
    <div className="grid min-w-0 gap-1 py-3 sm:grid-cols-[110px_minmax(0,1fr)] sm:items-center">
      <dt className="text-xs font-medium text-muted">{label}</dt>
      <dd className="min-w-0 text-sm">
        {copyLabel ? (
          <AdminTruncatedValue className="font-mono text-xs" copyLabel={copyLabel} value={value} />
        ) : (
          <span className="break-all">{value}</span>
        )}
      </dd>
    </div>
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
