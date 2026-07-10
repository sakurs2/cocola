"use client";

import { Graph as ArchitecturePageIcon } from "@phosphor-icons/react";
import { LoaderCircle } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { AdminPageHeader, AdminRefreshButton } from "@/components/admin/admin-ui";

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

export default function AdminArchitecturePage() {
  const [graph, setGraph] = useState<ArchitectureGraph | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

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

  return (
    <main className="flex min-h-screen flex-col gap-5 px-6 py-5">
      <AdminPageHeader
        icon={<ArchitecturePageIcon className="size-[18px]" weight="duotone" />}
        title="Architecture"
        description="System topology and health states for the current cocola deployment."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <div className="grid grid-cols-4 overflow-hidden rounded-md border border-border text-center text-xs">
              <Stat label="Nodes" value={stats.total} />
              <Stat label="Healthy" value={stats.healthy} />
              <Stat label="Attention" value={stats.attention} />
              <Stat label="Unknown" value={stats.unknown} />
            </div>
            <AdminRefreshButton
              className="inline-flex h-9 items-center justify-center gap-2 rounded-md border border-border bg-background px-3 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-50"
              onClick={() => void load()}
              disabled={loading}
              refreshing={loading}
              variant="outline"
              size="sm"
              type="button"
            >
              Refresh
            </AdminRefreshButton>
          </div>
        }
      />

      {error ? (
        <div className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600">
          {error}
        </div>
      ) : null}

      <section className="grid min-h-[620px] gap-4 xl:h-[calc(100vh-9.5rem)] xl:grid-cols-[minmax(0,1fr)_320px]">
        <div className="architecture-flow relative h-[620px] overflow-hidden rounded-lg border border-border bg-card xl:h-full">
          <div className="absolute inset-0 bg-[radial-gradient(circle_at_1px_1px,hsl(var(--border))_1px,transparent_0)] [background-size:32px_32px]" />
          <div className="absolute left-3 top-3 rounded-md border border-border bg-background/95 px-3 py-2 text-xs text-muted-foreground shadow-sm backdrop-blur">
            {graph?.generated_at
              ? `Generated ${formatDateTime(graph.generated_at)}`
              : "Loading graph"}
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

        <aside className="min-h-0 overflow-y-auto rounded-lg border border-border bg-card p-4">
          <div className="text-sm text-muted-foreground">Node details are hidden.</div>
        </aside>
      </section>
    </main>
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
