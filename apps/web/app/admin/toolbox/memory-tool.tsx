"use client";

import { Button, Card, Chip, Modal, Switch, Tooltip } from "@heroui/react";
import {
  BrainCircuit,
  Check,
  CircleAlert,
  Database,
  LoaderCircle,
  RotateCcw,
  Save,
  Sparkles,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { useTranslations } from "next-intl";
import { AdminErrorDialog } from "@/components/admin/admin-ui";
import { ActionConfirmDialog } from "@/components/ui/action-dialog";
import { SelectControl } from "@/components/ui/select-control";
import { ToolboxCard } from "./toolbox-card";

type MemoryStatus = "disabled" | "incomplete" | "ready" | "degraded";

type MemoryConfig = {
  enabled: boolean;
  resetting: boolean;
  extraction_model_route_id: string;
  embedding_model_route_id: string;
  version: number;
  status: MemoryStatus;
  can_enable: boolean;
  openviking_version: string;
  embedding_dimension: number;
  openviking_status: string;
  extraction_status: string;
  embedding_status: string;
  pending_capture_jobs: number;
  dead_capture_jobs: number;
  error?: string;
};

type ModelRoute = {
  id: string;
  label: string;
  alias: string;
  protocol: "anthropic-messages" | "openai-embeddings";
  enabled: boolean;
  embedding_dimension?: number;
};

const EMPTY_CONFIG: MemoryConfig = {
  enabled: false,
  resetting: false,
  extraction_model_route_id: "",
  embedding_model_route_id: "",
  version: 0,
  status: "disabled",
  can_enable: false,
  openviking_version: "0.4.12",
  embedding_dimension: 1024,
  openviking_status: "not_checked",
  extraction_status: "not_configured",
  embedding_status: "not_configured",
  pending_capture_jobs: 0,
  dead_capture_jobs: 0,
};

export function MemoryTool({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const t = useTranslations("admin.toolboxPage.memory");
  const [config, setConfig] = useState(EMPTY_CONFIG);
  const [models, setModels] = useState<ModelRoute[]>([]);
  const [enabled, setEnabled] = useState(false);
  const [extractionRouteID, setExtractionRouteID] = useState("");
  const [embeddingRouteID, setEmbeddingRouteID] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [resetting, setResetting] = useState(false);
  const [resetOpen, setResetOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const applyConfig = useCallback((next: MemoryConfig) => {
    setConfig(next);
    setEnabled(next.enabled);
    setExtractionRouteID(next.extraction_model_route_id || "");
    setEmbeddingRouteID(next.embedding_model_route_id || "");
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [configResponse, modelsResponse] = await Promise.all([
        fetch("/api/admin/memory/config", { cache: "no-store" }),
        fetch("/api/admin/models", { cache: "no-store" }),
      ]);
      if (!configResponse.ok) throw new Error(await readError(configResponse));
      if (!modelsResponse.ok) throw new Error(await readError(modelsResponse));
      applyConfig((await configResponse.json()) as MemoryConfig);
      const body = (await modelsResponse.json()) as { models?: ModelRoute[] };
      setModels(body.models ?? []);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoading(false);
    }
  }, [applyConfig]);

  useEffect(() => {
    void load();
  }, [load]);

  const extractionModels = useMemo(
    () => models.filter((model) => model.enabled && model.protocol === "anthropic-messages"),
    [models],
  );
  const embeddingModels = useMemo(
    () => models.filter((model) => model.enabled && model.protocol === "openai-embeddings"),
    [models],
  );
  const dirty =
    enabled !== config.enabled ||
    extractionRouteID !== config.extraction_model_route_id ||
    embeddingRouteID !== config.embedding_model_route_id;
  const selectionComplete = Boolean(extractionRouteID && embeddingRouteID);

  async function save() {
    if (!dirty || saving || resetting) return;
    setSaving(true);
    try {
      let current = config;
      const selectionChanged =
        extractionRouteID !== config.extraction_model_route_id ||
        embeddingRouteID !== config.embedding_model_route_id;
      if (selectionChanged) {
        current = await patchMemoryConfig({
          enabled: false,
          extractionRouteID,
          embeddingRouteID,
          expectedVersion: current.version,
        });
      }
      if (!selectionChanged || enabled) {
        current = await patchMemoryConfig({
          enabled,
          extractionRouteID,
          embeddingRouteID,
          expectedVersion: current.version,
        });
      }
      applyConfig(current);
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : String(cause);
      // The selection save and enable are distinct server transitions. Reload
      // whichever transition committed so the form never keeps a stale version.
      await load();
      setError(message);
    } finally {
      setSaving(false);
    }
  }

  async function resetMemory() {
    setResetting(true);
    try {
      const response = await fetch("/api/admin/memory/reset", { method: "POST" });
      if (!response.ok) throw new Error(await readError(response));
      applyConfig((await response.json()) as MemoryConfig);
      setResetOpen(false);
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : String(cause);
      await load();
      setError(message);
    } finally {
      setResetting(false);
    }
  }

  const status = memoryStatusMeta(config.status, Boolean(error), loading, t);

  return (
    <>
      <ToolboxCard
        icon={BrainCircuit}
        iconClassName="bg-violet-100 text-violet-600 dark:bg-violet-950 dark:text-violet-300"
        onPress={() => onOpenChange(true)}
        status={status.label}
        summary={t("summary")}
        title={t("title")}
      />

      <Modal
        isOpen={open}
        onOpenChange={(next) => {
          if (saving || resetting) return;
          if (next) void load();
          onOpenChange(next);
        }}
      >
        <Modal.Backdrop isDismissable={!saving && !resetting}>
          <Modal.Container placement="center" scroll="inside" size="lg">
            <Modal.Dialog>
              <Modal.CloseTrigger aria-label={t("closeSettings")} />
              <Modal.Header className="items-start">
                <Modal.Icon className="bg-violet-500/10 text-violet-600">
                  <BrainCircuit className="size-5" />
                </Modal.Icon>
                <div className="min-w-0">
                  <Modal.Heading>{t("title")}</Modal.Heading>
                  <p className="mt-1 text-sm text-muted">{t("description")}</p>
                </div>
              </Modal.Header>
              <Modal.Body className="space-y-4">
                {loading ? (
                  <div className="flex min-h-56 items-center justify-center gap-2 text-sm text-muted">
                    <LoaderCircle className="size-4 animate-spin" />
                    {t("loading")}
                  </div>
                ) : (
                  <>
                    <Card className="p-4">
                      <div className="flex items-center justify-between gap-4">
                        <div>
                          <div className="text-sm font-semibold">{t("useMemory")}</div>
                          <div className="mt-0.5 text-xs text-muted">
                            {t("useMemoryDescription")}
                          </div>
                        </div>
                        <Switch
                          isDisabled={
                            saving ||
                            resetting ||
                            config.resetting ||
                            (!enabled && !selectionComplete)
                          }
                          isSelected={enabled}
                          onChange={setEnabled}
                        >
                          <Switch.Content>
                            <Switch.Control>
                              <Switch.Thumb />
                            </Switch.Control>
                            {enabled ? t("on") : t("off")}
                          </Switch.Content>
                        </Switch>
                      </div>
                    </Card>

                    <div className="grid gap-3 md:grid-cols-2">
                      <ModelField
                        icon={<Sparkles className="size-4" />}
                        label={t("extractionModel")}
                        options={extractionModels}
                        placeholder={t("chooseChatModel")}
                        value={extractionRouteID}
                        disabled={saving || resetting || config.resetting}
                        onChange={setExtractionRouteID}
                      />
                      <ModelField
                        icon={<Database className="size-4" />}
                        label={t("embeddingModel")}
                        options={embeddingModels}
                        placeholder={t("chooseEmbeddingModel")}
                        value={embeddingRouteID}
                        disabled={saving || resetting || config.resetting}
                        onChange={setEmbeddingRouteID}
                      />
                    </div>

                    <Card className="p-4">
                      <div className="mb-3 flex items-center justify-between gap-3">
                        <div className="text-sm font-semibold">{t("serviceHealth")}</div>
                        <div className="flex items-center gap-2">
                          {config.pending_capture_jobs > 0 ? (
                            <Chip size="sm" variant="soft">
                              {t("pendingJobs", { count: config.pending_capture_jobs })}
                            </Chip>
                          ) : null}
                          {config.dead_capture_jobs > 0 ? (
                            <Chip color="danger" size="sm" variant="soft">
                              {t("failedJobs", { count: config.dead_capture_jobs })}
                            </Chip>
                          ) : null}
                          <Chip color={status.color} size="sm" variant="soft">
                            {status.label}
                          </Chip>
                        </div>
                      </div>
                      <div className="grid grid-cols-3 gap-2">
                        <HealthStep label={t("service")} state={config.openviking_status} />
                        <HealthStep label={t("extraction")} state={config.extraction_status} />
                        <HealthStep label={t("index")} state={config.embedding_status} />
                      </div>
                      <div className="mt-3 flex items-center justify-between text-xs text-muted">
                        <span>
                          {t("internalMemory", { version: config.openviking_version || "0.4.12" })}
                        </span>
                        <span>{t("dimensions", { count: config.embedding_dimension })}</span>
                      </div>
                    </Card>
                  </>
                )}
              </Modal.Body>
              <Modal.Footer className="justify-between">
                <Tooltip>
                  <Tooltip.Trigger>
                    <Button
                      isDisabled={loading || saving || resetting}
                      variant="danger-soft"
                      onPress={() => setResetOpen(true)}
                    >
                      <RotateCcw className="size-4" />
                      {config.resetting ? t("resumeReset") : t("reset")}
                    </Button>
                  </Tooltip.Trigger>
                  <Tooltip.Content>{t("resetHint")}</Tooltip.Content>
                </Tooltip>
                <div className="flex gap-2">
                  <Button variant="outline" onPress={() => onOpenChange(false)}>
                    {t("close")}
                  </Button>
                  <Button
                    isDisabled={!dirty || loading || saving || resetting || config.resetting}
                    isPending={saving}
                    onPress={() => void save()}
                  >
                    <Save className="size-4" />
                    {t("save")}
                  </Button>
                </div>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>

      <ActionConfirmDialog
        busy={resetting}
        confirmLabel={t("reset")}
        description={t("resetDescription")}
        icon={RotateCcw}
        open={resetOpen}
        title={t("resetTitle")}
        tone="danger"
        onConfirm={() => void resetMemory()}
        onOpenChange={setResetOpen}
      />
      <AdminErrorDialog
        error={error}
        onDismiss={() => setError(null)}
        onRetry={() => void load()}
      />
    </>
  );
}

function ModelField({
  icon,
  label,
  options,
  placeholder,
  value,
  disabled,
  onChange,
}: {
  icon: ReactNode;
  label: string;
  options: ModelRoute[];
  placeholder: string;
  value: string;
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <Card className="p-3.5">
      <label className="mb-2 flex items-center gap-2 text-sm font-medium">
        <span className="text-accent">{icon}</span>
        {label}
      </label>
      <SelectControl
        ariaLabel={label}
        options={options.map((model) => ({
          value: model.id,
          label:
            model.protocol === "openai-embeddings" && model.embedding_dimension
              ? `${model.label || model.alias} · ${model.embedding_dimension}d`
              : model.label || model.alias,
        }))}
        placeholder={placeholder}
        value={value}
        disabled={disabled}
        onValueChange={onChange}
      />
    </Card>
  );
}

async function patchMemoryConfig({
  enabled,
  extractionRouteID,
  embeddingRouteID,
  expectedVersion,
}: {
  enabled: boolean;
  extractionRouteID: string;
  embeddingRouteID: string;
  expectedVersion: number;
}) {
  const response = await fetch("/api/admin/memory/config", {
    method: "PATCH",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      enabled,
      extraction_model_route_id: extractionRouteID,
      embedding_model_route_id: embeddingRouteID,
      expected_version: expectedVersion,
    }),
  });
  if (!response.ok) throw new Error(await readError(response));
  return (await response.json()) as MemoryConfig;
}

