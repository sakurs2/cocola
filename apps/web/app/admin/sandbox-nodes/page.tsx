"use client";

import { Cpu as SandboxNodesPageIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  AdminEmptyState,
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
  AdminStatusBadge,
} from "@/components/admin/admin-ui";
import { cn } from "@/lib/utils";
import {
  AlertTriangle,
  Ban,
  Boxes,
  CheckCircle2,
  CircleDot,
  Copy,
  FolderTree,
  LoaderCircle,
  Plus,
  Power,
  Server,
  SlidersHorizontal,
} from "lucide-react";
import { signOut } from "next-auth/react";
import { useCallback, useEffect, useMemo, useState } from "react";

type SandboxNode = {
  name: string;
  status: "active" | "disabled" | "offline_pending" | "offline" | "unhealthy" | string;
  ready: boolean;
  schedulable: boolean;
  disk_pressure: boolean;
  cpu_capacity: string;
  memory_capacity: string;
  cpu_allocatable: string;
  memory_allocatable: string;
  sandbox_pods: number;
  max_sandbox_pods?: number | null;
  session_count: number;
  session_requested_bytes: number;
  workspace_reset_count: number;
  reason?: string;
  labels?: Record<string, string>;
};

type NodeListResponse = { nodes: SandboxNode[] };
type JoinCommand = { command: string; note: string };
type OfflineNodeResult = {
  node: SandboxNode;
  pending_pods?: string[];
  affected_sessions?: number;
  message: string;
};
type OfflineTarget = { node: SandboxNode; pendingPods: string[]; affectedSessions: number };

type BadgeTone = "neutral" | "sky" | "green" | "amber" | "red";

const STATUS_LABELS: Record<string, string> = {
  active: "Active",
  disabled: "Disabled",
  offline_pending: "Offline pending",
  offline: "Offline",
  unhealthy: "Unhealthy",
};

const STATUS_TONES: Record<string, BadgeTone> = {
  active: "green",
  disabled: "amber",
  offline_pending: "sky",
  offline: "neutral",
  unhealthy: "red",
};

const LIST_COLS = "1.6fr 0.9fr 0.8fr 1.5fr 0.7fr 1.4fr 0.9fr 1.4fr 1.1fr 1.7fr";

