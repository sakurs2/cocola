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
  AdminToast,
  AdminTruncatedValue,
} from "@/components/admin/admin-ui";
import { ActionConfirmDialog } from "@/components/ui/action-dialog";
import { cn } from "@/lib/utils";
import {
  AlertTriangle,
  Ban,
  CheckCircle2,
  LoaderCircle,
  Power,
  Server,
  SlidersHorizontal,
} from "lucide-react";
import { signOut } from "next-auth/react";
import { useCallback, useEffect, useState } from "react";
import { useTranslations } from "next-intl";

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

const STATUS_TONES: Record<string, BadgeTone> = {
  active: "green",
  disabled: "amber",
  offline_pending: "sky",
  offline: "neutral",
  unhealthy: "red",
};

export default function SandboxNodesPage() {
  const t = useTranslations("admin.nodesPage");
  const [nodes, setNodes] = useState<SandboxNode[]>([]);
  const [loading, setLoading] = useState(true);
  const [actingNode, setActingNode] = useState<string | null>(null);
  const [savingCapacity, setSavingCapacity] = useState<string | null>(null);
  const [capacityDrafts, setCapacityDrafts] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [unsupported, setUnsupported] = useState(false);
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
        setNotice(body.message || t("offlineRequested"));
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
      setError(t("capacity.invalid"));
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
      setNotice(max == null ? t("capacityCleared") : t("capacitySaved"));
      await refresh();
      return true;
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      return false;
    } finally {
      setSavingCapacity(null);
    }
  };

  const composeOnly =
    nodes.length > 0 &&
    nodes.every((node) => node.labels?.["cocola.dev/runtime-mode"] === "compose");

  const allColumns: DataGridColumn<SandboxNode>[] = [
    {
      id: "node",
      header: t("columns.node"),
      isRowHeader: true,
      minWidth: 240,
      cell: (node) => (
        <span className="block min-w-0">
          <AdminTruncatedValue
            className="font-mono text-xs font-semibold"
            copyLabel={t("copy.nodeName")}
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
      header: t("columns.status"),
      width: 150,
      cell: (node) => (
        <AdminStatusBadge tone={STATUS_TONES[node.status] ?? "neutral"} dot>
          {node.status === "active" ||
          node.status === "disabled" ||
          node.status === "offline_pending" ||
          node.status === "offline" ||
          node.status === "unhealthy"
            ? t(`status.${node.status}`)
            : node.status}
        </AdminStatusBadge>
      ),
    },
    {
      id: "resources",
      header: t("columns.resources"),
      minWidth: 190,
      cell: (node) => (
        <span className="text-xs">
          <span className="block">
            <span className="text-muted">CPU</span> {node.cpu_allocatable || "—"} /{" "}
            {node.cpu_capacity || "—"}
          </span>
          <span className="mt-1 block">
            <span className="text-muted">{t("memory")}</span>{" "}
            {formatMemoryQuantity(node.memory_allocatable)} /{" "}
            {formatMemoryQuantity(node.memory_capacity)}
          </span>
        </span>
      ),
    },
    {
      id: "workload",
      header: t("columns.workload"),
      minWidth: 180,
      cell: (node) => (
        <span className="text-xs">
          <span className="block">{t("sandboxPods", { count: node.sandbox_pods })}</span>
          <span className="text-muted mt-1 block">
            {t("workspaces", { count: node.session_count })} ·{" "}
            {formatBytes(node.session_requested_bytes)}
          </span>
        </span>
      ),
    },
    {
      id: "disk",
      header: t("columns.disk"),
      width: 120,
      cell: (node) => (
        <AdminStatusBadge tone={node.disk_pressure ? "red" : "green"} dot>
          {node.disk_pressure ? t("disk.pressure") : t("disk.normal")}
        </AdminStatusBadge>
      ),
    },
    {
      id: "capacity",
      header: t("columns.maxPods"),
      minWidth: 150,
      cell: (node) =>
        node.labels?.["cocola.dev/runtime-mode"] === "compose" ? (
          <span className="text-muted text-xs">—</span>
        ) : (
          <span className="flex items-center gap-2">
            <span className="font-mono text-xs">
              {node.max_sandbox_pods == null ? t("unlimited") : node.max_sandbox_pods}
            </span>
            <Tooltip delay={0}>
              <Button
                isIconOnly
                aria-label={t("actions.editCapacityFor", { node: node.name })}
                isDisabled={Boolean(savingCapacity)}
                size="sm"
                variant="outline"
                onPress={() => openCapacityDialog(node)}
              >
                <SlidersHorizontal className="size-4" />
              </Button>
              <Tooltip.Content>{t("actions.editCapacity")}</Tooltip.Content>
            </Tooltip>
          </span>
        ),
    },
    {
      id: "reason",
      header: t("columns.reason"),
      minWidth: 190,
      cell: (node) => (
        <AdminTruncatedValue
          className="text-muted text-xs"
          copyLabel={t("copy.reason")}
          value={node.reason || "—"}
        />
      ),
    },
    {
      id: "actions",
      header: t("columns.actions"),
      align: "center",
      width: 72,
      cell: (node) => {
        const composeNode = node.labels?.["cocola.dev/runtime-mode"] === "compose";
        const alreadyOffline = ["offline", "offline_pending"].includes(node.status);
        return composeNode ? (
          <span className="text-muted text-xs">—</span>
        ) : (
          <AdminRowActions
            label={t("actions.forNode", { node: node.name })}
            busy={Boolean(actingNode)}
            actions={[
              node.schedulable
                ? {
                    id: "disable",
                    label: t("actions.disable"),
                    icon: <Ban className="size-4" />,
                  }
                : {
                    id: "restore",
                    label: t("actions.restore"),
                    icon: <CheckCircle2 className="size-4" />,
                  },
              {
                id: "offline",
                label: alreadyOffline ? t("actions.offline") : t("actions.takeOffline"),
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
  const columns = allColumns.filter(
    (column) => !composeOnly || (column.id !== "capacity" && column.id !== "actions"),
  );

  return (
    <AdminPage className="admin-theme-sky">
      <AdminPageHeader
        icon={<SandboxNodesPageIcon className="size-5" />}
        title={t("title")}
        description={t("description")}
        actions={
          <AdminRefreshButton
            variant="outline"
            refreshing={loading}
            disabled={loading}
            onClick={() => void refresh()}
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
        message={!error ? notice : undefined}
        tone="success"
        onDismiss={() => setNotice("")}
      />

      {unsupported ? (
        <UnsupportedState />
      ) : (
        <AdminDataGrid
          aria-label={t("tableAria")}
          columns={columns}
          contentClassName={composeOnly ? "min-w-[920px]" : "min-w-[1160px]"}
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

      {offlineTarget && (
        <OfflineDialog
          target={offlineTarget}
          acting={actingNode === `${offlineTarget.node.name}:offline`}
          onCancel={() => setOfflineTarget(null)}
          onConfirm={() => void runNodeAction(offlineTarget.node, "offline", true)}
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

function UnsupportedState() {
  const t = useTranslations("admin.nodesPage");
  return (
    <Card className="p-8">
      <AdminEmptyState
        icon={<Server className="text-sky-500" />}
        title={t("unsupported.title")}
        description={t("unsupported.description")}
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
  const t = useTranslations("admin.nodesPage.offline");
  return (
    <ActionConfirmDialog
      open
      busy={acting}
      title={t("title", { node: target.node.name })}
      description={
        <span className="grid gap-2">
          <span>
            {t("summary", { workspaces: target.affectedSessions, pods: target.pendingPods.length })}
          </span>
          {target.affectedSessions > 0 ? (
            <span className="text-warning">{t("workspaceWarning")}</span>
          ) : null}
          {target.pendingPods.length > 0 ? <span>{t("sandboxWarning")}</span> : null}
        </span>
      }
      confirmLabel={t("action")}
      icon={AlertTriangle}
      showHint={false}
      tone="danger"
      onOpenChange={(open) => !open && onCancel()}
      onConfirm={onConfirm}
    />
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
  const t = useTranslations("admin.nodesPage.capacity");
  const [phase, setPhase] = useState<"edit" | "confirm">("edit");
  const [inputError, setInputError] = useState("");
  const trimmedValue = value.trim();
  const effect = capacityEffect(node, trimmedValue, t);

  useEffect(() => {
    setPhase("edit");
    setInputError("");
  }, [node.name]);

  const reviewChange = () => {
    const validationError = capacityInputError(trimmedValue);
    if (validationError) {
      setInputError(t("invalid"));
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
              <Sheet.CloseTrigger aria-label={t("close")} />
              <Sheet.Header>
                <span className="flex items-center gap-3">
                  <span className="bg-accent-soft text-accent grid size-10 place-items-center rounded-2xl">
                    <SlidersHorizontal className="size-5" />
                  </span>
                  <span>
                    <Sheet.Heading>{t("title")}</Sheet.Heading>
                    <span className="text-muted mt-1 block text-sm">
                      {t("description", { node: node.name })}
                    </span>
                  </span>
                </span>
              </Sheet.Header>
              <Sheet.Body className="grid content-start gap-4">
                <div className="bg-surface-secondary text-muted space-y-2 rounded-2xl p-4 text-sm">
                  <p>{t("unlimitedHint")}</p>
                  <p>{t("zeroHint")}</p>
                  <p>{t("positiveHint")}</p>
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
                  <Label>{t("field")}</Label>
                  <Input
                    type="number"
                    min={0}
                    step={1}
                    inputMode="numeric"
                    placeholder={t("unlimited")}
                  />
                </TextField>
                {inputError ? <p className="text-danger text-sm">{inputError}</p> : null}
              </Sheet.Body>
              <Sheet.Footer className="gap-2">
                <Button isDisabled={saving} variant="outline" onPress={onCancel}>
                  {t("cancel")}
                </Button>
                <Button isDisabled={saving} onPress={reviewChange}>
                  {t("continue")}
                </Button>
              </Sheet.Footer>
            </Sheet.Dialog>
          </Sheet.Content>
        </Sheet.Backdrop>
      </Sheet>
      <ActionConfirmDialog
        open={phase === "confirm"}
        busy={saving}
        title={t("confirmTitle")}
        description={`${trimmedValue === "" ? t("unlimited") : trimmedValue} · ${effect.title} ${effect.description}`}
        confirmLabel={t("apply")}
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
    return "invalid";
  }
  return "";
}

function capacityEffect(
  node: SandboxNode,
  raw: string,
  t: ReturnType<typeof useTranslations<"admin.nodesPage.capacity">>,
) {
  if (raw === "") {
    return {
      title: t("effects.clearedTitle"),
      description: t("effects.clearedDescription"),
    };
  }

  const max = Number(raw);
  if (max === 0) {
    return {
      title: t("effects.zeroTitle"),
      description: t("effects.zeroDescription"),
    };
  }

  if (node.sandbox_pods > max) {
    return {
      title: t("effects.belowTitle"),
      description: t("effects.belowDescription", { pods: node.sandbox_pods, max }),
    };
  }

  if (node.sandbox_pods === max) {
    return {
      title: t("effects.exactTitle"),
      description: t("effects.exactDescription"),
    };
  }

  return {
    title: t("effects.availableTitle"),
    description: t("effects.availableDescription", { pods: node.sandbox_pods, max }),
  };
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  const amount = value / 1024 ** index;
  return `${amount >= 10 || index === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[index]}`;
}

function formatMemoryQuantity(value: string) {
  const raw = value.trim();
  if (!raw) return "—";
  const match = raw.match(/^(\d+(?:\.\d+)?)(Ki|Mi|Gi|Ti)$/);
  if (!match) return raw;
  const unitPower = { Ki: 1, Mi: 2, Gi: 3, Ti: 4 }[match[2] as "Ki" | "Mi" | "Gi" | "Ti"];
  return formatBytes(Number(match[1]) * 1024 ** unitPower);
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
