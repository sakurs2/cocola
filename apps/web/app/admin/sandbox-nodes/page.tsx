"use client";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  AlertTriangle,
  Ban,
  CheckCircle2,
  Copy,
  LoaderCircle,
  Plus,
  Power,
  RefreshCw,
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
  cpu_capacity: string;
  memory_capacity: string;
  cpu_allocatable: string;
  memory_allocatable: string;
  sandbox_pods: number;
  max_sandbox_pods?: number | null;
  reason?: string;
  labels?: Record<string, string>;
};

type NodeListResponse = { nodes: SandboxNode[] };
type JoinCommand = { command: string; note: string };
type OfflineNodeResult = {
  node: SandboxNode;
  evicted_pods?: string[];
  pending_pods?: string[];
  message: string;
};
type OfflineTarget = { node: SandboxNode; pendingPods: string[] };

const STATUS_LABELS: Record<string, string> = {
  active: "Active",
  disabled: "Disabled",
  offline_pending: "Offline pending",
  offline: "Offline",
  unhealthy: "Unhealthy",
};

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
        if (!force && pendingPods.length > 0) {
          await refresh();
          setOfflineTarget({ node: body.node, pendingPods });
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
    <main className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border">
        <div className="mx-auto flex h-16 max-w-7xl items-center gap-3 px-6">
          <div className="grid size-9 place-items-center rounded-md bg-primary text-primary-foreground">
            <Server className="size-4" />
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-base font-semibold">Sandbox Nodes</h1>
            <p className="truncate text-xs text-muted-foreground">
              k3s node operations for OpenSandbox Kubernetes runtime
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
        {notice && (
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
              <Metric label="Nodes" value={String(totals.nodes)} />
              <Metric label="Active" value={String(totals.active)} />
              <Metric label="Disabled/Offline" value={String(totals.unavailable)} />
              <Metric label="Unhealthy" value={String(totals.unhealthy)} />
              <Metric label="Sandbox Pods" value={String(totals.sandboxPods)} />
            </section>

            <section className="rounded-lg border border-border bg-card px-4 py-3">
              <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
                <div className="grid size-8 place-items-center rounded-md bg-muted">
                  <Plus className="size-4 text-muted-foreground" />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium">Add node</div>
                  <div className="text-xs text-muted-foreground">
                    Join an existing machine to this k3s cluster.
                  </div>
                </div>
                <Button variant="outline" size="sm" onClick={() => setShowAddNode(true)}>
                  <Plus className="mr-2 size-4" />
                  Add node
                </Button>
              </div>
            </section>

            <section className="overflow-hidden rounded-lg border border-border bg-card">
              <table className="w-full min-w-[1080px] text-sm">
                <thead className="border-b border-border bg-muted/50 text-xs text-muted-foreground">
                  <tr>
                    <th className="px-4 py-3 text-left font-medium">Node</th>
                    <th className="px-4 py-3 text-left font-medium">Status</th>
                    <th className="px-4 py-3 text-left font-medium">CPU</th>
                    <th className="px-4 py-3 text-left font-medium">Memory</th>
                    <th className="px-4 py-3 text-left font-medium">Sandbox Pods</th>
                    <th className="px-4 py-3 text-left font-medium">Max Sandbox Pods</th>
                    <th className="px-4 py-3 text-left font-medium">Reason</th>
                    <th className="px-4 py-3 text-right font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {loading && nodes.length === 0 ? (
                    <tr>
                      <td colSpan={8} className="px-4 py-10 text-center text-muted-foreground">
                        Loading nodes...
                      </td>
                    </tr>
                  ) : nodes.length === 0 ? (
                    <tr>
                      <td colSpan={8} className="px-4 py-10 text-center text-muted-foreground">
                        No nodes found
                      </td>
                    </tr>
                  ) : (
                    nodes.map((node) => {
                      const offlining = actingNode === `${node.name}:offline`;
                      const alreadyOffline = node.status === "offline";
                      return (
                        <tr key={node.name} className="border-b border-border/70 last:border-0">
                          <td className="px-4 py-3">
                            <div className="font-medium">{node.name}</div>
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
                          </td>
                          <td className="px-4 py-3">
                            <StatusPill node={node} />
                          </td>
                          <td className="px-4 py-3 font-mono text-xs">
                            {node.cpu_allocatable || "-"} / {node.cpu_capacity || "-"}
                          </td>
                          <td className="px-4 py-3 font-mono text-xs">
                            {node.memory_allocatable || "-"} / {node.memory_capacity || "-"}
                          </td>
                          <td className="px-4 py-3">{node.sandbox_pods}</td>
                          <td className="px-4 py-3">
                            <div className="flex items-center gap-2">
                              <span
                                className={cn(
                                  "min-w-20 font-mono text-xs",
                                  node.max_sandbox_pods == null
                                    ? "text-muted-foreground"
                                    : "text-foreground",
                                )}
                              >
                                {node.max_sandbox_pods == null
                                  ? "Unlimited"
                                  : node.max_sandbox_pods}
                              </span>
                              <Button
                                variant="outline"
                                size="sm"
                                disabled={Boolean(savingCapacity)}
                                onClick={() => openCapacityDialog(node)}
                              >
                                <SlidersHorizontal className="mr-2 size-4" />
                                Edit
                              </Button>
                            </div>
                          </td>
                          <td
                            className="max-w-[220px] truncate px-4 py-3 text-muted-foreground"
                            title={node.reason}
                          >
                            {node.reason || "-"}
                          </td>
                          <td className="px-4 py-3">
                            <div className="flex justify-end gap-2">
                              {node.schedulable ? (
                                <Button
                                  variant="outline"
                                  size="sm"
                                  disabled={Boolean(actingNode)}
                                  onClick={() => void runNodeAction(node, "disable")}
                                >
                                  <Ban className="mr-2 size-4" />
                                  Disable
                                </Button>
                              ) : (
                                <Button
                                  variant="outline"
                                  size="sm"
                                  disabled={Boolean(actingNode)}
                                  onClick={() => void runNodeAction(node, "restore")}
                                >
                                  <CheckCircle2 className="mr-2 size-4" />
                                  Restore
                                </Button>
                              )}
                              <Button
                                variant="destructive"
                                size="sm"
                                disabled={Boolean(actingNode) || alreadyOffline}
                                title={alreadyOffline ? "Node is already offline" : undefined}
                                onClick={() => void runNodeAction(node, "offline", false)}
                              >
                                {offlining ? (
                                  <LoaderCircle className="mr-2 size-4 animate-spin" />
                                ) : (
                                  <Power className="mr-2 size-4" />
                                )}
                                {alreadyOffline
                                  ? "Offline"
                                  : offlining
                                    ? "Offlining..."
                                    : "Offline"}
                              </Button>
                            </div>
                          </td>
                        </tr>
                      );
                    })
                  )}
                </tbody>
              </table>
            </section>
          </>
        )}
      </div>

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

