"use client";

import { useCallback, useEffect, useState } from "react";
import { LayoutGrid, LoaderCircle, Plug, Power, Zap } from "lucide-react";
import Link from "next/link";

import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";

type MCPServer = {
  id: string;
  name: string;
  description: string;
  transport: string;
  command?: string;
  url_hint?: string;
  env_hints?: Record<string, string>;
  header_hints?: Record<string, string>;
  default_enabled: boolean;
  effective_enabled: boolean;
};

type MCPHub = {
  total_published: number;
  total_effective: number;
  transports: Record<string, number>;
};

export default function MCPPage() {
  const [mcps, setMcps] = useState<MCPServer[]>([]);
  const [hub, setHub] = useState<MCPHub | null>(null);
  const [loading, setLoading] = useState(true);
  const [workingId, setWorkingId] = useState<string | null>(null);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [res, hubRes] = await Promise.all([
        fetch("/api/mcps", { cache: "no-store" }),
        fetch("/api/mcps/hub", { cache: "no-store" }),
      ]);
      if (!res.ok) throw new Error(await readError(res));
      const data = (await res.json()) as { mcps?: MCPServer[] };
      setMcps(data.mcps ?? []);
      // The hub rollup is supplementary; a failure here must not blank the page.
      if (hubRes.ok) {
        setHub((await hubRes.json()) as MCPHub);
      } else {
        setHub(null);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  const refreshHub = useCallback(async () => {
    try {
      const res = await fetch("/api/mcps/hub", { cache: "no-store" });
      setHub(res.ok ? ((await res.json()) as MCPHub) : null);
    } catch {
      setHub(null);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const toggle = async (mcp: MCPServer) => {
    const previous = mcps;
    setWorkingId(mcp.id);
    setError("");
    setMcps((current) =>
      current.map((item) =>
        item.id === mcp.id ? { ...item, effective_enabled: !item.effective_enabled } : item,
      ),
    );
    try {
      const res = await fetch(
        `/api/mcps/${encodeURIComponent(mcp.id)}/${mcp.effective_enabled ? "disable" : "enable"}`,
        { method: "POST" },
      );
      if (!res.ok) throw new Error(await readError(res));
    } catch (err) {
      setMcps(previous);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setWorkingId(null);
      // Refresh the aggregated rollup so effective counts track the toggle.
      void refreshHub();
    }
  };

  return (
    <main className="h-full min-w-0 flex-1 overflow-y-auto bg-background">
      <div className="mx-auto max-w-6xl space-y-8 px-8 py-10">
        <header className="space-y-1">
          <h1 className="text-2xl font-bold tracking-tight">MCP</h1>
          <p className="text-sm text-muted-foreground">
            Choose which administrator-published MCP servers are available in your agent sessions.
          </p>
        </header>

        {hub ? (
          <section className="grid gap-3 sm:grid-cols-3">
            <div className="flex items-center gap-3 rounded-xl border border-border bg-card p-4 shadow-card">
              <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-orange-50 text-orange-600 ring-1 ring-orange-100">
                <LayoutGrid className="size-5" />
              </div>
              <div className="min-w-0">
                <div className="text-2xl font-bold leading-none">{hub.total_published}</div>
                <div className="mt-1 text-xs text-muted-foreground">Published servers</div>
              </div>
            </div>
            <div className="flex items-center gap-3 rounded-xl border border-border bg-card p-4 shadow-card">
              <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-emerald-50 text-emerald-600 ring-1 ring-emerald-100">
                <Zap className="size-5" />
              </div>
              <div className="min-w-0">
                <div className="text-2xl font-bold leading-none">{hub.total_effective}</div>
                <div className="mt-1 text-xs text-muted-foreground">Active in your sessions</div>
              </div>
            </div>
            <div className="flex items-center gap-3 rounded-xl border border-border bg-card p-4 shadow-card">
              <div className="min-w-0">
                <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  By transport
                </div>
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {Object.keys(hub.transports).length ? (
                    Object.entries(hub.transports).map(([transport, count]) => (
                      <Badge key={transport} variant="outline">
                        {transport} · {count}
                      </Badge>
                    ))
                  ) : (
                    <span className="text-xs text-muted-foreground">None active</span>
                  )}
                </div>
              </div>
            </div>
          </section>
        ) : null}

        {error ? (
          <div className="rounded-xl border border-red-500/30 bg-red-500/10 px-3.5 py-2.5 text-sm text-red-600">
            {error}
          </div>
        ) : null}

        {loading ? (
          <div className="flex h-40 items-center justify-center text-muted-foreground">
            <LoaderCircle className="mr-2 size-4 animate-spin" />
            Loading MCP servers
          </div>
        ) : mcps.length ? (
          <section className="grid gap-4 md:grid-cols-2">
            {mcps.map((mcp) => {
              const working = workingId === mcp.id;
              return (
                <Card key={mcp.id} className="flex flex-col p-5 shadow-card transition hover:-translate-y-0.5 hover:shadow-lg">
                  <div className="flex items-start gap-3">
                    <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-orange-50 text-orange-600 ring-1 ring-orange-100">
                      <Plug className="size-5" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <Link
                          href={`/mcps/${encodeURIComponent(mcp.id)}`}
                          className="truncate text-sm font-bold hover:underline"
                        >
                          {mcp.name || mcp.id}
                        </Link>
                        <Badge variant="outline">{mcp.transport}</Badge>
                      </div>
                      <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">
                        {mcp.description || "No description"}
                      </p>
                      <div className="mt-3 truncate text-xs text-muted-foreground">
                        {mcp.transport === "stdio" ? mcp.command : mcp.url_hint}
                      </div>
                    </div>
                  </div>
                  <div className="mt-4 flex flex-wrap items-center gap-2">
                    {mcp.effective_enabled ? (
                      <Badge variant="success">
                        <span className="size-1.5 rounded-full bg-emerald-500" /> enabled
                      </Badge>
                    ) : (
                      <Badge>disabled</Badge>
                    )}
                    <Badge>{mcp.default_enabled ? "default on" : "default off"}</Badge>
                  </div>
                  <div className="mt-5 flex items-center gap-2 border-t border-border pt-4">
                    {mcp.effective_enabled ? (
                      <button
                        type="button"
                        className="inline-flex h-8 flex-1 items-center justify-center gap-2 rounded-lg border border-border bg-background px-3 text-xs font-medium shadow-xs transition-colors hover:bg-muted disabled:opacity-50"
                        disabled={working}
                        onClick={() => void toggle(mcp)}
                      >
                        {working ? (
                          <LoaderCircle className="size-3.5 animate-spin" />
                        ) : (
                          <Power className="size-3.5 text-emerald-600" />
                        )}
                        Disable
                      </button>
                    ) : (
                      <button
                        type="button"
                        className="inline-flex h-8 flex-1 items-center justify-center gap-2 rounded-lg px-3 text-xs font-semibold text-white shadow-xs transition-opacity brand-gradient hover:opacity-90 disabled:opacity-50"
                        disabled={working}
                        onClick={() => void toggle(mcp)}
                      >
                        {working ? (
                          <LoaderCircle className="size-3.5 animate-spin" />
                        ) : (
                          <Power className="size-3.5" />
                        )}
                        Enable
                      </button>
                    )}
                  </div>
                </Card>
              );
            })}
          </section>
        ) : (
          <div className="flex min-h-[140px] flex-col items-center justify-center gap-2 rounded-2xl border border-dashed border-border p-8 text-center">
            <div className={cn("grid size-10 place-items-center rounded-xl bg-muted")}>
              <Plug className="size-4 text-muted-foreground" />
            </div>
            <p className="text-sm text-muted-foreground">
              No MCP servers published by administrators.
            </p>
          </div>
        )}
      </div>
    </main>
  );
}

async function readError(res: Response) {
  const text = await res.text();
  try {
    const json = JSON.parse(text);
    if (typeof json.error === "string") return json.error;
    if (json.error && typeof json.error === "object") {
      return json.error.message || json.error.code || text;
    }
    return json.message || text;
  } catch {
    return text || res.statusText;
  }
}
