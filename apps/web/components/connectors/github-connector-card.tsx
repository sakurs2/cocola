"use client";

import { ExternalLink, Loader2, RefreshCw, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslations } from "next-intl";
import { ActionConfirmDialog } from "@/components/ui/action-dialog";
import { connectorResponseError } from "@/lib/connector-response-error.mjs";
import {
  ConnectorSummaryCard,
  type ConnectorSummaryAction,
  type ConnectorSummaryStatus,
} from "./connector-summary-card";

type GitHubConnection = {
  enabled: boolean;
  status: string;
  external_login?: string;
  installation_url?: string;
};

type ConnectionLoadState = "checking" | "ready" | "failed";

export function GitHubConnectorCard() {
  const t = useTranslations("connectors");
  const [connection, setConnection] = useState<GitHubConnection | null>(null);
  const [loadState, setLoadState] = useState<ConnectionLoadState>("checking");
  const [busy, setBusy] = useState(false);
  const [disconnectOpen, setDisconnectOpen] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoadState("checking");
    setError("");
    try {
      const response = await fetch("/api/connectors/github", { cache: "no-store" });
      if (!response.ok) throw new Error(await connectorResponseError(response));
      setConnection((await response.json()) as GitHubConnection);
      setLoadState("ready");
    } catch (cause) {
      setError(errorMessage(cause));
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
        throw new Error(t("registrationIncomplete"));
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
      setError(errorMessage(cause));
      setBusy(false);
    }
  };

  const disconnect = async () => {
    setBusy(true);
    setError("");
    try {
      const response = await fetch("/api/connectors/github", { method: "DELETE" });
      if (!response.ok) throw new Error(await connectorResponseError(response));
      setDisconnectOpen(false);
      await load();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  };

  const status = githubStatus(connection, loadState, t);
  const action = githubAction({
    busy,
    connection,
    loadState,
    load,
    register,
    setDisconnectOpen,
    t,
  });

  return (
    <>
      <ConnectorSummaryCard
        action={action}
        description={t("githubDescription")}
        icon={<GitHubIcon className="size-6" />}
        provider="github"
        status={status}
        title="GitHub"
      />
      <ActionConfirmDialog
        busy={busy}
        confirmLabel={t("disconnectConfirm")}
        description={t("disconnectDescription")}
        error={error || null}
        icon={Trash2}
        open={disconnectOpen}
        title={t("disconnectTitle")}
        tone="danger"
        onConfirm={() => void disconnect()}
        onOpenChange={setDisconnectOpen}
      />
    </>
  );
}

function githubAction({
  busy,
  connection,
  loadState,
  load,
  register,
  setDisconnectOpen,
  t,
}: {
  busy: boolean;
  connection: GitHubConnection | null;
  loadState: ConnectionLoadState;
  load: () => Promise<void>;
  register: () => Promise<void>;
  setDisconnectOpen: (open: boolean) => void;
  t: ReturnType<typeof useTranslations<"connectors">>;
}): ConnectorSummaryAction {
  if (!connection && loadState === "checking") {
    return {
      disabled: true,
      icon: <Loader2 className="size-4 animate-spin motion-reduce:animate-none" />,
      label: t("checking"),
    };
  }
  if (!connection && loadState === "failed") {
    return {
      icon: <RefreshCw className="size-4" />,
      label: t("retry"),
      onPress: () => void load(),
      outline: true,
    };
  }
  if (connection?.status === "disabled") {
    return { disabled: true, label: t("unavailable") };
  }
  if (connection?.status === "installation_required") {
    return connection.installation_url
      ? {
          icon: <ExternalLink className="size-4" />,
          label: t("continueGithub"),
          onPress: () => window.location.assign(connection.installation_url!),
        }
      : { disabled: true, label: t("installationUnavailable") };
  }
  if (connection?.status === "ready") {
    return {
      disabled: busy,
      icon: busy ? (
        <Loader2 className="size-4 animate-spin motion-reduce:animate-none" />
      ) : (
        <Trash2 className="size-4" />
      ),
      label: `${t("disconnect")}${connection.external_login ? ` @${connection.external_login}` : ""}`,
      onPress: () => setDisconnectOpen(true),
      outline: true,
    };
  }
  if (connection?.status === "reauthorization_required") {
    return {
      disabled: busy,
      icon: busy ? (
        <Loader2 className="size-4 animate-spin motion-reduce:animate-none" />
      ) : (
        <GitHubIcon className="size-4" />
      ),
      label: t("reconnect"),
      onPress: () => void register(),
    };
  }
  if (connection?.status === "not_configured" || connection?.status === "error") {
    return {
      disabled: busy,
      icon: busy ? (
        <Loader2 className="size-4 animate-spin motion-reduce:animate-none" />
      ) : (
        <GitHubIcon className="size-4" />
      ),
      label: t("registerGithub"),
      onPress: () => void register(),
    };
  }
  return {
    disabled: busy,
    icon: <RefreshCw className="size-4" />,
    label: t("refresh"),
    onPress: () => void load(),
    outline: true,
  };
}

function githubStatus(
  connection: GitHubConnection | null,
  loadState: ConnectionLoadState,
  t: ReturnType<typeof useTranslations<"connectors">>,
): ConnectorSummaryStatus {
  if (!connection) {
    return loadState === "failed"
      ? {
          label: t("connectionCheckFailed"),
          dotClassName: "bg-danger",
          textClassName: "text-danger",
        }
      : {
          label: t("checkingConnection"),
          dotClassName: "bg-warning",
          textClassName: "text-foreground",
          checking: true,
        };
  }
  const states: Record<string, ConnectorSummaryStatus> = {
    disabled: {
      label: t("unavailable"),
      dotClassName: "bg-surface-secondary",
      textClassName: "text-muted",
    },
    not_configured: {
      label: t("notConnected"),
      dotClassName: "bg-surface-secondary",
      textClassName: "text-foreground",
    },
    error: { label: t("connectionError"), dotClassName: "bg-danger", textClassName: "text-danger" },
    installation_required: {
      label: t("setupRequired"),
      dotClassName: "bg-warning",
      textClassName: "text-foreground",
    },
    ready: { label: t("connected"), dotClassName: "bg-success", textClassName: "text-success" },
    reauthorization_required: {
      label: t("reconnectRequired"),
      dotClassName: "bg-warning",
      textClassName: "text-foreground",
    },
  };
  return (
    states[connection.status] ?? {
      label: t("statusUnavailable"),
      dotClassName: "bg-surface-secondary",
      textClassName: "text-muted",
    }
  );
}

function GitHubIcon({ className }: { className?: string }) {
  return (
    <svg aria-hidden="true" className={className} fill="currentColor" viewBox="0 0 16 16">
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82A7.68 7.68 0 0 1 8 3.75c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
    </svg>
  );
}

function errorMessage(cause: unknown) {
  return cause instanceof Error ? cause.message : String(cause);
}
