"use client";

import {
  ArrowRight,
  BrainCircuit,
  CircleAlert,
  Database,
  ExternalLink,
  LoaderCircle,
  Save,
  Sparkles,
  ToggleLeft,
  ToggleRight,
} from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import { AdminAlert, AdminDrawer, AdminStatusBadge } from "@/components/admin/admin-ui";
import { Button, buttonVariants } from "@/components/ui/button";

type MemoryStatus = "disabled" | "incomplete" | "ready" | "degraded";

type MemoryConfig = {
  enabled: boolean;
  extraction_model_route_id: string;
  embedding_model_route_id: string;
  version: number;
  status: MemoryStatus;
  can_enable: boolean;
  embedding_dimension: number;
  openviking_status: string;
  vlm_status: string;
  embedding_status: string;
  error?: string;
};

type ModelRoute = {
  id: string;
  label: string;
  alias: string;
  protocol: "anthropic-messages" | "openai-responses" | "openai-embeddings";
  enabled: boolean;
  embedding_dimension?: number;
};

const EMPTY_CONFIG: MemoryConfig = {
  enabled: false,
  extraction_model_route_id: "",
  embedding_model_route_id: "",
  version: 0,
  status: "disabled",
  can_enable: false,
  embedding_dimension: 1024,
  openviking_status: "not_ready",
  vlm_status: "not_configured",
  embedding_status: "not_configured",
};

const selectClass =
  "h-10 w-full rounded-xl border border-input bg-background px-3 text-sm outline-none focus:border-ring focus:ring-1 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-60";

