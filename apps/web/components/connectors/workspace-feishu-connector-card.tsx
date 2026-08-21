"use client";

import Image from "next/image";
import { Button, Input, Label, Modal, TextField } from "@heroui/react";
import { Segment } from "@cocola/ui-compat/segment";
import {
  Clock3,
  Copy,
  ExternalLink,
  Loader2,
  Pause,
  Play,
  RefreshCw,
  Settings2,
  Trash2,
} from "lucide-react";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { useFormatter, useTranslations } from "next-intl";
import { useWorkspaceToast } from "@/components/assistant-ui/workspace-toast";
import { ActionConfirmDialog } from "@/components/ui/action-dialog";
import { connectorResponseError } from "@/lib/connector-response-error.mjs";
import {
  ConnectorSummaryCard,
  type ConnectorSummaryAction,
  type ConnectorSummaryStatus,
} from "./connector-summary-card";

type FeishuConnection = {
  connected: boolean;
  enabled: boolean;
  status: string;
  domain?: "feishu" | "lark";
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

type ConnectionLoadState = "checking" | "ready" | "failed";

const endpoint = "/api/connectors/feishu";
const ACTIVE_FLOW_STATES = new Set(["starting", "awaiting_user", "authorizing"]);
const TERMINAL_FLOW_STATES = new Set([
  "ready",
  "denied",
  "expired",
  "failed",
  "interrupted",
  "cancelled",
]);

export function WorkspaceFeishuConnectorCard() {
  const t = useTranslations("connectors.workspaceFeishu");
  const format = useFormatter();
  const { showError, showSuccess } = useWorkspaceToast();
  const [connection, setConnection] = useState<FeishuConnection | null>(null);
  const [loadState, setLoadState] = useState<ConnectionLoadState>("checking");
  const [flow, setFlow] = useState<RegistrationFlow | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [setupOpen, setSetupOpen] = useState(false);
  const [manualOpen, setManualOpen] = useState(false);
  const [manageOpen, setManageOpen] = useState(false);
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
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

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
          setSetupOpen(false);
          showSuccess(t("authorizationCompleted"));
          return;
        }
        if (!TERMINAL_FLOW_STATES.has(next.status)) {
          timer = setTimeout(() => void poll(), 2000);
        }
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
  }, [activeFlowID, load, showError, showSuccess, t]);

  useEffect(() => {
    if (!activeFlowID) return;
    setClock(Date.now());
    const timer = window.setInterval(() => setClock(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [activeFlowID]);

  const startRegistration = async () => {
    setBusy("register");
    setError("");
    try {
      const response = await fetch(`${endpoint}/registrations`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: "{}",
      });
      if (!response.ok) throw new Error(await connectorResponseError(response));
      const next = (await response.json()) as RegistrationFlow;
      setFlow(next);
      if (next.status === "ready") {
        await load();
        setSetupOpen(false);
        showSuccess(t("authorizationCompleted"));
        return;
      }
      setSetupOpen(true);
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
    setError("");
    try {
      const response = await fetch(`${endpoint}/registrations/${encodeURIComponent(flow.id)}`, {
        method: "DELETE",
      });
      if (!response.ok) throw new Error(await connectorResponseError(response));
      setFlow(null);
      showSuccess(t("authorizationCancelled"));
    } catch (cause) {
      const message = errorMessage(cause);
      setError(message);
      showError(message);
    } finally {
      setBusy("");
    }
  };

  const run = async (action: string, request: () => Promise<Response>, success: string) => {
    setBusy(action);
    setError("");
    try {
      const response = await request();
      if (!response.ok) throw new Error(await connectorResponseError(response));
      showSuccess(success);
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

  const toggle = (enabled: boolean) =>
    run(
      enabled ? "enable" : "disable",
      () =>
        fetch(`${endpoint}/${enabled ? "enable" : "disable"}`, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: "{}",
        }),
      enabled ? t("enabled") : t("paused"),
    );

  const disconnect = async () => {
    const disconnected = await run(
      "disconnect",
      () =>
        fetch(endpoint, {
          method: "DELETE",
          headers: { "content-type": "application/json" },
          body: "{}",
        }),
      t("disconnected"),
    );
    if (!disconnected) return;
    setDisconnectOpen(false);
    setManageOpen(false);
    setFlow(null);
  };

  const openPrimary = () => {
    if (connection?.connected) {
      setManageOpen(true);
    } else {
      setSetupOpen(true);
    }
  };
  const status = feishuStatus(connection, loadState, Boolean(activeFlowID), t);
  const action = feishuAction({
    busy,
    connection,
    loadState,
    activeFlow: Boolean(activeFlowID),
    load,
    openPrimary,
    t,
  });
  const remainingSeconds = flow
    ? Math.max(0, Math.floor((new Date(flow.expires_at).getTime() - clock) / 1000))
    : 0;

  return (
    <>
      <ConnectorSummaryCard
        action={action}
        description={t("description")}
        icon={<Image alt="" aria-hidden src="/feishu-logo.svg" width={28} height={28} />}
        provider="feishu"
        status={status}
        title={t("title")}
      />

      <SetupDialog
        busy={busy}
        error={error}
        flow={flow}
        open={setupOpen}
        remainingSeconds={remainingSeconds}
        onCancelRegistration={() => void cancelRegistration()}
        onManual={() => {
          setSetupOpen(false);
          setManualOpen(true);
        }}
        onOpenChange={setSetupOpen}
        onStart={() => void startRegistration()}
      />

      <ManualAppDialog
        busy={busy === "manual"}
        open={manualOpen}
        onOpenChange={setManualOpen}
        onSubmit={async (domain, appID, appSecret) => {
          setBusy("manual");
          setError("");
          try {
            const response = await fetch(`${endpoint}/manual`, {
              method: "POST",
              headers: { "content-type": "application/json" },
              body: JSON.stringify({ domain, app_id: appID, app_secret: appSecret }),
            });
            if (!response.ok) throw new Error(await connectorResponseError(response));
            setConnection((await response.json()) as FeishuConnection);
            setLoadState("ready");
            setManualOpen(false);
            showSuccess(t("manual.connected"));
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

      <ManageDialog
        busy={busy}
        connection={connection}
        error={error}
        lastConnected={
          connection?.last_connected_at
            ? format.dateTime(new Date(connection.last_connected_at))
            : ""
        }
        open={manageOpen}
        onDisconnect={() => setDisconnectOpen(true)}
        onOpenChange={setManageOpen}
        onToggle={(enabled) => void toggle(enabled)}
      />

      <ActionConfirmDialog
        busy={busy === "disconnect"}
        confirmLabel={t("disconnect.confirm")}
        description={t("disconnect.description")}
        error={error || null}
        icon={Trash2}
        open={disconnectOpen}
        title={t("disconnect.title")}
        tone="danger"
        onConfirm={() => void disconnect()}
        onOpenChange={setDisconnectOpen}
      />
    </>
  );
}

function SetupDialog({
  open,
  busy,
  flow,
  error,
  remainingSeconds,
  onOpenChange,
  onStart,
  onManual,
  onCancelRegistration,
}: {
  open: boolean;
  busy: string;
  flow: RegistrationFlow | null;
  error: string;
  remainingSeconds: number;
  onOpenChange: (open: boolean) => void;
  onStart: () => void;
  onManual: () => void;
  onCancelRegistration: () => void;
}) {
  const t = useTranslations("connectors.workspaceFeishu.setup");
  const { showError, showSuccess } = useWorkspaceToast();
  const active = Boolean(flow && ACTIVE_FLOW_STATES.has(flow.status));
  const failed = Boolean(flow && TERMINAL_FLOW_STATES.has(flow.status) && flow.status !== "ready");
  const copyLink = async () => {
    if (!flow?.verification_url) return;
    try {
      await navigator.clipboard.writeText(flow.verification_url);
      showSuccess(t("linkCopied"));
    } catch {
      showError(t("copyFailed"));
    }
  };

  return (
    <Modal isOpen={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <Modal.Backdrop isDismissable={!busy}>
        <Modal.Container placement="center" size="sm">
          <Modal.Dialog className="mx-auto w-full max-w-[360px]">
            <Modal.CloseTrigger aria-label={t("close")} />
            <Modal.Header>
              <Modal.Heading>{t("title")}</Modal.Heading>
              <p className="text-muted text-sm">{t("description")}</p>
            </Modal.Header>
            <Modal.Body className="grid content-start gap-2">
              {active ? (
                <div className="bg-accent-soft rounded-2xl p-4">
                  <div className="text-accent flex items-center gap-2 text-sm font-medium">
                    <Clock3 className="size-4" />
                    {flow?.status === "awaiting_user"
                      ? t("expiresIn", { time: formatDuration(remainingSeconds) })
                      : t("preparing")}
                  </div>
                  {flow?.verification_url ? (
                    <>
                      <a
                        className="text-accent mt-3 block break-all text-xs underline underline-offset-2"
                        href={flow.verification_url}
                        rel="noopener noreferrer"
                        target="_blank"
                      >
                        {flow.verification_url}
                      </a>
                      <div className="mt-3 grid grid-cols-2 gap-2">
                        <Button
                          className="bg-[#3370FF] text-white hover:bg-[#2B60E8]"
                          onPress={() =>
                            window.open(flow.verification_url, "_blank", "noopener,noreferrer")
                          }
                        >
                          <ExternalLink className="size-4" />
                          {t("continue")}
                        </Button>
                        <Button variant="outline" onPress={() => void copyLink()}>
                          <Copy className="size-4" />
                          {t("copyLink")}
                        </Button>
                      </div>
                    </>
                  ) : null}
                </div>
              ) : null}
              {failed ? (
                <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">
                  {flow?.error_code === "app_in_use"
                    ? t("appInUse")
                    : flow?.error_code
                      ? t("failedCode", { code: flow.error_code })
                      : t("failed")}
                </div>
              ) : null}
              {!active && !failed ? (
                <div className="grid gap-2">
                  <Button
                    className="h-12 w-full justify-start gap-3 bg-[#3370FF] px-4 text-white hover:bg-[#2B60E8]"
                    isPending={busy === "register"}
                    onPress={onStart}
                  >
                    <ExternalLink className="size-4 shrink-0" />
                    {t("quick")}
                  </Button>
                  <Button
                    className="h-12 w-full justify-start gap-3 px-4"
                    isDisabled={Boolean(busy)}
                    variant="outline"
                    onPress={onManual}
                  >
                    <Settings2 className="size-4 shrink-0" />
                    {t("manual")}
                  </Button>
                </div>
              ) : null}
              {error ? (
                <div
                  role="alert"
                  className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm"
                >
                  {error}
                </div>
              ) : null}
            </Modal.Body>
            {active || failed ? (
              <Modal.Footer className="gap-2">
                {active ? (
                  <Button
                    isDisabled={busy === "cancel"}
                    variant="outline"
                    onPress={onCancelRegistration}
                  >
                    {busy === "cancel" ? t("cancelling") : t("cancelAuthorization")}
                  </Button>
                ) : null}
                {failed ? (
                  <Button isPending={busy === "register"} onPress={onStart}>
                    <RefreshCw className="size-4" />
                    {t("tryAgain")}
                  </Button>
                ) : null}
              </Modal.Footer>
            ) : null}
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
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
  const t = useTranslations("connectors.workspaceFeishu.manual");
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
    <Modal isOpen={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <Modal.Backdrop isDismissable={!busy}>
        <Modal.Container placement="center" size="sm">
          <Modal.Dialog>
            <Modal.CloseTrigger aria-label={t("close")} />
            <Modal.Header>
              <Modal.Heading>{t("title")}</Modal.Heading>
              <p className="text-muted text-sm">{t("description")}</p>
            </Modal.Header>
            <form className="contents" onSubmit={(event) => void submit(event)}>
              <Modal.Body className="grid content-start gap-4">
                <div>
                  <Label>{t("platform")}</Label>
                  <Segment
                    aria-label={t("platform")}
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
                  <div
                    role="alert"
                    className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm"
                  >
                    {error}
                  </div>
                ) : null}
              </Modal.Body>
              <Modal.Footer className="gap-2">
                <Button isDisabled={busy} variant="outline" onPress={() => onOpenChange(false)}>
                  {t("cancel")}
                </Button>
                <Button
                  className="bg-[#3370FF] text-white hover:bg-[#2B60E8]"
                  isDisabled={!appID.trim() || !appSecret.trim()}
                  isPending={busy}
                  type="submit"
                >
                  {t("connect")}
                </Button>
              </Modal.Footer>
            </form>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}

function ManageDialog({
  open,
  busy,
  connection,
  error,
  lastConnected,
  onOpenChange,
  onToggle,
  onDisconnect,
}: {
  open: boolean;
  busy: string;
  connection: FeishuConnection | null;
  error: string;
  lastConnected: string;
  onOpenChange: (open: boolean) => void;
  onToggle: (enabled: boolean) => void;
  onDisconnect: () => void;
}) {
  const t = useTranslations("connectors.workspaceFeishu.manage");
  return (
    <Modal isOpen={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <Modal.Backdrop isDismissable={!busy}>
        <Modal.Container placement="center" size="sm">
          <Modal.Dialog>
            <Modal.CloseTrigger aria-label={t("close")} />
            <Modal.Header>
              <Modal.Heading>{t("title")}</Modal.Heading>
              <p className="text-muted text-sm">{t("description")}</p>
            </Modal.Header>
            <Modal.Body className="grid content-start gap-3">
              <div className="bg-surface-secondary flex items-center justify-between rounded-2xl px-4 py-3 text-sm">
                <span className="text-muted">{t("status")}</span>
                <span className="font-medium">
                  {connection?.enabled ? t("enabled") : t("paused")}
                </span>
              </div>
              {lastConnected ? (
                <p className="text-muted text-xs">{t("lastConnected", { date: lastConnected })}</p>
              ) : null}
              {error ? (
                <div
                  role="alert"
                  className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm"
                >
                  {error}
                </div>
              ) : null}
            </Modal.Body>
            <Modal.Footer className="justify-between gap-2">
              <Button isDisabled={Boolean(busy)} variant="ghost" onPress={onDisconnect}>
                <Trash2 className="size-4" />
                {t("disconnect")}
              </Button>
              <div className="flex gap-2">
                <Button variant="outline" onPress={() => onOpenChange(false)}>
                  {t("done")}
                </Button>
                <Button
                  className={
                    connection?.enabled ? "" : "bg-[#3370FF] text-white hover:bg-[#2B60E8]"
                  }
                  isPending={busy === "enable" || busy === "disable"}
                  variant={connection?.enabled ? "outline" : "primary"}
                  onPress={() => onToggle(!connection?.enabled)}
                >
                  {connection?.enabled ? <Pause className="size-4" /> : <Play className="size-4" />}
                  {connection?.enabled ? t("pause") : t("enable")}
                </Button>
              </div>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}

function feishuAction({
  busy,
  connection,
  loadState,
  activeFlow,
  load,
  openPrimary,
  t,
}: {
  busy: string;
  connection: FeishuConnection | null;
  loadState: ConnectionLoadState;
  activeFlow: boolean;
  load: () => Promise<void>;
  openPrimary: () => void;
  t: ReturnType<typeof useTranslations<"connectors.workspaceFeishu">>;
}): ConnectorSummaryAction {
  if (loadState === "checking" && !connection) {
    return {
      disabled: true,
      icon: <Loader2 className="size-4 animate-spin motion-reduce:animate-none" />,
      label: t("checking"),
    };
  }
  if (loadState === "failed") {
    return {
      icon: <RefreshCw className="size-4" />,
      label: t("retry"),
      outline: true,
      onPress: () => void load(),
    };
  }
  if (connection?.status === "disabled" && !connection.connected) {
    return { disabled: true, label: t("unavailable") };
  }
  if (connection?.connected) {
    return {
      disabled: Boolean(busy),
      icon: <Settings2 className="size-4" />,
      label: t("manageConnection"),
      outline: true,
      onPress: openPrimary,
    };
  }
  return {
    disabled: Boolean(busy),
    icon: activeFlow ? <Clock3 className="size-4" /> : <ExternalLink className="size-4" />,
    label: activeFlow ? t("continueSetup") : t("connect"),
    onPress: openPrimary,
  };
}

function feishuStatus(
  connection: FeishuConnection | null,
  loadState: ConnectionLoadState,
  activeFlow: boolean,
  t: ReturnType<typeof useTranslations<"connectors.workspaceFeishu">>,
): ConnectorSummaryStatus {
  if (loadState === "checking" && !connection) {
    return {
      label: t("status.checking"),
      dotClassName: "bg-warning",
      textClassName: "text-foreground",
      checking: true,
    };
  }
  if (loadState === "failed") {
    return {
      label: t("status.unavailable"),
      dotClassName: "bg-danger",
      textClassName: "text-danger",
    };
  }
  if (connection?.status === "disabled" && !connection.connected) {
    return {
      label: t("status.unavailable"),
      dotClassName: "bg-danger",
      textClassName: "text-danger",
    };
  }
  if (activeFlow) {
    return {
      label: t("status.connecting"),
      dotClassName: "bg-warning",
      textClassName: "text-foreground",
      checking: true,
    };
  }
  if (!connection || connection.status === "not_configured") {
    return {
      label: t("status.notConnected"),
      dotClassName: "bg-surface-secondary",
      textClassName: "text-foreground",
    };
  }
  if (connection.status === "ready") {
    return {
      label: t("status.connected"),
      dotClassName: "bg-success",
      textClassName: "text-success",
    };
  }
  if (connection.status === "disabled") {
    return {
      label: t("status.paused"),
      dotClassName: "bg-surface-secondary",
      textClassName: "text-muted",
    };
  }
  return {
    label: t("status.attention"),
    dotClassName: "bg-danger",
    textClassName: "text-danger",
  };
}

function formatDuration(totalSeconds: number) {
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${seconds.toString().padStart(2, "0")}`;
}

function errorMessage(cause: unknown) {
  return cause instanceof Error ? cause.message : String(cause);
}
