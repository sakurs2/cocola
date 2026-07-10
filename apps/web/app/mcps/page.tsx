"use client";

import { useCallback, useEffect, useState } from "react";
import { LoaderCircle, PlugZap, ToggleLeft, ToggleRight } from "lucide-react";
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

const btn =
  "inline-flex h-9 items-center justify-center gap-2 rounded-md border border-border bg-background px-3 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-50";

export default function MCPPage() {
  const [mcps, setMcps] = useState<MCPServer[]>([]);
  const [loading, setLoading] = useState(true);
  const [workingId, setWorkingId] = useState<string | null>(null);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await fetch("/api/mcps", { cache: "no-store" });
      if (!res.ok) throw new Error(await readError(res));
      const data = (await res.json()) as { mcps?: MCPServer[] };
      setMcps(data.mcps ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
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
    }
  };

  return (
    <main className="h-full min-w-0 overflow-y-auto">
      <div className="mx-auto max-w-6xl space-y-6 px-6 py-6">
        <header>
          <h1 className="text-xl font-semibold">MCP</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Choose which administrator-published MCP servers are available in your agent sessions.
          </p>
        </header>

        {error ? (
          <div className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600">
            {error}
          </div>
        ) : null}

        {loading ? (
          <div className="flex h-40 items-center justify-center text-muted-foreground">
            <LoaderCircle className="mr-2 size-4 animate-spin" />
            Loading MCP servers
          </div>
        ) : mcps.length ? (
          <section className="grid gap-3 md:grid-cols-2">
            {mcps.map((mcp) => (
              <article key={mcp.id} className="rounded-lg border border-border bg-card p-4">
                <div className="flex items-start gap-3">
                  <div className="grid size-10 shrink-0 place-items-center rounded-md bg-muted">
                    <PlugZap className="size-5 text-muted-foreground" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <Link
                        href={`/mcps/${encodeURIComponent(mcp.id)}`}
                        className="truncate text-sm font-semibold hover:underline"
                      >
                        {mcp.name || mcp.id}
                      </Link>
                      <span className="rounded-md border border-border px-2 py-0.5 text-xs text-muted-foreground">
                        {mcp.transport}
                      </span>
                    </div>
                    <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">
                      {mcp.description || "No description"}
                    </p>
                    <div className="mt-3 truncate text-xs text-muted-foreground">
                      {mcp.transport === "stdio" ? mcp.command : mcp.url_hint}
                    </div>
                  </div>
                  <button
                    type="button"
                    className={btn}
                    disabled={workingId === mcp.id}
                    onClick={() => void toggle(mcp)}
                  >
                    {workingId === mcp.id ? (
                      <LoaderCircle className="size-4 animate-spin" />
                    ) : mcp.effective_enabled ? (
                      <ToggleRight className="size-4" />
                    ) : (
                      <ToggleLeft className="size-4" />
                    )}
                    {mcp.effective_enabled ? "Disable" : "Enable"}
                  </button>
                </div>
                <div className="mt-3 flex flex-wrap gap-2 text-xs text-muted-foreground">
                  <span className="rounded-md border border-border px-2 py-0.5">
                    {mcp.effective_enabled ? "enabled" : "disabled"}
                  </span>
                  <span className="rounded-md border border-border px-2 py-0.5">
                    {mcp.default_enabled ? "default on" : "default off"}
                  </span>
                </div>
              </article>
            ))}
          </section>
        ) : (
          <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
            No MCP servers published by administrators.
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
