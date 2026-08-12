"use client";

import { HardDrive as StoragePageIcon } from "lucide-react";
import { Gauge, HardDrive, LoaderCircle, Trash2 } from "lucide-react";
import { Button, Card } from "@heroui/react";
import { type DataGridColumn } from "@cocola/ui-compat/data-grid";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { signOut } from "next-auth/react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useFormatter, useTranslations } from "next-intl";
import {
  AdminConfirmDialog,
  AdminDataGrid,
  AdminErrorDialog,
  AdminMetric,
  AdminPage,
  AdminPageHeader,
  AdminPagination,
  AdminRefreshButton,
  AdminRowActions,
  AdminStatusBadge,
  AdminToast,
  AdminTruncatedValue,
} from "@/components/admin/admin-ui";
import { cn } from "@/lib/utils";

type NodeFilesystem = {
  node_name: string;
  available: boolean;
  total_bytes: number;
  used_bytes: number;
  available_bytes: number;
  measured_at?: string;
  error?: string;
};

type SessionVolume = {
  storage_id: string;
  session_id: string;
  user_id: string;
  pvc_name: string;
  pvc_phase: string;
  node_name: string;
  requested_bytes: number;
  conversation_exists: boolean;
  delete_allowed: boolean;
  measurement?: StorageMeasurement;
};

type StorageMeasurement = {
  storage_id: string;
  pvc_name: string;
  node_name: string;
  allocated_bytes: number;
  file_count: number;
  directory_count: number;
  measured_at: string;
};

const SESSION_STORAGE_PAGE_SIZE = 25;

