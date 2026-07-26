"use client";

import * as Dialog from "@radix-ui/react-dialog";
import {
  AlertTriangle,
  ChevronDown,
  Clock3,
  Copy,
  ExternalLink,
  Loader2,
  MessageCircleMore,
  Pause,
  Play,
  RefreshCw,
  Settings2,
  Trash2,
  X,
  type LucideIcon,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import Image from "next/image";
import { useWorkspaceToast } from "@/components/assistant-ui/workspace-toast";
import { ActionConfirmDialog } from "@/components/ui/action-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { connectorResponseError } from "@/lib/connector-response-error.mjs";

type FeishuConnection = {
  connected: boolean;
  enabled: boolean;
  status: string;
  domain?: "feishu" | "lark";
  bot_name?: string;
  model_route_id?: string;
  model_alias?: string;
  last_connected_at?: string;
  last_error_code?: string;
  registration?: RegistrationFlow;
};

type ConnectorModel = {
  id: string;
  alias: string;
  label: string;
  provider?: string;
  protocols?: string[];
  is_default?: boolean;
};

type AgentRuntime = {
  id: string;
  model_protocol: string;
  is_default?: boolean;
};

type ProductConfig = {
  agent_runtime?: {
    default_id?: string;
  };
};

type RegistrationFlow = {
  id: string;
  provider: "feishu";
  status: string;
  verification_url?: string;
  expires_at: string;
  error_code?: string;
};

type ManualResult = {
  connection: FeishuConnection;
  bind_code: string;
  expires_at: string;
};

type ConnectionLoadState = "checking" | "ready" | "failed";

const ACTIVE_FLOW_STATES = new Set(["starting", "awaiting_user", "authorizing"]);
const TERMINAL_FLOW_STATES = new Set([
  "ready",
  "denied",
  "expired",
  "failed",
  "interrupted",
  "cancelled",
]);

export function FeishuConnectorCard() {
  const { showError, showSuccess } = useWorkspaceToast();
  const [connection, setConnection] = useState<FeishuConnection | null>(null);
  const [loadState, setLoadState] = useState<ConnectionLoadState>("checking");
  const [flow, setFlow] = useState<RegistrationFlow | null>(null);
  const [bind, setBind] = useState<{ code: string; expiresAt: string } | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [manualOpen, setManualOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [models, setModels] = useState<ConnectorModel[]>([]);
  const [modelsLoading, setModelsLoading] = useState(false);
  const [modelsError, setModelsError] = useState("");
  const [disconnectOpen, setDisconnectOpen] = useState(false);
  const [clock, setClock] = useState(() => Date.now());
  const activeFlowID = flow && ACTIVE_FLOW_STATES.has(flow.status) ? flow.id : "";

  const load = useCallback(async () => {
    setLoadState("checking");
    setError("");
    try {
      const response = await fetch("/api/connectors/feishu", { cache: "no-store" });
      if (!response.ok) throw new Error(await connectorResponseError(response));
      const next = (await response.json()) as FeishuConnection;
      setConnection(next);
      if (next.registration) setFlow(next.registration);
      setLoadState("ready");
    } catch (cause) {
      setError(errorMessage(cause));
      setLoadState("failed");
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const connecting = connection?.connected && connection.status === "connecting";
  useEffect(() => {
    if (!connecting) return;
    let stopped = false;
    let attempts = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const controller = new AbortController();
    const pollConnection = async () => {
      attempts += 1;
      try {
        const response = await fetch("/api/connectors/feishu", {
          cache: "no-store",
          signal: controller.signal,
        });
        if (!response.ok) throw new Error(await connectorResponseError(response));
        const next = (await response.json()) as FeishuConnection;
        if (stopped) return;
        setConnection(next);
        if (next.registration) setFlow(next.registration);
        if (next.status !== "connecting" || attempts >= 60) return;
        timer = setTimeout(() => void pollConnection(), 2000);
      } catch (cause) {
        if (stopped) return;
        const message = errorMessage(cause);
        setError(message);
        showError(message);
      }
    };
    timer = setTimeout(() => void pollConnection(), 2000);
    return () => {
      stopped = true;
      controller.abort();
      if (timer) clearTimeout(timer);
    };
  }, [connecting, showError]);

  useEffect(() => {
    if (!activeFlowID) return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const controller = new AbortController();
    const poll = async () => {
      try {
        const response = await fetch(
          `/api/connectors/feishu/registrations/${encodeURIComponent(activeFlowID)}`,
          { cache: "no-store", signal: controller.signal },
        );
        if (!response.ok) throw new Error(await connectorResponseError(response));
        const next = (await response.json()) as RegistrationFlow;
        if (stopped) return;
        setFlow(next);
        if (next.status === "ready") {
          await load();
          showSuccess("Feishu authorization completed");
          return;
        }
        if (TERMINAL_FLOW_STATES.has(next.status)) return;
        timer = setTimeout(() => void poll(), 2000);
      } catch (cause) {
        if (stopped) return;
        const message = errorMessage(cause);
        setError(message);
        showError(message);
      }
    };
    timer = setTimeout(() => void poll(), 2000);
    return () => {
      stopped = true;
      controller.abort();
      if (timer) clearTimeout(timer);
    };
  }, [activeFlowID, load, showError, showSuccess]);

  useEffect(() => {
    if (!activeFlowID) return;
    setClock(Date.now());
    const timer = window.setInterval(() => setClock(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [activeFlowID]);

  useEffect(() => {
    if (!settingsOpen) return;
    const controller = new AbortController();
    setModelsLoading(true);
    setModelsError("");
    void (async () => {
      try {
        const [modelsResponse, runtimesResponse, configResponse] = await Promise.all([
          fetch("/api/models", { cache: "no-store", signal: controller.signal }),
          fetch("/api/agent-runtimes", { cache: "no-store", signal: controller.signal }),
          fetch("/api/product-config", { cache: "no-store", signal: controller.signal }),
        ]);
        if (!modelsResponse.ok || !runtimesResponse.ok || !configResponse.ok) {
          throw new Error("Model configuration is temporarily unavailable.");
        }
        const modelRows = (await modelsResponse.json()) as ConnectorModel[];
        const runtimeRows = (await runtimesResponse.json()) as AgentRuntime[];
        const config = (await configResponse.json()) as ProductConfig;
        const defaultRuntimeID = config.agent_runtime?.default_id?.trim() ?? "";
        const runtime =
          runtimeRows.find((item) => item.id === defaultRuntimeID) ??
          runtimeRows.find((item) => item.is_default);
        if (!runtime?.model_protocol) {
          throw new Error("The default Agent runtime is not configured.");
        }
        const compatible = Array.isArray(modelRows)
          ? modelRows.filter(
              (model) =>
                typeof model.id === "string" &&
                typeof model.alias === "string" &&
                typeof model.label === "string" &&
                Array.isArray(model.protocols) &&
                model.protocols.includes(runtime.model_protocol),
            )
          : [];
        setModels(compatible);
        if (compatible.length === 0) {
          setModelsError("No model is available for the default Agent runtime.");
        }
      } catch (cause) {
        if (controller.signal.aborted) return;
        setModels([]);
        setModelsError(errorMessage(cause));
      } finally {
        if (!controller.signal.aborted) setModelsLoading(false);
      }
    })();
    return () => controller.abort();
  }, [settingsOpen]);

  const run = async (action: string, request: () => Promise<Response>, successMessage: string) => {
    setBusy(action);
    setError("");
    try {
      const response = await request();
      if (!response.ok) throw new Error(await connectorResponseError(response));
      showSuccess(successMessage);
      await load();
    } catch (cause) {
      const message = errorMessage(cause);
      setError(message);
      showError(message);
      throw cause;
    } finally {
      setBusy("");
    }
  };

  const startRegistration = async () => {
    setBusy("register");
    setError("");
    setBind(null);
    try {
      const response = await fetch("/api/connectors/feishu/registrations", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: "{}",
      });
      if (!response.ok) throw new Error(await connectorResponseError(response));
      const next = (await response.json()) as RegistrationFlow;
      setFlow(next);
      if (next.status === "ready") await load();
    } catch (cause) {
      const message = errorMessage(cause);
      setError(message);
      showError(message);
    } finally {
      setBusy("");
    }
  };

  const cancelRegistration = async () => {
    if (!flow) return;
    setBusy("cancel");
    try {
      const response = await fetch(
        `/api/connectors/feishu/registrations/${encodeURIComponent(flow.id)}`,
        { method: "DELETE" },
      );
      if (!response.ok) throw new Error(await connectorResponseError(response));
      setFlow(null);
      showSuccess("Feishu authorization cancelled");
    } catch (cause) {
      const message = errorMessage(cause);
      setError(message);
      showError(message);
    } finally {
      setBusy("");
    }
  };

  const toggle = async (enabled: boolean) => {
    try {
      await run(
        enabled ? "enable" : "disable",
        () =>
          fetch(`/api/connectors/feishu/${enabled ? "enable" : "disable"}`, {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: "{}",
          }),
        enabled ? "Feishu connector enabled" : "Feishu connector paused",
      );
    } catch {
      // run already presents the error.
    }
  };

  const disconnect = async () => {
    try {
      await run(
        "disconnect",
        () =>
          fetch("/api/connectors/feishu", {
            method: "DELETE",
            headers: { "content-type": "application/json" },
            body: "{}",
          }),
        "Feishu connector disconnected",
      );
      setFlow(null);
      setBind(null);
      setDisconnectOpen(false);
    } catch {
      // Keep the confirmation dialog open so the user can retry.
    }
  };

  const copy = async (value: string, message: string) => {
    try {
      await navigator.clipboard.writeText(value);
      showSuccess(message);
    } catch {
      showError("Copy failed. Please select and copy the value manually.");
    }
  };

  const remainingSeconds = flow
    ? Math.max(0, Math.ceil((new Date(flow.expires_at).getTime() - clock) / 1000))
    : 0;
  const displayState = useMemo(
    () => connectionState(connection, flow, loadState),
    [connection, flow, loadState],
  );

  return (
    <>
      <article className="w-full rounded-3xl border border-border bg-card p-5 shadow-card">
        <div className="flex items-center gap-3.5">
          <div className="grid size-12 shrink-0 place-items-center rounded-2xl border border-blue-100 bg-white">
            <Image src="/feishu-logo.svg" alt="" width={32} height={32} className="size-8" />
          </div>
          <div className="min-w-0 flex-1">
            <h2 className="text-base font-semibold tracking-tight">Feishu</h2>
            <p className="mt-0.5 text-xs leading-4 text-muted-foreground">
              Private chat with your Agent
            </p>
          </div>
          {connection?.connected ? (
            <button
              type="button"
              aria-label="Configure Feishu"
              title="Configure Feishu"
              onClick={() => setSettingsOpen(true)}
              className="relative grid size-9 shrink-0 place-items-center rounded-xl text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <Settings2 className="size-4" />
              {!connection.model_route_id ? (
                <span
                  aria-hidden="true"
                  className="absolute right-1.5 top-1.5 size-2 rounded-full border-2 border-card bg-amber-500"
                />
              ) : null}
            </button>
          ) : null}
        </div>

        <div className="mt-4 flex items-center gap-2 text-xs">
          <span className={`size-2 rounded-full ${displayState.dot}`} />
          <span className="font-medium text-foreground">{displayState.label}</span>
          {connection?.domain ? (
            <span className="ml-auto rounded-full bg-muted px-2 py-1 text-[11px] text-muted-foreground">
              {connection.domain === "lark" ? "Lark" : "Feishu"}
            </span>
          ) : null}
        </div>

        {connection?.bot_name ? (
          <p className="mt-3 truncate text-sm text-muted-foreground">
            Bot: <span className="text-foreground">{connection.bot_name}</span>
          </p>
        ) : null}

        {connection?.connected ? (
          <div className="mt-3 flex items-center justify-between gap-3 rounded-xl bg-muted/60 px-3 py-2 text-xs">
            <span className="text-muted-foreground">Model</span>
            <span
              className={`truncate font-medium ${
                connection.model_alias ? "text-foreground" : "text-amber-700"
              }`}
            >
              {connection.model_alias || "Choose a model"}
            </span>
          </div>
        ) : null}

        {flow && ACTIVE_FLOW_STATES.has(flow.status) ? (
          <div className="mt-4 rounded-2xl border border-blue-500/20 bg-blue-500/5 p-3">
            <div className="flex items-center gap-2 text-xs font-medium text-blue-700">
              <Clock3 className="size-3.5" />
              {flow.status === "awaiting_user"
                ? `Authorization expires in ${formatDuration(remainingSeconds)}`
                : "Preparing authorization…"}
            </div>
            {flow.verification_url ? (
              <div className="mt-3 space-y-2">
                <a
                  href={flow.verification_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex h-9 w-full items-center justify-center gap-2 rounded-xl bg-blue-600 px-3 text-sm font-medium text-white transition hover:bg-blue-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500/30"
                >
                  Continue in Feishu <ExternalLink className="size-4" />
                </a>
                <button
                  type="button"
                  onClick={() => void copy(flow.verification_url!, "Authorization link copied")}
                  className="inline-flex h-8 w-full items-center justify-center gap-2 rounded-xl text-xs text-muted-foreground transition hover:bg-muted hover:text-foreground"
                >
                  <Copy className="size-3.5" /> Copy link
                </button>
              </div>
            ) : null}
            <button
              type="button"
              disabled={busy === "cancel"}
              onClick={() => void cancelRegistration()}
              className="mt-2 w-full text-center text-xs text-muted-foreground transition hover:text-foreground disabled:opacity-50"
            >
              {busy === "cancel" ? "Cancelling…" : "Cancel authorization"}
            </button>
          </div>
        ) : null}

        {flow && ["denied", "expired", "failed", "interrupted"].includes(flow.status) ? (
          <StatusNotice icon={AlertTriangle} tone="danger" text={registrationFailureText(flow)} />
        ) : null}

        {bind ? (
          <div className="mt-4 rounded-2xl border border-amber-500/20 bg-amber-500/5 p-3">
            <p className="text-xs leading-5 text-amber-800">
              Send this once to the bot before the code expires:
            </p>
            <button
              type="button"
              onClick={() => void copy(`/bind ${bind.code}`, "Binding command copied")}
              className="mt-2 flex w-full items-center justify-between rounded-xl bg-background px-3 py-2 font-mono text-sm shadow-sm"
            >
              <span>/bind {bind.code}</span>
              <Copy className="size-3.5 text-muted-foreground" />
            </button>
            <p className="mt-2 text-[11px] text-amber-700">Expires {formatDate(bind.expiresAt)}</p>
          </div>
        ) : null}

        {connection?.status === "awaiting_bind" && !bind ? (
          <StatusNotice
            icon={AlertTriangle}
            tone="warning"
            text="This app is waiting for its owner. Re-enter the existing app credentials to generate a new one-time binding code."
          />
        ) : null}

        {connection?.status === "action_required" ? (
          <div className="mt-4 rounded-2xl border border-amber-500/20 bg-amber-500/5 p-3 text-xs leading-5 text-amber-800">
            The bot is waiting for publishing or tenant approval in Feishu.
            <a
              href={developerConsoleURL(connection.domain)}
              target="_blank"
              rel="noopener noreferrer"
              className="mt-2 inline-flex items-center gap-1 font-medium underline underline-offset-2"
            >
              Open developer console <ExternalLink className="size-3.5" />
            </a>
          </div>
        ) : null}

        {connection?.status === "error" ? (
          <StatusNotice
            icon={AlertTriangle}
            tone="danger"
            text={connectorErrorText(connection.last_error_code)}
          />
        ) : null}

        {error ? (
          <div
            role="alert"
            className="mt-4 flex items-center justify-between gap-3 rounded-xl bg-destructive/10 px-3 py-2 text-xs leading-5 text-destructive"
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

        <div className="mt-5 space-y-2">
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
          {connection?.status === "not_configured" && !flow ? (
            <ConnectorButton
              onClick={() => void startRegistration()}
              disabled={busy !== ""}
              icon={
                busy === "register" ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <MessageCircleMore className="size-4" />
                )
              }
            >
              Connect Feishu
            </ConnectorButton>
          ) : null}
          {connection?.status === "disabled" && connection.connected ? (
            <ConnectorButton
              onClick={() => void toggle(true)}
              disabled={busy !== ""}
              icon={
                busy === "enable" ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <Play className="size-4" />
                )
              }
            >
              Enable
            </ConnectorButton>
          ) : null}
          {connection?.status === "error" ? (
            <ConnectorButton
              onClick={() => void toggle(true)}
              disabled={busy !== ""}
              icon={
                busy === "enable" ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <RefreshCw className="size-4" />
                )
              }
            >
              Reconnect
            </ConnectorButton>
          ) : null}
          {connection?.connected &&
          connection.enabled &&
          ["connecting", "ready", "action_required", "awaiting_bind"].includes(
            connection.status,
          ) ? (
            <ConnectorButton
              onClick={() => void toggle(false)}
              disabled={busy !== ""}
              variant="outline"
              icon={
                busy === "disable" ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <Pause className="size-4" />
                )
              }
            >
              Pause
            </ConnectorButton>
          ) : null}
          {connection?.connected ? (
            <ConnectorButton
              onClick={() => setDisconnectOpen(true)}
              disabled={busy !== ""}
              variant="ghost"
              icon={<Trash2 className="size-4" />}
            >
              Disconnect
            </ConnectorButton>
          ) : null}
          {connection?.status === "not_configured" && !flow ? (
            <button
              type="button"
              onClick={() => setManualOpen(true)}
              className="inline-flex h-8 w-full items-center justify-center gap-1.5 rounded-xl text-xs text-muted-foreground transition hover:bg-muted hover:text-foreground"
            >
              <Settings2 className="size-3.5" /> Use an existing Feishu app
            </button>
          ) : null}
          {flow && TERMINAL_FLOW_STATES.has(flow.status) && flow.status !== "ready" ? (
            <ConnectorButton
              onClick={() => {
                setFlow(null);
                void startRegistration();
              }}
              disabled={busy !== ""}
              icon={<RefreshCw className="size-4" />}
            >
              Try again
            </ConnectorButton>
          ) : null}
        </div>

        {connection?.last_connected_at ? (
          <p className="mt-4 text-[11px] text-muted-foreground">
            Last connected {formatDate(connection.last_connected_at)}
          </p>
        ) : null}
      </article>

      <FeishuSettingsDialog
        open={settingsOpen}
        busy={busy === "settings"}
        loading={modelsLoading}
        loadError={modelsError}
        models={models}
        currentModelID={connection?.model_route_id ?? ""}
        onOpenChange={setSettingsOpen}
        onSubmit={async (model) => {
          setBusy("settings");
          setError("");
          try {
            const response = await fetch("/api/connectors/feishu", {
              method: "PATCH",
              headers: { "content-type": "application/json" },
              body: JSON.stringify({
                model_route_id: model.id,
                model_alias: model.alias,
              }),
            });
            if (!response.ok) throw new Error(await connectorResponseError(response));
            setConnection((await response.json()) as FeishuConnection);
            setSettingsOpen(false);
            showSuccess("Feishu model updated");
          } catch (cause) {
            const message = errorMessage(cause);
            setError(message);
            showError(message);
            throw cause;
          } finally {
            setBusy("");
          }
        }}
      />

      <ManualAppDialog
        open={manualOpen}
        busy={busy === "manual"}
        onOpenChange={setManualOpen}
        onSubmit={async (domain, appID, appSecret) => {
          setBusy("manual");
          setError("");
          try {
            const response = await fetch("/api/connectors/feishu/manual", {
              method: "POST",
              headers: { "content-type": "application/json" },
              body: JSON.stringify({
                domain,
                app_id: appID,
                app_secret: appSecret,
              }),
            });
            if (!response.ok) throw new Error(await connectorResponseError(response));
            const result = (await response.json()) as ManualResult;
            setConnection(result.connection);
            setBind({ code: result.bind_code, expiresAt: result.expires_at });
            setManualOpen(false);
            showSuccess("Existing Feishu app connected");
          } catch (cause) {
            const message = errorMessage(cause);
            setError(message);
            showError(message);
            throw cause;
          } finally {
            setBusy("");
          }
        }}
      />

      <ActionConfirmDialog
        open={disconnectOpen}
        title="Disconnect Feishu?"
        description={
          <>
            Cocola will delete the encrypted app credential, owner binding, and channel session
            mapping. Conversation history remains. The Feishu app itself is not deleted; remove it
            separately in Feishu if needed.
          </>
        }
        confirmLabel="Disconnect"
        busy={busy === "disconnect"}
        error={busy === "disconnect" ? null : error || null}
        tone="danger"
        icon={Trash2}
        onOpenChange={setDisconnectOpen}
        onConfirm={() => void disconnect()}
      />
    </>
  );
}

function FeishuSettingsDialog({
  open,
  busy,
  loading,
  loadError,
  models,
  currentModelID,
  onOpenChange,
  onSubmit,
}: {
  open: boolean;
  busy: boolean;
  loading: boolean;
  loadError: string;
  models: ConnectorModel[];
  currentModelID: string;
  onOpenChange: (open: boolean) => void;
  onSubmit: (model: ConnectorModel) => Promise<void>;
}) {
  const modelInput = useId();
  const [selectedID, setSelectedID] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) {
      setSelectedID("");
      setError("");
      return;
    }
    const nextID =
      (models.some((model) => model.id === currentModelID) ? currentModelID : "") ||
      models.find((model) => model.is_default)?.id ||
      models[0]?.id ||
      "";
    setSelectedID(nextID);
  }, [currentModelID, models, open]);

  const selected = models.find((model) => model.id === selectedID);
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!selected) return;
    setError("");
    try {
      await onSubmit(selected);
    } catch (cause) {
      setError(errorMessage(cause));
    }
  };

  return (
    <Dialog.Root open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-[70] bg-slate-950/30 backdrop-blur-[2px] data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />
        <Dialog.Content className="cocola-user-ui fixed left-1/2 top-1/2 z-[71] w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-border bg-background p-5 text-foreground shadow-2xl outline-none">
          <form onSubmit={(event) => void submit(event)}>
            <div className="flex items-start gap-3">
              <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-blue-500/10 text-blue-600">
                <Settings2 className="size-5" />
              </div>
              <div className="min-w-0 flex-1">
                <Dialog.Title className="text-base font-semibold">Feishu settings</Dialog.Title>
                <Dialog.Description className="mt-1.5 text-sm leading-6 text-muted-foreground">
                  Choose the model used for new messages from this bot.
                </Dialog.Description>
              </div>
              <Dialog.Close asChild>
                <button
                  type="button"
                  disabled={busy}
                  aria-label="Close"
                  className="grid size-8 shrink-0 place-items-center rounded-lg text-muted-foreground transition hover:bg-muted hover:text-foreground disabled:opacity-50"
                >
                  <X className="size-4" />
                </button>
              </Dialog.Close>
            </div>

            <div className="mt-5 space-y-2">
              <Label htmlFor={modelInput}>Model</Label>
              <div className="relative">
                <select
                  id={modelInput}
                  value={selectedID}
                  disabled={busy || loading || models.length === 0}
                  onChange={(event) => setSelectedID(event.target.value)}
                  className="h-10 w-full appearance-none rounded-xl border border-input bg-background px-3 pr-10 text-sm shadow-sm outline-none transition focus:border-ring focus:ring-2 focus:ring-ring/20 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {loading ? <option value="">Loading models…</option> : null}
                  {!loading && models.length === 0 ? (
                    <option value="">No compatible models</option>
                  ) : null}
                  {models.map((model) => (
                    <option key={model.id} value={model.id}>
                      {model.label}
                      {model.provider ? ` · ${model.provider}` : ""}
                    </option>
                  ))}
                </select>
                <ChevronDown
                  aria-hidden="true"
                  className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
                />
              </div>
              {selected ? (
                <p className="text-xs text-muted-foreground">
                  Route alias: <span className="font-mono text-foreground">{selected.alias}</span>
                </p>
              ) : null}
              <p className="text-xs leading-5 text-muted-foreground">
                The change applies to the next new message. An Agent already waiting for an answer
                keeps its original model.
              </p>
            </div>

            {loadError || error ? (
              <div
                role="alert"
                className="mt-3 rounded-xl border border-red-500/20 bg-red-500/10 px-3 py-2 text-sm text-red-600"
              >
                {error || loadError}
              </div>
            ) : null}

            <div className="mt-5 flex justify-end gap-2">
              <Dialog.Close asChild>
                <button
                  type="button"
                  disabled={busy}
                  className="h-9 rounded-xl px-3 text-sm text-muted-foreground transition hover:bg-muted hover:text-foreground disabled:opacity-50"
                >
                  Cancel
                </button>
              </Dialog.Close>
              <button
                type="submit"
                disabled={busy || loading || !selected}
                className="inline-flex h-9 items-center gap-2 rounded-xl bg-blue-600 px-4 text-sm font-medium text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {busy ? <Loader2 className="size-4 animate-spin" /> : null}
                {busy ? "Saving…" : "Save"}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function ManualAppDialog({
  open,
  busy,
  onOpenChange,
  onSubmit,
}: {
  open: boolean;
  busy: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (domain: "feishu" | "lark", appID: string, appSecret: string) => Promise<void>;
}) {
  const appIDInput = useId();
  const secretInput = useId();
  const [domain, setDomain] = useState<"feishu" | "lark">("feishu");
  const [appID, setAppID] = useState("");
  const [appSecret, setAppSecret] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) {
      setAppID("");
      setAppSecret("");
      setError("");
    }
  }, [open]);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError("");
    try {
      await onSubmit(domain, appID.trim(), appSecret.trim());
      setAppSecret("");
    } catch (cause) {
      setError(errorMessage(cause));
    }
  };

  return (
    <Dialog.Root open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-[70] bg-slate-950/30 backdrop-blur-[2px] data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />
        <Dialog.Content className="cocola-user-ui fixed left-1/2 top-1/2 z-[71] w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-border bg-background p-5 text-foreground shadow-2xl outline-none">
          <form onSubmit={(event) => void submit(event)}>
            <div className="flex items-start gap-3">
              <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-blue-500/10 text-blue-600">
                <Settings2 className="size-5" />
              </div>
              <div className="min-w-0 flex-1">
                <Dialog.Title className="text-base font-semibold">
                  Use an existing Feishu app
                </Dialog.Title>
                <Dialog.Description className="mt-1.5 text-sm leading-6 text-muted-foreground">
                  Advanced fallback only. Cocola encrypts the secret and never returns it through
                  the API.
                </Dialog.Description>
              </div>
              <Dialog.Close asChild>
                <button
                  type="button"
                  disabled={busy}
                  aria-label="Close"
                  className="grid size-8 shrink-0 place-items-center rounded-lg text-muted-foreground transition hover:bg-muted hover:text-foreground disabled:opacity-50"
                >
                  <X className="size-4" />
                </button>
              </Dialog.Close>
            </div>

            <fieldset disabled={busy} className="mt-5 space-y-4">
              <div className="space-y-2">
                <Label>App region</Label>
                <div className="grid grid-cols-2 gap-2">
                  {(["feishu", "lark"] as const).map((value) => (
                    <button
                      key={value}
                      type="button"
                      aria-pressed={domain === value}
                      onClick={() => setDomain(value)}
                      className={`h-9 rounded-xl border text-sm transition ${
                        domain === value
                          ? "border-blue-500 bg-blue-500/10 text-blue-700"
                          : "border-border text-muted-foreground hover:bg-muted"
                      }`}
                    >
                      {value === "feishu" ? "Feishu" : "Lark"}
                    </button>
                  ))}
                </div>
              </div>
              <div className="space-y-2">
                <Label htmlFor={appIDInput}>App ID</Label>
                <Input
                  id={appIDInput}
                  autoComplete="off"
                  value={appID}
                  onChange={(event) => setAppID(event.target.value)}
                  placeholder="cli_..."
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor={secretInput}>App Secret</Label>
                <Input
                  id={secretInput}
                  type="password"
                  autoComplete="off"
                  value={appSecret}
                  onChange={(event) => setAppSecret(event.target.value)}
                  required
                />
              </div>
            </fieldset>

            {error ? (
              <div className="mt-3 rounded-xl border border-red-500/20 bg-red-500/10 px-3 py-2 text-sm text-red-600">
                {error}
              </div>
            ) : null}

            <div className="mt-5 flex justify-end gap-2">
              <Dialog.Close asChild>
                <button
                  type="button"
                  disabled={busy}
                  className="h-9 rounded-xl px-3 text-sm text-muted-foreground transition hover:bg-muted hover:text-foreground disabled:opacity-50"
                >
                  Cancel
                </button>
              </Dialog.Close>
              <button
                type="submit"
                disabled={busy || !appID.trim() || !appSecret.trim()}
                className="h-9 rounded-xl bg-blue-600 px-4 text-sm font-medium text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {busy ? "Connecting…" : "Connect app"}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function StatusNotice({
  icon: Icon,
  text,
  tone,
}: {
  icon: LucideIcon;
  text: string;
  tone: "warning" | "danger";
}) {
  const classes =
    tone === "danger"
      ? "border-red-500/20 bg-red-500/5 text-red-700"
      : "border-amber-500/20 bg-amber-500/5 text-amber-800";
  return (
    <div className={`mt-4 flex gap-2 rounded-2xl border p-3 text-xs leading-5 ${classes}`}>
      <Icon className="mt-0.5 size-3.5 shrink-0" />
      <p>{text}</p>
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
  variant?: "solid" | "outline" | "ghost";
}) {
  const styles = {
    solid: "bg-foreground text-background hover:bg-foreground/90",
    outline: "border border-border text-foreground hover:bg-muted",
    ghost: "text-muted-foreground hover:bg-muted hover:text-foreground",
  };
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={`inline-flex h-10 w-full items-center justify-center gap-2 rounded-xl px-4 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 ${styles[variant]}`}
    >
      {icon}
      <span className="truncate">{children}</span>
    </button>
  );
}

