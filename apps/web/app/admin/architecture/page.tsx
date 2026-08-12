"use client";

import { Workflow as ArchitecturePageIcon } from "lucide-react";
import { ChevronRight, ExternalLink, LoaderCircle, Server } from "lucide-react";
import { Button, Card } from "@heroui/react";
import { Sheet } from "@cocola/ui-compat/sheet";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useFormatter, useTranslations } from "next-intl";
import {
  AdminErrorDialog,
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
  AdminStatusBadge,
} from "@/components/admin/admin-ui";

type Status = "healthy" | "degraded" | "unhealthy" | "unknown" | string;

type ArchitectureNode = {
  id: string;
  label: string;
  kind: string;
  layer: string;
  status: Status;
  detail?: string;
  endpoint?: string;
  admin_href?: string;
  external_href?: string;
  metadata?: Record<string, unknown>;
};

type ArchitectureEdge = {
  from: string;
  to: string;
  label?: string;
  kind?: string;
};

type ArchitectureGraph = {
  generated_at: string;
  nodes: ArchitectureNode[];
  edges: ArchitectureEdge[];
};

const LAYER_ORDER = [
  "Client / UI",
  "Control Plane",
  "Runtime Plane",
  "Sandbox Plane",
  "Infrastructure",
];
const LAYER_KEYS = {
  "Client / UI": "layers.Client / UI",
  "Control Plane": "layers.Control Plane",
  "Runtime Plane": "layers.Runtime Plane",
  "Sandbox Plane": "layers.Sandbox Plane",
  Infrastructure: "layers.Infrastructure",
} as const;

type BadgeTone = "neutral" | "sky" | "green" | "amber" | "red";

const STATUS_STYLES: Record<string, { dot: string; ring: string; tone: BadgeTone }> = {
  healthy: {
    dot: "bg-emerald-400",
    ring: "ring-emerald-400/40",
    tone: "green",
  },
  degraded: {
    dot: "bg-amber-400",
    ring: "ring-amber-400/40",
    tone: "amber",
  },
  unhealthy: {
    dot: "bg-red-400",
    ring: "ring-red-400/40",
    tone: "red",
  },
  unknown: {
    dot: "bg-slate-400",
    ring: "ring-slate-400/30",
    tone: "neutral",
  },
};

function statusStyle(status: Status) {
  return STATUS_STYLES[status] ?? STATUS_STYLES.unknown!;
}

function statusKey(status: Status) {
  if (status === "healthy" || status === "degraded" || status === "unhealthy") {
    return `status.${status}` as const;
  }
  return "status.unknown" as const;
}

export default function AdminArchitecturePage() {
  const t = useTranslations("admin.architecture");
  const format = useFormatter();
  const [graph, setGraph] = useState<ArchitectureGraph | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await fetch("/api/admin/architecture", { cache: "no-store" });
      if (!res.ok) throw new Error(await readError(res));
      const data = (await res.json()) as ArchitectureGraph;
      setGraph(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const columns = useMemo(() => {
    const grouped: ArchitectureNode[][] = LAYER_ORDER.map(() => []);
    const extras: ArchitectureNode[] = [];
    (graph?.nodes ?? []).forEach((node) => {
      const idx = LAYER_ORDER.indexOf(node.layer);
      if (idx >= 0) grouped[idx]!.push(node);
      else extras.push(node);
    });
    return { grouped, extras };
  }, [graph]);

  const selected = useMemo(
    () => graph?.nodes.find((node) => node.id === selectedId) ?? null,
    [graph, selectedId],
  );
  const rowCount = Math.max(1, ...columns.grouped.map((nodes) => nodes.length));

  return (
    <AdminPage className="admin-theme-fuchsia">
      <AdminPageHeader
        icon={<ArchitecturePageIcon className="size-5" />}
        title={t("title")}
        description={t("description")}
        actions={
          <AdminRefreshButton
            variant="outline"
            onClick={() => void load()}
            disabled={loading}
            refreshing={loading}
          >
            {t("refresh")}
          </AdminRefreshButton>
        }
      />

      <AdminErrorDialog
        error={error}
        title={t("loadFailed")}
        onDismiss={() => setError("")}
        onRetry={() => void load()}
      />

      <section className="min-w-0">
        <Card className="architecture-flow relative min-h-[620px] w-full max-w-full min-w-0 overflow-auto p-5">
          <Card.Content className="min-w-[920px] p-0">
            <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_1px_1px,var(--separator)_1px,transparent_0)] [background-size:32px_32px]" />
            <div className="pointer-events-none absolute left-3 top-3 z-10 rounded-md border border-border bg-background/95 px-3 py-2 text-xs text-muted shadow-sm backdrop-blur">
              {graph?.generated_at
                ? t("generated", {
                    time: format.dateTime(new Date(graph.generated_at), {
                      month: "short",
                      day: "2-digit",
                      hour: "2-digit",
                      minute: "2-digit",
                    }),
                  })
                : t("loadingGraph")}
            </div>

            {graph ? (
              <div className="relative min-w-max pt-10">
                <div className="grid grid-cols-5 gap-4">
                  {LAYER_ORDER.map((layer) => (
                    <p key={layer} className="text-muted px-1 text-center text-xs font-medium">
                      {t(LAYER_KEYS[layer as keyof typeof LAYER_KEYS])}
                    </p>
                  ))}
                </div>
                <div className="mt-3 grid grid-cols-5 auto-rows-[7rem] gap-4">
                  {Array.from({ length: rowCount }, (_, rowIndex) =>
                    columns.grouped.map((nodes, layerIndex) => {
                      const node = nodes[rowIndex];
                      return node ? (
                        <NodeCard
                          key={node.id}
                          node={node}
                          selected={node.id === selectedId}
                          onSelect={() => setSelectedId(node.id)}
                        />
                      ) : (
                        <span
                          key={`${LAYER_ORDER[layerIndex]}-${rowIndex}`}
                          aria-hidden="true"
                          className="h-28"
                        />
                      );
                    }),
                  )}
                </div>

                {columns.extras.length > 0 ? (
                  <div className="mt-8 border-t border-border pt-4">
                    <div className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[0.09em] text-muted/70">
                      {t("other")}
                    </div>
                    <div className="flex flex-wrap gap-4">
                      {columns.extras.map((node) => (
                        <NodeCard
                          key={node.id}
                          node={node}
                          selected={node.id === selectedId}
                          onSelect={() => setSelectedId(node.id)}
                          className="w-[200px]"
                        />
                      ))}
                    </div>
                  </div>
                ) : null}
              </div>
            ) : null}

            {loading && !graph ? (
              <div className="absolute inset-0 grid place-items-center bg-background/60">
                <div className="flex items-center gap-2 rounded-md border border-border bg-surface px-3 py-2 text-sm text-muted">
                  <LoaderCircle className="size-4 animate-spin" />
                  {t("loading")}
                </div>
              </div>
            ) : null}
          </Card.Content>
        </Card>
      </section>

      <Sheet
        isOpen={selected !== null}
        placement="right"
        onOpenChange={(open) => !open && setSelectedId(null)}
      >
        <Sheet.Backdrop>
          <Sheet.Content className="w-full md:w-[480px]">
            <Sheet.Dialog>
              <Sheet.CloseTrigger aria-label={t("closeDetails")} />
              <Sheet.Header>
                <Sheet.Heading>{selected?.label ?? t("componentStatus")}</Sheet.Heading>
                <p className="text-muted text-sm">
                  {selected ? `${selected.layer} · ${selected.kind}` : ""}
                </p>
              </Sheet.Header>
              <Sheet.Body>{selected ? <NodeDetail node={selected} /> : null}</Sheet.Body>
              <Sheet.Footer>
                <Button variant="outline" onPress={() => setSelectedId(null)}>
                  {t("close")}
                </Button>
              </Sheet.Footer>
            </Sheet.Dialog>
          </Sheet.Content>
        </Sheet.Backdrop>
      </Sheet>
    </AdminPage>
  );
}

