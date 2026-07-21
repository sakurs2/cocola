"use client";

import {
  CheckCircle2,
  ExternalLink,
  GitFork,
  Loader2,
  RefreshCw,
  ShieldCheck,
  Trash2,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";

type GitHubConnection = {
  enabled: boolean;
  status: string;
  external_login?: string;
  installation_url?: string;
};

export default function ConnectorsPage() {
  const [connection, setConnection] = useState<GitHubConnection | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setError("");
    try {
      const response = await fetch("/api/connectors/github", { cache: "no-store" });
      if (!response.ok) throw new Error(await responseError(response));
      setConnection((await response.json()) as GitHubConnection);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const register = async () => {
    setBusy(true);
    setError("");
    try {
      const response = await fetch("/api/connectors/github/manifest/start", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ return_to: "/connectors" }),
      });
      if (!response.ok) throw new Error(await responseError(response));
      const result = (await response.json()) as {
        registration_url?: string;
        state?: string;
        manifest?: Record<string, unknown>;
      };
      if (!result.registration_url || !result.state || !result.manifest) {
        throw new Error("GitHub registration response was incomplete");
      }
      sessionStorage.setItem("cocola.github.manifest.state", result.state);
      const form = document.createElement("form");
      form.method = "POST";
      form.action = result.registration_url;
      const input = document.createElement("input");
      input.type = "hidden";
      input.name = "manifest";
      input.value = JSON.stringify(result.manifest);
      form.appendChild(input);
      document.body.appendChild(form);
      form.submit();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
      setBusy(false);
    }
  };

  const disconnect = async () => {
    if (!window.confirm("Disconnect GitHub? Existing projects remain but cannot access GitHub.")) {
      return;
    }
    setBusy(true);
    setError("");
    try {
      const response = await fetch("/api/connectors/github", { method: "DELETE" });
      if (!response.ok) throw new Error(await responseError(response));
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="h-full overflow-y-auto px-5 py-8 sm:px-8">
      <main className="mx-auto w-full max-w-4xl pb-16">
        <div className="flex items-start gap-4">
          <div className="grid size-11 shrink-0 place-items-center rounded-2xl bg-emerald-500/10 text-emerald-600">
            <ShieldCheck className="size-5" />
          </div>
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">Connectors</h1>
            <p className="mt-1 text-sm text-muted-foreground">
              Connect personal services without sharing credentials with other Cocola users.
            </p>
          </div>
        </div>

        <section className="mt-9 overflow-hidden rounded-2xl border border-border bg-card shadow-card">
          <div className="flex items-center gap-4 p-5">
            <div className="grid size-11 shrink-0 place-items-center rounded-2xl bg-foreground text-background">
              <GitFork className="size-5" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="font-semibold">GitHub</h2>
                <StatusBadge status={connection?.status ?? "checking"} />
              </div>
              <p className="mt-1 text-sm text-muted-foreground">
                A private GitHub App owned by you powers repository projects and Agent operations.
              </p>
            </div>
            <button
              type="button"
              onClick={() => void load()}
              disabled={busy}
              aria-label="Refresh GitHub connector"
              className="grid size-9 place-items-center rounded-xl border border-border text-muted-foreground hover:bg-muted disabled:opacity-50"
            >
              <RefreshCw className="size-4" />
            </button>
          </div>

          <div className="border-t border-border bg-muted/20 px-5 py-5">
            {!connection ? (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Loader2 className="size-4 animate-spin" /> Checking connection…
              </div>
            ) : null}
            {connection?.status === "disabled" ? (
              <div>
                <p className="text-sm font-medium">GitHub Connector is disabled</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  Ask an administrator to enable the GitHub Manifest Connector feature.
                </p>
              </div>
            ) : null}
            {connection?.status === "not_configured" || connection?.status === "error" ? (
              <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <p className="text-sm font-medium">Create your personal GitHub App</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    GitHub opens a pre-filled registration page. Cocola encrypts the returned App
                    credentials.
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => void register()}
                  disabled={busy}
                  className="inline-flex h-10 shrink-0 items-center justify-center gap-2 rounded-xl bg-foreground px-4 text-sm font-medium text-background disabled:opacity-50"
                >
                  {busy ? (
                    <Loader2 className="size-4 animate-spin" />
                  ) : (
                    <GitFork className="size-4" />
                  )}
                  Register GitHub App
                </button>
              </div>
            ) : null}
            {connection?.status === "installation_required" ? (
              <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <p className="text-sm font-medium">Install the App on your personal account</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    Choose the repositories Cocola may access. Organization installations are not
                    accepted.
                  </p>
                </div>
                <a
                  href={connection.installation_url}
                  className="inline-flex h-10 shrink-0 items-center justify-center gap-2 rounded-xl bg-foreground px-4 text-sm font-medium text-background"
                >
                  Install App <ExternalLink className="size-4" />
                </a>
              </div>
            ) : null}
            {connection?.status === "ready" ? (
              <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
                <div className="flex items-center gap-3">
                  <CheckCircle2 className="size-5 text-emerald-500" />
                  <div>
                    <p className="text-sm font-medium">Connected as {connection.external_login}</p>
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      Repository access is checked when a Project or Agent operation starts.
                    </p>
                  </div>
                </div>
                <button
                  type="button"
                  onClick={() => void disconnect()}
                  disabled={busy}
                  className="inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-xl border border-border px-3 text-sm text-muted-foreground hover:bg-background hover:text-destructive disabled:opacity-50"
                >
                  <Trash2 className="size-4" /> Disconnect
                </button>
              </div>
            ) : null}
            {connection?.status === "reauthorization_required" ? (
              <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <p className="text-sm font-medium">GitHub authorization expired</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    Reinstall or reconnect your App to restore GitHub Projects.
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => void register()}
                  disabled={busy}
                  className="inline-flex h-10 items-center justify-center rounded-xl bg-foreground px-4 text-sm font-medium text-background disabled:opacity-50"
                >
                  Reconnect
                </button>
              </div>
            ) : null}
          </div>
        </section>

        {error ? (
          <p
            role="alert"
            className="mt-5 rounded-xl bg-destructive/10 px-4 py-3 text-sm text-destructive"
          >
            {error}
          </p>
        ) : null}
      </main>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const ready = status === "ready";
  const label = status.replaceAll("_", " ").replace(/^./, (value) => value.toUpperCase());
  return (
    <span
      className={
        ready
          ? "rounded-full bg-emerald-500/10 px-2 py-0.5 text-xs font-medium text-emerald-600"
          : "rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground"
      }
    >
      {label}
    </span>
  );
}

async function responseError(response: Response) {
  const payload = (await response.json().catch(() => null)) as {
    error?: { message?: string };
  } | null;
  return payload?.error?.message || `Request failed (${response.status})`;
}
