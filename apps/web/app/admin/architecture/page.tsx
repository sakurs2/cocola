"use client";

import { Workflow as ArchitecturePageIcon } from "lucide-react";
import { ExternalLink, LoaderCircle, Server } from "lucide-react";
import { Button, Card } from "@heroui/react";
import { Sheet } from "@cocola/ui-compat/sheet";
import { useCallback, useEffect, useMemo, useState } from "react";
import {
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

type BadgeTone = "neutral" | "sky" | "green" | "amber" | "red";

const STATUS_STYLES: Record<string, { dot: string; ring: string; tone: BadgeTone; label: string }> =
  {
    healthy: {
      dot: "bg-emerald-400",
      ring: "ring-emerald-400/40",
      tone: "green",
      label: "Healthy",
    },
    degraded: {
      dot: "bg-amber-400",
      ring: "ring-amber-400/40",
      tone: "amber",
      label: "Degraded",
    },
    unhealthy: {
      dot: "bg-red-400",
      ring: "ring-red-400/40",
      tone: "red",
      label: "Unhealthy",
    },
    unknown: {
      dot: "bg-slate-400",
      ring: "ring-slate-400/30",
      tone: "neutral",
      label: "Unknown",
    },
  };

function statusStyle(status: Status) {
  return STATUS_STYLES[status] ?? STATUS_STYLES.unknown!;
}

export default function AdminArchitecturePage() {
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
        title="Architecture"
        description="System topology and health states for the current cocola deployment."
        actions={
          <AdminRefreshButton
            variant="outline"
            onClick={() => void load()}
            disabled={loading}
            refreshing={loading}
          >
            Refresh
          </AdminRefreshButton>
        }
      />

      {error ? (
        <div className="rounded-xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
          {error}
        </div>
      ) : null}

      <section className="min-w-0">
        <Card className="architecture-flow relative min-h-[620px] w-full max-w-full min-w-0 overflow-auto p-5">
          <Card.Content className="min-w-[920px] p-0">
            <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_1px_1px,var(--separator)_1px,transparent_0)] [background-size:32px_32px]" />
            <div className="pointer-events-none absolute left-3 top-3 z-10 rounded-md border border-border bg-background/95 px-3 py-2 text-xs text-muted shadow-sm backdrop-blur">
              {graph?.generated_at
                ? `Generated ${formatDateTime(graph.generated_at)}`
                : "Loading graph"}
            </div>

            {graph ? (
              <div className="relative min-w-max pt-10">
                <div className="grid grid-cols-5 gap-4">
                  {LAYER_ORDER.map((layer) => (
                    <p key={layer} className="text-muted px-1 text-center text-xs font-medium">
                      {layer}
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
                      Other
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
                  Loading architecture...
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
              <Sheet.CloseTrigger aria-label="Close component details" />
              <Sheet.Header>
                <Sheet.Heading>{selected?.label ?? "Component status"}</Sheet.Heading>
                <p className="text-muted text-sm">
                  {selected ? `${selected.layer} · ${selected.kind}` : ""}
                </p>
              </Sheet.Header>
              <Sheet.Body>{selected ? <NodeDetail node={selected} /> : null}</Sheet.Body>
              <Sheet.Footer>
                <Button variant="outline" onPress={() => setSelectedId(null)}>
                  Close
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
          <AdminStatusBadge tone={s.tone}>{s.label}</AdminStatusBadge>
        </span>
        <span className="mt-auto min-w-0">
          <span className="block truncate text-sm font-semibold">{node.label}</span>
          <span className="text-muted block truncate font-mono text-[10px]">{node.kind}</span>
        </span>
      </button>
    </Card>
  );
}

function NodeDetail({ node }: { node: ArchitectureNode }) {
  const s = statusStyle(node.status);
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <h3 className="text-base font-semibold text-foreground">{node.label}</h3>
        <AdminStatusBadge tone={s.tone} dot>
          {s.label}
        </AdminStatusBadge>
      </div>
      <div className="text-xs text-muted">
        {node.layer} · {node.kind}
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
            Open in admin
          </Button>
        ) : null}
        {node.external_href ? (
          <Button
            className="w-full"
            variant="outline"
            onPress={() => window.open(node.external_href!, "_blank", "noopener,noreferrer")}
          >
            External console
            <ExternalLink className="size-3.5" />
          </Button>
        ) : null}
      </div>
    </div>
  );
}

function formatDateTime(value: string) {
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
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
