"use client";

import {
  HardDrive as StoragePageIcon,
} from "lucide-react";
import { AlertTriangle, Database, Gauge, HardDrive, LoaderCircle, Trash2 } from "lucide-react";
import { signOut } from "next-auth/react";
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  AdminAlert,
  AdminMetric,
  AdminPage,
  AdminPageHeader,
  AdminPanel,
  AdminRefreshButton,
  AdminStatusBadge,
  AdminTable,
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

export default function StoragePage() {
  const [nodes, setNodes] = useState<NodeFilesystem[]>([]);
  const [volumes, setVolumes] = useState<SessionVolume[]>([]);
  const [measurements, setMeasurements] = useState<Record<string, StorageMeasurement>>({});
  const [loading, setLoading] = useState(true);
  const [measuring, setMeasuring] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [unsupported, setUnsupported] = useState(false);

  const refresh = useCallback(async () => {
    setError("");
    setLoading(true);
    try {
      const [nodesRes, volumesRes] = await Promise.all([
        fetch("/api/admin/storage/nodes", { cache: "no-store" }),
        fetch("/api/admin/session-storage", { cache: "no-store" }),
      ]);
      if (isAccountDisabledResponse(nodesRes) || isAccountDisabledResponse(volumesRes)) {
        await signOut({ callbackUrl: "/login?error=account_disabled" });
        return;
      }
      if (await isUnsupportedResponse(nodesRes)) {
        setUnsupported(true);
        setNodes([]);
        setVolumes([]);
        return;
      }
      if (!nodesRes.ok) throw new Error(await responseError(nodesRes));
      if (!volumesRes.ok) throw new Error(await responseError(volumesRes));
      const nodeBody = (await nodesRes.json()) as { nodes?: NodeFilesystem[] };
      const volumeBody = (await volumesRes.json()) as { volumes?: SessionVolume[] };
      setUnsupported(false);
      setNodes(Array.isArray(nodeBody.nodes) ? nodeBody.nodes : []);
      setVolumes(Array.isArray(volumeBody.volumes) ? volumeBody.volumes : []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const totals = useMemo(() => {
    const measured = nodes.filter((node) => node.available);
    return {
      nodeCount: nodes.length,
      measuredCount: measured.length,
      totalBytes: measured.reduce((sum, node) => sum + node.total_bytes, 0),
      availableBytes: measured.reduce((sum, node) => sum + node.available_bytes, 0),
      requestedBytes: volumes.reduce((sum, volume) => sum + volume.requested_bytes, 0),
    };
  }, [nodes, volumes]);

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

  const deleteOrphanVolume = async (volume: SessionVolume) => {
    if (!volume.delete_allowed) return;
    if (!window.confirm(`Delete orphan Workspace ${volume.pvc_name}?`)) return;
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
      if (!res.ok) throw new Error(await responseError(res));
      setNotice("Orphan Workspace deletion submitted");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setDeleting(null);
    }
  };

  return (
    <AdminPage>
      <AdminPageHeader
        eyebrow="Node-local persistence"
        title="Storage"
        description="Inspect physical node headroom and measure individual Session Volumes without starting their Sandboxes."
        icon={<StoragePageIcon className="size-5" />}
        actions={
          <AdminRefreshButton
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
        <AdminPanel>
          <div className="py-8 text-center">
            <HardDrive className="mx-auto size-8 text-muted-foreground" />
            <h2 className="mt-3 text-sm font-semibold">Node-local storage is not configured</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              Start Cocola with the k3s runtime profile to enable storage visibility.
            </p>
          </div>
        </AdminPanel>
      ) : (
        <>
          <section className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <AdminMetric
              label="Storage nodes"
              value={totals.nodeCount}
              detail={`${totals.measuredCount} reporting`}
              icon={<HardDrive className="size-4" />}
            />
            <AdminMetric
              label="Physical capacity"
              value={formatBytes(totals.totalBytes)}
              detail="Across reporting node filesystems"
              icon={<Gauge className="size-4" />}
              tone="sky"
            />
            <AdminMetric
              label="Physical available"
              value={formatBytes(totals.availableBytes)}
              detail="Available to local-path storage"
              icon={<HardDrive className="size-4" />}
              tone={capacityTone(totals.availableBytes, totals.totalBytes)}
            />
            <AdminMetric
              label="Session requests"
              value={formatBytes(totals.requestedBytes)}
              detail={`${volumes.length} PVCs · soft requests`}
              icon={<Database className="size-4" />}
              tone="violet"
            />
          </section>

          <AdminPanel
            title="Node filesystems"
            description="Physical usage is read from the filesystem backing /var/lib/cocola/storage. It can include non-Session data on the same filesystem."
          >
            {loading && nodes.length === 0 ? (
              <div className="py-8 text-center text-sm text-muted-foreground">Loading storage…</div>
            ) : nodes.length === 0 ? (
              <div className="py-8 text-center text-sm text-muted-foreground">
                No Kubernetes nodes found
              </div>
            ) : (
              <div className="grid gap-3 lg:grid-cols-2">
                {nodes.map((node) => (
                  <NodeStorageCard key={node.node_name} node={node} />
                ))}
              </div>
            )}
          </AdminPanel>

          <AdminPanel
            title="Session Storage"
            description="PVC requests are soft limits. Actual disk usage is measured only when you request it."
            contentClassName="p-0 sm:p-0"
          >
            <AdminTable className="rounded-none border-0">
              <table className="w-full min-w-[1180px] text-sm">
                <thead className="border-b border-border/70 bg-muted/45 text-xs text-muted-foreground">
                  <tr>
                    <th className="px-4 py-3 text-left font-medium">Session / User</th>
                    <th className="px-4 py-3 text-left font-medium">Node</th>
                    <th className="px-4 py-3 text-left font-medium">PVC</th>
                    <th className="px-4 py-3 text-left font-medium">Generation</th>
                    <th className="px-4 py-3 text-left font-medium">Requested (soft)</th>
                    <th className="px-4 py-3 text-left font-medium">Actual usage</th>
                    <th className="px-4 py-3 text-left font-medium">Last reset</th>
                    <th className="px-4 py-3 text-right font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {volumes.length === 0 ? (
                    <tr>
                      <td colSpan={8} className="px-4 py-10 text-center text-muted-foreground">
                        No Session Volumes
                      </td>
                    </tr>
                  ) : (
                    volumes.map((volume) => {
                      const key = volumeKey(volume);
                      const measurement = measurements[key];
                      return (
                        <tr key={key} className="border-b border-border/60 last:border-0">
                          <td className="px-4 py-3">
                            <div className="font-mono text-xs">
                              {volume.session_id || "Detached PVC"}
                            </div>
                            <div className="mt-1 text-xs text-muted-foreground">
                              {volume.user_id || "No database binding"}
                            </div>
                          </td>
                          <td className="px-4 py-3">{volume.node_name || "-"}</td>
                          <td className="px-4 py-3">
                            <div className="font-mono text-xs">{volume.pvc_name}</div>
                            <AdminStatusBadge
                              className="mt-1"
                              tone={volume.pvc_phase === "Bound" ? "green" : "amber"}
                            >
                              {volume.pvc_phase}
                            </AdminStatusBadge>
                          </td>
                          <td className="px-4 py-3 tabular-nums">{volume.generation}</td>
                          <td className="px-4 py-3 tabular-nums">
                            {formatBytes(volume.requested_bytes)}
                          </td>
                          <td className="px-4 py-3">
                            {measurement ? (
                              <div>
                                <div className="font-medium tabular-nums">
                                  {formatBytes(measurement.allocated_bytes)}
                                </div>
                                <div className="mt-1 text-xs text-muted-foreground">
                                  {measurement.file_count} files · {measurement.directory_count}{" "}
                                  dirs
                                </div>
                              </div>
                            ) : (
                              <span className="text-xs text-muted-foreground">Not measured</span>
                            )}
                          </td>
                          <td className="max-w-[220px] px-4 py-3 text-xs text-muted-foreground">
                            {volume.last_reset_at
                              ? `${new Date(volume.last_reset_at).toLocaleString()} · ${volume.last_reset_reason || "reset"}`
                              : "-"}
                          </td>
                          <td className="px-4 py-3">
                            <div className="flex justify-end gap-2">
                              <Button
                                variant="outline"
                                size="sm"
                                disabled={measuring === key || volume.pvc_phase !== "Bound"}
                                onClick={() => void measureVolume(volume)}
                              >
                                {measuring === key ? (
                                  <LoaderCircle className="mr-2 size-4 animate-spin" />
                                ) : (
                                  <Gauge className="mr-2 size-4" />
                                )}
                                Measure
                              </Button>
                              {volume.delete_allowed ? (
                                <Button
                                  variant="destructive"
                                  size="sm"
                                  disabled={deleting === key}
                                  onClick={() => void deleteOrphanVolume(volume)}
                                >
                                  <Trash2 className="mr-2 size-4" />
                                  Delete orphan
                                </Button>
                              ) : null}
                            </div>
                          </td>
                        </tr>
                      );
                    })
                  )}
                </tbody>
              </table>
            </AdminTable>
          </AdminPanel>
        </>
      )}
    </AdminPage>
  );
}

function NodeStorageCard({ node }: { node: NodeFilesystem }) {
  if (!node.available) {
    return (
      <div className="rounded-xl border border-amber-500/25 bg-amber-500/5 p-4">
        <div className="flex items-center justify-between gap-3">
          <div className="font-mono text-sm font-medium">{node.node_name}</div>
          <AdminStatusBadge tone="amber">Probe unavailable</AdminStatusBadge>
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
    <div className="rounded-xl border border-border/70 bg-background/55 p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="font-mono text-sm font-medium">{node.node_name}</div>
          <div className="mt-1 text-xs text-muted-foreground">
            {formatBytes(node.available_bytes)} available of {formatBytes(node.total_bytes)}
          </div>
        </div>
        <AdminStatusBadge tone={tone === "red" ? "red" : tone === "amber" ? "amber" : "green"}>
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
