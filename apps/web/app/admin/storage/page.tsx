"use client";

import { HardDrive as StoragePageIcon } from "lucide-react";
import { AlertTriangle, Database, Gauge, HardDrive, LoaderCircle, Trash2 } from "lucide-react";
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
} from "@/components/admin/admin-ui";
import { Button } from "@/components/ui/button";
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
const LIST_COLS = "1.6fr 1fr 1.4fr 0.8fr 0.9fr 1fr 1fr 0.8fr";

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
        <section className="admin-surface px-4 py-10 text-center">
          <div className="mx-auto grid size-10 place-items-center rounded-md bg-muted">
            <HardDrive className="size-5 text-muted-foreground" />
          </div>
          <h2 className="mt-4 text-sm font-semibold">Node-local storage is not configured</h2>
          <p className="mx-auto mt-2 max-w-xl text-sm text-muted-foreground">
            Start Cocola with the k3s runtime profile to enable storage visibility.
          </p>
        </section>
      ) : (
        <>
          <section className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <Metric
              label="Storage nodes"
              value={String(totals.nodeCount)}
              detail={`${totals.measuredCount} reporting`}
              tone="purple"
              icon={<HardDrive />}
            />
            <Metric
              label="Physical capacity"
              value={formatBytes(totals.totalBytes)}
              detail="Across reporting node filesystems"
              tone="sky"
              icon={<Gauge />}
            />
            <Metric
              label="Physical available"
              value={formatBytes(totals.availableBytes)}
              detail="Available to local-path storage"
              tone={metricTone(capacityTone(totals.availableBytes, totals.totalBytes))}
              icon={<HardDrive />}
            />
            <Metric
              label="Session requests"
              value={formatBytes(totals.requestedBytes)}
              detail={`${volumeTotal} PVCs · soft requests`}
              tone="violet"
              icon={<Database />}
            />
          </section>

          <section className="space-y-3">
            <div>
              <h2 className="text-sm font-semibold">Node filesystems</h2>
              <p className="mt-0.5 text-xs text-muted-foreground">
                Physical usage is read from the filesystem backing /var/lib/cocola/storage. It can
                include non-Session data on the same filesystem.
              </p>
            </div>
            {loading && nodes.length === 0 ? (
              <div className="admin-list-empty">Loading storage…</div>
            ) : nodes.length === 0 ? (
              <div className="admin-list-empty">No Kubernetes nodes found</div>
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
                <p className="mt-0.5 text-xs text-muted-foreground">
                  PVC requests are soft limits. Actual disk usage is measured only when you request
                  it.
                </p>
              </div>
              {orphanCount > 0 ? (
                <button
                  type="button"
                  className="admin-card-btn admin-card-btn--danger"
                  disabled={loading || bulkDeleting || Boolean(deleting)}
                  onClick={() => setBulkDeleteOpen(true)}
                >
                  <Trash2 className="size-3.5" />
                  Delete all orphans ({orphanCount})
                </button>
              ) : null}
            </div>

            <div className="admin-list">
              <div className="admin-list-scroll">
                <div className="min-w-[1180px]">
                  <div className="admin-list-cols" style={{ gridTemplateColumns: LIST_COLS }}>
                    <div>Session / User</div>
                    <div>Node</div>
                    <div>PVC</div>
                    <div>Generation</div>
                    <div>Requested (soft)</div>
                    <div>Actual usage</div>
                    <div>Last reset</div>
                    <div className="text-right">Actions</div>
                  </div>
                  {volumes.length === 0 ? (
                    <div className="admin-list-empty">No Session Volumes</div>
                  ) : (
                    volumes.map((volume) => {
                      const key = volumeKey(volume);
                      const measurement = measurements[key];
                      return (
                        <div
                          key={key}
                          className="admin-list-row"
                          style={{ gridTemplateColumns: LIST_COLS }}
                        >
                          <div className="min-w-0">
                            <div className="admin-list-primary admin-list-mono">
                              {volume.session_id || "Detached PVC"}
                            </div>
                            <div className="admin-list-sub">
                              {volume.user_id || "No database binding"}
                            </div>
                          </div>
                          <div className="admin-list-cell admin-list-mono">
                            {volume.node_name || "—"}
                          </div>
                          <div className="admin-list-cell">
                            <div className="admin-list-mono">{volume.pvc_name}</div>
                            <AdminStatusBadge
                              className="mt-1"
                              tone={volume.pvc_phase === "Bound" ? "green" : "amber"}
                              dot
                            >
                              {volume.pvc_phase}
                            </AdminStatusBadge>
                          </div>
                          <div className="admin-list-cell tabular-nums">{volume.generation}</div>
                          <div className="admin-list-cell admin-list-mono tabular-nums">
                            {formatBytes(volume.requested_bytes)}
                          </div>
                          <div className="admin-list-cell">
                            {measurement ? (
                              <div>
                                <div className="admin-list-mono font-medium tabular-nums">
                                  {formatBytes(measurement.allocated_bytes)}
                                </div>
                                <div className="admin-list-sub">
                                  {measurement.file_count} files · {measurement.directory_count} dirs
                                </div>
                              </div>
                            ) : (
                              <span className="admin-list-muted">Not measured</span>
                            )}
                          </div>
                          <div className="admin-list-cell admin-list-muted">
                            {volume.last_reset_at
                              ? `${new Date(volume.last_reset_at).toLocaleString()} · ${volume.last_reset_reason || "reset"}`
                              : "—"}
                          </div>
                          <div className="flex items-center justify-end gap-2">
                            <button
                              type="button"
                              className="admin-card-btn"
                              disabled={measuring === key || volume.pvc_phase !== "Bound"}
                              onClick={() => void measureVolume(volume)}
                            >
                              {measuring === key ? (
                                <LoaderCircle className="size-3.5 animate-spin" />
                              ) : (
                                <Gauge className="size-3.5" />
                              )}
                              Measure
                            </button>
                            {volume.delete_allowed ? (
                              <button
                                type="button"
                                className="admin-card-btn admin-card-btn--danger"
                                disabled={deleting === key}
                                onClick={() => setPendingDelete(volume)}
                              >
                                <Trash2 className="size-3.5" />
                                Delete orphan
                              </button>
                            ) : null}
                          </div>
                        </div>
                      );
                    })
                  )}
                </div>
              </div>
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

function Metric({
  label,
  value,
  detail,
  tone,
  icon,
}: {
  label: string;
  value: string;
  detail?: string;
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
      {detail ? <div className="admin-metric-detail">{detail}</div> : null}
    </div>
  );
}

function NodeStorageCard({ node }: { node: NodeFilesystem }) {
  if (!node.available) {
    return (
      <div className="admin-entity-card">
        <div className="flex items-center justify-between gap-3">
          <div className="admin-list-primary admin-list-mono">{node.node_name}</div>
          <AdminStatusBadge tone="amber" dot>
            Probe unavailable
          </AdminStatusBadge>
        </div>
        <p className="mt-3 text-xs text-muted-foreground">
          {node.error || "Storage probe is not reporting from this node."}
        </p>
      </div>
    );
  }
  const availableRatio = node.total_bytes > 0 ? node.available_bytes / node.total_bytes : 0;
  const occupiedRatio = 1 - availableRatio;
  const tone = capacityTone(node.available_bytes, node.total_bytes);
  return (
    <div className="admin-entity-card admin-entity-card--hover">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="admin-list-primary admin-list-mono">{node.node_name}</div>
          <div className="admin-list-sub mt-1">
            {formatBytes(node.available_bytes)} available of {formatBytes(node.total_bytes)}
          </div>
        </div>
        <AdminStatusBadge tone={tone === "red" ? "red" : tone === "amber" ? "amber" : "green"} dot>
          {formatPercent(availableRatio)} available
        </AdminStatusBadge>
      </div>
      <div className="mt-4 h-2 overflow-hidden rounded-full bg-muted">
        <div
          className={cn(
            "h-full rounded-full transition-[width]",
            tone === "red" && "bg-destructive",
            tone === "amber" && "bg-amber-500",
            tone === "green" && "bg-emerald-500",
          )}
          style={{ width: `${Math.min(Math.max(occupiedRatio * 100, 0), 100)}%` }}
        />
      </div>
      <div className="mt-2 flex justify-between text-[11px] text-muted-foreground">
        <span>{formatBytes(node.used_bytes)} filesystem used</span>
        <span>{formatPercent(occupiedRatio)} unavailable</span>
      </div>
    </div>
  );
}

function volumeKey(volume: Pick<SessionVolume, "storage_id" | "pvc_name">) {
  return `${volume.storage_id}:${volume.pvc_name}`;
}

function capacityTone(available: number, total: number): "green" | "amber" | "red" {
  if (total <= 0) return "amber";
  const ratio = available / total;
  if (ratio < 0.1) return "red";
  if (ratio < 0.2) return "amber";
  return "green";
}

function metricTone(tone: "green" | "amber" | "red"): string {
  if (tone === "red") return "rose";
  if (tone === "amber") return "amber";
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