export default function StoragePage() {
  const t = useTranslations("admin.storagePage");
  const format = useFormatter();
  const [nodes, setNodes] = useState<NodeFilesystem[]>([]);
  const [volumes, setVolumes] = useState<SessionVolume[]>([]);
  const [volumePage, setVolumePage] = useState(0);
  const [volumeTotal, setVolumeTotal] = useState(0);
  const [orphanCount, setOrphanCount] = useState(0);
  const [loading, setLoading] = useState(true);
  const [measuringKeys, setMeasuringKeys] = useState<Set<string>>(() => new Set());
  const [bulkMeasuring, setBulkMeasuring] = useState(false);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<SessionVolume | null>(null);
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false);
  const [bulkDeleting, setBulkDeleting] = useState(false);
  const [error, setError] = useState("");
  const [toast, setToast] = useState<{
    message: string;
    tone: "loading" | "success";
  } | null>(null);
  const [unsupported, setUnsupported] = useState(false);

  const refresh = useCallback(async () => {
    setError("");
    setLoading(true);
    const volumeQuery = new URLSearchParams({
      limit: String(SESSION_STORAGE_PAGE_SIZE),
      offset: String(volumePage * SESSION_STORAGE_PAGE_SIZE),
    });
    try {
      const [nodesRes, volumesRes] = await Promise.all([
        fetch("/api/admin/storage/nodes", { cache: "no-store" }),
        fetch(`/api/admin/session-storage?${volumeQuery}`, { cache: "no-store" }),
      ]);
      if (isAccountDisabledResponse(nodesRes) || isAccountDisabledResponse(volumesRes)) {
        await signOut({ callbackUrl: "/login?error=account_disabled" });
        return;
      }
      if (await isUnsupportedResponse(nodesRes)) {
        setUnsupported(true);
        setNodes([]);
        setVolumes([]);
        setVolumeTotal(0);
        setOrphanCount(0);
        return;
      }
      if (!nodesRes.ok) throw new Error(await responseError(nodesRes));
      if (!volumesRes.ok) throw new Error(await responseError(volumesRes));
      const nodeBody = (await nodesRes.json()) as { nodes?: NodeFilesystem[] };
      const volumeBody = (await volumesRes.json()) as {
        volumes?: SessionVolume[];
        total?: number;
        orphan_count?: number;
      };
      setUnsupported(false);
      setNodes(Array.isArray(nodeBody.nodes) ? nodeBody.nodes : []);
      setVolumes(Array.isArray(volumeBody.volumes) ? volumeBody.volumes : []);
      setVolumeTotal(typeof volumeBody.total === "number" ? volumeBody.total : 0);
      setOrphanCount(typeof volumeBody.orphan_count === "number" ? volumeBody.orphan_count : 0);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [volumePage]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    const lastPage = Math.max(0, Math.ceil(volumeTotal / SESSION_STORAGE_PAGE_SIZE) - 1);
    if (volumePage > lastPage) setVolumePage(lastPage);
  }, [volumePage, volumeTotal]);

  const totals = useMemo(() => {
    const measured = nodes.filter((node) => node.available);
    return {
      nodeCount: nodes.length,
      measuredCount: measured.length,
      totalBytes: measured.reduce((sum, node) => sum + node.total_bytes, 0),
      availableBytes: measured.reduce((sum, node) => sum + node.available_bytes, 0),
    };
  }, [nodes]);

  const requestMeasurement = async (volume: SessionVolume) => {
    const query = new URLSearchParams({ pvc_name: volume.pvc_name });
    const res = await fetch(
      `/api/admin/session-storage/${encodeURIComponent(volume.storage_id)}/measure?${query}`,
      { method: "POST" },
    );
    if (isAccountDisabledResponse(res)) {
      await signOut({ callbackUrl: "/login?error=account_disabled" });
      throw new Error(t("accountDisabled"));
    }
    if (!res.ok) throw new Error(await responseError(res));
    return (await res.json()) as StorageMeasurement;
  };

  const recordMeasurement = (volume: SessionVolume, result: StorageMeasurement) => {
    const key = volumeKey(volume);
    setVolumes((current) =>
      current.map((item) => (volumeKey(item) === key ? { ...item, measurement: result } : item)),
    );
  };

  const setVolumesMeasuring = (keys: string[], active: boolean) => {
    setMeasuringKeys((current) => {
      const next = new Set(current);
      for (const key of keys) {
        if (active) next.add(key);
        else next.delete(key);
      }
      return next;
    });
  };

  const measureVolume = async (volume: SessionVolume) => {
    if (bulkMeasuring || measuringKeys.size > 0) return;
    const key = volumeKey(volume);
    setError("");
    setVolumesMeasuring([key], true);
    setToast({ message: t("toast.measuring"), tone: "loading" });
    try {
      const result = await requestMeasurement(volume);
      recordMeasurement(volume, result);
      setToast({
        message: t("toast.measured", {
          size: formatBytes(result.allocated_bytes),
          count: result.file_count,
        }),
        tone: "success",
      });
    } catch (err) {
      setToast(null);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setVolumesMeasuring([key], false);
    }
  };

  const measureCurrentPage = async () => {
    if (bulkMeasuring || measuringKeys.size > 0) return;
    const targets = volumes.filter(isAttachedVolume);
    if (targets.length === 0) return;
    setError("");
    setBulkMeasuring(true);
    setToast({ message: t("toast.measuringPage", { count: targets.length }), tone: "loading" });
    let measured = 0;
    let failed = 0;
    try {
      const nodeGroups = groupVolumesByNode(targets);
      for (let index = 0; index < nodeGroups.length; index += 4) {
        const results = await Promise.all(
          nodeGroups.slice(index, index + 4).map(async (group) => {
            let groupMeasured = 0;
            let groupFailed = 0;
            for (const volume of group) {
              const key = volumeKey(volume);
              setVolumesMeasuring([key], true);
              try {
                const result = await requestMeasurement(volume);
                recordMeasurement(volume, result);
                groupMeasured++;
              } catch {
                groupFailed++;
              } finally {
                setVolumesMeasuring([key], false);
              }
            }
            return { measured: groupMeasured, failed: groupFailed };
          }),
        );
        measured += results.reduce((total, result) => total + result.measured, 0);
        failed += results.reduce((total, result) => total + result.failed, 0);
      }
      if (failed > 0) {
        setToast(null);
        setError(t("toast.measureFailed", { measured, failed }));
      } else {
        setToast({ message: t("toast.measuredPage", { count: measured }), tone: "success" });
      }
    } finally {
      setBulkMeasuring(false);
    }
  };

  const deleteOrphanVolume = async () => {
    const volume = pendingDelete;
    if (!volume) return;
    if (!volume.delete_allowed) return;
    const key = volumeKey(volume);
    setError("");
    setToast(null);
    setDeleting(key);
    try {
      const query = new URLSearchParams({ pvc_name: volume.pvc_name });
      const res = await fetch(
        `/api/admin/session-storage/${encodeURIComponent(volume.storage_id)}?${query}`,
        { method: "DELETE" },
      );
      if (isAccountDisabledResponse(res)) {
        await signOut({ callbackUrl: "/login?error=account_disabled" });
        return;
      }
      if (!res.ok) throw new Error(await responseError(res));
      setPendingDelete(null);
      setToast({
        message: isMissingVolume(volume) ? t("toast.bindingRemoved") : t("toast.orphanDeleted"),
        tone: "success",
      });
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setDeleting(null);
    }
  };

  const deleteAllOrphanVolumes = async () => {
    setError("");
    setToast(null);
    setBulkDeleting(true);
    try {
      const res = await fetch("/api/admin/session-storage/orphans", {
        method: "DELETE",
        cache: "no-store",
      });
      if (isAccountDisabledResponse(res)) {
        await signOut({ callbackUrl: "/login?error=account_disabled" });
        return;
      }
      if (!res.ok) throw new Error(await responseError(res));
      const result = (await res.json()) as { deleted?: number; failed?: number };
      const deleted = typeof result.deleted === "number" ? result.deleted : 0;
      const failed = typeof result.failed === "number" ? result.failed : 0;
      setBulkDeleteOpen(false);
      if (failed > 0) {
        setError(t("toast.deleteFailed", { deleted, failed }));
      } else {
        setToast({ message: t("toast.deleted", { count: deleted }), tone: "success" });
      }
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBulkDeleting(false);
    }
  };

  const measurementTime = (value: string) => {
    const date = new Date(value);
    return Number.isNaN(date.getTime())
      ? t("justNow")
      : format.dateTime(date, { hour: "2-digit", minute: "2-digit" });
  };

  const columns: DataGridColumn<SessionVolume>[] = [
    {
      id: "session",
      header: t("columns.sessionUser"),
      isRowHeader: true,
      width: 260,
      cell: (volume) => (
        <span className="block min-w-0">
          <AdminTruncatedValue
            className="max-w-[190px] font-mono text-xs font-medium"
            copyLabel={t("copy.sessionId")}
            value={volume.session_id || t("detached")}
          />
          <AdminTruncatedValue
            className="text-muted max-w-[190px] text-xs"
            copyLabel={t("copy.userId")}
            value={volume.user_id || t("noBinding")}
          />
        </span>
      ),
    },
    {
      id: "node",
      header: t("columns.node"),
      width: 160,
      cell: (volume) => (
        <AdminTruncatedValue
          className="max-w-[110px] font-mono text-xs"
          copyLabel={t("copy.nodeName")}
          value={volume.node_name || "—"}
        />
      ),
    },
    {
      id: "volume",
      header: t("columns.volume"),
      width: 280,
      cell: (volume) => {
        const missing = isMissingVolume(volume);
        return (
          <span className="block min-w-0">
            <AdminTruncatedValue
              className="max-w-[210px] font-mono text-xs"
              copyLabel={t("copy.pvcName")}
              value={volume.pvc_name}
            />
            <span className="mt-1 flex flex-wrap items-center gap-1">
              <AdminStatusBadge
                tone={missing ? "red" : isAttachedVolume(volume) ? "green" : "amber"}
                dot
              >
                {missing ? t("states.missing") : volume.pvc_phase}
              </AdminStatusBadge>
              {volume.delete_allowed && !missing ? (
                <AdminStatusBadge tone="red">{t("states.orphan")}</AdminStatusBadge>
              ) : null}
            </span>
            {missing ? (
              <span className="text-muted mt-1 block text-[11px]">
                {volume.delete_allowed ? t("states.staleBinding") : t("states.freshNextRun")}
              </span>
            ) : null}
          </span>
        );
      },
    },
    {
      id: "requested",
      header: t("columns.requested"),
      minWidth: 130,
      cell: (volume) => (
        <span className="font-mono text-xs tabular-nums">
          {formatBytes(volume.requested_bytes)}
        </span>
      ),
    },
    {
      id: "usage",
      header: t("columns.actualUsage"),
      minWidth: 170,
      cell: (volume) => {
        const key = volumeKey(volume);
        const measurement = volume.measurement;
        return measuringKeys.has(key) ? (
          <span className="text-muted flex items-center gap-2 text-xs">
            <LoaderCircle className="size-3.5 animate-spin" />
            {t("measuring")}
          </span>
        ) : measurement ? (
          <span>
            <span className="block font-mono text-xs font-medium tabular-nums">
              {formatBytes(measurement.allocated_bytes)}
            </span>
            <span className="text-muted block text-xs">
              {t("usageCounts", {
                files: measurement.file_count,
                directories: measurement.directory_count,
              })}
            </span>
            <span className="text-muted mt-0.5 block text-[11px]">
              {t("measuredAt", { time: measurementTime(measurement.measured_at) })}
            </span>
          </span>
        ) : (
          <span className="text-muted">{t("notMeasured")}</span>
        );
      },
    },
    {
      id: "actions",
      header: t("columns.actions"),
      align: "center",
      width: 72,
      cell: (volume) => {
        const key = volumeKey(volume);
        const attached = isAttachedVolume(volume);
        const missing = isMissingVolume(volume);
        const actions = [
          {
            id: "measure",
            label: attached
              ? volume.measurement
                ? t("actions.measureAgain")
                : t("actions.measure")
              : t("actions.measureUnavailable"),
            icon: <Gauge className="size-4" />,
            disabled: !attached || bulkMeasuring || bulkDeleting || measuringKeys.size > 0,
          },
          ...(volume.delete_allowed
            ? [
                {
                  id: "delete",
                  label: missing ? t("actions.cleanBinding") : t("actions.deleteOrphanVolume"),
                  icon: <Trash2 className="size-4" />,
                  destructive: true,
                  disabled:
                    bulkMeasuring || bulkDeleting || measuringKeys.size > 0 || Boolean(deleting),
                },
              ]
            : []),
        ];
        return (
          <AdminRowActions
            label={t("actions.forVolume", { volume: volume.pvc_name })}
            busy={measuringKeys.has(key) || deleting === key}
            actions={actions}
            onAction={(action) => {
              if (action === "measure") void measureVolume(volume);
              if (action === "delete") setPendingDelete(volume);
            }}
          />
        );
      },
    },
  ];

  return (
    <AdminPage className="admin-theme-purple">
      <AdminPageHeader
        title={t("title")}
        description={t("description")}
        icon={<StoragePageIcon className="size-5" />}
        actions={
          <AdminRefreshButton
            variant="outline"
            onClick={() => void refresh()}
            disabled={loading}
            refreshing={loading}
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
        message={toast?.message}
        tone={toast?.tone ?? "success"}
        onDismiss={() => setToast(null)}
      />

      {!unsupported && nodes.length > 0 ? (
        <section className="grid gap-3 sm:grid-cols-2">
          <AdminMetric
            label={t("metrics.probes")}
            value={`${totals.measuredCount}/${totals.nodeCount}`}
            detail={t("metrics.nodesReporting")}
            tone={totals.measuredCount === totals.nodeCount ? "green" : "amber"}
          />
          <AdminMetric
            label={t("metrics.available")}
            value={formatBytes(totals.availableBytes)}
            detail={t("metrics.ofTotal", { total: formatBytes(totals.totalBytes) })}
            tone="violet"
          />
        </section>
      ) : null}

      {unsupported ? (
        <Card className="p-8">
          <EmptyState>
            <EmptyState.Header>
              <EmptyState.Media variant="icon">
                <HardDrive className="text-purple-500" />
              </EmptyState.Media>
              <EmptyState.Title>{t("unsupported.title")}</EmptyState.Title>
              <EmptyState.Description>{t("unsupported.description")}</EmptyState.Description>
            </EmptyState.Header>
          </EmptyState>
        </Card>
      ) : (
        <>
          <section className="space-y-3">
            <div>
              <h2 className="text-sm font-semibold">{t("nodes.title")}</h2>
              <p className="mt-0.5 text-xs text-muted">{t("nodes.description")}</p>
            </div>
            {loading && nodes.length === 0 ? (
              <Card className="p-6">
                <EmptyState>
                  <EmptyState.Header>
                    <EmptyState.Media variant="icon">
                      <LoaderCircle className="animate-spin text-purple-500" />
                    </EmptyState.Media>
                    <EmptyState.Title>{t("nodes.loading")}</EmptyState.Title>
                    <EmptyState.Description>{t("nodes.loadingDescription")}</EmptyState.Description>
                  </EmptyState.Header>
                </EmptyState>
              </Card>
            ) : nodes.length === 0 ? (
              <Card className="p-6">
                <EmptyState>
                  <EmptyState.Header>
                    <EmptyState.Media variant="icon">
                      <HardDrive className="text-purple-500" />
                    </EmptyState.Media>
                    <EmptyState.Title>{t("nodes.empty")}</EmptyState.Title>
                    <EmptyState.Description>{t("nodes.emptyDescription")}</EmptyState.Description>
                  </EmptyState.Header>
                </EmptyState>
              </Card>
            ) : (
              <div className="grid gap-4 lg:grid-cols-2">
                {nodes.map((node) => (
                  <NodeStorageCard key={node.node_name} node={node} />
                ))}
              </div>
            )}
          </section>

          <section className="space-y-3">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
              <div>
                <h2 className="text-sm font-semibold">{t("sessions.title")}</h2>
                <p className="mt-0.5 text-xs text-muted">{t("sessions.description")}</p>
              </div>
              <div className="flex shrink-0 flex-wrap items-center gap-2">
                <Button
                  isDisabled={
                    loading ||
                    bulkMeasuring ||
                    bulkDeleting ||
                    Boolean(deleting) ||
                    measuringKeys.size > 0 ||
                    volumes.every((volume) => !isAttachedVolume(volume))
                  }
                  size="sm"
                  variant="outline"
                  onPress={() => void measureCurrentPage()}
                >
                  {bulkMeasuring ? (
                    <LoaderCircle className="size-3.5 animate-spin" />
                  ) : (
                    <Gauge className="size-3.5" />
                  )}
                  {t("actions.measurePage")}
                </Button>
                {orphanCount > 0 ? (
                  <Button
                    isDisabled={
                      loading ||
                      bulkMeasuring ||
                      bulkDeleting ||
                      measuringKeys.size > 0 ||
                      Boolean(deleting)
                    }
                    size="sm"
                    variant="danger-soft"
                    onPress={() => setBulkDeleteOpen(true)}
                  >
                    <Trash2 className="size-3.5" />
                    {t("actions.deleteOrphans", { count: orphanCount })}
                  </Button>
                ) : null}
              </div>
            </div>

            <div className="min-w-0">
              <AdminDataGrid
                aria-label={t("sessions.tableAria")}
                columns={columns}
                contentClassName="min-w-[940px]"
                data={volumes}
                getRowId={volumeKey}
                selectionMode="none"
                variant="primary"
                renderEmptyState={() => (
                  <EmptyState>
                    <EmptyState.Header>
                      <EmptyState.Media variant="icon">
                        <HardDrive className="text-purple-500" />
                      </EmptyState.Media>
                      <EmptyState.Title>
                        {loading ? t("sessions.loading") : t("sessions.empty")}
                      </EmptyState.Title>
                      <EmptyState.Description>
                        {t("sessions.emptyDescription")}
                      </EmptyState.Description>
                    </EmptyState.Header>
                  </EmptyState>
                )}
              />
              <AdminPagination
                page={volumePage}
                pageSize={SESSION_STORAGE_PAGE_SIZE}
                count={volumes.length}
                total={volumeTotal}
                loading={loading}
                label={t("sessions.paginationLabel")}
                onPageChange={setVolumePage}
                variant="embedded"
              />
            </div>
          </section>
        </>
      )}
      <AdminConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null);
        }}
        title={
          pendingDelete && isMissingVolume(pendingDelete)
            ? t("confirm.cleanTitle")
            : t("confirm.deleteTitle")
        }
        description={
          pendingDelete
            ? isMissingVolume(pendingDelete)
              ? t("confirm.cleanDescription")
              : t("confirm.deleteDescription", { volume: pendingDelete.pvc_name })
            : ""
        }
        confirmLabel={
          pendingDelete && isMissingVolume(pendingDelete)
            ? t("actions.cleanBinding")
            : t("actions.deleteOrphan")
        }
        busy={deleting !== null}
        destructive
        onConfirm={() => void deleteOrphanVolume()}
      />
      <AdminConfirmDialog
        open={bulkDeleteOpen}
        onOpenChange={setBulkDeleteOpen}
        title={t("confirm.bulkTitle", { count: orphanCount })}
        description={t("confirm.bulkDescription")}
        confirmLabel={t("confirm.bulkAction")}
        busy={bulkDeleting}
        destructive
        onConfirm={() => void deleteAllOrphanVolumes()}
      />
    </AdminPage>
  );
}