export default function SandboxNodesPage() {
  const [nodes, setNodes] = useState<SandboxNode[]>([]);
  const [join, setJoin] = useState<JoinCommand | null>(null);
  const [loading, setLoading] = useState(true);
  const [actingNode, setActingNode] = useState<string | null>(null);
  const [savingCapacity, setSavingCapacity] = useState<string | null>(null);
  const [capacityDrafts, setCapacityDrafts] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [unsupported, setUnsupported] = useState(false);
  const [showAddNode, setShowAddNode] = useState(false);
  const [capacityTarget, setCapacityTarget] = useState<SandboxNode | null>(null);
  const [offlineTarget, setOfflineTarget] = useState<OfflineTarget | null>(null);

  const refresh = useCallback(async () => {
    setError("");
    setLoading(true);
    try {
      const [nodesRes, joinRes] = await Promise.all([
        fetch("/api/admin/sandbox-nodes", { cache: "no-store" }),
        fetch("/api/admin/sandbox-nodes/join-command", { cache: "no-store" }),
      ]);
      if (isAccountDisabledResponse(nodesRes)) return redirectAccountDisabled();
      if (await isUnsupportedResponse(nodesRes)) {
        setUnsupported(true);
        setNodes([]);
        setJoin(null);
        return;
      }
      if (!nodesRes.ok) throw new Error(await responseError(nodesRes));
      const nodeBody = (await nodesRes.json()) as NodeListResponse;
      const nextNodes = Array.isArray(nodeBody.nodes) ? nodeBody.nodes : [];
      setUnsupported(false);
      setNodes(nextNodes);
      setCapacityDrafts(
        Object.fromEntries(
          nextNodes.map((node) => [
            node.name,
            node.max_sandbox_pods == null ? "" : String(node.max_sandbox_pods),
          ]),
        ),
      );
      if (isAccountDisabledResponse(joinRes)) return redirectAccountDisabled();
      if (joinRes.ok) setJoin((await joinRes.json()) as JoinCommand);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const totals = useMemo(
    () => ({
      nodes: nodes.length,
      active: nodes.filter((n) => n.status === "active").length,
      unavailable: nodes.filter((n) =>
        ["disabled", "offline_pending", "offline"].includes(n.status),
      ).length,
      unhealthy: nodes.filter((n) => n.status === "unhealthy").length,
      sandboxPods: nodes.reduce((sum, n) => sum + n.sandbox_pods, 0),
      sessions: nodes.reduce((sum, n) => sum + n.session_count, 0),
    }),
    [nodes],
  );

  const runNodeAction = async (
    node: SandboxNode,
    action: "disable" | "restore" | "offline",
    force = false,
  ) => {
    setError("");
    setNotice("");
    setActingNode(`${node.name}:${action}`);
    try {
      const res = await fetch(
        `/api/admin/sandbox-nodes/${encodeURIComponent(node.name)}/${action}`,
        {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: action === "offline" ? JSON.stringify({ force }) : undefined,
        },
      );
      if (isAccountDisabledResponse(res)) return redirectAccountDisabled();
      if (!res.ok) throw new Error(await responseError(res));
      if (action === "offline") {
        const body = (await res.json()) as OfflineNodeResult;
        const pendingPods = body.pending_pods ?? [];
        const affectedSessions = body.affected_sessions ?? body.node.session_count ?? 0;
        if (!force && affectedSessions > 0) {
          await refresh();
          setOfflineTarget({ node: body.node, pendingPods, affectedSessions });
          return;
        }
        setNotice(body.message || "Node offline requested");
      }
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setActingNode(null);
      if (force || action !== "offline") setOfflineTarget(null);
    }
  };

  const copyJoinCommand = async () => {
    if (!join?.command) return;
    setError("");
    try {
      await navigator.clipboard.writeText(join.command);
      setNotice("Join command copied");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to copy join command");
    }
  };

  const openCapacityDialog = (node: SandboxNode) => {
    setCapacityDrafts((prev) => ({
      ...prev,
      [node.name]: node.max_sandbox_pods == null ? "" : String(node.max_sandbox_pods),
    }));
    setCapacityTarget(node);
  };

  const saveCapacity = async (node: SandboxNode) => {
    const raw = (capacityDrafts[node.name] ?? "").trim();
    const validationError = capacityInputError(raw);
    if (validationError) {
      setError(validationError);
      return false;
    }
    const max = raw === "" ? null : Number(raw);
    setError("");
    setNotice("");
    setSavingCapacity(node.name);
    try {
      const res = await fetch(
        `/api/admin/sandbox-nodes/${encodeURIComponent(node.name)}/capacity`,
        {
          method: "PATCH",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ max_sandbox_pods: max }),
        },
      );
      if (isAccountDisabledResponse(res)) return redirectAccountDisabled();
      if (!res.ok) throw new Error(await responseError(res));
      setNotice(max == null ? "Sandbox capacity limit cleared" : "Sandbox capacity limit saved");
      await refresh();
      return true;
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      return false;
    } finally {
      setSavingCapacity(null);
    }
  };

  return (
    <AdminPage className="admin-theme-sky">
      <AdminPageHeader
        icon={<SandboxNodesPageIcon className="size-5" />}
        title="Nodes"
        description="k3s node operations for OpenSandbox Kubernetes runtime"
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
      {notice && !error ? (
        <div className="flex items-center gap-2 rounded-xl border border-emerald-500/30 bg-emerald-500/10 px-4 py-3 text-sm text-emerald-700">
          <CheckCircle2 className="size-4 shrink-0" />
          <span>{notice}</span>
        </div>
      ) : null}

      {unsupported ? (
        <UnsupportedState />
      ) : (
        <>
          <section className="grid gap-4 md:grid-cols-2 xl:grid-cols-6">
            <Metric label="Nodes" value={String(totals.nodes)} tone="sky" icon={<Server />} />
            <Metric
              label="Active"
              value={String(totals.active)}
              tone="green"
              icon={<CheckCircle2 />}
            />
            <Metric
              label="Disabled/Offline"
              value={String(totals.unavailable)}
              tone="amber"
              icon={<Power />}
            />
            <Metric
              label="Unhealthy"
              value={String(totals.unhealthy)}
              tone="rose"
              icon={<AlertTriangle />}
            />
            <Metric
              label="Sandbox Pods"
              value={String(totals.sandboxPods)}
              tone="violet"
              icon={<Boxes />}
            />
            <Metric
              label="Session Workspaces"
              value={String(totals.sessions)}
              tone="slate"
              icon={<FolderTree />}
            />
          </section>

          <div className="admin-entity-card flex flex-row items-center gap-4">
            <span className="admin-entity-glyph">
              <Plus className="size-5" />
            </span>
            <div className="min-w-0 flex-1">
              <div className="admin-list-primary">Add node</div>
              <div className="admin-list-sub">Join an existing machine to this k3s cluster.</div>
            </div>
            <button
              type="button"
              className="admin-card-btn"
              onClick={() => setShowAddNode(true)}
            >
              <Plus className="size-3.5" />
              Add node
            </button>
          </div>

          <div className="admin-list">
            {loading && nodes.length === 0 ? (
              <div className="admin-list-empty">Loading nodes…</div>
            ) : nodes.length === 0 ? (
              <AdminEmptyState
                icon={<SandboxNodesPageIcon className="size-6" />}
                title="No nodes found"
                description="Nodes will appear here once they join the k3s cluster."
              />
            ) : (
              <div className="admin-list-scroll">
                <div className="min-w-[1600px]">
                  <div className="admin-list-cols admin-nodes-center" style={{ gridTemplateColumns: LIST_COLS }}>
                    <div>Node</div>
                    <div>Status</div>
                    <div>CPU</div>
                    <div>Memory</div>
                    <div>Sandbox Pods</div>
                    <div>Local Workspaces</div>
                    <div>Disk</div>
                    <div>Max Sandbox Pods</div>
                    <div>Reason</div>
                    <div>Actions</div>
                  </div>
                  {nodes.map((node) => {
                    const offlining = actingNode === `${node.name}:offline`;
                    const alreadyOffline = ["offline", "offline_pending"].includes(node.status);
                    return (
                      <div
                        key={node.name}
                        className="admin-list-row admin-nodes-center"
                        style={{ gridTemplateColumns: LIST_COLS }}
                      >
                        <div className="min-w-0">
                          <div className="admin-list-primary admin-list-mono">{node.name}</div>
                          <div className="mt-1 flex flex-wrap gap-1">
                            {Object.entries(node.labels ?? {})
                              .filter(
                                ([key]) =>
                                  key.startsWith("node-role.kubernetes.io/") ||
                                  key === "kubernetes.io/arch",
                              )
                              .slice(0, 3)
                              .map(([key, value]) => (
                                <span
                                  key={key}
                                  className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground"
                                >
                                  {labelName(key, value)}
                                </span>
                              ))}
                          </div>
                        </div>
                        <div className="admin-list-cell">
                          <AdminStatusBadge tone={STATUS_TONES[node.status] ?? "neutral"} dot>
                            {STATUS_LABELS[node.status] ?? node.status}
                          </AdminStatusBadge>
                        </div>
                        <div className="admin-list-cell admin-list-mono">
                          {node.cpu_allocatable || "—"} / {node.cpu_capacity || "—"}
                        </div>
                        <div className="admin-list-cell admin-list-mono">
                          {node.memory_allocatable || "—"} / {node.memory_capacity || "—"}
                        </div>
                        <div className="admin-list-cell">{node.sandbox_pods}</div>
                        <div className="admin-list-cell">
                          <div>{node.session_count}</div>
                          <div className="admin-list-sub">
                            {formatBytes(node.session_requested_bytes)} requested ·{" "}
                            {node.workspace_reset_count} resets
                          </div>
                        </div>
                        <div className="admin-list-cell">
                          <AdminStatusBadge tone={node.disk_pressure ? "red" : "green"} dot>
                            {node.disk_pressure ? "Pressure" : "Normal"}
                          </AdminStatusBadge>
                        </div>
                        <div className="admin-list-cell">
                          <div className="flex items-center justify-center gap-2">
                            <span
                              className={cn(
                                "admin-list-mono",
                                node.max_sandbox_pods == null
                                  ? "text-muted-foreground"
                                  : "text-foreground",
                              )}
                            >
                              {node.max_sandbox_pods == null ? "Unlimited" : node.max_sandbox_pods}
                            </span>
                            <button
                              type="button"
                              className="admin-card-btn"
                              disabled={Boolean(savingCapacity)}
                              onClick={() => openCapacityDialog(node)}
                            >
                              <SlidersHorizontal className="size-3.5" />
                              Edit
                            </button>
                          </div>
                        </div>
                        <div
                          className="admin-list-cell admin-list-muted truncate"
                          title={node.reason}
                        >
                          {node.reason || "—"}
                        </div>
                        <div className="flex items-center justify-center gap-2">
                          {node.schedulable ? (
                            <button
                              type="button"
                              className="admin-card-btn"
                              disabled={Boolean(actingNode)}
                              onClick={() => void runNodeAction(node, "disable")}
                            >
                              <Ban className="size-3.5" />
                              Disable
                            </button>
                          ) : (
                            <button
                              type="button"
                              className="admin-card-btn"
                              disabled={Boolean(actingNode)}
                              onClick={() => void runNodeAction(node, "restore")}
                            >
                              <CheckCircle2 className="size-3.5" />
                              Restore
                            </button>
                          )}
                          <button
                            type="button"
                            className="admin-card-btn admin-card-btn--danger"
                            disabled={Boolean(actingNode) || alreadyOffline}
                            title={alreadyOffline ? "Node is already offline" : undefined}
                            onClick={() => void runNodeAction(node, "offline", false)}
                          >
                            {offlining ? (
                              <LoaderCircle className="size-3.5 animate-spin" />
                            ) : (
                              <Power className="size-3.5" />
                            )}
                            {alreadyOffline ? "Offline" : offlining ? "Offlining…" : "Offline"}
                          </button>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            )}
          </div>
        </>
      )}

      {offlineTarget && (
        <OfflineDialog
          target={offlineTarget}
          acting={actingNode === `${offlineTarget.node.name}:offline`}
          onCancel={() => setOfflineTarget(null)}
          onConfirm={() => void runNodeAction(offlineTarget.node, "offline", true)}
        />
      )}
      {showAddNode && (
        <AddNodeDialog
          join={join}
          onCancel={() => setShowAddNode(false)}
          onCopy={() => void copyJoinCommand()}
        />
      )}
      {capacityTarget && (
        <CapacityDialog
          node={capacityTarget}
          value={capacityDrafts[capacityTarget.name] ?? ""}
          saving={savingCapacity === capacityTarget.name}
          onChange={(value) =>
            setCapacityDrafts((prev) => ({ ...prev, [capacityTarget.name]: value }))
          }
          onCancel={() => setCapacityTarget(null)}
          onSave={async () => {
            if (await saveCapacity(capacityTarget)) setCapacityTarget(null);
          }}
        />
      )}
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

function UnsupportedState() {
  return (
    <section className="admin-surface px-4 py-10 text-center">
      <div className="mx-auto grid size-10 place-items-center rounded-md bg-muted">
        <Server className="size-5 text-muted-foreground" />
      </div>
      <h2 className="mt-4 text-sm font-semibold">
        Cluster management is not supported in the current runtime mode.
      </h2>
      <p className="mx-auto mt-2 max-w-xl text-sm text-muted-foreground">
        Start cocola with the k3s runtime profile to enable node operations.
      </p>
    </section>
  );
}

function OfflineDialog({
  target,
  acting,
  onCancel,
  onConfirm,
}: {
  target: OfflineTarget;
  acting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-black/40 px-4">
      <div
        role="dialog"
        aria-modal="true"
        className="w-full max-w-md rounded-lg border border-border bg-background p-4 shadow-xl"
      >
        <div className="flex items-start gap-3">
          <div className="grid size-9 shrink-0 place-items-center rounded-md bg-destructive/10 text-destructive">
            <AlertTriangle className="size-4" />
          </div>
          <div className="min-w-0 flex-1">
            <h2 className="text-sm font-semibold">Offline {target.node.name}</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              This node holds {target.affectedSessions} local Workspace
              {target.affectedSessions === 1 ? "" : "s"} and runs {target.pendingPods.length}{" "}
              sandbox pod{target.pendingPods.length === 1 ? "" : "s"}.
            </p>
          </div>
        </div>
        {target.affectedSessions > 0 && (
          <div className="mt-3 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-300">
            Existing conversations cannot resume while this node is offline. No Workspace will be
            cleared automatically.
          </div>
        )}
        {target.pendingPods.length > 0 && (
          <div className="mt-3 rounded-md border border-border bg-muted px-3 py-2">
            <p className="mb-2 text-xs text-muted-foreground">
              Offlining cordons the node. Running sandboxes are not evicted and remain until they
              stop or are reclaimed.
            </p>
            <div className="max-h-24 overflow-y-auto">
              {target.pendingPods.map((pod) => (
                <div key={pod} className="truncate font-mono text-xs text-muted-foreground">
                  {pod}
                </div>
              ))}
            </div>
          </div>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="ghost" size="sm" disabled={acting} onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="destructive" size="sm" disabled={acting} onClick={onConfirm}>
            {acting ? "Offlining..." : "Cordon node"}
          </Button>
        </div>
      </div>
    </div>
  );
}

function AddNodeDialog({
  join,
  onCancel,
  onCopy,
}: {
  join: JoinCommand | null;
  onCancel: () => void;
  onCopy: () => void;
}) {
  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-black/40 px-4">
      <div
        role="dialog"
        aria-modal="true"
        className="w-full max-w-2xl rounded-lg border border-border bg-background p-4 shadow-xl"
      >
        <div className="flex items-start gap-3">
          <div className="grid size-9 shrink-0 place-items-center rounded-md bg-muted">
            <Plus className="size-4 text-muted-foreground" />
          </div>
          <div className="min-w-0 flex-1">
            <h2 className="text-sm font-semibold">Add node</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              Run the join command on the target machine. The node will appear here after k3s
              registers it.
            </p>
          </div>
        </div>
        <div className="mt-4 rounded-md border border-border bg-muted/50 px-3 py-2 text-sm text-muted-foreground">
          {join?.note ?? "Join command is not configured."}
        </div>
        <div className="mt-3">
          <code className="block max-h-48 overflow-auto whitespace-pre-wrap rounded-md border border-border bg-muted px-3 py-2 font-mono text-xs text-foreground">
            {join?.command ?? "COCOLA_K3S_JOIN_COMMAND is not set"}
          </code>
        </div>
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onCancel}>
            Close
          </Button>
          <Button variant="outline" size="sm" disabled={!join?.command} onClick={onCopy}>
            <Copy className="mr-2 size-4" />
            Copy command
          </Button>
        </div>
      </div>
    </div>
  );
}

function CapacityDialog({
  node,
  value,
  saving,
  onChange,
  onCancel,
  onSave,
}: {
  node: SandboxNode;
  value: string;
  saving: boolean;
  onChange: (value: string) => void;
  onCancel: () => void;
  onSave: () => void;
}) {
  const [phase, setPhase] = useState<"edit" | "confirm">("edit");
  const [inputError, setInputError] = useState("");
  const trimmedValue = value.trim();
  const effect = capacityEffect(node, trimmedValue);

  useEffect(() => {
    setPhase("edit");
    setInputError("");
  }, [node.name]);

  const reviewChange = () => {
    const validationError = capacityInputError(trimmedValue);
    if (validationError) {
      setInputError(validationError);
      return;
    }
    setInputError("");
    setPhase("confirm");
  };

  if (phase === "confirm") {
    return (
      <div className="fixed inset-0 z-[80] grid place-items-center bg-black/40 px-4">
        <div
          role="dialog"
          aria-modal="true"
          className="w-full max-w-lg rounded-lg border border-border bg-background p-4 shadow-xl"
        >
          <div className="flex items-start gap-3">
            <div className="grid size-9 shrink-0 place-items-center rounded-md bg-amber-500/10 text-amber-500">
              <AlertTriangle className="size-4" />
            </div>
            <div className="min-w-0 flex-1">
              <h2 className="text-sm font-semibold">Confirm sandbox capacity</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                Review the expected effect before applying this change to {node.name}.
              </p>
            </div>
          </div>
          <div className="mt-4 rounded-md border border-border bg-muted/50 px-3 py-2">
            <div className="text-xs font-medium uppercase text-muted-foreground">New limit</div>
            <div className="mt-1 font-mono text-sm">
              {trimmedValue === "" ? "Unlimited" : trimmedValue}
            </div>
          </div>
          <div className="mt-3 rounded-md border border-border px-3 py-2 text-sm">
            <div className="font-medium">{effect.title}</div>
            <p className="mt-1 text-muted-foreground">{effect.description}</p>
          </div>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="ghost" size="sm" disabled={saving} onClick={() => setPhase("edit")}>
              Cancel
            </Button>
            <Button variant="outline" size="sm" disabled={saving} onClick={onSave}>
              {saving ? "Saving..." : "Confirm"}
            </Button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-black/40 px-4">
      <div
        role="dialog"
        aria-modal="true"
        className="w-full max-w-lg rounded-lg border border-border bg-background p-4 shadow-xl"
      >
        <div className="flex items-start gap-3">
          <div className="grid size-9 shrink-0 place-items-center rounded-md bg-muted">
            <SlidersHorizontal className="size-4 text-muted-foreground" />
          </div>
          <div className="min-w-0 flex-1">
            <h2 className="text-sm font-semibold">Edit sandbox capacity</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              Configure the maximum number of running sandbox pods allowed on {node.name}.
            </p>
          </div>
        </div>
        <div className="mt-4 space-y-2 rounded-md border border-border bg-muted/50 px-3 py-2 text-sm text-muted-foreground">
          <p>Leave the value empty to allow unlimited sandbox pods on this node.</p>
          <p>Set 0 to make this node contribute no sandbox capacity.</p>
          <p>Set a positive integer to cap concurrent running sandbox pods.</p>
        </div>
        <label className="mt-4 block text-sm font-medium" htmlFor="sandbox-capacity-input">
          Max sandbox pods
        </label>
        <input
          id="sandbox-capacity-input"
          type="number"
          min={0}
          step={1}
          inputMode="numeric"
          value={value}
          placeholder="Unlimited"
          disabled={saving}
          onChange={(event) => {
            setInputError("");
            onChange(event.target.value);
          }}
          className="mt-2 h-10 w-full rounded-md border border-border bg-background px-3 text-sm outline-none transition-colors placeholder:text-muted-foreground focus:border-ring"
        />
        {inputError && <p className="mt-2 text-sm text-destructive">{inputError}</p>}
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="ghost" size="sm" disabled={saving} onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="outline" size="sm" disabled={saving} onClick={reviewChange}>
            Continue
          </Button>
        </div>
      </div>
    </div>
  );
}

function capacityInputError(raw: string) {
  if (raw === "") return "";
  const max = Number(raw);
  if (!Number.isInteger(max) || max < 0) {
    return "Max sandbox pods must be a non-negative integer";
  }
  return "";
}

function capacityEffect(node: SandboxNode, raw: string) {
  if (raw === "") {
    return {
      title: "The node limit will be cleared.",
      description:
        "Existing sandbox pods keep running, and cocola may create new sandboxes when the cluster has available capacity.",
    };
  }

  const max = Number(raw);
  if (max === 0) {
    return {
      title: "The node will contribute zero sandbox capacity.",
      description:
        "Existing sandbox pods keep running. For strict no-new-pods scheduling, use Disable to cordon the node.",
    };
  }

  if (node.sandbox_pods > max) {
    return {
      title: "The new limit is below current usage.",
      description: `This node currently has ${node.sandbox_pods} sandbox pods. Existing pods keep running, and this node will not contribute free capacity until usage drops below ${max}.`,
    };
  }

  if (node.sandbox_pods === max) {
    return {
      title: "The node will be exactly at capacity.",
      description:
        "Existing sandbox pods keep running, and this node will not contribute free capacity until one of them exits.",
    };
  }

  return {
    title: "The node will keep available sandbox capacity.",
    description: `This node currently has ${node.sandbox_pods} sandbox pods and will allow up to ${max}.`,
  };
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
      error?: { code?: string; message?: string };
      message?: string;
      error_description?: string;
    };
    if (body.error?.code === "NOT_CONFIGURED") {
      return "Cluster management is not supported in the current runtime mode.";
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

function labelName(key: string, value: string) {
  if (key.startsWith("node-role.kubernetes.io/"))
    return key.slice("node-role.kubernetes.io/".length) || "role";
  if (key === "kubernetes.io/arch") return value || "arch";
  return value ? `${key}=${value}` : key;
}
