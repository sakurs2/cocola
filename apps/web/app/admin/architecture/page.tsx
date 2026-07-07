"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { PointerEvent, WheelEvent } from "react";
import {
  Activity,
  ArrowRight,
  Box,
  Database,
  ExternalLink,
  HardDrive,
  LoaderCircle,
  Minus,
  Network,
  Plus,
  RefreshCw,
  Server,
} from "lucide-react";
import Link from "next/link";
import { cn } from "@/lib/utils";

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

const NODE_WIDTH = 210;
const NODE_HEIGHT = 108;
const COLUMN_GAP = 424;
const ROW_GAP = 214;
const MARGIN_X = 184;
const CONTENT_TOP = 128;
const MARGIN_BOTTOM = 120;

const LAYERS = ["Client / UI", "Control Plane", "Runtime Plane", "Sandbox Plane", "Infrastructure"];

type Point = { x: number; y: number };

type Layout = {
  positions: Record<string, Point>;
  columnX: number[];
  width: number;
  height: number;
};

// Data-driven layered layout: each node is placed into the column that matches
// its `layer`, and the nodes inside a column are vertically centered. This keeps
// the column headers aligned with their nodes no matter how many nodes a layer
// holds, instead of relying on hand-tuned pixel coordinates.
function computeLayout(nodes: ArchitectureNode[]): Layout {
  const columnX = LAYERS.map((_, i) => MARGIN_X + NODE_WIDTH / 2 + i * COLUMN_GAP);
  const positions: Record<string, Point> = {};

  const columns = LAYERS.map((layer) => nodes.filter((node) => node.layer === layer));
  // Nodes with an unrecognized layer fall into the last column so they stay visible.
  const known = new Set(LAYERS);
  const overflow = columns[columns.length - 1];
  for (const node of nodes) {
    if (!known.has(node.layer)) overflow?.push(node);
  }

  const maxCount = Math.max(1, ...columns.map((col) => col.length));
  const contentHeight = (maxCount - 1) * ROW_GAP + NODE_HEIGHT;
  const centerY = CONTENT_TOP + contentHeight / 2;

  columns.forEach((colNodes, colIndex) => {
    const cx = columnX[colIndex] ?? 0;
    const span = (colNodes.length - 1) * ROW_GAP;
    const startY = centerY - span / 2;
    colNodes.forEach((node, rowIndex) => {
      positions[node.id] = { x: cx, y: startY + rowIndex * ROW_GAP };
    });
  });

  return {
    positions,
    columnX,
    width: MARGIN_X * 2 + NODE_WIDTH + (LAYERS.length - 1) * COLUMN_GAP,
    height: CONTENT_TOP + contentHeight + MARGIN_BOTTOM,
  };
}

const statusLabel: Record<string, string> = {
  healthy: "Healthy",
  degraded: "Degraded",
  unhealthy: "Unhealthy",
  unknown: "Unknown",
};

const statusClasses: Record<string, string> = {
  healthy: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  degraded: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300",
  unhealthy: "border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300",
  unknown: "border-muted bg-muted text-muted-foreground",
};

