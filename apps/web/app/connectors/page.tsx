"use client";

import { Button, Card } from "@heroui/react";
import { Sheet } from "@heroui-pro/react/sheet";
import { ExternalLink, Loader2, RefreshCw, ShieldCheck, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import {
  WorkspacePageFrame,
  WorkspacePageHeader,
} from "@/components/heroui-workspace/workspace-ui";
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
  const [disconnectOpen, setDisconnectOpen] = useState(false);
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
    setBusy(true);
    setError("");
    try {
      const response = await fetch("/api/connectors/github", { method: "DELETE" });
      if (!response.ok) throw new Error(await connectorResponseError(response));
      setDisconnectOpen(false);
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  const action = connectorAction({
    busy,
    connection,
    loadState,
    load,
    register,
    disconnect,
    setDisconnectOpen,
  });

  return (
    <WorkspacePageFrame>
      <WorkspacePageHeader
        description="Connect external services to power your agents."
        icon={<ShieldCheck className="size-5" />}
        title="Connectors"
      />

      {error ? (
        <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">{error}</div>
      ) : null}

      <section className="max-w-[300px]">
        <Card className="cocola-web-connector-card p-4">
          <Card.Content className="flex min-w-0 flex-col p-0">
            <div className="flex min-w-0 items-center gap-3">
              <span className="cocola-web-connector-icon flex size-12 shrink-0 items-center justify-center rounded-2xl bg-zinc-950 text-white dark:bg-white dark:text-zinc-950">
                <GitHubIcon className="size-6" />
              </span>
              <span className="min-w-0">
                <span className="text-foreground block font-semibold">GitHub</span>
                <span className="text-muted mt-1 block truncate text-sm">
                  Repositories and agent tools
                </span>
              </span>
            </div>
            <div className="mt-4 flex items-center gap-2 text-xs font-medium">
              <span className={`size-2 rounded-full ${displayState.dot}`} />
              <span className={displayState.text}>{displayState.label}</span>
            </div>
            <Button
              fullWidth
              className="cocola-web-connector-action mt-5 bg-zinc-950 text-white dark:bg-white dark:text-zinc-950"
              isDisabled={action.disabled}
              variant={action.outline ? "outline" : "primary"}
              onPress={action.onPress}
            >
              {action.icon}
              {action.label}
            </Button>
          </Card.Content>
        </Card>
      </section>

      <Sheet
        isOpen={disconnectOpen}
        placement="right"
        onOpenChange={(open) => !busy && setDisconnectOpen(open)}
      >
        <Sheet.Backdrop>
          <Sheet.Content className="w-full md:w-[440px]">
            <Sheet.Dialog>
              <Sheet.CloseTrigger aria-label="Close GitHub disconnect confirmation" />
              <Sheet.Header>
                <Sheet.Heading>Disconnect GitHub?</Sheet.Heading>
                <p className="text-muted text-sm leading-6">
                  Existing Projects remain available, but Cocola will no longer be able to access
                  their GitHub repositories.
                </p>
              </Sheet.Header>
              <Sheet.Body>
                <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">
                  You can reconnect the GitHub App later.
                </div>
              </Sheet.Body>
              <Sheet.Footer className="gap-2">
                <Button
                  isDisabled={busy}
                  variant="outline"
                  onPress={() => setDisconnectOpen(false)}
                >
                  Cancel
                </Button>
                <Button isPending={busy} variant="danger" onPress={() => void disconnect()}>
                  Disconnect GitHub
                </Button>
              </Sheet.Footer>
            </Sheet.Dialog>
          </Sheet.Content>
        </Sheet.Backdrop>
      </Sheet>
    </WorkspacePageFrame>
  );
}

function connectorAction({
  busy,
  connection,
  loadState,
  load,
  register,
  disconnect,
  setDisconnectOpen,
}: {
  busy: boolean;
  connection: GitHubConnection | null;
  loadState: ConnectionLoadState;
  load: () => Promise<void>;
  register: () => Promise<void>;
  disconnect: () => Promise<void>;
  setDisconnectOpen: (open: boolean) => void;
}) {
  if (!connection && loadState === "checking") {
    return {
      disabled: true,
      icon: <Loader2 className="size-4 animate-spin" />,
      label: "Checking…",
      onPress: undefined,
      outline: false,
    };
  }
  if (!connection && loadState === "failed") {
    return {
      disabled: false,
      icon: <RefreshCw className="size-4" />,
      label: "Retry",
      onPress: () => void load(),
      outline: true,
    };
  }
  if (connection?.status === "disabled") {
    return { disabled: true, icon: null, label: "Unavailable", onPress: undefined, outline: false };
  }
  if (connection?.status === "installation_required") {
    return connection.installation_url
      ? {
          disabled: false,
          icon: <ExternalLink className="size-4" />,
          label: "Continue on GitHub",
          onPress: () => window.location.assign(connection.installation_url!),
          outline: false,
        }
      : {
          disabled: true,
          icon: null,
          label: "Installation unavailable",
          onPress: undefined,
          outline: false,
        };
  }
  if (connection?.status === "ready") {
    return {
      disabled: busy,
      icon: busy ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />,
      label: `Disconnect${connection.external_login ? ` @${connection.external_login}` : ""}`,
      onPress: () => setDisconnectOpen(true),
      outline: true,
    };
  }
  if (connection?.status === "reauthorization_required") {
    return {
      disabled: busy,
      icon: busy ? <Loader2 className="size-4 animate-spin" /> : <GitHubIcon className="size-4" />,
      label: "Reconnect",
      onPress: () => void register(),
      outline: false,
    };
  }
  if (connection?.status === "not_configured" || connection?.status === "error") {
    return {
      disabled: busy,
      icon: busy ? <Loader2 className="size-4 animate-spin" /> : <GitHubIcon className="size-4" />,
      label: "Register on GitHub",
      onPress: () => void register(),
      outline: false,
    };
  }
  return {
    disabled: busy,
    icon: <RefreshCw className="size-4" />,
    label: "Refresh",
    onPress: () => void load(),
    outline: true,
  };
}

function githubConnectionState(
  connection: GitHubConnection | null,
  loadState: ConnectionLoadState,
) {
  if (!connection) {
    return loadState === "failed"
      ? { label: "Connection check failed", dot: "bg-danger", text: "text-danger" }
      : { label: "Checking connection", dot: "bg-warning animate-pulse", text: "text-foreground" };
  }
  const states: Record<string, { label: string; dot: string; text: string }> = {
    disabled: { label: "Unavailable", dot: "bg-surface-secondary", text: "text-muted" },
    not_configured: {
      label: "Not connected",
      dot: "bg-surface-secondary",
      text: "text-foreground",
    },
    error: { label: "Connection error", dot: "bg-danger", text: "text-danger" },
    installation_required: { label: "Setup required", dot: "bg-warning", text: "text-foreground" },
    ready: { label: "Connected", dot: "bg-success", text: "text-success" },
    reauthorization_required: {
      label: "Reconnect required",
      dot: "bg-warning",
      text: "text-foreground",
    },
  };
  return (
    states[connection.status] ?? {
      label: "Status unavailable",
      dot: "bg-surface-secondary",
      text: "text-muted",
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
