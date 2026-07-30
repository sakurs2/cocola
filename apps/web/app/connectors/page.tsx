"use client";

import { ExternalLink, Loader2, RefreshCw, ShieldCheck, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState, type ReactNode } from "react";
import { connectorResponseError } from "@/lib/connector-response-error.mjs";

type GitHubConnection = {
  enabled: boolean;
  status: string;
  external_login?: string;
  installation_url?: string;
};

type ConnectionLoadState = "checking" | "ready" | "failed";

export default function ConnectorsPage() {
  const [connection, setConnection] = useState<GitHubConnection | null>(null);
  const [loadState, setLoadState] = useState<ConnectionLoadState>("checking");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const displayState = githubConnectionState(connection, loadState);

  const load = useCallback(async () => {
    setLoadState("checking");
    setError("");
    try {
      const response = await fetch("/api/connectors/github", { cache: "no-store" });
      if (!response.ok) throw new Error(await connectorResponseError(response));
      setConnection((await response.json()) as GitHubConnection);
      setLoadState("ready");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
      setLoadState("failed");
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
      if (!response.ok) throw new Error(await connectorResponseError(response));
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
      if (!response.ok) throw new Error(await connectorResponseError(response));
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  return (
    <main className="user-canvas user-page user-theme-emerald h-full min-w-0 flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-5xl space-y-8 px-8 py-10 pb-16">
        <header className="flex items-center gap-4">
          <span className="user-page-icon">
            <ShieldCheck className="size-6" />
          </span>
          <div>
            <div className="user-eyebrow">Integrations</div>
            <h1 className="mt-0.5 text-2xl font-extrabold tracking-tight">Connectors</h1>
            <p className="mt-1 text-sm text-muted-foreground">
              Connect external services to power your Agents.
            </p>
          </div>
        </header>

        <section>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,18.75rem),18.75rem))] gap-4">
            <article className="user-card user-card--hover group">
              <div className="flex items-center gap-3.5">
                <div className="user-connector-logo grid size-12 shrink-0 place-items-center rounded-2xl bg-foreground text-background">
                  <GitHubIcon className="size-6" />
                </div>
                <div className="min-w-0">
                  <h2 className="user-card-name">GitHub</h2>
                  <p className="user-card-desc mt-0.5">Repositories and Agent tools</p>
                </div>
              </div>

              <div className="mt-4 flex items-center gap-2 text-xs">
                <span className={`size-2 rounded-full ${displayState.dot}`} />
                <span className="font-medium text-foreground">{displayState.label}</span>
              </div>

              <div className="mt-5">
                {!connection && loadState === "checking" ? (
                  <ConnectorButton disabled icon={<Loader2 className="size-4 animate-spin" />}>
                    Checking…
                  </ConnectorButton>
                ) : null}
                {!connection && loadState === "failed" ? (
                  <ConnectorButton
                    onClick={() => void load()}
                    variant="outline"
                    icon={<RefreshCw className="size-4" />}
                  >
                    Retry
                  </ConnectorButton>
                ) : null}
                {connection?.status === "disabled" ? (
                  <ConnectorButton disabled>Unavailable</ConnectorButton>
                ) : null}
                {connection?.status === "not_configured" || connection?.status === "error" ? (
                  <ConnectorButton
                    onClick={() => void register()}
                    disabled={busy}
                    icon={
                      busy ? (
                        <Loader2 className="size-4 animate-spin" />
                      ) : (
                        <GitHubIcon className="size-4" />
                      )
                    }
                  >
                    Register on GitHub
                  </ConnectorButton>
                ) : null}
                {connection?.status === "installation_required" ? (
                  connection.installation_url ? (
                    <a
                      href={connection.installation_url}
                      className="user-connector-btn inline-flex h-10 w-full items-center justify-center gap-2 rounded-xl bg-foreground px-4 text-sm font-medium text-background hover:bg-foreground/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    >
                      Continue on GitHub <ExternalLink className="size-4" />
                    </a>
                  ) : (
                    <ConnectorButton disabled>Installation unavailable</ConnectorButton>
                  )
                ) : null}
                {connection?.status === "ready" ? (
                  <ConnectorButton
                    onClick={() => void disconnect()}
                    disabled={busy}
                    variant="outline"
                    icon={
                      busy ? (
                        <Loader2 className="size-4 animate-spin" />
                      ) : (
                        <Trash2 className="size-4" />
                      )
                    }
                  >
                    Disconnect{connection.external_login ? ` @${connection.external_login}` : ""}
                  </ConnectorButton>
                ) : null}
                {connection?.status === "reauthorization_required" ? (
                  <ConnectorButton
                    onClick={() => void register()}
                    disabled={busy}
                    icon={
                      busy ? (
                        <Loader2 className="size-4 animate-spin" />
                      ) : (
                        <GitHubIcon className="size-4" />
                      )
                    }
                  >
                    Reconnect
                  </ConnectorButton>
                ) : null}
                {connection &&
                ![
                  "disabled",
                  "not_configured",
                  "error",
                  "installation_required",
                  "ready",
                  "reauthorization_required",
                ].includes(connection.status) ? (
                  <ConnectorButton
                    onClick={() => void load()}
                    disabled={busy}
                    variant="outline"
                    icon={<RefreshCw className="size-4" />}
                  >
                    Refresh
                  </ConnectorButton>
                ) : null}
              </div>
            </article>
          </div>
        </section>

        {error ? (
          <div
            role="alert"
            className="flex items-center justify-between gap-3 rounded-xl bg-destructive/10 px-4 py-3 text-sm text-destructive"
          >
            <span>{error}</span>
            {connection && loadState === "failed" ? (
              <button
                type="button"
                onClick={() => void load()}
                className="shrink-0 rounded-lg px-2 py-1 font-medium hover:bg-destructive/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                Retry
              </button>
            ) : null}
          </div>
        ) : null}
      </div>
    </main>
  );
}