function NodeStorageCard({ node }: { node: NodeFilesystem }) {
  const t = useTranslations("admin.storagePage.nodeCard");
  const format = useFormatter();
  if (!node.available) {
    return (
      <Card className="p-5">
        <Card.Content className="p-0">
          <div className="flex items-center justify-between gap-3">
            <div className="font-mono text-sm font-medium">{node.node_name}</div>
            <AdminStatusBadge tone="amber" dot>
              {t("unavailable")}
            </AdminStatusBadge>
          </div>
          <p className="text-muted mt-3 text-xs">{node.error || t("notReporting")}</p>
        </Card.Content>
      </Card>
    );
  }
  const availableRatio = node.total_bytes > 0 ? node.available_bytes / node.total_bytes : 0;
  const occupiedRatio = 1 - availableRatio;
  const tone = capacityTone(node.available_bytes, node.total_bytes);
  return (
    <Card className="cocola-admin-module-card p-5">
      <Card.Content className="p-0">
        <div className="min-w-0">
          <div className="min-w-0">
            <AdminTruncatedValue
              className="font-mono text-sm font-medium"
              copyLabel={t("nodeName")}
              value={node.node_name}
            />
            <div className="text-muted mt-1 text-xs">
              {t("availableOf", {
                available: formatBytes(node.available_bytes),
                total: formatBytes(node.total_bytes),
              })}
            </div>
          </div>
        </div>
        <div className="bg-surface-secondary mt-4 h-2 overflow-hidden rounded-full">
          <div
            className={cn(
              "h-full rounded-full transition-[width]",
              tone === "red" && "bg-danger",
              tone === "amber" && "bg-amber-500",
              tone === "green" && "bg-emerald-500",
            )}
            style={{ width: `${Math.min(Math.max(occupiedRatio * 100, 0), 100)}%` }}
          />
        </div>
        <div className="text-muted mt-2 flex justify-between text-[11px]">
          <span>{t("used", { used: formatBytes(node.used_bytes) })}</span>
          <span
            className={cn(
              "font-medium",
              tone === "red" && "text-danger",
              tone === "amber" && "text-amber-600 dark:text-amber-400",
              tone === "green" && "text-emerald-700 dark:text-emerald-300",
            )}
          >
            {t("availablePercent", {
              percent: format.number(availableRatio, {
                style: "percent",
                maximumFractionDigits: 0,
              }),
            })}
          </span>
        </div>
      </Card.Content>
    </Card>
  );
}

