"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { ArrowLeft, LoaderCircle, Plug } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";

type MCPServer = {
  id: string;
  name: string;
  description: string;
  transport: string;
  command?: string;
  url_hint?: string;
  default_enabled: boolean;
  effective_enabled: boolean;
};

export default function MCPDetailPage({ params }: { params: { id: string } }) {
  const [mcp, setMcp] = useState<MCPServer | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const res = await fetch("/api/mcps", { cache: "no-store" });
        if (!res.ok) throw new Error(await readError(res));
        const data = (await res.json()) as { mcps?: MCPServer[] };
        const found = (data.mcps || []).find((item) => item.id === params.id);
        if (!found) throw new Error("MCP not found");
        if (!cancelled) setMcp(found);
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [params.id]);

  return (
    <main className="h-full min-w-0 flex-1 overflow-y-auto bg-background">
      <div className="mx-auto max-w-5xl space-y-6 px-8 py-10">
        <header className="flex items-center gap-3">
          <Link
            href="/mcps"
            className="grid size-9 shrink-0 place-items-center rounded-xl border border-border bg-background text-muted-foreground shadow-xs transition-colors hover:bg-muted hover:text-foreground"
            title="Back"
          >
            <ArrowLeft className="size-4" />
          </Link>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-2xl font-bold tracking-tight">{mcp?.name || params.id}</h1>
            <p className="truncate text-sm text-muted-foreground">{mcp?.id || params.id}</p>
          </div>
        </header>

        {error ? (
          <div className="rounded-xl border border-red-500/30 bg-red-500/10 px-3.5 py-2.5 text-sm text-red-600">
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
          <Card className="p-5 shadow-card">
            <div className="flex items-start gap-4">
              <div className="grid size-11 shrink-0 place-items-center rounded-xl bg-orange-50 text-orange-600 ring-1 ring-orange-100">
                <Plug className="size-5" />
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <h2 className="text-lg font-bold">{mcp.name || mcp.id}</h2>
                  <Badge variant="outline">{mcp.transport}</Badge>
                  {mcp.effective_enabled ? (
                    <Badge variant="success">
                      <span className="size-1.5 rounded-full bg-emerald-500" /> enabled
                    </Badge>
                  ) : (
                    <Badge>disabled</Badge>
                  )}
                  <Badge>{mcp.default_enabled ? "default on" : "default off"}</Badge>
                </div>
                <p className="mt-2 text-sm text-muted-foreground">
                  {mcp.description || "No description"}
                </p>
                <div className="mt-4 rounded-xl border border-border bg-muted/40 p-3 font-mono text-sm break-all">
                  {mcp.transport === "stdio" ? mcp.command || "-" : mcp.url_hint || "-"}
                </div>
              </div>
            </div>
          </Card>
        ) : null}
      </div>
    </main>
  );
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