function UnsupportedState() {
  return (
    <section className="rounded-lg border border-border bg-card px-4 py-10 text-center">
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

function StatusPill({ node }: { node: SandboxNode }) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium",
        node.status === "active" && "bg-emerald-500/15 text-emerald-400",
        node.status === "disabled" && "bg-amber-500/15 text-amber-400",
        node.status === "offline_pending" && "bg-sky-500/15 text-sky-400",
        node.status === "offline" && "bg-muted text-muted-foreground",
        node.status === "unhealthy" && "bg-destructive/15 text-destructive",
      )}
    >
      {STATUS_LABELS[node.status] ?? node.status}
    </span>
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
              This will cordon the node and request eviction for {target.pendingPods.length} sandbox
              pod{target.pendingPods.length === 1 ? "" : "s"}.
            </p>
          </div>
        </div>
        {target.pendingPods.length > 0 && (
          <div className="mt-3 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-300">
            Running sandboxes may lose local-path workspace state if storage is node-local.
          </div>
        )}
        {target.pendingPods.length > 0 && (
          <div className="mt-3 max-h-32 overflow-y-auto rounded-md border border-border bg-muted px-3 py-2">
            {target.pendingPods.map((pod) => (
              <div key={pod} className="truncate font-mono text-xs text-muted-foreground">
                {pod}
              </div>
            ))}
          </div>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="ghost" size="sm" disabled={acting} onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="destructive" size="sm" disabled={acting} onClick={onConfirm}>
            {acting ? "Offlining..." : "Offline"}
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