function volumeKey(volume: Pick<SessionVolume, "storage_id" | "pvc_name">) {
  return `${volume.storage_id}:${volume.pvc_name}`;
}

function groupVolumesByNode(volumes: SessionVolume[]) {
  const groups = new Map<string, SessionVolume[]>();
  for (const volume of volumes) {
    const nodeName = volume.node_name || "unknown";
    const group = groups.get(nodeName);
    if (group) group.push(volume);
    else groups.set(nodeName, [volume]);
  }
  return Array.from(groups.values());
}

function isAttachedVolume(volume: Pick<SessionVolume, "pvc_phase">) {
  return volume.pvc_phase === "Bound" || volume.pvc_phase === "Mounted";
}

function isMissingVolume(volume: Pick<SessionVolume, "pvc_phase">) {
  return volume.pvc_phase === "Missing";
}

function capacityTone(available: number, total: number): "green" | "amber" | "red" {
  if (total <= 0) return "amber";
  const ratio = available / total;
  if (ratio < 0.1) return "red";
  if (ratio < 0.2) return "amber";
  return "green";
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  const amount = value / 1024 ** index;
  return `${amount >= 10 || index === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[index]}`;
}

async function responseError(res: Response) {
  const text = await res.text();
  try {
    const body = JSON.parse(text) as {
      error?: { code?: string; message?: string } | string;
      message?: string;
    };
    if (typeof body.error === "string") return body.error;
    return body.error?.message ?? body.message ?? `${res.status} ${res.statusText}`;
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
  return res.status === 403 && res.headers.get("x-cocola-auth-error") === "account_disabled";
}