function HealthStep({ label, state }: { label: string; state: string }) {
  const ready = state === "ready";
  return (
    <div
      className={`flex min-w-0 items-center gap-2 rounded-xl px-3 py-2 text-xs font-medium ${
        ready ? "bg-success/10 text-success" : "bg-surface-secondary text-muted"
      }`}
    >
      {ready ? (
        <Check className="size-3.5 shrink-0" />
      ) : (
        <CircleAlert className="size-3.5 shrink-0" />
      )}
      <span className="truncate">{label}</span>
    </div>
  );
}

function memoryStatusMeta(
  status: MemoryStatus,
  failed: boolean,
  loading: boolean,
  t: ReturnType<typeof useTranslations<"admin.toolboxPage.memory">>,
) {
  if (loading) return { label: t("checking"), color: "default" as const };
  if (failed || status === "degraded") return { label: t("degraded"), color: "danger" as const };
  if (status === "ready") return { label: t("ready"), color: "success" as const };
  if (status === "incomplete") return { label: t("incomplete"), color: "warning" as const };
  return { label: t("disabled"), color: "default" as const };
}

async function readError(response: Response) {
  try {
    const body = await response.json();
    if (typeof body?.error === "string") return body.error;
    if (typeof body?.error?.message === "string") return body.error.message;
    if (typeof body?.message === "string") return body.message;
  } catch {
    // Fall back to the HTTP status.
  }
  return `${response.status} ${response.statusText}`;
}
