"use client";

import { HardDrive as StoragePageIcon } from "lucide-react";
import { AlertTriangle, Gauge, HardDrive, LoaderCircle, Trash2 } from "lucide-react";
import { Button, Card, Tooltip } from "@heroui/react";
import { DataGrid, type DataGridColumn } from "@cocola/ui-compat/data-grid";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { signOut } from "next-auth/react";
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  AdminAlert,
  AdminConfirmDialog,
  AdminPage,
  AdminPageHeader,
  AdminPagination,
  AdminRefreshButton,
  AdminStatusBadge,
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
  generation: number;
  requested_bytes: number;
  last_reset_reason?: string;
  last_reset_at?: string;
  conversation_exists: boolean;
  delete_allowed: boolean;
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
  const [nodes, setNodes] = useState<NodeFilesystem[]>([]);
  const [volumes, setVolumes] = useState<SessionVolume[]>([]);
  const [volumePage, setVolumePage] = useState(0);
  const [volumeTotal, setVolumeTotal] = useState(0);
  const [orphanCount, setOrphanCount] = useState(0);
  const [requestedBytes, setRequestedBytes] = useState(0);
  const [measurements, setMeasurements] = useState<Record<string, StorageMeasurement>>({});
  const [loading, setLoading] = useState(true);
  const [measuring, setMeasuring] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<SessionVolume | null>(null);
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false);
  const [bulkDeleting, setBulkDeleting] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
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
        setRequestedBytes(0);
        return;
      }
      if (!nodesRes.ok) throw new Error(await responseError(nodesRes));
      if (!volumesRes.ok) throw new Error(await responseError(volumesRes));
      const nodeBody = (await nodesRes.json()) as { nodes?: NodeFilesystem[] };
      const volumeBody = (await volumesRes.json()) as {
        volumes?: SessionVolume[];
        total?: number;
        requested_bytes?: number;
        orphan_count?: number;
      };
      setUnsupported(false);
      setNodes(Array.isArray(nodeBody.nodes) ? nodeBody.nodes : []);
      setVolumes(Array.isArray(volumeBody.volumes) ? volumeBody.volumes : []);
      setVolumeTotal(typeof volumeBody.total === "number" ? volumeBody.total : 0);
      setOrphanCount(typeof volumeBody.orphan_count === "number" ? volumeBody.orphan_count : 0);
      setRequestedBytes(
        typeof volumeBody.requested_bytes === "number" ? volumeBody.requested_bytes : 0,
      );
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
      requestedBytes,
    };
  }, [nodes, requestedBytes]);

  const measureVolume = async (volume: SessionVolume) => {
    const key = volumeKey(volume);
    setError("");
    setMeasuring(key);
    try {
      const query = new URLSearchParams({ pvc_name: volume.pvc_name });
      const res = await fetch(
        `/api/admin/session-storage/${encodeURIComponent(volume.storage_id)}/measure?${query}`,
        { method: "POST" },
      );
      if (isAccountDisabledResponse(res)) {
        await signOut({ callbackUrl: "/login?error=account_disabled" });
        return;
      }
      if (!res.ok) throw new Error(await responseError(res));
      const result = (await res.json()) as StorageMeasurement;
      setMeasurements((current) => ({ ...current, [key]: result }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setMeasuring(null);
    }
  };

  const deleteOrphanVolume = async () => {
    const volume = pendingDelete;
    if (!volume) return;
    if (!volume.delete_allowed) return;
    const key = volumeKey(volume);
    setError("");
    setNotice("");
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
      setNotice(`Orphan Session Volume ${volume.pvc_name} deletion submitted`);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setDeleting(null);
    }
  };

  const deleteAllOrphanVolumes = async () => {
    setError("");
    setNotice("");
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
      const result = (await res.json()) as { matched?: number; deleted?: number; failed?: number };
      const deleted = typeof result.deleted === "number" ? result.deleted : 0;
      const failed = typeof result.failed === "number" ? result.failed : 0;
      setBulkDeleteOpen(false);
      setNotice(
        failed > 0
          ? `Deleted ${deleted} orphan Session Volumes; ${failed} could not be deleted.`
          : `Deleted ${deleted} orphan Session Volumes.`,
      );
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBulkDeleting(false);
    }
  };

  const columns: DataGridColumn<SessionVolume>[] = [
    {
      id: "session",
      header: "Session / User",
      isRowHeader: true,
      minWidth: 260,
      cell: (volume) => (
        <span className="block min-w-0">
          <AdminTruncatedValue
            className="font-mono text-xs font-medium"
            copyLabel="session ID"
            value={volume.session_id || "Detached volume"}
          />
          <AdminTruncatedValue
            className="text-muted text-xs"
            copyLabel="user ID"
            value={volume.user_id || "No database binding"}
          />
        </span>
      ),
    },
    {
      id: "node",
      header: "Node",
      minWidth: 150,
      cell: (volume) => (
        <AdminTruncatedValue
          className="font-mono text-xs"
          copyLabel="node name"
          value={volume.node_name || "—"}
        />
      ),
    },
    {
      id: "volume",
      header: "Volume",
      minWidth: 220,
      cell: (volume) => (
        <span className="block min-w-0">
          <AdminTruncatedValue
            className="font-mono text-xs"
            copyLabel="PVC name"
            value={volume.pvc_name}
          />
          <span className="mt-1 flex flex-wrap items-center gap-1">
            <AdminStatusBadge tone={isAttachedVolume(volume) ? "green" : "amber"} dot>
              {volume.pvc_phase}
            </AdminStatusBadge>
            {volume.delete_allowed ? <AdminStatusBadge tone="red">Orphan</AdminStatusBadge> : null}
          </span>
        </span>
      ),
    },
    {
      id: "generation",
      header: "Generation",
      width: 110,
      cell: (volume) => <span className="tabular-nums">{volume.generation}</span>,
    },
    {
      id: "requested",
      header: "Requested",
      minWidth: 130,
      cell: (volume) => (
        <span className="font-mono text-xs tabular-nums">
          {formatBytes(volume.requested_bytes)}
        </span>
      ),
    },
    {
      id: "usage",
      header: "Actual usage",
      minWidth: 170,
      cell: (volume) => {
        const measurement = measurements[volumeKey(volume)];
        return measurement ? (
          <span>
            <span className="block font-mono text-xs font-medium tabular-nums">
              {formatBytes(measurement.allocated_bytes)}
            </span>
            <span className="text-muted block text-xs">
              {measurement.file_count} files · {measurement.directory_count} dirs
            </span>
          </span>
        ) : (
          <span className="text-muted">Not measured</span>
        );
      },
    },
    {
      id: "reset",
      header: "Last reset",
      minWidth: 190,
      cell: (volume) => (
        <span
          className="text-muted block truncate text-xs"
          title={
            volume.last_reset_at
              ? `${new Date(volume.last_reset_at).toLocaleString()} · ${volume.last_reset_reason || "reset"}`
              : "—"
          }
        >
          {volume.last_reset_at
            ? `${new Date(volume.last_reset_at).toLocaleString()} · ${volume.last_reset_reason || "reset"}`
            : "—"}
        </span>
      ),
    },
    {
      id: "actions",
      header: "Actions",
      align: "center",
      pinned: "end",
      width: 150,
      cell: (volume) => {
        const key = volumeKey(volume);
        return (
          <span className="flex justify-center gap-1">
            <Tooltip delay={0}>
              <Button
                isIconOnly
                aria-label={`Measure ${volume.pvc_name}`}
                isDisabled={measuring === key || !isAttachedVolume(volume)}
                isPending={measuring === key}
                size="sm"
                variant="outline"
                onPress={() => void measureVolume(volume)}
              >
                {measuring === key ? (
                  <LoaderCircle className="size-4 animate-spin" />
                ) : (
                  <Gauge className="size-4" />
                )}
              </Button>
              <Tooltip.Content>
                {!isAttachedVolume(volume)
                  ? "Measurement requires an attached volume"
                  : measurements[key]
                    ? "Measure again"
                    : "Measure actual usage"}
              </Tooltip.Content>
            </Tooltip>
            {volume.delete_allowed ? (
              <Tooltip delay={0}>
                <Button
                  isIconOnly
                  aria-label={`Delete orphan ${volume.pvc_name}`}
                  isDisabled={deleting === key}
                  size="sm"
                  variant="danger-soft"
                  onPress={() => setPendingDelete(volume)}
                >
                  <Trash2 className="size-4" />
                </Button>
                <Tooltip.Content>Delete orphan volume</Tooltip.Content>
              </Tooltip>
            ) : null}
          </span>
        );
      },
    },
  ];

  return (
    <AdminPage className="admin-theme-purple">
      <AdminPageHeader
        title="Storage"
        description="Inspect physical node headroom and measure individual Session Volumes without starting their Sandboxes."
        icon={<StoragePageIcon className="size-5" />}
        actions={
          <AdminRefreshButton
            variant="outline"
            onClick={() => void refresh()}
            disabled={loading}
            refreshing={loading}
          >
            Refresh
          </AdminRefreshButton>
        }
      />

      {error ? (
        <AdminAlert tone="error" icon={<AlertTriangle className="size-4" />}>
          {error}
        </AdminAlert>
      ) : null}
      {notice ? <AdminAlert tone="success">{notice}</AdminAlert> : null}

      {unsupported ? (
        <Card className="p-8">
          <EmptyState>
            <EmptyState.Header>
              <EmptyState.Media variant="icon">
                <HardDrive className="text-purple-500" />
              </EmptyState.Media>
              <EmptyState.Title>Node-local storage is not configured</EmptyState.Title>
              <EmptyState.Description>
                Start Cocola with the k3s runtime profile to enable storage visibility.
              </EmptyState.Description>
            </EmptyState.Header>
          </EmptyState>
        </Card>
      ) : (
        <>
          <section className="space-y-3">
            <div>
              <h2 className="text-sm font-semibold">Node filesystems</h2>
              <p className="mt-0.5 text-xs text-muted">
                Physical usage is read from the filesystem backing Cocola Session storage. It can
                include non-Session data on the same filesystem.
              </p>
            </div>
            {loading && nodes.length === 0 ? (
              <Card className="p-6">
                <EmptyState>
                  <EmptyState.Header>
                    <EmptyState.Media variant="icon">
                      <LoaderCircle className="animate-spin text-purple-500" />
                    </EmptyState.Media>
                    <EmptyState.Title>Loading storage</EmptyState.Title>
                    <EmptyState.Description>
                      Reading node filesystem capacity…
                    </EmptyState.Description>
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
                    <EmptyState.Title>No storage nodes found</EmptyState.Title>
                    <EmptyState.Description>
                      Storage nodes will appear after the runtime is connected.
                    </EmptyState.Description>
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
            <div className="flex items-center justify-between gap-3">
              <div>
                <h2 className="text-sm font-semibold">Session Storage</h2>
                <p className="mt-0.5 text-xs text-muted">
                  Volume requests are soft limits. Actual disk usage is measured only when you
                  request it.
                </p>
              </div>
              <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
                <AdminStatusBadge tone={orphanCount > 0 ? "amber" : "green"} dot>
                  {orphanCount === 0
                    ? "No orphan volumes"
                    : `${orphanCount} orphan ${orphanCount === 1 ? "volume" : "volumes"}`}
                </AdminStatusBadge>
                <Button
                  isDisabled={orphanCount === 0 || loading || bulkDeleting || Boolean(deleting)}
                  size="sm"
                  variant="danger-soft"
                  onPress={() => setBulkDeleteOpen(true)}
                >
                  <Trash2 className="size-3.5" />
                  Delete all orphans ({orphanCount})
                </Button>
              </div>
            </div>

            <div className="min-w-0">
              <DataGrid
                aria-label="Session storage"
                columns={columns}
                contentClassName="min-w-[1240px]"
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
                        {loading ? "Loading storage" : "No Session Volumes"}
                      </EmptyState.Title>
                      <EmptyState.Description>
                        Session storage will appear after a workspace is created.
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
                label="volumes"
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
        title="Delete orphan Session Volume?"
        description={
          pendingDelete
            ? `This permanently deletes ${pendingDelete.pvc_name}. Active Session Volumes are not affected.`
            : ""
        }
        confirmLabel="Delete orphan"
        busy={deleting !== null}
        destructive
        onConfirm={() => void deleteOrphanVolume()}
      />
      <AdminConfirmDialog
        open={bulkDeleteOpen}
        onOpenChange={setBulkDeleteOpen}
        title={`Delete all ${orphanCount} orphan Session Volumes?`}
        description="This permanently deletes every Session Volume currently marked as an orphan. Active Session Volumes are not affected."
        confirmLabel="Delete all orphans"
        busy={bulkDeleting}
        destructive
        onConfirm={() => void deleteAllOrphanVolumes()}
      />
    </AdminPage>
  );
}

function NodeStorageCard({ node }: { node: NodeFilesystem }) {
  if (!node.available) {
    return (
      <Card className="p-5">
        <Card.Content className="p-0">
          <div className="flex items-center justify-between gap-3">
            <div className="font-mono text-sm font-medium">{node.node_name}</div>
            <AdminStatusBadge tone="amber" dot>
              Probe unavailable
            </AdminStatusBadge>
          </div>
          <p className="text-muted mt-3 text-xs">
            {node.error || "Storage probe is not reporting from this node."}
          </p>
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
        <div className="flex items-start justify-between gap-3">
          <div>
            <div className="font-mono text-sm font-medium">{node.node_name}</div>
            <div className="text-muted mt-1 text-xs">
              {formatBytes(node.available_bytes)} available of {formatBytes(node.total_bytes)}
            </div>
          </div>
          <AdminStatusBadge
            tone={tone === "red" ? "red" : tone === "amber" ? "amber" : "green"}
            dot
          >
            {formatPercent(availableRatio)} available
          </AdminStatusBadge>
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
          <span>{formatBytes(node.used_bytes)} filesystem used</span>
          <span>{formatPercent(occupiedRatio)} unavailable</span>
        </div>
      </Card.Content>
    </Card>
  );
}

function volumeKey(volume: Pick<SessionVolume, "storage_id" | "pvc_name">) {
  return `${volume.storage_id}:${volume.pvc_name}`;
}

function isAttachedVolume(volume: Pick<SessionVolume, "pvc_phase">) {
  return volume.pvc_phase === "Bound" || volume.pvc_phase === "Mounted";
}

function capacityTone(available: number, total: number): "green" | "amber" | "red" {
  if (total <= 0) return "amber";
  const ratio = available / total;
  if (ratio < 0.1) return "red";
  if (ratio < 0.2) return "amber";
  return "green";
}

function formatPercent(value: number) {
  return `${Math.round(Math.min(Math.max(value, 0), 1) * 100)}%`;
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
