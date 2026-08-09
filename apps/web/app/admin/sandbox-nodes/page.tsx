"use client";

import { Cpu as SandboxNodesPageIcon } from "lucide-react";
import { Button, Card, Chip, Input, Label, TextField, Tooltip } from "@heroui/react";
import { type DataGridColumn } from "@cocola/ui-compat/data-grid";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { Sheet } from "@cocola/ui-compat/sheet";
import {
  AdminDataGrid,
  AdminEmptyState,
  AdminErrorDialog,
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
  AdminRowActions,
  AdminStatusBadge,
  AdminTruncatedValue,
} from "@/components/admin/admin-ui";
import { ActionConfirmDialog } from "@/components/ui/action-dialog";
import { cn } from "@/lib/utils";
import {
  AlertTriangle,
  Ban,
  CheckCircle2,
  LoaderCircle,
  Plus,
  Power,
  Server,
  SlidersHorizontal,
} from "lucide-react";
import { signOut } from "next-auth/react";
import { useCallback, useEffect, useState } from "react";

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

export default function SandboxNodesPage() {
  const [nodes, setNodes] = useState<SandboxNode[]>([]);
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
      const nodesRes = await fetch("/api/admin/sandbox-nodes", { cache: "no-store" });
      if (isAccountDisabledResponse(nodesRes)) return redirectAccountDisabled();
      if (await isUnsupportedResponse(nodesRes)) {
        setUnsupported(true);
        setNodes([]);
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
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

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

  const columns: DataGridColumn<SandboxNode>[] = [
    {
      id: "node",
      header: "Node",
      isRowHeader: true,
      minWidth: 240,
      cell: (node) => (
        <span className="block min-w-0">
          <AdminTruncatedValue
            className="font-mono text-xs font-semibold"
            copyLabel="node name"
            value={node.name}
          />
          <span className="mt-1 flex flex-wrap gap-1">
            {Object.entries(node.labels ?? {})
              .filter(
                ([key]) =>
                  key.startsWith("node-role.kubernetes.io/") || key === "kubernetes.io/arch",
              )
              .slice(0, 3)
              .map(([key, value]) => (
                <Chip key={key} size="sm" variant="soft">
                  {labelName(key, value)}
                </Chip>
              ))}
          </span>
        </span>
      ),
    },
    {
      id: "status",
      header: "Status",
      width: 150,
      cell: (node) => (
        <AdminStatusBadge tone={STATUS_TONES[node.status] ?? "neutral"} dot>
          {STATUS_LABELS[node.status] ?? node.status}
        </AdminStatusBadge>
      ),
    },
    {
      id: "resources",
      header: "Resources",
      minWidth: 190,
      cell: (node) => (
        <span className="text-xs">
          <span className="block">
            <span className="text-muted">CPU</span> {node.cpu_allocatable || "—"} /{" "}
            {node.cpu_capacity || "—"}
          </span>
          <span className="mt-1 block">
            <span className="text-muted">Memory</span> {node.memory_allocatable || "—"} /{" "}
            {node.memory_capacity || "—"}
          </span>
        </span>
      ),
    },
    {
      id: "workload",
      header: "Workload",
      minWidth: 180,
      cell: (node) => (
        <span className="text-xs">
          <span className="block">{node.sandbox_pods} sandbox pods</span>
          <span className="text-muted mt-1 block">
            {node.session_count} workspaces · {formatBytes(node.session_requested_bytes)}
          </span>
        </span>
      ),
    },
    {
      id: "disk",
      header: "Disk",
      width: 120,
      cell: (node) => (
        <AdminStatusBadge tone={node.disk_pressure ? "red" : "green"} dot>
          {node.disk_pressure ? "Pressure" : "Normal"}
        </AdminStatusBadge>
      ),
    },
    {
      id: "capacity",
      header: "Max pods",
      minWidth: 150,
      cell: (node) =>
        node.labels?.["cocola.dev/runtime-mode"] === "compose" ? (
          <span className="text-muted text-xs">Managed by Compose</span>
        ) : (
          <span className="flex items-center gap-2">
            <span className="font-mono text-xs">
              {node.max_sandbox_pods == null ? "Unlimited" : node.max_sandbox_pods}
            </span>
            <Tooltip delay={0}>
              <Button
                isIconOnly
                aria-label={`Edit capacity for ${node.name}`}
                isDisabled={Boolean(savingCapacity)}
                size="sm"
                variant="outline"
                onPress={() => openCapacityDialog(node)}
              >
                <SlidersHorizontal className="size-4" />
              </Button>
              <Tooltip.Content>Edit sandbox capacity</Tooltip.Content>
            </Tooltip>
          </span>
        ),
    },
    {
      id: "reason",
      header: "Reason",
      minWidth: 190,
      cell: (node) => (
        <AdminTruncatedValue
          className="text-muted text-xs"
          copyLabel="node reason"
          value={node.reason || "—"}
        />
      ),
    },
    {
      id: "actions",
      header: "Actions",
      align: "center",
      width: 72,
      cell: (node) => {
        const composeNode = node.labels?.["cocola.dev/runtime-mode"] === "compose";
        const alreadyOffline = ["offline", "offline_pending"].includes(node.status);
        return composeNode ? (
          <span className="text-muted text-xs">Single-node runtime</span>
        ) : (
          <AdminRowActions
            label={`Actions for node ${node.name}`}
            busy={Boolean(actingNode)}
            actions={[
              node.schedulable
                ? {
                    id: "disable",
                    label: "Disable scheduling",
                    icon: <Ban className="size-4" />,
                  }
                : {
                    id: "restore",
                    label: "Restore scheduling",
                    icon: <CheckCircle2 className="size-4" />,
                  },
              {
                id: "offline",
                label: alreadyOffline ? "Node is offline" : "Take node offline",
                icon: <Power className="size-4" />,
                disabled: alreadyOffline,
                destructive: true,
              },
            ]}
            onAction={(action) => {
              if (action === "disable" || action === "restore") {
                void runNodeAction(node, action);
              }
              if (action === "offline") void runNodeAction(node, "offline", false);
            }}
          />
        );
      },
    },
  ];

  return (
    <AdminPage className="admin-theme-sky">
      <AdminPageHeader
        icon={<SandboxNodesPageIcon className="size-5" />}
        title="Nodes"
        description="Runtime capacity and node health for Cocola sandboxes"
        actions={
          <>
            <AdminRefreshButton
              variant="outline"
              refreshing={loading}
              disabled={loading}
              onClick={() => void refresh()}
            >
              Refresh
            </AdminRefreshButton>
            <Button onPress={() => setShowAddNode(true)}>
              <Plus className="size-4" />
              Add node
            </Button>
          </>
        }
      />

      <AdminErrorDialog
        error={error}
        title="Node operation failed"
        onDismiss={() => setError("")}
        onRetry={() => void refresh()}
      />
      {notice && !error ? (
        <div className="flex items-center gap-2 rounded-xl border border-emerald-500/30 bg-emerald-500/10 px-4 py-3 text-sm text-emerald-700">
          <CheckCircle2 className="size-4 shrink-0" />
          <span>{notice}</span>
        </div>
      ) : null}

      {unsupported ? (
        <UnsupportedState />
      ) : (
        <AdminDataGrid
          aria-label="Sandbox nodes"
          columns={columns}
          contentClassName="min-w-[1160px]"
          data={nodes}
          getRowId={(node) => node.name}
          selectionMode="none"
          variant="primary"
          renderEmptyState={() => (
            <EmptyState>
              <EmptyState.Header>
                <EmptyState.Media variant="icon">
                  <SandboxNodesPageIcon className="text-sky-500" />
                </EmptyState.Media>
                <EmptyState.Title>{loading ? "Loading nodes" : "No nodes found"}</EmptyState.Title>
                <EmptyState.Description>
                  {loading
                    ? "Fetching cluster capacity…"
                    : "Nodes will appear here once they join the k3s cluster."}
                </EmptyState.Description>
              </EmptyState.Header>
            </EmptyState>
          )}
        />
      )}

      {offlineTarget && (
        <OfflineDialog
          target={offlineTarget}
          acting={actingNode === `${offlineTarget.node.name}:offline`}
          onCancel={() => setOfflineTarget(null)}
          onConfirm={() => void runNodeAction(offlineTarget.node, "offline", true)}
        />
      )}
      {showAddNode && <AddNodeDialog onClose={() => setShowAddNode(false)} />}
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

function UnsupportedState() {
  return (
    <Card className="p-8">
      <AdminEmptyState
        icon={<Server className="text-sky-500" />}
        title="Cluster management is unavailable"
        description="Start Cocola with the k3s runtime profile to enable node operations."
      />
    </Card>
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
    <ActionConfirmDialog
      open
      busy={acting}
      title={`Offline ${target.node.name}?`}
      description={
        <span className="grid gap-2">
          <span>
            This node holds {target.affectedSessions} local Workspace
            {target.affectedSessions === 1 ? "" : "s"} and runs {target.pendingPods.length} sandbox
            pod{target.pendingPods.length === 1 ? "" : "s"}.
          </span>
          {target.affectedSessions > 0 ? (
            <span className="text-warning">
              Existing conversations cannot resume while this node is offline. No Workspace will be
              cleared automatically.
            </span>
          ) : null}
          {target.pendingPods.length > 0 ? (
            <span>
              Running sandboxes are not evicted and remain until they stop or are reclaimed.
            </span>
          ) : null}
        </span>
      }
      confirmLabel="Cordon node"
      icon={AlertTriangle}
      showHint={false}
      tone="danger"
      onOpenChange={(open) => !open && onCancel()}
      onConfirm={onConfirm}
    />
  );
}

function AddNodeDialog({ onClose }: { onClose: () => void }) {
  return (
    <Sheet isOpen placement="right" onOpenChange={(open) => !open && onClose()}>
      <Sheet.Backdrop>
        <Sheet.Content className="w-full md:w-[440px]">
          <Sheet.Dialog>
            <Sheet.CloseTrigger aria-label="Close add node" />
            <Sheet.Header>
              <span className="flex items-center gap-3">
                <span className="bg-accent-soft text-accent grid size-10 place-items-center rounded-2xl">
                  <Plus className="size-5" />
                </span>
                <Sheet.Heading>Node onboarding is coming soon</Sheet.Heading>
              </span>
            </Sheet.Header>
            <Sheet.Body>
              <p className="text-muted text-sm leading-6">
                We&apos;re building a guided and secure way to add nodes to your Cocola cluster.
                This feature is not available yet.
              </p>
            </Sheet.Body>
            <Sheet.Footer>
              <Button variant="outline" onPress={onClose}>
                Close
              </Button>
            </Sheet.Footer>
          </Sheet.Dialog>
        </Sheet.Content>
      </Sheet.Backdrop>
    </Sheet>
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

  return (
    <>
      <Sheet
        isOpen={phase === "edit"}
        placement="right"
        onOpenChange={(open) => {
          if (!open && phase === "edit") onCancel();
        }}
      >
        <Sheet.Backdrop>
          <Sheet.Content className="w-full md:w-[500px]">
            <Sheet.Dialog>
              <Sheet.CloseTrigger aria-label="Close capacity editor" />
              <Sheet.Header>
                <span className="flex items-center gap-3">
                  <span className="bg-accent-soft text-accent grid size-10 place-items-center rounded-2xl">
                    <SlidersHorizontal className="size-5" />
                  </span>
                  <span>
                    <Sheet.Heading>Edit sandbox capacity</Sheet.Heading>
                    <span className="text-muted mt-1 block text-sm">
                      Configure the maximum number of running sandbox pods allowed on {node.name}.
                    </span>
                  </span>
                </span>
              </Sheet.Header>
              <Sheet.Body className="grid content-start gap-4">
                <div className="bg-surface-secondary text-muted space-y-2 rounded-2xl p-4 text-sm">
                  <p>Leave the value empty to allow unlimited sandbox pods.</p>
                  <p>Set 0 to contribute no new sandbox capacity.</p>
                  <p>Set a positive integer to cap concurrent running sandbox pods.</p>
                </div>
                <TextField
                  isDisabled={saving}
                  value={value}
                  variant="secondary"
                  onChange={(next) => {
                    setInputError("");
                    onChange(next);
                  }}
                >
                  <Label>Max sandbox pods</Label>
                  <Input
                    type="number"
                    min={0}
                    step={1}
                    inputMode="numeric"
                    placeholder="Unlimited"
                  />
                </TextField>
                {inputError ? <p className="text-danger text-sm">{inputError}</p> : null}
              </Sheet.Body>
              <Sheet.Footer className="gap-2">
                <Button isDisabled={saving} variant="outline" onPress={onCancel}>
                  Cancel
                </Button>
                <Button isDisabled={saving} onPress={reviewChange}>
                  Continue
                </Button>
              </Sheet.Footer>
            </Sheet.Dialog>
          </Sheet.Content>
        </Sheet.Backdrop>
      </Sheet>
      <ActionConfirmDialog
        open={phase === "confirm"}
        busy={saving}
        title="Confirm sandbox capacity"
        description={`${trimmedValue === "" ? "Unlimited" : trimmedValue} · ${effect.title} ${effect.description}`}
        confirmLabel="Apply capacity"
        icon={AlertTriangle}
        showHint={false}
        tone="warning"
        onOpenChange={(open) => {
          if (!open && !saving) setPhase("edit");
        }}
        onConfirm={onSave}
      />
    </>
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
