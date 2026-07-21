"use client";

import { useCallback, useEffect, useState } from "react";
import { ExternalLink, GitFork, Loader2, RefreshCw, Unplug } from "lucide-react";
import Link from "next/link";

type Connection = {
  enabled: boolean;
  status: string;
  external_login?: string;
  installation_url?: string;
};

export function GitHubIntegrationPanel() {
  const [connection, setConnection] = useState<Connection | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      const response = await fetch("/api/scm/github/connection", { cache: "no-store" });
      if (!response.ok) throw new Error("Could not load GitHub integration");
      setConnection((await response.json()) as Connection);
    } catch (loadError) {
      setError(
        loadError instanceof Error ? loadError.message : "Could not load GitHub integration",
      );
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const disconnect = async () => {
    if (
      !window.confirm(
        "Disconnect GitHub? Existing project snapshots remain, but clone and inspect will stop.",
      )
    )
      return;
    setBusy(true);
    try {
      const response = await fetch("/api/scm/github/connection", { method: "DELETE" });
      if (!response.ok) throw new Error("Could not disconnect GitHub");
      await refresh();
    } catch (disconnectError) {
      setError(
        disconnectError instanceof Error ? disconnectError.message : "Could not disconnect GitHub",
      );
      setBusy(false);
    }
  };

  return (
    <section className="rounded-2xl border border-border bg-card shadow-card">
      <div className="flex items-center gap-3 border-b border-border px-4 py-3">
        <div className="grid size-8 place-items-center rounded-xl bg-foreground/10">
          <GitFork className="size-4" />
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold">GitHub Integration</h2>
          <p className="text-xs text-muted-foreground">Personal repositories for Cocola Projects</p>
        </div>
        {busy ? <Loader2 className="size-4 animate-spin text-muted-foreground" /> : null}
      </div>
      <div className="p-4">
        <div className="flex flex-wrap items-center gap-3">
          <span className="rounded-full bg-muted px-2.5 py-1 text-xs font-medium capitalize">
            {connection?.status?.replaceAll("_", " ") || "Checking"}
          </span>
          {connection?.external_login ? (
            <span className="text-sm text-muted-foreground">@{connection.external_login}</span>
          ) : null}
        </div>
        <div className="mt-4 flex flex-wrap gap-2">
          {connection?.status === "ready" ? (
            <>
              <Link
                href="/projects/new"
                className="rounded-xl bg-primary px-3 py-2 text-sm font-medium text-primary-foreground"
              >
                New project
              </Link>
              <button
                type="button"
                onClick={() => void disconnect()}
                className="inline-flex items-center gap-2 rounded-xl border border-border px-3 py-2 text-sm"
              >
                <Unplug className="size-4" /> Disconnect
              </button>
            </>
          ) : null}
          {connection?.status === "disconnected" ||
          connection?.status === "reauthorization_required" ? (
            <Link
              href="/projects/new"
              className="rounded-xl bg-foreground px-3 py-2 text-sm font-medium text-background"
            >
              Connect GitHub
            </Link>
          ) : null}
          {connection?.status === "installation_required" ? (
            <>
              <a
                href={connection.installation_url}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-2 rounded-xl bg-foreground px-3 py-2 text-sm font-medium text-background"
              >
                Install App <ExternalLink className="size-4" />
              </a>
              <button
                type="button"
                onClick={() => void refresh()}
                className="inline-flex items-center gap-2 rounded-xl border border-border px-3 py-2 text-sm"
              >
                <RefreshCw className="size-4" /> Refresh
              </button>
            </>
          ) : null}
        </div>
        {connection?.status === "disabled" ? (
          <p className="mt-3 text-sm text-muted-foreground">
            GitHub Projects are disabled by the administrator.
          </p>
        ) : null}
        {error ? <p className="mt-3 text-sm text-red-600">{error}</p> : null}
      </div>
    </section>
  );
}