function connectionState(
  connection: FeishuConnection | null,
  flow: RegistrationFlow | null,
  loadState: ConnectionLoadState,
) {
  if (!connection) {
    return loadState === "failed"
      ? { label: "Connection check failed", dot: "bg-red-500" }
      : { label: "Checking", dot: "bg-muted-foreground" };
  }
  if (flow && ACTIVE_FLOW_STATES.has(flow.status)) {
    return { label: "Authorizing", dot: "animate-pulse bg-blue-500" };
  }
  const states: Record<string, { label: string; dot: string }> = {
    not_configured: { label: "Not connected", dot: "bg-muted-foreground" },
    awaiting_bind: { label: "Waiting for owner", dot: "bg-amber-500" },
    connecting: { label: "Connecting", dot: "animate-pulse bg-blue-500" },
    ready: { label: "Connected", dot: "bg-emerald-500" },
    action_required: { label: "Action required", dot: "bg-amber-500" },
    disabled: { label: connection.connected ? "Paused" : "Unavailable", dot: "bg-slate-400" },
    error: { label: "Connection error", dot: "bg-red-500" },
  };
  return states[connection.status] ?? { label: connection.status, dot: "bg-muted-foreground" };
}

function registrationFailureText(flow: RegistrationFlow) {
  switch (flow.status) {
    case "denied":
      return "Authorization was denied in Feishu. You can start a new authorization.";
    case "expired":
      return "The authorization link expired. Start again to receive a new link.";
    case "interrupted":
      return "Gateway restarted before authorization completed. Start again to continue.";
    default:
      return `Feishu authorization failed${flow.error_code ? ` (${flow.error_code})` : ""}.`;
  }
}

function connectorErrorText(code?: string) {
  const messages: Record<string, string> = {
    bot_not_active: "The bot has not been published or approved in Feishu.",
    credential_decrypt_failed: "The saved credential cannot be opened. Disconnect and reconnect.",
    channel_configuration_failed: "The Feishu app configuration is incomplete or invalid.",
    connection_failed: "The long connection could not be established. Check the app status.",
    connection_error: "The Feishu long connection was interrupted.",
  };
  return (code && messages[code]) || "The Feishu connector could not establish a connection.";
}

function developerConsoleURL(domain?: string) {
  return domain === "lark" ? "https://open.larksuite.com/app" : "https://open.feishu.cn/app";
}

function formatDuration(totalSeconds: number) {
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

function formatDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function errorMessage(cause: unknown) {
  return cause instanceof Error ? cause.message : String(cause);
}
