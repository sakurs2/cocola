"use client";

import { Workflow as ArchitecturePageIcon } from "lucide-react";
import {
  Boxes,
  CheckCircle2,
  CircleHelp,
  ExternalLink,
  LoaderCircle,
  TriangleAlert,
} from "lucide-react";
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

const STATUS_STYLES: Record<
  string,
  { dot: string; ring: string; tone: BadgeTone; label: string }
> = {
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

  const stats = useMemo(() => {
    const source = graph?.nodes ?? [];
    return {
      total: source.length,
      healthy: source.filter((node) => node.status === "healthy").length,
      attention: source.filter((node) => node.status === "degraded" || node.status === "unhealthy")
        .length,
      unknown: source.filter((node) => node.status === "unknown").length,
    };
  }, [graph]);

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
        <div className="rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
          {error}
        </div>
      ) : null}

      <section className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Metric label="Nodes" value={String(stats.total)} tone="fuchsia" icon={<Boxes />} />
        <Metric
          label="Healthy"
          value={String(stats.healthy)}
          tone="green"
          icon={<CheckCircle2 />}
        />
        <Metric
          label="Attention"
          value={String(stats.attention)}
          tone="amber"
          icon={<TriangleAlert />}
        />
        <Metric label="Unknown" value={String(stats.unknown)} tone="slate" icon={<CircleHelp />} />
      </section>

      <section className="grid min-h-[620px] gap-4 xl:h-[calc(100vh-14rem)] xl:grid-cols-[minmax(0,1fr)_340px]">
        <div className="architecture-flow relative h-[620px] overflow-auto rounded-2xl border border-border bg-card xl:h-full">
          <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_1px_1px,hsl(var(--border))_1px,transparent_0)] [background-size:32px_32px]" />
          <div className="pointer-events-none absolute left-3 top-3 z-10 rounded-md border border-border bg-background/95 px-3 py-2 text-xs text-muted-foreground shadow-sm backdrop-blur">
            {graph?.generated_at
              ? `Generated ${formatDateTime(graph.generated_at)}`
              : "Loading graph"}
          </div>

          {graph ? (
            <div className="relative min-w-max p-6 pt-14">
              <div
                className="grid items-start gap-x-6 gap-y-4"
                style={{
                  gridTemplateColumns: `repeat(${LAYER_ORDER.length}, minmax(168px, 1fr))`,
                }}
              >
                {LAYER_ORDER.map((layer, ci) => (
                  <div
                    key={`band-${layer}`}
                    className="pb-1 text-center font-mono text-[10px] font-semibold uppercase tracking-[0.09em] text-muted-foreground/70"
                    style={{ gridColumn: ci + 1, gridRow: 1 }}
                  >
                    {layer}
                  </div>
                ))}

                {columns.grouped.map((list, ci) =>
                  list.map((node, ri) => (
                    <NodeCard
                      key={node.id}
                      node={node}
                      selected={node.id === selectedId}
                      onSelect={() => setSelectedId(node.id)}
                      style={{ gridColumn: ci + 1, gridRow: ri + 2 }}
                    />
                  )),
                )}
              </div>

              {columns.extras.length > 0 ? (
                <div className="mt-8 border-t border-border pt-4">
                  <div className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[0.09em] text-muted-foreground/70">
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
              <div className="flex items-center gap-2 rounded-md border border-border bg-card px-3 py-2 text-sm text-muted-foreground">
                <LoaderCircle className="size-4 animate-spin" />
                Loading architecture...
              </div>
            </div>
          ) : null}
        </div>

        <aside className="admin-surface min-h-0 overflow-y-auto p-4">
          {selected ? (
            <NodeDetail node={selected} />
          ) : (
            <div className="text-sm text-muted-foreground">
              Select a node to inspect its layer, status, and endpoint.
            </div>
          )}
        </aside>
      </section>
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
    <button
      type="button"
      onClick={onSelect}
      style={style}
      className={`group flex w-full items-center gap-3 rounded-xl border bg-background px-3.5 py-3 text-left transition-colors ${
        selected ? "border-primary ring-1 ring-primary/40" : "border-border hover:border-primary/50"
      } ${className ?? ""}`}
    >
      <span className={`grid size-4.5 shrink-0 place-items-center rounded-full ring-4 ${s.ring}`}>
        <span className={`size-2 rounded-full ${s.dot}`} />
      </span>
      <span className="min-w-0">
        <span className="block truncate text-sm font-semibold text-foreground">{node.label}</span>
        <span className="block truncate font-mono text-[10px] text-muted-foreground">
          {node.kind}
        </span>
      </span>
    </button>
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
      <div className="text-xs text-muted-foreground">
        {node.layer} · {node.kind}
      </div>
      {node.detail ? <p className="text-sm text-foreground">{node.detail}</p> : null}
      {node.endpoint ? (
        <div className="rounded-md border border-border bg-background px-2.5 py-1.5 font-mono text-xs text-muted-foreground">
          {node.endpoint}
        </div>
      ) : null}
      <div className="flex flex-col gap-2">
        {node.admin_href ? (
          <a href={node.admin_href} className="admin-card-btn justify-center">
            Open in admin
          </a>
        ) : null}
        {node.external_href ? (
          <a
            href={node.external_href}
            target="_blank"
            rel="noreferrer"
            className="admin-card-btn justify-center"
          >
            External console
            <ExternalLink className="size-3.5" />
          </a>
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
