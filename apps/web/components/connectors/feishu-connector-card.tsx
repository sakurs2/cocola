"use client";

import * as Dialog from "@radix-ui/react-dialog";
import {
  AlertTriangle,
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
import { useWorkspaceToast } from "@/components/assistant-ui/workspace-toast";
import { ActionConfirmDialog } from "@/components/ui/action-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { connectorResponseError } from "@/lib/connector-response-error.mjs";

type FeishuConnection = {
  agent_id?: string;
  connected: boolean;
  enabled: boolean;
  status: string;
  domain?: "feishu" | "lark";
  bot_name?: string;
  last_connected_at?: string;
  last_error_code?: string;
  registration?: RegistrationFlow;
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

export function FeishuConnectorCard({ agentId }: { agentId: string }) {
  const { showError, showSuccess } = useWorkspaceToast();
  const endpoint = useMemo(
    () => `/api/agents/${encodeURIComponent(agentId)}/channels/feishu`,
    [agentId],
  );
  const [connection, setConnection] = useState<FeishuConnection | null>(null);
  const [loadState, setLoadState] = useState<ConnectionLoadState>("checking");
  const [flow, setFlow] = useState<RegistrationFlow | null>(null);
  const [bind, setBind] = useState<{ code: string; expiresAt: string } | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [manualOpen, setManualOpen] = useState(false);
  const [disconnectOpen, setDisconnectOpen] = useState(false);
  const [clock, setClock] = useState(() => Date.now());
  const activeFlowID = flow && ACTIVE_FLOW_STATES.has(flow.status) ? flow.id : "";

  const load = useCallback(async () => {
    setLoadState("checking");
    setError("");
    try {
      const response = await fetch(endpoint, { cache: "no-store" });
      if (!response.ok) throw new Error(await connectorResponseError(response));
      const next = (await response.json()) as FeishuConnection;
      setConnection(next);
      setFlow(next.registration ?? null);
      setLoadState("ready");
    } catch (cause) {
      setError(errorMessage(cause));
      setLoadState("failed");
    }
  }, [endpoint]);

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
        const response = await fetch(endpoint, {
          cache: "no-store",
          signal: controller.signal,
        });
        if (!response.ok) throw new Error(await connectorResponseError(response));
        const next = (await response.json()) as FeishuConnection;
        if (stopped) return;
        setConnection(next);
        setFlow(next.registration ?? null);
        if (next.status !== "connecting" || attempts >= 60) return;
        timer = setTimeout(() => void pollConnection(), 2000);
      } catch (cause) {
        if (stopped || controller.signal.aborted) return;
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
  }, [connecting, endpoint, showError]);

  useEffect(() => {
    if (!activeFlowID) return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const controller = new AbortController();
    const poll = async () => {
      try {
        const response = await fetch(
          `${endpoint}/registrations/${encodeURIComponent(activeFlowID)}`,
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
        if (stopped || controller.signal.aborted) return;
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
  }, [activeFlowID, endpoint, load, showError, showSuccess]);

  useEffect(() => {
    if (!activeFlowID) return;
    setClock(Date.now());
    const timer = window.setInterval(() => setClock(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [activeFlowID]);

  const run = async (action: string, request: () => Promise<Response>, successMessage: string) => {
    setBusy(action);
    setError("");
    try {
      const response = await request();
      if (!response.ok) throw new Error(await connectorResponseError(response));
      showSuccess(successMessage);
      await load();
      return true;
    } catch (cause) {
      const message = errorMessage(cause);
      setError(message);
      showError(message);
      return false;
    } finally {
      setBusy("");
    }
  };

  const startRegistration = async () => {
    setBusy("register");
    setError("");
    setBind(null);
    try {
      const response = await fetch(`${endpoint}/registrations`, {
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
      const response = await fetch(`${endpoint}/registrations/${encodeURIComponent(flow.id)}`, {
        method: "DELETE",
      });
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

  const toggle = (enabled: boolean) =>
    run(
      enabled ? "enable" : "disable",
      () =>
        fetch(`${endpoint}/${enabled ? "enable" : "disable"}`, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: "{}",
        }),
      enabled ? "Feishu bot enabled" : "Feishu bot paused",
    );

  const disconnect = async () => {
    const disconnected = await run(
      "disconnect",
      () => fetch(endpoint, { method: "DELETE" }),
      "Feishu bot disconnected",
    );
    if (!disconnected) return;
    setDisconnectOpen(false);
    setBind(null);
    setFlow(null);
  };

  const remainingSeconds = flow
    ? Math.max(0, Math.floor((new Date(flow.expires_at).getTime() - clock) / 1000))
    : 0;
  const state = connectionState(connection, loadState);
  const copyValue = async (value: string, successMessage: string) => {
    try {
      await navigator.clipboard.writeText(value);
      showSuccess(successMessage);
    } catch {
      showError("Could not copy to clipboard");
    }
  };

  return (
    <>
      <section className="rounded-2xl border border-border bg-card p-5 shadow-sm">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="flex min-w-0 items-start gap-3">
            <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-blue-50 text-blue-600 ring-1 ring-inset ring-blue-100">
              <MessageCircleMore className="size-5" />
            </span>
            <div className="min-w-0">
              <h2 className="text-sm font-semibold">Feishu bot</h2>
              <p className="mt-1 text-sm leading-6 text-muted-foreground">
                Give this Agent its own Feishu entry point. One Agent can have one bot.
              </p>
            </div>
          </div>
          <span
            className={`inline-flex items-center rounded-full px-2.5 py-1 text-xs font-medium ${state.className}`}
          >
            {state.label}
          </span>
        </div>

        {connection?.bot_name ? (
          <div className="mt-4 flex items-center justify-between gap-3 rounded-xl bg-muted/60 px-3 py-2 text-sm">
            <span className="text-muted-foreground">Bot</span>
            <span className="truncate font-medium">{connection.bot_name}</span>
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
                  className="inline-flex h-9 w-full items-center justify-center gap-2 rounded-xl bg-blue-600 px-3 text-sm font-medium text-white transition hover:bg-blue-700"
                >
                  Continue in Feishu <ExternalLink className="size-4" />
                </a>
                <button
                  type="button"
                  onClick={() =>
                    void copyValue(flow.verification_url!, "Authorization link copied")
                  }
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
          <StatusNotice icon={AlertTriangle} tone="danger">
            {registrationFailureText(flow)}
          </StatusNotice>
        ) : null}

        {bind ? (
          <div className="mt-4 rounded-2xl border border-amber-500/20 bg-amber-500/5 p-3">
            <p className="text-xs leading-5 text-amber-800">
              Send this once to the bot before the code expires:
            </p>
            <button
              type="button"
              onClick={() => void copyValue(`/bind ${bind.code}`, "Binding command copied")}
              className="mt-2 flex w-full items-center justify-between rounded-xl bg-background px-3 py-2 font-mono text-sm shadow-sm"
            >
              <span>/bind {bind.code}</span>
              <Copy className="size-3.5 text-muted-foreground" />
            </button>
            <p className="mt-2 text-[11px] text-amber-700">Expires {formatDate(bind.expiresAt)}</p>
          </div>
        ) : null}

        {connection?.status === "awaiting_bind" && !bind ? (
          <StatusNotice icon={AlertTriangle} tone="warning">
            This app is waiting for its owner. Re-enter its credentials to create a new binding
            code.
          </StatusNotice>
        ) : null}

        {connection?.status === "action_required" ? (
          <div className="mt-4 rounded-2xl border border-amber-500/20 bg-amber-500/5 p-3 text-xs leading-5 text-amber-800">
            The bot is waiting for publishing or tenant approval in Feishu.
            <a
              href={developerConsoleURL(connection.domain)}
              target="_blank"
              rel="noopener noreferrer"
              className="mt-2 flex items-center gap-1 font-medium underline underline-offset-2"
            >
              Open developer console <ExternalLink className="size-3.5" />
            </a>
          </div>
        ) : null}

        {connection?.status === "error" ? (
          <StatusNotice icon={AlertTriangle} tone="danger">
            {connectorErrorText(connection.last_error_code)}
          </StatusNotice>
        ) : null}

        {error ? (
          <div
            role="alert"
            className="mt-4 rounded-xl bg-destructive/10 px-3 py-2 text-xs text-destructive"
          >
            {error}
          </div>
        ) : null}

        <div className="mt-5 flex flex-wrap gap-2">
          {loadState === "checking" && !connection ? (
            <ConnectorButton disabled icon={<Loader2 className="size-4 animate-spin" />}>
              Checking…
            </ConnectorButton>
          ) : null}
          {loadState === "failed" ? (
            <ConnectorButton
              onClick={() => void load()}
              variant="outline"
              icon={<RefreshCw className="size-4" />}
            >
              Retry
            </ConnectorButton>
          ) : null}
          {connection?.status === "not_configured" && !flow ? (
            <>
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
              <ConnectorButton
                onClick={() => setManualOpen(true)}
                disabled={busy !== ""}
                variant="outline"
                icon={<Settings2 className="size-4" />}
              >
                Use existing app
              </ConnectorButton>
            </>
          ) : null}
          {connection?.status === "awaiting_bind" && !bind ? (
            <ConnectorButton
              onClick={() => setManualOpen(true)}
              disabled={busy !== ""}
              variant="outline"
              icon={<Settings2 className="size-4" />}
            >
              Use existing app
            </ConnectorButton>
          ) : null}
          {connection?.connected && !connection.enabled ? (
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
          {connection?.connected && connection.enabled ? (
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
      </section>

      <ManualAppDialog
        open={manualOpen}
        busy={busy === "manual"}
        onOpenChange={setManualOpen}
        onSubmit={async (domain, appID, appSecret) => {
          setBusy("manual");
          setError("");
          try {
            const response = await fetch(`${endpoint}/manual`, {
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
        description="Cocola will delete the encrypted app credential and owner binding. Conversation history remains."
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
    if (open) return;
    setDomain("feishu");
    setAppID("");
    setAppSecret("");
    setError("");
  }, [open]);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!appID.trim() || !appSecret.trim()) return;
    setError("");
    try {
      await onSubmit(domain, appID.trim(), appSecret.trim());
    } catch (cause) {
      setError(errorMessage(cause));
    }
  };

  return (
    <Dialog.Root open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-[70] bg-slate-950/30 backdrop-blur-[2px]" />
        <Dialog.Content className="cocola-user-ui fixed left-1/2 top-1/2 z-[71] w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-border bg-background p-5 text-foreground shadow-2xl outline-none">
          <form onSubmit={(event) => void submit(event)}>
            <div className="flex items-start gap-3">
              <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-blue-500/10 text-blue-600">
                <Settings2 className="size-5" />
              </span>
              <div className="min-w-0 flex-1">
                <Dialog.Title className="text-base font-semibold">Use an existing app</Dialog.Title>
                <Dialog.Description className="mt-1.5 text-sm leading-6 text-muted-foreground">
                  Cocola encrypts the app secret and returns a one-time command to bind the bot
                  owner.
                </Dialog.Description>
              </div>
              <Dialog.Close asChild>
                <button
                  type="button"
                  disabled={busy}
                  aria-label="Close"
                  className="grid size-8 place-items-center rounded-lg text-muted-foreground hover:bg-muted"
                >
                  <X className="size-4" />
                </button>
              </Dialog.Close>
            </div>

            <div className="mt-5 space-y-4">
              <div className="space-y-1.5">
                <Label>Platform</Label>
                <div className="grid grid-cols-2 gap-2">
                  {(["feishu", "lark"] as const).map((value) => (
                    <button
                      key={value}
                      type="button"
                      onClick={() => setDomain(value)}
                      className={`h-9 rounded-xl border text-sm font-medium ${
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
              <div className="space-y-1.5">
                <Label htmlFor={appIDInput}>App ID</Label>
                <Input
                  id={appIDInput}
                  autoComplete="off"
                  value={appID}
                  onChange={(event) => setAppID(event.target.value)}
                  placeholder="cli_..."
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor={secretInput}>App secret</Label>
                <Input
                  id={secretInput}
                  type="password"
                  autoComplete="new-password"
                  value={appSecret}
                  onChange={(event) => setAppSecret(event.target.value)}
                />
              </div>
            </div>

            {error ? (
              <div className="mt-4 rounded-xl bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {error}
              </div>
            ) : null}

            <div className="mt-5 flex justify-end gap-2">
              <Dialog.Close asChild>
                <button
                  type="button"
                  disabled={busy}
                  className="h-9 rounded-xl px-3 text-sm text-muted-foreground hover:bg-muted"
                >
                  Cancel
                </button>
              </Dialog.Close>
              <button
                type="submit"
                disabled={busy || !appID.trim() || !appSecret.trim()}
                className="h-9 rounded-xl bg-blue-600 px-4 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
              >
                {busy ? "Connecting…" : "Connect"}
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
  tone,
  children,
}: {
  icon: LucideIcon;
  tone: "warning" | "danger";
  children: ReactNode;
}) {
  return (
    <div
      className={`mt-4 flex items-start gap-2 rounded-xl border px-3 py-2 text-xs leading-5 ${
        tone === "danger"
          ? "border-red-500/20 bg-red-500/5 text-red-700"
          : "border-amber-500/20 bg-amber-500/5 text-amber-800"
      }`}
    >
      <Icon className="mt-0.5 size-3.5 shrink-0" />
      <span>{children}</span>
    </div>
  );
}

function ConnectorButton({
  children,
  icon,
  variant = "primary",
  ...props
}: {
  children: ReactNode;
  icon: ReactNode;
  variant?: "primary" | "outline" | "ghost";
} & React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={`inline-flex h-9 items-center justify-center gap-2 rounded-xl px-3 text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-50 ${
        variant === "primary"
          ? "bg-blue-600 text-white hover:bg-blue-700"
          : variant === "outline"
            ? "border border-border bg-background text-foreground hover:bg-muted"
            : "text-muted-foreground hover:bg-muted hover:text-foreground"
      }`}
      {...props}
    >
      {icon}
      {children}
    </button>
  );
}

function connectionState(
  connection: FeishuConnection | null,
  loadState: ConnectionLoadState,
): { label: string; className: string } {
  if (loadState === "checking") {
    return { label: "Checking", className: "bg-muted text-muted-foreground" };
  }
  if (loadState === "failed") {
    return { label: "Unavailable", className: "bg-red-500/10 text-red-700" };
  }
  if (!connection || connection.status === "not_configured") {
    return { label: "Not connected", className: "bg-muted text-muted-foreground" };
  }
  if (connection.status === "ready") {
    return { label: "Ready", className: "bg-emerald-500/10 text-emerald-700" };
  }
  if (connection.status === "disabled") {
    return { label: "Paused", className: "bg-slate-500/10 text-slate-700" };
  }
  if (connection.status === "error") {
    return { label: "Needs attention", className: "bg-red-500/10 text-red-700" };
  }
  return { label: "Connecting", className: "bg-amber-500/10 text-amber-700" };
}

function registrationFailureText(flow: RegistrationFlow) {
  if (flow.status === "denied") return "Feishu authorization was denied.";
  if (flow.status === "expired") return "Feishu authorization expired.";
  if (flow.status === "interrupted") return "Feishu authorization was interrupted.";
  return flow.error_code
    ? `Feishu authorization failed (${flow.error_code}).`
    : "Feishu authorization failed.";
}

function connectorErrorText(code?: string) {
  if (code === "app_not_published") return "Publish the app in Feishu, then reconnect.";
  if (code === "tenant_approval_required") return "The app is waiting for tenant approval.";
  if (code === "credentials_invalid") return "The app credentials are no longer valid.";
  return "The Feishu bot could not connect. Retry or check the app configuration.";
}

function developerConsoleURL(domain?: string) {
  return domain === "lark" ? "https://open.larksuite.com/app" : "https://open.feishu.cn/app";
}

function formatDuration(totalSeconds: number) {
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${seconds.toString().padStart(2, "0")}`;
}

function formatDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function errorMessage(cause: unknown) {
  return cause instanceof Error ? cause.message : String(cause);
}
