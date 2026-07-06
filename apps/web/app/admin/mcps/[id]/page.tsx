"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { ArrowLeft, LoaderCircle, PlugZap } from "lucide-react";

type MCPServer = {
  id: string;
  name: string;
  description: string;
  transport: string;
  command?: string;
  args?: string[];
  url?: string;
  url_var_hints?: Record<string, string>;
  env_hints?: Record<string, string>;
  header_hints?: Record<string, string>;
  enabled: boolean;
  default_enabled: boolean;
  source: string;
  status: string;
};

export default function AdminMCPDetailPage({ params }: { params: { id: string } }) {
  const [mcp, setMcp] = useState<MCPServer | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const res = await fetch(`/api/admin/mcps/${encodeURIComponent(params.id)}`, {
          cache: "no-store",
        });
        if (!res.ok) throw new Error(await readError(res));
        const data = (await res.json()) as MCPServer;
        if (!cancelled) setMcp(data);
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [params.id]);

  return (
    <main className="mx-auto max-w-5xl space-y-5 px-6 py-6">
      <header className="flex items-center gap-3">
        <Link
          href="/admin/mcps"
          className="grid size-9 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-accent-foreground"
          title="Back"
        >
          <ArrowLeft className="size-4" />
        </Link>
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-xl font-semibold">{mcp?.name || params.id}</h1>
          <p className="truncate text-sm text-muted-foreground">{mcp?.id || params.id}</p>
        </div>
      </header>

      {error ? (
        <div className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600">
          {error}
        </div>
      ) : null}

      {!mcp && !error ? (
        <div className="flex h-40 items-center justify-center text-muted-foreground">
          <LoaderCircle className="mr-2 size-4 animate-spin" />
          Loading MCP
        </div>
      ) : null}

      {mcp ? (
        <>
          <section className="rounded-lg border border-border bg-card p-5">
            <div className="flex items-start gap-4">
              <div className="grid size-11 shrink-0 place-items-center rounded-md bg-muted">
                <PlugZap className="size-5 text-muted-foreground" />
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <h2 className="text-base font-semibold">{mcp.name || mcp.id}</h2>
                  <Badge>{mcp.transport}</Badge>
                  <Badge>{mcp.enabled ? "enabled" : "disabled"}</Badge>
                  <Badge>{mcp.default_enabled ? "default on" : "default off"}</Badge>
                </div>
                <p className="mt-2 text-sm text-muted-foreground">
                  {mcp.description || "No description"}
                </p>
              </div>
            </div>
          </section>
          <section className="grid gap-3 md:grid-cols-2">
            <Info label="Command" value={mcp.command || "-"} />
            <Info label="Args" value={(mcp.args || []).join(" ") || "-"} />
            <Info label="URL" value={mcp.url || "-"} />
            <Info label="Status" value={mcp.status || "active"} />
            <Info label="URL Variables" value={formatHints(mcp.url_var_hints)} />
            <Info label="Env" value={formatHints(mcp.env_hints)} />
            <Info label="Headers" value={formatHints(mcp.header_hints)} />
          </section>
        </>
      ) : null}
    </main>
  );
}

function Badge({ children }: { children: string }) {
  return (
    <span className="rounded-md border border-border px-2 py-0.5 text-xs text-muted-foreground">
      {children}
    </span>
  );
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 break-words text-sm font-medium">{value}</div>
    </div>
  );
}

function formatHints(hints?: Record<string, string>) {
  const entries = Object.entries(hints || {});
  return entries.length ? entries.map(([k, v]) => `${k}=${v}`).join(", ") : "-";
}

async function readError(res: Response) {
  const text = await res.text();
  try {
    const json = JSON.parse(text);
    if (typeof json.error === "string") return json.error;
    if (json.error && typeof json.error === "object")
      return json.error.message || json.error.code || text;
    return json.message || text;
  } catch {
    return text || res.statusText;
  }
}
