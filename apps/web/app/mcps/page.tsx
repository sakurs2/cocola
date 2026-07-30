"use client";

import { useCallback, useEffect, useState } from "react";
import { LayoutGrid, LoaderCircle, Plug, Power, Table2, Zap } from "lucide-react";
import Link from "next/link";

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
    <main className="user-canvas user-page user-theme-orange h-full min-w-0 flex-1 overflow-y-auto px-4 sm:px-6">
      <div className="mx-auto w-full max-w-6xl space-y-6 py-10">
        <header className="flex items-center gap-3.5">
          <span className="user-page-icon">
            <Plug className="size-6" />
          </span>
          <div className="space-y-1">
            <div className="user-eyebrow">Connectors</div>
            <h1 className="text-2xl font-bold tracking-tight">MCP</h1>
            <p className="text-sm text-muted-foreground">
              Choose which administrator-published MCP servers are available in your agent sessions.
            </p>
          </div>
        </header>

        {hub ? (
          <section className="grid gap-4 sm:grid-cols-3">
            <div className="user-metric-card" data-tone="orange">
              <div className="user-metric-head">
                <span className="user-metric-glyph">
                  <LayoutGrid className="size-[22px]" />
                </span>
                <span className="user-metric-key">Published servers</span>
              </div>
              <div className="user-metric-val">{hub.total_published}</div>
              <div className="user-metric-detail">Available from administrators</div>
            </div>
            <div className="user-metric-card" data-tone="emerald">
              <div className="user-metric-head">
                <span className="user-metric-glyph">
                  <Zap className="size-[22px]" />
                </span>
                <span className="user-metric-key">Active in your sessions</span>
              </div>
              <div className="user-metric-val">{hub.total_effective}</div>
              <div className="user-metric-detail">Enabled effective connectors</div>
            </div>
            <div className="user-metric-card" data-tone="amber">
              <div className="user-metric-head">
                <span className="user-metric-glyph">
                  <Table2 className="size-[22px]" />
                </span>
                <span className="user-metric-key">By transport</span>
              </div>
              <div className="mt-1 flex flex-wrap gap-1.5">
                {Object.keys(hub.transports).length ? (
                  Object.entries(hub.transports).map(([transport, count]) => (
                    <span key={transport} className="user-metric-chip">
                      {transport} · {count}
                    </span>
                  ))
                ) : (
                  <span className="text-xs text-muted-foreground">None active</span>
                )}
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
          <section className="space-y-4">
            <div className="flex items-center gap-2.5">
              <h2 className="user-section-title">Published Servers</h2>
              <span className="user-count-badge">{mcps.length}</span>
            </div>
            <div className="grid gap-4 md:grid-cols-2">
              {mcps.map((mcp) => {
                const working = workingId === mcp.id;
                return (
                  <div key={mcp.id} className="user-card user-card--hover">
                    <div className="flex items-start gap-3">
                      <span className="user-card-glyph">
                        <Plug className="size-5" />
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <Link
                            href={`/mcps/${encodeURIComponent(mcp.id)}`}
                            className="user-card-name truncate hover:underline"
                          >
                            {mcp.name || mcp.id}
                          </Link>
                          <span className="user-tag user-tag--accent">{mcp.transport}</span>
                        </div>
                        <p className="user-card-desc mt-1 line-clamp-2">
                          {mcp.description || "No description"}
                        </p>
                        <div className="user-card-mono mt-2.5 truncate">
                          {mcp.transport === "stdio" ? mcp.command : mcp.url_hint}
                        </div>
                      </div>
                    </div>
                    <div className="mt-3 flex flex-wrap items-center gap-1.5">
                      {mcp.effective_enabled ? (
                        <span className="user-tag user-tag--ok">
                          <span className="user-tag-dot" /> enabled
                        </span>
                      ) : (
                        <span className="user-tag">disabled</span>
                      )}
                      <span className="user-tag">
                        {mcp.default_enabled ? "default on" : "default off"}
                      </span>
                    </div>
                    <div className="mt-4 flex items-center gap-2 border-t border-border/60 pt-4">
                      {mcp.effective_enabled ? (
                        <button
                          type="button"
                          className="user-tbtn user-tbtn--ghost flex-1"
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
                          className="user-tbtn user-tbtn--fill flex-1"
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
                  </div>
                );
              })}
            </div>
          </section>
        ) : (
          <div className="user-empty">
            <div className="grid size-10 place-items-center rounded-xl bg-muted">
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
