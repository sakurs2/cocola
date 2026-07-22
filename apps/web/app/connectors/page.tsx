"use client";

import { ExternalLink, Loader2, RefreshCw, ShieldCheck, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState, type ReactNode } from "react";

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
        <div className="flex items-center gap-4">
          <div className="grid size-11 shrink-0 place-items-center rounded-2xl bg-emerald-500/10 text-emerald-600">
            <ShieldCheck className="size-5" />
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">Connectors</h1>
        </div>

        <section className="mt-9 grid grid-cols-[repeat(auto-fill,minmax(min(100%,17.5rem),17.5rem))] gap-4">
          <article className="w-full rounded-3xl border border-border bg-card p-5 shadow-card">
            <div className="flex items-center gap-3.5">
              <div className="grid size-12 shrink-0 place-items-center rounded-2xl bg-foreground text-background">
                <GitHubIcon className="size-6" />
              </div>
              <div className="min-w-0">
                <h2 className="text-base font-semibold tracking-tight">GitHub</h2>
                <p className="mt-0.5 text-xs leading-4 text-muted-foreground">
                  Repositories and Agent tools
                </p>
              </div>
            </div>

            <div className="mt-5">
              {!connection ? (
                <ConnectorButton disabled icon={<Loader2 className="size-4 animate-spin" />}>
                  Checking…
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
                    className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-xl bg-foreground px-4 text-sm font-medium text-background transition-colors hover:bg-foreground/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
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
      className={`inline-flex h-10 w-full items-center justify-center gap-2 rounded-xl px-4 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 ${
        variant === "solid"
          ? "bg-foreground text-background hover:bg-foreground/90"
          : "border border-border text-foreground hover:bg-muted"
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

async function responseError(response: Response) {
  const payload = (await response.json().catch(() => null)) as {
    error?: { message?: string };
  } | null;
  return payload?.error?.message || `Request failed (${response.status})`;
}