function githubConnectionState(
  connection: GitHubConnection | null,
  loadState: ConnectionLoadState,
) {
  if (!connection) {
    return loadState === "failed"
      ? { label: "Connection check failed", dot: "bg-red-500" }
      : { label: "Checking", dot: "bg-muted-foreground" };
  }
  const states: Record<string, { label: string; dot: string }> = {
    disabled: { label: "Unavailable", dot: "bg-slate-400" },
    not_configured: { label: "Not connected", dot: "bg-muted-foreground" },
    error: { label: "Connection error", dot: "bg-red-500" },
    installation_required: { label: "Setup required", dot: "bg-amber-500" },
    ready: { label: "Connected", dot: "bg-emerald-500" },
    reauthorization_required: { label: "Reconnect required", dot: "bg-amber-500" },
  };
  return states[connection.status] ?? { label: "Status unavailable", dot: "bg-muted-foreground" };
}

function ConnectorButton({
  children,
  disabled = false,
  icon,
  onClick,
  variant = "solid",
}: {
  children: ReactNode;
  disabled?: boolean;
  icon?: ReactNode;
  onClick?: () => void;
  variant?: "solid" | "outline";
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={`inline-flex h-10 w-full items-center justify-center gap-2 rounded-xl px-4 text-sm font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 ${
        variant === "solid"
          ? "user-connector-btn bg-foreground text-background hover:bg-foreground/90"
          : "border border-border text-foreground transition-colors hover:bg-muted"
      }`}
    >
      {icon}
      <span className="truncate">{children}</span>
    </button>
  );
}

function GitHubIcon({ className }: { className?: string }) {
  return (
    <svg
      aria-hidden="true"
      className={className}
      viewBox="0 0 16 16"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82A7.68 7.68 0 0 1 8 3.75c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
    </svg>
  );
}
