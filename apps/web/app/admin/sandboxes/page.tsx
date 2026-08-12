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
import { useFormatter, useTranslations } from "next-intl";

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
  const t = useTranslations("admin.sandboxesPage");
  const format = useFormatter();
  const [sandboxes, setSandboxes] = useState<SandboxRuntime[]>([]);
  const [loading, setLoading] = useState(true);
  const [unsupported, setUnsupported] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [deletingId, setDeletingId] = useState("");
  const [pendingDeleteId, setPendingDeleteId] = useState("");
  const [detailSandbox, setDetailSandbox] = useState<SandboxRuntime | null>(null);

  const refresh = useCallback(
    async (notify = false) => {
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
        if (notify) setNotice(t("refreshed"));
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setLoading(false);
      }
    },
    [t],
  );

  const handleDelete = useCallback(
    async (sandboxID: string) => {
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
        setNotice(t("deleted", { id: sandboxID }));
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setDeletingId("");
      }
    },
    [t],
  );

  const formatDate = (value?: string) => {
    if (!value) return "—";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) || date.getTime() <= 0
      ? "—"
      : format.dateTime(date, {
          year: "numeric",
          month: "2-digit",
          day: "2-digit",
          hour: "2-digit",
          minute: "2-digit",
        });
  };

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const columns: DataGridColumn<SandboxRuntime>[] = [
    {
      id: "sandbox",
      header: t("columns.sandbox"),
      isRowHeader: true,
      width: 220,
      cell: (sandbox) => (
        <AdminTruncatedValue
          className="font-mono text-xs font-medium"
          copyLabel={t("copy.sandboxId")}
          value={sandbox.sandbox_id}
        />
      ),
    },
    {
      id: "status",
      header: t("columns.status"),
      width: 135,
      cell: (sandbox) => (
        <AdminStatusBadge tone={STATUS_TONES[sandbox.status] ?? "neutral"} dot>
          {t.has(`status.${sandbox.status}` as never)
            ? t(`status.${sandbox.status}` as never)
            : sandbox.status || t("status.unknown")}
        </AdminStatusBadge>
      ),
    },
    {
      id: "owner",
      header: t("columns.owner"),
      width: 190,
      cell: (sandbox) => (
        <AdminTruncatedValue
          className="text-sm"
          copyLabel={sandbox.username ? t("copy.username") : t("copy.userId")}
          value={sandbox.username || sandbox.user_id || "—"}
        />
      ),
    },
    {
      id: "created",
      header: t("columns.created"),
      width: 145,
      cell: (sandbox) => (
        <span className="text-muted text-xs tabular-nums">{formatDate(sandbox.created_at)}</span>
      ),
    },
    {
      id: "node",
      header: t("columns.node"),
      width: 190,
      cell: (sandbox) => (
        <AdminTruncatedValue
          className="text-xs"
          copyLabel={t("copy.nodeName")}
          value={sandbox.node_name || t("unassigned")}
        />
      ),
    },
    {
      id: "actions",
      header: t("columns.actions"),
      align: "center",
      width: 72,
      cell: (sandbox) => (
        <AdminRowActions
          label={t("actions.forSandbox", { id: sandbox.sandbox_id })}
          busy={deletingId === sandbox.sandbox_id}
          actions={[
            {
              id: "details",
              label: t("actions.details"),
              icon: <Eye className="size-4" />,
            },
            {
              id: "delete",
              label:
                sandbox.status === "running" ? t("actions.deleteRunning") : t("actions.delete"),
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
        title={t("title")}
        description={t("description")}
        actions={
          <AdminRefreshButton
            variant="outline"
            refreshing={loading}
            disabled={loading}
            onClick={() => void refresh(true)}
          >
            {t("refresh")}
          </AdminRefreshButton>
        }
      />

      <AdminErrorDialog
        error={error}
        title={t("operationFailed")}
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
          aria-label={t("tableAria")}
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
                  {loading ? t("empty.loading") : t("empty.title")}
                </EmptyState.Title>
                <EmptyState.Description>
                  {loading ? t("empty.loadingDescription") : t("empty.description")}
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
        title={t("confirm.title")}
        description={t("confirm.description", {
          sandbox: pendingDeleteId || t("confirm.thisSandbox"),
        })}
        confirmLabel={t("confirm.action")}
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
  const t = useTranslations("admin.sandboxesPage");
  const format = useFormatter();
  const formatDate = (value?: string) => {
    if (!value) return "—";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) || date.getTime() <= 0
      ? "—"
      : format.dateTime(date, {
          year: "numeric",
          month: "2-digit",
          day: "2-digit",
          hour: "2-digit",
          minute: "2-digit",
        });
  };
  return (
    <Modal isOpen={Boolean(sandbox)} onOpenChange={onOpenChange}>
      <Modal.Backdrop>
        <Modal.Container placement="center" scroll="inside" size="md">
          <Modal.Dialog>
            <Modal.CloseTrigger aria-label={t("details.close")} />
            <Modal.Header className="items-start">
              <Modal.Icon className="bg-teal-500/10 text-teal-600">
                <SandboxesPageIcon className="size-5" />
              </Modal.Icon>
              <div className="min-w-0">
                <Modal.Heading>{t("details.title")}</Modal.Heading>
                <p className="mt-1 text-sm text-muted">{t("details.description")}</p>
              </div>
            </Modal.Header>
            <Modal.Body>
              {sandbox ? (
                <dl className="divide-y divide-border/70 rounded-2xl border border-border/70 px-4">
                  <SandboxDetailValue
                    label={t("details.sandboxId")}
                    value={sandbox.sandbox_id}
                    copyLabel={t("copy.sandboxId")}
                  />
                  <SandboxDetailValue
                    label={t("details.sessionId")}
                    value={sandbox.session_id || "—"}
                    copyLabel={t("copy.sessionId")}
                  />
                  <SandboxDetailValue
                    label={t("details.userId")}
                    value={sandbox.user_id || "—"}
                    copyLabel={t("copy.userId")}
                  />
                  <SandboxDetailValue
                    label={t("details.lifecycle")}
                    value={sandbox.lifecycle_state || t("status.unknown")}
                  />
                  <SandboxDetailValue
                    label={t("details.image")}
                    value={sandbox.image || "—"}
                    copyLabel={t("copy.image")}
                  />
                  <SandboxDetailValue
                    label={t("columns.node")}
                    value={sandbox.node_name || t("unassigned")}
                    copyLabel={t("copy.nodeName")}
                  />
                  <SandboxDetailValue
                    label={t("details.pod")}
                    value={sandbox.pod_name || "—"}
                    copyLabel={t("copy.podName")}
                  />
                  <SandboxDetailValue
                    label={t("details.podPhase")}
                    value={sandbox.pod_phase || "—"}
                  />
                  <SandboxDetailValue
                    label={t("columns.created")}
                    value={formatDate(sandbox.created_at)}
                  />
                  <SandboxDetailValue
                    label={t("details.paused")}
                    value={formatDate(sandbox.paused_at)}
                  />
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
  const t = useTranslations("admin.sandboxesPage");
  return (
    <Card className="p-8">
      <AdminEmptyState
        icon={<Server className="text-teal-500" />}
        title={t("unsupported.title")}
        description={t("unsupported.description")}
      />
    </Card>
  );
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