export default function AdminArchitecturePage() {
  const [graph, setGraph] = useState<ArchitectureGraph | null>(null);
  const [selectedId, setSelectedId] = useState("gateway");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [panning, setPanning] = useState(false);
  const [hoveredId, setHoveredId] = useState("");
  const [zoom, setZoom] = useState(0.78);
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const isPanningRef = useRef(false);
  const panStartRef = useRef({ left: 0, top: 0, x: 0, y: 0 });

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await fetch("/api/admin/architecture", { cache: "no-store" });
      if (!res.ok) throw new Error(await readError(res));
      const data = (await res.json()) as ArchitectureGraph;
      setGraph(data);
      setSelectedId((current) =>
        data.nodes.some((node) => node.id === current) ? current : (data.nodes[0]?.id ?? ""),
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const nodesById = useMemo(() => {
    const map = new Map<string, ArchitectureNode>();
    for (const node of graph?.nodes ?? []) map.set(node.id, node);
    return map;
  }, [graph]);

  const layout = useMemo(() => computeLayout(graph?.nodes ?? []), [graph]);

  const selected = nodesById.get(selectedId) ?? graph?.nodes[0];
  const activeId = hoveredId || selected?.id || "";
  const selectedEdges = useMemo(
    () =>
      (graph?.edges ?? []).filter((edge) => edge.from === selected?.id || edge.to === selected?.id),
    [graph?.edges, selected?.id],
  );

  const stats = useMemo(() => {
    const nodes = graph?.nodes ?? [];
    return {
      total: nodes.length,
      healthy: nodes.filter((node) => node.status === "healthy").length,
      attention: nodes.filter((node) => node.status === "degraded" || node.status === "unhealthy")
        .length,
      unknown: nodes.filter((node) => node.status === "unknown").length,
    };
  }, [graph]);

  const relatedIds = useMemo(() => {
    if (!activeId) return new Set<string>();
    const ids = new Set<string>([activeId]);
    for (const edge of graph?.edges ?? []) {
      if (edge.from === activeId) ids.add(edge.to);
      if (edge.to === activeId) ids.add(edge.from);
    }
    return ids;
  }, [activeId, graph?.edges]);

  const relatedEdgeKeys = useMemo(() => {
    if (!activeId) return new Set<string>();
    const keys = new Set<string>();
    for (const edge of graph?.edges ?? []) {
      if (edge.from === activeId || edge.to === activeId) {
        keys.add(edgeKey(edge));
      }
    }
    return keys;
  }, [activeId, graph?.edges]);

  const setClampedZoom = useCallback((next: number) => {
    setZoom(Math.min(1.3, Math.max(0.5, Number(next.toFixed(2)))));
  }, []);

  const beginPan = useCallback((event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    const target = event.target;
    if (
      target instanceof HTMLElement &&
      (target.closest("[data-topology-node]") || target.closest("[data-canvas-control]"))
    ) {
      return;
    }
    const viewport = viewportRef.current;
    if (!viewport) return;
    isPanningRef.current = true;
    setPanning(true);
    panStartRef.current = {
      left: viewport.scrollLeft,
      top: viewport.scrollTop,
      x: event.clientX,
      y: event.clientY,
    };
    viewport.setPointerCapture(event.pointerId);
  }, []);

  const movePan = useCallback((event: PointerEvent<HTMLDivElement>) => {
    if (!isPanningRef.current) return;
    const viewport = viewportRef.current;
    if (!viewport) return;
    const start = panStartRef.current;
    viewport.scrollLeft = start.left - (event.clientX - start.x);
    viewport.scrollTop = start.top - (event.clientY - start.y);
  }, []);

  const endPan = useCallback((event: PointerEvent<HTMLDivElement>) => {
    if (!isPanningRef.current) return;
    isPanningRef.current = false;
    setPanning(false);
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
  }, []);

  const handleWheel = useCallback(
    (event: WheelEvent<HTMLDivElement>) => {
      if (!event.metaKey && !event.ctrlKey) return;
      event.preventDefault();
      const viewport = viewportRef.current;
      if (!viewport) return;
      const rect = viewport.getBoundingClientRect();
      const before = zoom;
      const next = Math.min(1.3, Math.max(0.5, before + (event.deltaY > 0 ? -0.08 : 0.08)));
      if (next === before) return;
      const pointerX = event.clientX - rect.left;
      const pointerY = event.clientY - rect.top;
      const localX = (viewport.scrollLeft + pointerX) / before;
      const localY = (viewport.scrollTop + pointerY) / before;
      setZoom(Number(next.toFixed(2)));
      requestAnimationFrame(() => {
        viewport.scrollLeft = localX * next - pointerX;
        viewport.scrollTop = localY * next - pointerY;
      });
    },
    [zoom],
  );

  return (
    <main className="mx-auto max-w-7xl space-y-6 px-6 py-6">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold">Architecture</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            System topology and health states for the current cocola deployment.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <div className="grid grid-cols-4 overflow-hidden rounded-md border border-border text-center text-xs">
            <Stat label="Nodes" value={stats.total} />
            <Stat label="Healthy" value={stats.healthy} />
            <Stat label="Attention" value={stats.attention} />
            <Stat label="Unknown" value={stats.unknown} />
          </div>
          <button
            className="inline-flex h-9 items-center justify-center gap-2 rounded-md border border-border bg-background px-3 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-50"
            onClick={() => void load()}
            disabled={loading}
            type="button"
          >
            {loading ? (
              <LoaderCircle className="size-4 animate-spin" />
            ) : (
              <RefreshCw className="size-4" />
            )}
            Refresh
          </button>
        </div>
      </header>

      {error ? (
        <div className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600">
          {error}
        </div>
      ) : null}

      <section className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
        <div
          ref={viewportRef}
          className={cn(
            "relative h-[720px] overflow-auto rounded-lg border border-border bg-card select-none touch-none",
            panning ? "cursor-grabbing" : "cursor-grab",
          )}
          onPointerCancel={endPan}
          onPointerDown={beginPan}
          onPointerLeave={endPan}
          onPointerMove={movePan}
          onPointerUp={endPan}
          onWheel={handleWheel}
        >
          <div
            className="relative"
            style={{ width: layout.width * zoom, height: layout.height * zoom }}
          >
            <div
              className="relative bg-[linear-gradient(to_right,hsl(var(--border))_1px,transparent_1px),linear-gradient(to_bottom,hsl(var(--border))_1px,transparent_1px)] bg-[size:64px_64px]"
              style={{
                width: layout.width,
                height: layout.height,
                transform: `scale(${zoom})`,
                transformOrigin: "top left",
              }}
            >
              <LayerLabels columnX={layout.columnX} height={layout.height} />
              <TopologySvg
                activeId={activeId}
                graph={graph}
                layout={layout}
                nodesById={nodesById}
                relatedEdgeKeys={relatedEdgeKeys}
              />
              {(graph?.nodes ?? []).map((node) => (
                <TopologyNode
                  key={node.id}
                  dimmed={relatedIds.size > 0 && !relatedIds.has(node.id)}
                  node={node}
                  pos={layout.positions[node.id]}
                  selected={node.id === selected?.id}
                  onHover={(id) => setHoveredId(id)}
                  onSelect={() => setSelectedId(node.id)}
                />
              ))}
            </div>
            {loading && !graph ? (
              <div className="absolute inset-0 grid place-items-center bg-background/60">
                <div className="flex items-center gap-2 rounded-md border border-border bg-card px-3 py-2 text-sm text-muted-foreground">
                  <LoaderCircle className="size-4 animate-spin" />
                  Loading architecture...
                </div>
              </div>
            ) : null}
          </div>
          <div
            className="sticky bottom-3 left-3 z-20 mt-[-44px] flex w-fit items-center gap-1 rounded-md border border-border bg-background/95 p-1 shadow-sm backdrop-blur"
            data-canvas-control
          >
            <button
              className="inline-flex size-8 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              type="button"
              onClick={() => setClampedZoom(zoom - 0.1)}
              title="Zoom out"
            >
              <Minus className="size-4" />
            </button>
            <span className="min-w-12 text-center text-xs font-medium text-muted-foreground">
              {Math.round(zoom * 100)}%
            </span>
            <button
              className="inline-flex size-8 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              type="button"
              onClick={() => setClampedZoom(zoom + 0.1)}
              title="Zoom in"
            >
              <Plus className="size-4" />
            </button>
          </div>
        </div>

        <aside className="rounded-lg border border-border bg-card p-4">
          {selected ? (
            <NodeDetail node={selected} edges={selectedEdges} nodesById={nodesById} />
          ) : (
            <div className="text-sm text-muted-foreground">Select a node</div>
          )}
        </aside>
      </section>
    </main>
  );
}

function TopologySvg({
  activeId,
  graph,
  layout,
  nodesById,
  relatedEdgeKeys,
}: {
  activeId: string;
  graph: ArchitectureGraph | null;
  layout: Layout;
  nodesById: Map<string, ArchitectureNode>;
  relatedEdgeKeys: Set<string>;
}) {
  return (
    <svg
      className="pointer-events-none absolute inset-0 size-full"
      viewBox={`0 0 ${layout.width} ${layout.height}`}
    >
      <defs>
        <marker
          id="arrow-muted"
          markerHeight="12"
          markerUnits="userSpaceOnUse"
          markerWidth="12"
          orient="auto"
          refX="11"
          refY="6"
        >
          <path
            d="M1,1 L11,6 L1,11 Q3.5,6 1,1"
            fill="hsl(var(--muted-foreground))"
            opacity="0.42"
          />
        </marker>
        <marker
          id="arrow-active"
          markerHeight="14"
          markerUnits="userSpaceOnUse"
          markerWidth="14"
          orient="auto"
          refX="13"
          refY="7"
        >
          <path d="M1,1 L13,7 L1,13 Q4,7 1,1" fill="hsl(var(--primary))" opacity="0.95" />
        </marker>
      </defs>
      <style>{`
        @keyframes architecture-flow {
          from { stroke-dashoffset: 42; }
          to { stroke-dashoffset: 0; }
        }
        .architecture-flow-line {
          stroke-dasharray: 14 28;
          animation: architecture-flow 1.15s linear infinite;
        }
        @media (prefers-reduced-motion: reduce) {
          .architecture-flow-line { animation: none; }
        }
      `}</style>
      {(graph?.edges ?? []).map((edge) => {
        const from = layout.positions[edge.from];
        const to = layout.positions[edge.to];
        if (!from || !to || !nodesById.has(edge.from) || !nodesById.has(edge.to)) return null;
        const active = activeId ? relatedEdgeKeys.has(edgeKey(edge)) : true;
        const start = edgeAnchor(from, to, true);
        const end = edgeAnchor(from, to, false);
        const path = edgePath(start, end);
        return (
          <g key={`${edge.from}-${edge.to}-${edge.label}`}>
            <path
              d={path}
              fill="none"
              markerEnd={active ? "url(#arrow-active)" : "url(#arrow-muted)"}
              opacity={active ? 0.62 : 0.13}
              stroke="hsl(var(--foreground))"
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={active ? 5 : 3}
            />
            <path
              className="architecture-flow-line"
              d={path}
              fill="none"
              opacity={active ? 0.95 : 0.18}
              stroke="hsl(var(--primary))"
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={active ? 3.25 : 1.75}
            />
          </g>
        );
      })}
    </svg>
  );
}

// Anchor an edge on the node border closest to the peer node. The exit/entry
// direction (horizontal vs vertical) is remembered so the curve can leave and
// arrive perpendicular to the card edge for a clean, consistent look.
function edgeAnchor(
  from: Point,
  to: Point,
  isStart: boolean,
): Point & { axis: "x" | "y"; dir: number } {
  const base = isStart ? from : to;
  const peer = isStart ? to : from;
  const dx = peer.x - base.x;
  const dy = peer.y - base.y;
  if (Math.abs(dx) >= Math.abs(dy)) {
    const dir = Math.sign(dx || 1);
    return { x: base.x + dir * (NODE_WIDTH / 2 + 8), y: base.y, axis: "x", dir };
  }
  const dir = Math.sign(dy || 1);
  return { x: base.x, y: base.y + dir * (NODE_HEIGHT / 2 + 8), axis: "y", dir };
}

// Every edge is drawn as a single smooth cubic Bezier. Control points are pushed
// out along each anchor's exit axis so all connectors share one consistent curved
// style, whether they run across columns or between rows in the same column.
function edgePath(
  start: Point & { axis: "x" | "y"; dir: number },
  end: Point & { axis: "x" | "y"; dir: number },
) {
  const dx = end.x - start.x;
  const dy = end.y - start.y;
  // Push each control point out along its own anchor's exit axis, scaled by the
  // gap the curve has to cross in that direction. This yields smooth, balanced
  // S-curves for cross-column links and gentle arcs for same-column links.
  const startSpan = start.axis === "x" ? Math.abs(dx) : Math.abs(dy);
  const endSpan = end.axis === "x" ? Math.abs(dx) : Math.abs(dy);
  const startReach = Math.min(Math.max(startSpan * 0.55, 64), 240);
  const endReach = Math.min(Math.max(endSpan * 0.55, 64), 240);
  const c1x = start.axis === "x" ? start.x + start.dir * startReach : start.x;
  const c1y = start.axis === "y" ? start.y + start.dir * startReach : start.y;
  const c2x = end.axis === "x" ? end.x + end.dir * endReach : end.x;
  const c2y = end.axis === "y" ? end.y + end.dir * endReach : end.y;
  return `M ${start.x} ${start.y} C ${c1x} ${c1y}, ${c2x} ${c2y}, ${end.x} ${end.y}`;
}

function edgeKey(edge: ArchitectureEdge) {
  return `${edge.from}->${edge.to}:${edge.label ?? ""}:${edge.kind ?? ""}`;
}

function TopologyNode({
  dimmed,
  node,
  pos,
  selected,
  onHover,
  onSelect,
}: {
  dimmed: boolean;
  node: ArchitectureNode;
  pos?: Point;
  selected: boolean;
  onHover: (id: string) => void;
  onSelect: () => void;
}) {
  const position = pos ?? { x: 50, y: 50 };
  const Icon = iconForKind(node.kind);
  return (
    <button
      data-topology-node
      className={cn(
        "absolute flex -translate-x-1/2 -translate-y-1/2 flex-col justify-between rounded-lg border border-border bg-background p-3 text-left shadow-sm transition",
        "hover:border-primary/40 hover:shadow-md focus:outline-none focus:ring-2 focus:ring-ring",
        selected && "border-primary/60 shadow-md ring-2 ring-primary/20",
        dimmed && "opacity-35",
      )}
      style={{ left: position.x, top: position.y, width: NODE_WIDTH, height: NODE_HEIGHT }}
      type="button"
      onMouseEnter={() => onHover(node.id)}
      onMouseLeave={() => onHover("")}
      onClick={onSelect}
    >
      <div className="flex items-start gap-2">
        <div className="grid size-8 shrink-0 place-items-center rounded-md bg-muted">
          <Icon className="size-4 text-muted-foreground" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold">{node.label}</div>
          <div className="mt-0.5 truncate text-xs text-muted-foreground">{node.layer}</div>
        </div>
      </div>
      <div className="flex items-center justify-between gap-2">
        <StatusBadge status={node.status} />
        <span className="truncate text-xs text-muted-foreground">{node.kind}</span>
      </div>
    </button>
  );
}

function NodeDetail({
  node,
  edges,
  nodesById,
}: {
  node: ArchitectureNode;
  edges: ArchitectureEdge[];
  nodesById: Map<string, ArchitectureNode>;
}) {
  const metadata = Object.entries(node.metadata ?? {});
  return (
    <div className="space-y-4">
      <div>
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="text-base font-semibold">{node.label}</h2>
            <p className="mt-1 text-xs text-muted-foreground">{node.layer}</p>
          </div>
          <StatusBadge status={node.status} />
        </div>
        {node.detail ? <p className="mt-3 text-sm text-muted-foreground">{node.detail}</p> : null}
      </div>

      <div className="space-y-2 text-sm">
        <DetailRow label="Kind" value={node.kind} />
        <DetailRow label="Endpoint" value={node.endpoint || "Not configured"} />
      </div>

      {metadata.length > 0 ? (
        <div>
          <div className="mb-2 text-xs font-medium text-muted-foreground">Metadata</div>
          <div className="space-y-2">
            {metadata.map(([key, value]) => (
              <DetailRow key={key} label={key.replaceAll("_", " ")} value={String(value)} />
            ))}
          </div>
        </div>
      ) : null}

      <div>
        <div className="mb-2 text-xs font-medium text-muted-foreground">Links</div>
        <div className="flex flex-wrap gap-2">
          {node.admin_href ? (
            <Link
              className="inline-flex h-8 items-center gap-2 rounded-md border border-border px-2 text-xs hover:bg-accent"
              href={node.admin_href}
            >
              Open admin
              <ArrowRight className="size-3" />
            </Link>
          ) : null}
          {node.external_href ? (
            <a
              className="inline-flex h-8 items-center gap-2 rounded-md border border-border px-2 text-xs hover:bg-accent"
              href={node.external_href}
              rel="noreferrer"
              target="_blank"
            >
              Open external
              <ExternalLink className="size-3" />
            </a>
          ) : null}
        </div>
      </div>

      <div>
        <div className="mb-2 text-xs font-medium text-muted-foreground">Connections</div>
        <div className="space-y-2">
          {edges.map((edge) => {
            const incoming = edge.to === node.id;
            const peer = nodesById.get(incoming ? edge.from : edge.to);
            return (
              <div
                key={`${edge.from}-${edge.to}-${edge.label}`}
                className="flex items-center justify-between gap-3 rounded-md border border-border bg-background px-2 py-2 text-xs"
              >
                <span className="truncate">
                  {incoming ? "From" : "To"} {peer?.label ?? (incoming ? edge.from : edge.to)}
                </span>
                <span className="shrink-0 text-muted-foreground">
                  {edge.label || edge.kind || "link"}
                </span>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

function LayerLabels({ columnX, height }: { columnX: number[]; height: number }) {
  return (
    <>
      {LAYERS.map((layer, i) => (
        <div key={layer}>
          <div
            className="absolute top-0 -z-10 w-px bg-border/40"
            style={{ left: columnX[i], height }}
          />
          <div
            className="absolute top-6 -translate-x-1/2 whitespace-nowrap text-[11px] font-medium uppercase tracking-wide text-muted-foreground"
            style={{ left: columnX[i] }}
          >
            {layer}
          </div>
        </div>
      ))}
    </>
  );
}

function StatusBadge({ status }: { status: Status }) {
  return (
    <span
      className={cn(
        "inline-flex h-6 items-center gap-1 rounded-md border px-2 text-xs font-medium",
        statusClasses[status] ?? statusClasses.unknown,
      )}
    >
      <span className="size-1.5 rounded-full bg-current" />
      {statusLabel[status] ?? status}
    </span>
  );
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-md bg-muted/50 px-2 py-2">
      <span className="shrink-0 text-xs text-muted-foreground">{label}</span>
      <span className="truncate text-xs font-medium">{value}</span>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="min-w-20 border-r border-border px-3 py-2 last:border-r-0">
      <div className="text-muted-foreground">{label}</div>
      <div className="mt-1 text-sm font-semibold text-foreground">{value}</div>
    </div>
  );
}

function iconForKind(kind: string) {
  if (kind === "database") return Database;
  if (kind === "cache") return Activity;
  if (kind === "object-store") return HardDrive;
  if (kind === "runtime" || kind === "workload") return Box;
  if (kind === "frontend") return Network;
  return Server;
}

async function readError(res: Response) {
  try {
    const data = await res.json();
    if (typeof data?.error === "string") return data.error;
    if (typeof data?.message === "string") return data.message;
  } catch {
    // fall through
  }
  return `${res.status} ${res.statusText}`;
}
