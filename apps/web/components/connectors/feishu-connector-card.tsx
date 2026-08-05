"use client";

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
  type LucideIcon,
} from "lucide-react";
import { Button, Card, Chip, Input, Label, TextField } from "@heroui/react";
import { Segment } from "@heroui-pro/react/segment";
import { Sheet } from "@heroui-pro/react/sheet";
import { useCallback, useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";
import { useWorkspaceToast } from "@/components/assistant-ui/workspace-toast";
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
  const stateColor =
    state.label === "Ready"
      ? "success"
      : state.label === "Unavailable" || state.label === "Needs attention"
        ? "danger"
        : "warning";
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
      <Card className="p-5">
        <Card.Header className="flex-row items-start justify-between gap-4 p-0">
          <span className="flex min-w-0 items-start gap-3">
            <span className="bg-blue-500/15 text-blue-600 flex size-10 shrink-0 items-center justify-center rounded-2xl dark:text-blue-300">
              <MessageCircleMore className="size-5" />
            </span>
            <span className="min-w-0">
              <Card.Title>Feishu Bot</Card.Title>
              <Card.Description>
                Give this Agent its own Feishu entry point. One Agent can have one Bot.
              </Card.Description>
            </span>
          </span>
          <Chip color={stateColor} size="sm" variant="soft">
            {state.label}
          </Chip>
        </Card.Header>
        <Card.Content className="p-0">
          {connection?.bot_name ? (
            <div className="bg-surface-secondary flex items-center justify-between gap-3 rounded-2xl px-4 py-3 text-sm">
              <span className="text-muted">Bot</span>
              <span className="truncate font-medium">{connection.bot_name}</span>
            </div>
          ) : null}

          {flow && ACTIVE_FLOW_STATES.has(flow.status) ? (
            <div className="bg-accent-soft mt-4 rounded-2xl p-4">
              <div className="text-accent flex items-center gap-2 text-sm font-medium">
                <Clock3 className="size-3.5" />
                {flow.status === "awaiting_user"
                  ? `Authorization expires in ${formatDuration(remainingSeconds)}`
                  : "Preparing authorization…"}
              </div>
              {flow.verification_url ? (
                <div className="mt-3 grid gap-2">
                  <Button
                    className="w-full"
                    onPress={() =>
                      window.open(flow.verification_url, "_blank", "noopener,noreferrer")
                    }
                  >
                    Continue in Feishu <ExternalLink className="size-4" />
                  </Button>
                  <Button
                    className="w-full"
                    variant="ghost"
                    onPress={() =>
                      void copyValue(flow.verification_url!, "Authorization link copied")
                    }
                  >
                    <Copy className="size-4" />
                    Copy authorization link
                  </Button>
                </div>
              ) : null}
              <Button
                className="mt-1 w-full"
                isDisabled={busy === "cancel"}
                variant="ghost"
                onPress={() => void cancelRegistration()}
              >
                {busy === "cancel" ? "Cancelling…" : "Cancel authorization"}
              </Button>
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
              <Button
                className="bg-surface mt-2 w-full justify-between font-mono"
                variant="outline"
                onPress={() => void copyValue(`/bind ${bind.code}`, "Binding command copied")}
              >
                <span>/bind {bind.code}</span>
                <Copy className="text-muted size-3.5" />
              </Button>
              <p className="mt-2 text-[11px] text-amber-700">
                Expires {formatDate(bind.expiresAt)}
              </p>
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
              className="mt-4 rounded-xl bg-danger/10 px-3 py-2 text-xs text-danger"
            >
              {error}
            </div>
          ) : null}

          <div className="mt-5 flex flex-wrap gap-2">
            {loadState === "checking" && !connection ? (
              <Button isDisabled isPending>
                Checking…
              </Button>
            ) : null}
            {loadState === "failed" ? (
              <Button variant="outline" onPress={() => void load()}>
                <RefreshCw className="size-4" />
                Retry
              </Button>
            ) : null}
            {connection?.status === "not_configured" && !flow ? (
              <>
                <Button
                  isDisabled={busy !== ""}
                  isPending={busy === "register"}
                  onPress={() => void startRegistration()}
                >
                  <MessageCircleMore className="size-4" />
                  Connect Feishu
                </Button>
                <Button
                  isDisabled={busy !== ""}
                  variant="outline"
                  onPress={() => setManualOpen(true)}
                >
                  <Settings2 className="size-4" />
                  Use existing App
                </Button>
              </>
            ) : null}
            {connection?.status === "awaiting_bind" && !bind ? (
              <Button
                isDisabled={busy !== ""}
                variant="outline"
                onPress={() => setManualOpen(true)}
              >
                <Settings2 className="size-4" />
                Use existing App
              </Button>
            ) : null}
            {connection?.connected && !connection.enabled ? (
              <Button
                isDisabled={busy !== ""}
                isPending={busy === "enable"}
                onPress={() => void toggle(true)}
              >
                <Play className="size-4" />
                Enable
              </Button>
            ) : null}
            {connection?.connected && connection.enabled ? (
              <Button
                isDisabled={busy !== ""}
                isPending={busy === "disable"}
                variant="outline"
                onPress={() => void toggle(false)}
              >
                <Pause className="size-4" />
                Pause
              </Button>
            ) : null}
            {connection?.connected ? (
              <Button
                isDisabled={busy !== ""}
                variant="ghost"
                onPress={() => setDisconnectOpen(true)}
              >
                <Trash2 className="size-4" />
                Disconnect
              </Button>
            ) : null}
            {flow && TERMINAL_FLOW_STATES.has(flow.status) && flow.status !== "ready" ? (
              <Button
                isDisabled={busy !== ""}
                onPress={() => {
                  setFlow(null);
                  void startRegistration();
                }}
              >
                <RefreshCw className="size-4" />
                Try again
              </Button>
            ) : null}
          </div>

          {connection?.last_connected_at ? (
            <p className="text-muted mt-4 text-xs">
              Last connected {formatDate(connection.last_connected_at)}
            </p>
          ) : null}
        </Card.Content>
      </Card>

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

      <Sheet isOpen={disconnectOpen} placement="right" onOpenChange={setDisconnectOpen}>
        <Sheet.Backdrop>
          <Sheet.Content className="w-full md:w-[420px]">
            <Sheet.Dialog>
              <Sheet.CloseTrigger aria-label="Close disconnect confirmation" />
              <Sheet.Header>
                <Sheet.Heading>Disconnect Feishu Bot?</Sheet.Heading>
                <p className="text-muted text-sm">
                  Cocola will delete the encrypted app credential and owner binding. Conversation
                  history remains.
                </p>
              </Sheet.Header>
              <Sheet.Footer className="gap-2">
                <Button variant="outline" onPress={() => setDisconnectOpen(false)}>
                  Cancel
                </Button>
                <Button
                  isPending={busy === "disconnect"}
                  variant="danger-soft"
                  onPress={() => void disconnect()}
                >
                  Disconnect
                </Button>
              </Sheet.Footer>
            </Sheet.Dialog>
          </Sheet.Content>
        </Sheet.Backdrop>
      </Sheet>
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
    <Sheet
      isOpen={open}
      placement="right"
      onOpenChange={(next) => {
        if (!busy) onOpenChange(next);
      }}
    >
      <Sheet.Backdrop>
        <Sheet.Content className="w-full md:w-[460px]">
          <Sheet.Dialog>
            <Sheet.CloseTrigger aria-label="Close existing Feishu App settings" />
            <Sheet.Header>
              <Sheet.Heading>Use Existing App</Sheet.Heading>
              <p className="text-muted text-sm">
                Cocola encrypts the App Secret and returns a one-time command to bind the Bot owner.
              </p>
            </Sheet.Header>
            <form className="contents" onSubmit={(event) => void submit(event)}>
              <Sheet.Body className="grid content-start gap-4">
                <div>
                  <Label>Platform</Label>
                  <Segment
                    aria-label="Feishu platform"
                    className="mt-2"
                    selectedKey={domain}
                    onSelectionChange={(key) => setDomain(String(key) as "feishu" | "lark")}
                  >
                    <Segment.Item id="feishu">Feishu</Segment.Item>
                    <Segment.Item id="lark">Lark</Segment.Item>
                  </Segment>
                </div>
                <TextField value={appID} variant="secondary" onChange={setAppID}>
                  <Label>App ID</Label>
                  <Input autoComplete="off" placeholder="cli_xxxxxxxxxxxxxxxx" />
                </TextField>
                <TextField value={appSecret} variant="secondary" onChange={setAppSecret}>
                  <Label>App Secret</Label>
                  <Input autoComplete="new-password" type="password" />
                </TextField>
                {error ? (
                  <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">
                    {error}
                  </div>
                ) : null}
              </Sheet.Body>
              <Sheet.Footer className="gap-2">
                <Button isDisabled={busy} variant="outline" onPress={() => onOpenChange(false)}>
                  Cancel
                </Button>
                <Button
                  isDisabled={!appID.trim() || !appSecret.trim()}
                  isPending={busy}
                  type="submit"
                >
                  Connect App
                </Button>
              </Sheet.Footer>
            </form>
          </Sheet.Dialog>
        </Sheet.Content>
      </Sheet.Backdrop>
    </Sheet>
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
            ? "border border-border bg-background text-foreground hover:bg-surface-secondary"
            : "text-muted hover:bg-surface-secondary hover:text-foreground"
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
    return { label: "Checking", className: "bg-surface-secondary text-muted" };
  }
  if (loadState === "failed") {
    return { label: "Unavailable", className: "bg-red-500/10 text-red-700" };
  }
  if (!connection || connection.status === "not_configured") {
    return { label: "Not connected", className: "bg-surface-secondary text-muted" };
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