export function MemoryTool({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [config, setConfig] = useState(EMPTY_CONFIG);
  const [models, setModels] = useState<ModelRoute[]>([]);
  const [enabled, setEnabled] = useState(false);
  const [extractionRouteID, setExtractionRouteID] = useState("");
  const [embeddingRouteID, setEmbeddingRouteID] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const applyConfig = useCallback((next: MemoryConfig) => {
    setConfig(next);
    setEnabled(next.enabled);
    setExtractionRouteID(next.extraction_model_route_id ?? "");
    setEmbeddingRouteID(next.embedding_model_route_id ?? "");
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
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

  const dirty = useMemo(
    () =>
      enabled !== config.enabled ||
      extractionRouteID !== config.extraction_model_route_id ||
      embeddingRouteID !== config.embedding_model_route_id,
    [config, embeddingRouteID, enabled, extractionRouteID],
  );
  const extractionModels = models.filter(
    (model) =>
      model.enabled &&
      (model.protocol === "anthropic-messages" || model.protocol === "openai-responses"),
  );
  const embeddingModels = models.filter(
    (model) => model.enabled && model.protocol === "openai-embeddings",
  );
  const selectionsChanged =
    extractionRouteID !== config.extraction_model_route_id ||
    embeddingRouteID !== config.embedding_model_route_id;
  const enableBlocked = !config.can_enable || selectionsChanged;

  async function save() {
    if (!dirty || loading) return;
    setSaving(true);
    setError("");
    try {
      const response = await fetch("/api/admin/memory/config", {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          enabled,
          extraction_model_route_id: extractionRouteID,
          embedding_model_route_id: embeddingRouteID,
          expected_version: config.version,
        }),
      });
      if (!response.ok) throw new Error(await readError(response));
      applyConfig((await response.json()) as MemoryConfig);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  function setOpen(nextOpen: boolean) {
    if (saving) return;
    if (nextOpen) void load();
    onOpenChange(nextOpen);
  }

  return (
    <>
      <button
        type="button"
        className="admin-module-card group w-full text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
        onClick={() => setOpen(true)}
      >
        <span className="admin-module-icon bg-emerald-500/10 text-emerald-700">
          <BrainCircuit className="size-[18px]" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-sm font-semibold text-foreground">Memory</span>
          <span className="mt-1 block text-xs leading-5 text-muted-foreground">
            Configure OpenViking-backed user memory and its platform models.
          </span>
          <span className="mt-4 flex items-center gap-2">
            <MemoryStatusBadge loading={loading} status={config.status} error={Boolean(error)} />
            {!loading && !error ? (
              <span className="font-mono text-[10px] text-muted-foreground">v{config.version}</span>
            ) : null}
          </span>
        </span>
        <ArrowRight className="mt-1 size-4 shrink-0 -translate-x-1 text-muted-foreground opacity-0 transition-all duration-200 group-hover:translate-x-0 group-hover:text-primary group-hover:opacity-100" />
      </button>

      <AdminDrawer
        open={open}
        onOpenChange={setOpen}
        title="Memory"
        description="Configure the global OpenViking memory capability."
        size="lg"
        footer={
          <div className="flex items-center justify-end gap-2">
            <Button variant="outline" disabled={saving} onClick={() => setOpen(false)}>
              Close
            </Button>
            <Button disabled={saving || loading || !dirty} onClick={() => void save()}>
              {saving ? (
                <LoaderCircle className="mr-2 size-4 animate-spin" />
              ) : (
                <Save className="mr-2 size-4" />
              )}
              Save changes
            </Button>
          </div>
        }
      >
        <div className="space-y-5">
          {error ? (
            <AdminAlert tone="error" icon={<CircleAlert className="size-4" />}>
              <div className="flex items-center justify-between gap-3">
                <span>{error}</span>
                <Button size="sm" variant="outline" onClick={() => void load()}>
                  Retry
                </Button>
              </div>
            </AdminAlert>
          ) : null}

          <div className="flex items-start justify-between gap-4 rounded-2xl border border-border/70 p-4">
            <div>
              <div className="text-sm font-semibold">Global availability</div>
              <p className="mt-1 text-xs leading-5 text-muted-foreground">
                Disabling immediately stops new recall and capture. Existing memories remain
                manageable by users.
              </p>
            </div>
            <Button
              type="button"
              variant="outline"
              className="shrink-0 gap-2"
              disabled={saving || loading || (!enabled && enableBlocked)}
              onClick={() => setEnabled((current) => !current)}
            >
              {enabled ? <ToggleRight className="size-4" /> : <ToggleLeft className="size-4" />}
              {enabled ? "Enabled" : "Disabled"}
            </Button>
          </div>

          {!enabled && enableBlocked && (extractionRouteID || embeddingRouteID) ? (
            <AdminAlert tone="info">
              Save valid model selections first. Enabling becomes available after validation.
            </AdminAlert>
          ) : null}
          {config.error ? <AdminAlert tone="warning">{config.error}</AdminAlert> : null}

          <div className="grid gap-4">
            <label className="grid gap-1.5 text-sm font-medium">
              <span className="flex items-center gap-2">
                <Sparkles className="size-4 text-muted-foreground" /> Extraction model
              </span>
              <select
                className={selectClass}
                value={extractionRouteID}
                disabled={saving || config.enabled}
                onChange={(event) => setExtractionRouteID(event.target.value)}
              >
                <option value="">Select extraction model</option>
                {extractionModels.map((model) => (
                  <option key={model.id} value={model.id}>
                    {model.label || model.alias} · {model.protocol}
                  </option>
                ))}
              </select>
            </label>

            <label className="grid gap-1.5 text-sm font-medium">
              <span className="flex items-center gap-2">
                <Database className="size-4 text-muted-foreground" /> Embedding model
              </span>
              <select
                className={selectClass}
                value={embeddingRouteID}
                disabled={saving || config.enabled}
                onChange={(event) => setEmbeddingRouteID(event.target.value)}
              >
                <option value="">Select embedding model</option>
                {embeddingModels.map((model) => (
                  <option key={model.id} value={model.id}>
                    {model.label || model.alias} · {model.embedding_dimension ?? "?"} dimensions
                  </option>
                ))}
              </select>
              <span className="text-xs font-normal text-muted-foreground">
                Memory index dimension is locked to {config.embedding_dimension}.
              </span>
            </label>
          </div>

          <div className="grid gap-2 sm:grid-cols-3">
            <HealthItem label="OpenViking" status={config.openviking_status} />
            <HealthItem label="VLM route" status={config.vlm_status} />
            <HealthItem label="Embedding route" status={config.embedding_status} />
          </div>

          <Link
            href="/admin/models"
            className={buttonVariants({ variant: "outline", className: "w-full gap-2" })}
          >
            Create or repair models <ExternalLink className="size-4" />
          </Link>
        </div>
      </AdminDrawer>
    </>
  );
}

function MemoryStatusBadge({
  loading,
  status,
  error,
}: {
  loading: boolean;
  status: MemoryStatus;
  error: boolean;
}) {
  if (loading) return <AdminStatusBadge>Loading</AdminStatusBadge>;
  if (error) return <AdminStatusBadge tone="red">Unavailable</AdminStatusBadge>;
  const tone =
    status === "ready"
      ? "green"
      : status === "degraded"
        ? "red"
        : status === "incomplete"
          ? "amber"
          : "neutral";
  return (
    <AdminStatusBadge tone={tone} dot={status === "ready"}>
      {status.charAt(0).toUpperCase() + status.slice(1)}
    </AdminStatusBadge>
  );
}

function HealthItem({ label, status }: { label: string; status: string }) {
  const ready = status === "ready";
  return (
    <div className="rounded-xl border border-border/70 bg-muted/25 p-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div
        className={
          ready ? "mt-1 text-sm font-semibold text-emerald-700" : "mt-1 text-sm font-semibold"
        }
      >
        {ready ? "Ready" : "Not ready"}
      </div>
    </div>
  );
}

async function readError(response: Response) {
  try {
    const data = await response.json();
    if (typeof data?.error === "string") return data.error;
    if (typeof data?.error?.message === "string") return data.error.message;
    if (typeof data?.message === "string") return data.message;
  } catch {
    // Fall back to status text.
  }
  return `${response.status} ${response.statusText}`;
}