function NodeCard({
  node,
  selected,
  onSelect,
  style,
  className,
}: {
  node: ArchitectureNode;
  selected: boolean;
  onSelect: () => void;
  style?: React.CSSProperties;
  className?: string;
}) {
  const t = useTranslations("admin.architecture");
  const s = statusStyle(node.status);
  return (
    <Card
      style={style}
      className={`admin-architecture-node-card group h-28 w-full min-w-0 overflow-hidden border p-0 ${
        selected ? "border-accent ring-accent ring-2" : "border-separator/70"
      } ${className ?? ""}`}
    >
      <button
        type="button"
        aria-pressed={selected}
        onClick={onSelect}
        className="flex h-full w-full min-w-0 flex-col items-start p-4 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-focus"
      >
        <span className="flex w-full items-center justify-between gap-2">
          <Server
            className={`admin-architecture-node-icon size-5 ${s.tone === "green" ? "text-success" : s.tone === "amber" ? "text-warning" : s.tone === "red" ? "text-danger" : "text-muted"}`}
          />
          <span className="flex items-center gap-1.5">
            <AdminStatusBadge tone={s.tone}>{t(statusKey(node.status))}</AdminStatusBadge>
            <ChevronRight
              aria-hidden="true"
              className="size-4 shrink-0 text-muted transition-transform group-hover:translate-x-0.5"
            />
          </span>
        </span>
        <span className="mt-auto min-w-0">
          <span className="block truncate text-sm font-semibold">{node.label}</span>
          <span className="text-muted mt-0.5 block truncate text-xs">{node.kind}</span>
        </span>
      </button>
    </Card>
  );
}

function NodeDetail({ node }: { node: ArchitectureNode }) {
  const t = useTranslations("admin.architecture");
  const s = statusStyle(node.status);
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium uppercase tracking-[0.08em] text-muted">
          {t("currentStatus")}
        </span>
        <AdminStatusBadge tone={s.tone} dot>
          {t(statusKey(node.status))}
        </AdminStatusBadge>
      </div>
      {node.detail ? <p className="text-sm text-foreground">{node.detail}</p> : null}
      {node.endpoint ? (
        <div className="rounded-md border border-border bg-background px-2.5 py-1.5 font-mono text-xs text-muted">
          {node.endpoint}
        </div>
      ) : null}
      <div className="flex flex-col gap-2">
        {node.admin_href ? (
          <Button className="w-full" onPress={() => window.location.assign(node.admin_href!)}>
            {t("openAdmin")}
          </Button>
        ) : null}
        {node.external_href ? (
          <Button
            className="w-full"
            variant="outline"
            onPress={() => window.open(node.external_href!, "_blank", "noopener,noreferrer")}
          >
            {t("externalConsole")}
            <ExternalLink className="size-3.5" />
          </Button>
        ) : null}
      </div>
    </div>
  );
}

async function readError(res: Response) {
  try {
    const data = (await res.json()) as {
      error?: string | { message?: string };
      message?: string;
    };
    if (typeof data.error === "string") return data.error;
    if (typeof data.error?.message === "string") return data.error.message;
    if (typeof data.message === "string") return data.message;
  } catch {
    // fall through
  }
  return `${res.status} ${res.statusText}`;
}
