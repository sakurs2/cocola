"use client";

import { Cpu as ModelsPageIcon } from "lucide-react";
import {
  Binary,
  Boxes,
  Check,
  ChevronDown,
  CircleCheck,
  KeyRound,
  LoaderCircle,
  MoreHorizontal,
  PlugZap,
  Plus,
  Route,
  Star,
  Trash2,
  Upload,
} from "lucide-react";
import Image from "next/image";
import { Button, Chip, Dropdown, Input, SearchField, Switch } from "@heroui/react";
import { type DataGridColumn } from "@cocola/ui-compat/data-grid";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { Segment } from "@cocola/ui-compat/segment";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  AdminAlert,
  AdminConfirmDialog,
  AdminDataGrid,
  AdminDrawer,
  AdminErrorDialog,
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
  AdminTruncatedValue,
} from "@/components/admin/admin-ui";
import { SelectControl } from "@/components/ui/select-control";
import {
  LOCAL_SIMPLE_ICON_PATHS,
  lobeIconPath,
  normalizeLobeIconSlug,
  SIMPLE_ICON_FALLBACK_BADGES,
  SIMPLE_ICON_LABELS,
  SIMPLE_ICON_SLUGS,
} from "@/lib/model-icons";
import { cn } from "@/lib/utils";

type ProviderType = "anthropic" | "openai_embeddings";
type ModelProtocol = "anthropic-messages" | "openai-embeddings";
type View = "models" | "providers";
type ModelKind = "chat" | "embedding";
type ModelIconType = "simple-icons" | "image";

type LLMProvider = {
  id: string;
  name: string;
  type: ProviderType;
  base_url: string;
  api_key_hint: string;
  icon_type: ModelIconType;
  icon_slug: string;
  icon_url: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
};

type LLMModel = {
  id: string;
  alias: string;
  provider_id: string;
  protocol: ModelProtocol;
  real_model: string;
  label: string;
  icon_type: ModelIconType;
  icon_slug: string;
  icon_url: string;
  enabled: boolean;
  visible: boolean;
  is_default: boolean;
  sort_order: number;
  embedding_dimension: number;
  created_at: string;
  updated_at: string;
};

type ProviderForm = {
  id: string;
  name: string;
  type: "anthropic";
  base_url: string;
  api_key: string;
  icon_type: ModelIconType;
  icon_slug: string;
  icon_url: string;
  enabled: boolean;
};

type ModelForm = {
  alias: string;
  provider_id: string;
  real_model: string;
  label: string;
  icon_type: LLMModel["icon_type"];
  icon_slug: string;
  icon_url: string;
  enabled: boolean;
  visible: boolean;
  is_default: boolean;
  sort_order: string;
};

function DisclosureSummary({ children }: { children: ReactNode }) {
  return (
    <summary className="flex cursor-pointer list-none items-center justify-between gap-3 text-sm font-medium outline-none focus-visible:ring-2 focus-visible:ring-focus [&::-webkit-details-marker]:hidden">
      <span>{children}</span>
      <ChevronDown
        aria-hidden="true"
        className="size-4 shrink-0 text-muted transition-transform group-open:rotate-180"
      />
    </summary>
  );
}

type EmbeddingForm = {
  model: string;
  base_url: string;
  api_key: string;
};

type EmbeddingTestResult = {
  ok: boolean;
  latency_ms: number;
  dimension?: number;
  error_code?: string;
  error?: string;
};

type DeleteTarget =
  | { kind: "model"; id: string; name: string }
  | { kind: "provider"; id: string; name: string };

const EMPTY_PROVIDER: ProviderForm = {
  id: "",
  name: "",
  type: "anthropic",
  base_url: "https://api.anthropic.com",
  api_key: "",
  icon_type: "simple-icons",
  icon_slug: "anthropic",
  icon_url: "",
  enabled: true,
};

const EMPTY_MODEL: ModelForm = {
  alias: "",
  provider_id: "",
  real_model: "",
  label: "",
  icon_type: "simple-icons",
  icon_slug: "anthropic",
  icon_url: "",
  enabled: true,
  visible: true,
  is_default: false,
  sort_order: "0",
};

const EMPTY_EMBEDDING: EmbeddingForm = {
  model: "",
  base_url: "https://api.openai.com/v1",
  api_key: "",
};

const inputClass =
  "h-10 w-full min-w-0 rounded-xl border border-separator bg-background px-3 text-sm text-foreground outline-none transition disabled:cursor-not-allowed disabled:bg-surface-secondary/50 disabled:text-muted";

function useObjectURL(file: File | null) {
  const [url, setURL] = useState("");
  useEffect(() => {
    if (!file) {
      setURL("");
      return;
    }
    const next = URL.createObjectURL(file);
    setURL(next);
    return () => URL.revokeObjectURL(next);
  }, [file]);
  return url;
}

export default function AdminModelsPage() {
  const [providers, setProviders] = useState<LLMProvider[]>([]);
  const [models, setModels] = useState<LLMModel[]>([]);
  const [view, setView] = useState<View>("models");
  const [query, setQuery] = useState("");
  const [providerForm, setProviderForm] = useState<ProviderForm>(EMPTY_PROVIDER);
  const [providerIconFile, setProviderIconFile] = useState<File | null>(null);
  const [modelForm, setModelForm] = useState<ModelForm>(EMPTY_MODEL);
  const [modelIconFile, setModelIconFile] = useState<File | null>(null);
  const [modelKind, setModelKind] = useState<ModelKind>("chat");
  const [embeddingForm, setEmbeddingForm] = useState<EmbeddingForm>(EMPTY_EMBEDDING);
  const [embeddingTest, setEmbeddingTest] = useState<EmbeddingTestResult | null>(null);
  const [testingEmbedding, setTestingEmbedding] = useState(false);
  const [editingProvider, setEditingProvider] = useState<string | null>(null);
  const [editingModel, setEditingModel] = useState<string | null>(null);
  const [providerDrawerOpen, setProviderDrawerOpen] = useState(false);
  const [modelDrawerOpen, setModelDrawerOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [formError, setFormError] = useState("");
  const [refreshTick, setRefreshTick] = useState(0);
  const providerIconPreview = useObjectURL(providerIconFile);
  const modelIconPreview = useObjectURL(modelIconFile);

  const providerByID = useMemo(
    () => new Map(providers.map((provider) => [provider.id, provider])),
    [providers],
  );
  const routeCountByProvider = useMemo(() => {
    const counts = new Map<string, number>();
    for (const model of models)
      counts.set(model.provider_id, (counts.get(model.provider_id) ?? 0) + 1);
    return counts;
  }, [models]);

  const visibleModels = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return models;
    return models.filter((model) =>
      [model.label, model.alias, model.real_model, model.provider_id]
        .join(" ")
        .toLowerCase()
        .includes(needle),
    );
  }, [models, query]);

  const visibleProviders = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return providers.filter(
      (provider) =>
        provider.type !== "openai_embeddings" &&
        (!needle ||
          [provider.name, provider.id, provider.base_url, providerTypeMeta(provider.type).label]
            .join(" ")
            .toLowerCase()
            .includes(needle)),
    );
  }, [providers, query]);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [providersRes, modelsRes] = await Promise.all([
        fetch("/api/admin/model-providers", { cache: "no-store" }),
        fetch("/api/admin/models", { cache: "no-store" }),
      ]);
      if (!providersRes.ok) throw new Error(await errorText(providersRes));
      if (!modelsRes.ok) throw new Error(await errorText(modelsRes));
      const providerBody = (await providersRes.json()) as { providers?: LLMProvider[] };
      const modelBody = (await modelsRes.json()) as { models?: LLMModel[] };
      setProviders(providerBody.providers ?? []);
      setModels(modelBody.models ?? []);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  function createProvider() {
    setEditingProvider(null);
    setProviderForm(EMPTY_PROVIDER);
    setProviderIconFile(null);
    setFormError("");
    setProviderDrawerOpen(true);
  }

  function editProvider(provider: LLMProvider) {
    if (provider.type === "openai_embeddings") return;
    setEditingProvider(provider.id);
    setProviderForm({
      id: provider.id,
      name: provider.name,
      type: provider.type,
      base_url: provider.base_url,
      api_key: "",
      icon_type: provider.icon_type || "simple-icons",
      icon_slug: provider.icon_slug || providerTypeMeta(provider.type).defaultIconSlug,
      icon_url: provider.icon_url || "",
      enabled: provider.enabled,
    });
    setProviderIconFile(null);
    setFormError("");
    setProviderDrawerOpen(true);
  }

  function createModel() {
    const firstChatProvider = providers.find((provider) => provider.type !== "openai_embeddings");
    setEditingModel(null);
    setModelKind("chat");
    setModelForm({
      ...EMPTY_MODEL,
      provider_id: firstChatProvider?.id ?? "",
      icon_type: firstChatProvider?.icon_type || "simple-icons",
      icon_slug:
        firstChatProvider?.icon_slug ||
        (firstChatProvider
          ? providerTypeMeta(firstChatProvider.type).defaultIconSlug
          : EMPTY_MODEL.icon_slug),
      icon_url: firstChatProvider?.icon_url || "",
    });
    setModelIconFile(null);
    setEmbeddingForm(EMPTY_EMBEDDING);
    setEmbeddingTest(null);
    setFormError("");
    setModelDrawerOpen(true);
  }

  function editModel(model: LLMModel) {
    setEditingModel(model.id);
    const embedding = model.protocol === "openai-embeddings";
    setModelKind(embedding ? "embedding" : "chat");
    if (embedding) {
      setEmbeddingForm({
        model: model.real_model,
        base_url: providerByID.get(model.provider_id)?.base_url ?? "",
        api_key: "",
      });
      setEmbeddingTest(null);
    }
    setModelForm({
      alias: model.alias,
      provider_id: model.provider_id,
      real_model: model.real_model,
      label: model.label,
      icon_type: model.icon_type,
      icon_slug: model.icon_slug,
      icon_url: model.icon_url,
      enabled: model.enabled,
      visible: model.visible,
      is_default: model.is_default,
      sort_order: String(model.sort_order),
    });
    setModelIconFile(null);
    setFormError("");
    setModelDrawerOpen(true);
  }

  async function saveProvider() {
    setSaving(true);
    setFormError("");
    try {
      let iconURL = providerForm.icon_type === "image" ? providerForm.icon_url : "";
      if (providerForm.icon_type === "image" && providerIconFile) {
        iconURL = await uploadModelIcon(providerIconFile);
      }
      if (providerForm.icon_type === "image" && !iconURL) {
        throw new Error("Choose an image for this provider icon.");
      }
      const body: Record<string, unknown> = {
        id: providerForm.id.trim() || providerIDFromName(providerForm.name),
        name: providerForm.name,
        type: providerForm.type,
        base_url: providerForm.base_url,
        icon_type: providerForm.icon_type,
        icon_slug: providerForm.icon_slug,
        icon_url: iconURL,
        enabled: providerForm.enabled,
      };
      if (providerForm.api_key.trim()) body.api_key = providerForm.api_key.trim();
      const url = editingProvider
        ? `/api/admin/model-providers/${encodeURIComponent(editingProvider)}`
        : "/api/admin/model-providers";
      const response = await fetch(url, {
        method: editingProvider ? "PATCH" : "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!response.ok) throw new Error(await errorText(response));
      setProviderIconFile(null);
      setProviderDrawerOpen(false);
      await load();
    } catch (cause) {
      setFormError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  async function saveModel() {
    setSaving(true);
    setFormError("");
    try {
      if (modelKind === "embedding") {
        const body: Record<string, string> = {
          model: embeddingForm.model.trim(),
          base_url: embeddingForm.base_url.trim(),
        };
        if (embeddingForm.api_key.trim()) body.api_key = embeddingForm.api_key.trim();
        const url = editingModel
          ? `/api/admin/embedding-models/${encodeURIComponent(editingModel)}`
          : "/api/admin/embedding-models";
        const response = await fetch(url, {
          method: editingModel ? "PATCH" : "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify(body),
        });
        if (!response.ok) throw new Error(await errorText(response));
        setModelDrawerOpen(false);
        await load();
        return;
      }
      let iconURL = modelForm.icon_type === "image" ? modelForm.icon_url : "";
      if (modelForm.icon_type === "image" && modelIconFile) {
        iconURL = await uploadModelIcon(modelIconFile);
      }
      if (modelForm.icon_type === "image" && !iconURL) {
        throw new Error("Choose an image for this model icon.");
      }
      const body = {
        alias:
          modelForm.alias ||
          providerIDFromName(modelForm.label) ||
          providerIDFromName(modelForm.real_model),
        provider_id: modelForm.provider_id,
        real_model: modelForm.real_model,
        label: modelForm.label,
        icon_type: modelForm.icon_type,
        icon_slug: modelForm.icon_slug,
        icon_url: iconURL,
        enabled: modelForm.enabled,
        visible: modelForm.visible,
        is_default: modelForm.is_default,
        sort_order: Number.parseInt(modelForm.sort_order || "0", 10) || 0,
      };
      const url = editingModel
        ? `/api/admin/models/${encodeURIComponent(editingModel)}`
        : "/api/admin/models";
      const response = await fetch(url, {
        method: editingModel ? "PATCH" : "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!response.ok) throw new Error(await errorText(response));
      setModelIconFile(null);
      setModelDrawerOpen(false);
      await load();
    } catch (cause) {
      setFormError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  async function testEmbeddingConnection() {
    if (testingEmbedding) return;
    setTestingEmbedding(true);
    setEmbeddingTest(null);
    setFormError("");
    try {
      const body: Record<string, string> = {
        model: embeddingForm.model.trim(),
        base_url: embeddingForm.base_url.trim(),
      };
      if (editingModel) body.route_id = editingModel;
      if (embeddingForm.api_key.trim()) body.api_key = embeddingForm.api_key.trim();
      const response = await fetch("/api/admin/embedding-models/test", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!response.ok) throw new Error(await errorText(response));
      setEmbeddingTest((await response.json()) as EmbeddingTestResult);
    } catch (cause) {
      setFormError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setTestingEmbedding(false);
    }
  }

  async function setDefault(model: LLMModel) {
    setSaving(true);
    setError("");
    try {
      const response = await fetch(`/api/admin/models/${encodeURIComponent(model.id)}/default`, {
        method: "POST",
      });
      if (!response.ok) throw new Error(await errorText(response));
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  async function deleteResource() {
    if (!deleteTarget) return;
    setSaving(true);
    setError("");
    try {
      const prefix = deleteTarget.kind === "model" ? "models" : "model-providers";
      const response = await fetch(`/api/admin/${prefix}/${encodeURIComponent(deleteTarget.id)}`, {
        method: "DELETE",
      });
      if (!response.ok) throw new Error(await errorText(response));
      setDeleteTarget(null);
      await load();
    } catch (cause) {
      setDeleteTarget(null);
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  const selectedProvider = providerByID.get(modelForm.provider_id);
  const embeddingProvider = editingModel
    ? providerByID.get(models.find((model) => model.id === editingModel)?.provider_id ?? "")
    : undefined;
  const canTestEmbedding =
    embeddingForm.model.trim() !== "" &&
    embeddingForm.base_url.trim() !== "" &&
    (embeddingForm.api_key.trim() !== "" || Boolean(embeddingProvider?.api_key_hint));
  return (
    <AdminPage>
      <AdminPageHeader
        icon={<ModelsPageIcon className="size-5" />}
        title="Models"
        description="Connect model providers and decide which routes are available to each Agent Runtime."
        actions={
          <AdminRefreshButton
            refreshing={loading}
            disabled={loading}
            onClick={() => {
              setRefreshTick((tick) => tick + 1);
              void load();
            }}
          >
            Refresh
          </AdminRefreshButton>
        }
      />

      <AdminErrorDialog
        error={error}
        title="Model configuration operation failed"
        onDismiss={() => setError("")}
        onRetry={() => void load()}
      />

      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <Segment
          aria-label="Model configuration"
          selectedKey={view}
          onSelectionChange={(key) => {
            setView(String(key) as View);
            setQuery("");
          }}
        >
          <Segment.Item id="models">
            <Route className="size-4" />
            Model routes · {models.length}
          </Segment.Item>
          <Segment.Item id="providers">
            <Boxes className="size-4" />
            Providers · {visibleProviders.length}
          </Segment.Item>
        </Segment>
        <Button onClick={view === "models" ? createModel : createProvider}>
          <Plus className="size-4" />
          {view === "models" ? "Add model" : "Add provider"}
        </Button>
      </div>

      <SearchField
        aria-label={view === "models" ? "Find a model route" : "Find a provider"}
        className="w-full max-w-sm"
        value={query}
        onChange={setQuery}
      >
        <SearchField.Group>
          <SearchField.SearchIcon />
          <SearchField.Input
            placeholder={view === "models" ? "Find a model route" : "Find a provider"}
          />
          <SearchField.ClearButton />
        </SearchField.Group>
      </SearchField>

      {view === "models" ? (
        <ModelsList
          models={visibleModels}
          providerByID={providerByID}
          loading={loading}
          saving={saving}
          onEdit={editModel}
          onDefault={(model) => void setDefault(model)}
          onDelete={(model) =>
            setDeleteTarget({ kind: "model", id: model.id, name: model.label || model.alias })
          }
        />
      ) : (
        <ProvidersList
          providers={visibleProviders}
          routeCountByProvider={routeCountByProvider}
          loading={loading}
          onEdit={editProvider}
          onDelete={(provider) =>
            setDeleteTarget({
              kind: "provider",
              id: provider.id,
              name: provider.name || provider.id,
            })
          }
        />
      )}

      <AdminDrawer
        open={providerDrawerOpen}
        onOpenChange={(open) => !saving && setProviderDrawerOpen(open)}
        title={editingProvider ? "Edit provider" : "Add provider"}
        description="Connect an Anthropic Messages-compatible provider."
        size="lg"
        footer={
          <DrawerFooter
            saving={saving}
            saveLabel={editingProvider ? "Save changes" : "Add provider"}
            onCancel={() => setProviderDrawerOpen(false)}
            onSave={() => void saveProvider()}
          />
        }
      >
        <div className="grid gap-5">
          {formError ? <AdminAlert tone="error">{formError}</AdminAlert> : null}
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Name">
              <Input
                className={inputClass}
                value={providerForm.name}
                onChange={(event) => setProviderForm({ ...providerForm, name: event.target.value })}
                placeholder="Production provider"
              />
            </Field>
            <Field label="Status">
              <Toggle
                checked={providerForm.enabled}
                onChange={(enabled) => setProviderForm({ ...providerForm, enabled })}
                label="Provider enabled"
              />
            </Field>
          </div>

          <IconPicker
            iconType={providerForm.icon_type}
            iconSlug={providerForm.icon_slug}
            imageSrc={providerIconPreview || providerForm.icon_url}
            fileName={providerIconFile?.name}
            fallbackText={(providerForm.name || providerForm.id || "AI").slice(0, 2).toUpperCase()}
            disabled={saving}
            onTypeChange={(iconType) => {
              setProviderIconFile(null);
              setProviderForm((current) => ({
                ...current,
                icon_type: iconType,
              }));
            }}
            onSlugChange={(iconSlug) =>
              setProviderForm((current) => ({ ...current, icon_slug: iconSlug }))
            }
            onFileChange={setProviderIconFile}
            onInvalid={setFormError}
          />

          <Field label="Base URL">
            <Input
              className={inputClass}
              value={providerForm.base_url}
              onChange={(event) =>
                setProviderForm({ ...providerForm, base_url: event.target.value })
              }
              placeholder={providerTypeMeta(providerForm.type).defaultBaseURL}
            />
          </Field>

          <div className="rounded-2xl border border-[#7828c8]/20 bg-[#7828c8]/[0.06] p-3">
            <div className="text-[11px] font-semibold uppercase tracking-[0.12em] text-[#7828c8]/80">
              Request path
            </div>
            <code className="mt-1 block break-all text-xs text-foreground">
              {providerEndpoint(providerForm.base_url, providerForm.type)}
            </code>
            <p className="mt-2 text-xs leading-5 text-muted">
              This route uses the native Anthropic Messages API.
            </p>
          </div>

          <Field
            label="API key"
            hint={editingProvider ? "Leave blank to keep the current key." : undefined}
          >
            <div className="relative">
              <KeyRound
                aria-hidden="true"
                className="pointer-events-none absolute left-3 top-1/2 z-10 size-4 -translate-y-1/2 text-muted"
              />
              <Input
                className={cn(inputClass, "pl-10")}
                value={providerForm.api_key}
                onChange={(event) =>
                  setProviderForm({ ...providerForm, api_key: event.target.value })
                }
                placeholder={editingProvider ? "Keep current key" : "Enter API key"}
                type="password"
                autoComplete="new-password"
              />
            </div>
          </Field>

          <details className="group rounded-2xl border border-border/70 p-3">
            <DisclosureSummary>Advanced</DisclosureSummary>
            <div className="mt-4 border-t border-border/70 pt-4">
              <Field
                label="Provider ID"
                hint="Generated from the provider name when left blank; it cannot be changed later."
              >
                <Input
                  className={cn(inputClass, "font-mono text-xs")}
                  value={providerForm.id}
                  disabled={Boolean(editingProvider)}
                  onChange={(event) => setProviderForm({ ...providerForm, id: event.target.value })}
                  placeholder="openai-prod"
                />
              </Field>
            </div>
          </details>
        </div>
      </AdminDrawer>

      <AdminDrawer
        open={modelDrawerOpen}
        onOpenChange={(open) => !saving && setModelDrawerOpen(open)}
        title={
          editingModel
            ? modelKind === "embedding"
              ? "Edit embedding model"
              : "Edit model route"
            : "Add model"
        }
        description={
          modelKind === "embedding"
            ? "Add an OpenAI-compatible embedding model for Memory and future knowledge sources."
            : "Connect a user-visible model to one provider and upstream model ID."
        }
        size="lg"
        footer={
          <DrawerFooter
            saving={saving}
            saveLabel={editingModel ? "Save changes" : "Add model"}
            onCancel={() => setModelDrawerOpen(false)}
            onSave={() => void saveModel()}
          />
        }
      >
        <div className="grid gap-5">
          {formError ? <AdminAlert tone="error">{formError}</AdminAlert> : null}
          {!editingModel ? (
            <FormGroup label="Model type">
              <div className="grid gap-2 sm:grid-cols-[repeat(2,minmax(0,1fr))]">
                <Button
                  variant="ghost"
                  onPress={() => setModelKind("chat")}
                  className={cn(
                    "h-auto min-w-0 w-full flex-col items-stretch gap-1 overflow-hidden whitespace-normal rounded-2xl border p-3 text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus/30",
                    modelKind === "chat"
                      ? "border-accent/45 bg-accent/5 shadow-sm"
                      : "border-border bg-background hover:border-accent/25 hover:bg-surface-secondary/25",
                  )}
                >
                  <span className="flex w-full min-w-0 items-center justify-between gap-3 text-sm font-semibold">
                    <span className="truncate">Chat model</span>
                    {modelKind === "chat" ? <Check className="size-4 text-accent" /> : null}
                  </span>
                  <span className="block w-full text-xs leading-5 text-muted">
                    For Agent conversations.
                  </span>
                </Button>
                <Button
                  variant="ghost"
                  onPress={() => setModelKind("embedding")}
                  className={cn(
                    "h-auto min-w-0 w-full flex-col items-stretch gap-1 overflow-hidden whitespace-normal rounded-2xl border p-3 text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus/30",
                    modelKind === "embedding"
                      ? "border-accent/45 bg-accent/5 shadow-sm"
                      : "border-border bg-background hover:border-accent/25 hover:bg-surface-secondary/25",
                  )}
                >
                  <span className="flex w-full min-w-0 items-center justify-between gap-3 text-sm font-semibold">
                    <span className="truncate">Embedding model</span>
                    {modelKind === "embedding" ? <Check className="size-4 text-accent" /> : null}
                  </span>
                  <span className="block w-full text-xs leading-5 text-muted">
                    For Memory and knowledge.
                  </span>
                </Button>
              </div>
            </FormGroup>
          ) : null}

          {modelKind === "embedding" ? (
            <div className="grid gap-5">
              <Field label="Model name">
                <Input
                  className={inputClass}
                  value={embeddingForm.model}
                  onChange={(event) => {
                    setEmbeddingForm({ ...embeddingForm, model: event.target.value });
                    setEmbeddingTest(null);
                  }}
                  placeholder="text-embedding-3-large"
                />
              </Field>

              <Field label="Base URL">
                <Input
                  className={inputClass}
                  value={embeddingForm.base_url}
                  onChange={(event) => {
                    setEmbeddingForm({ ...embeddingForm, base_url: event.target.value });
                    setEmbeddingTest(null);
                  }}
                  placeholder="https://api.openai.com/v1"
                  inputMode="url"
                />
              </Field>

              <Field
                label="API key"
                hint={editingModel ? "Leave blank to keep the current key." : undefined}
              >
                <div className="relative">
                  <KeyRound
                    aria-hidden="true"
                    className="pointer-events-none absolute left-3 top-1/2 z-10 size-4 -translate-y-1/2 text-muted"
                  />
                  <Input
                    className={cn(inputClass, "pl-10")}
                    value={embeddingForm.api_key}
                    onChange={(event) => {
                      setEmbeddingForm({ ...embeddingForm, api_key: event.target.value });
                      setEmbeddingTest(null);
                    }}
                    placeholder={
                      editingModel && embeddingProvider?.api_key_hint
                        ? `Keep current key (${embeddingProvider.api_key_hint})`
                        : "Enter API key"
                    }
                    type="password"
                    autoComplete="new-password"
                  />
                </div>
              </Field>

              <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-border/70 bg-surface-secondary/20 p-3">
                <div className="min-w-0">
                  <div className="text-xs font-medium text-foreground">OpenAI Embeddings</div>
                  <code className="mt-1 block break-all text-[11px] text-muted">
                    {embeddingEndpoint(embeddingForm.base_url)}
                  </code>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  isDisabled={!canTestEmbedding || testingEmbedding || saving}
                  onPress={() => void testEmbeddingConnection()}
                >
                  {testingEmbedding ? (
                    <LoaderCircle className="mr-2 size-4 animate-spin" />
                  ) : (
                    <PlugZap className="mr-2 size-4" />
                  )}
                  Test connection
                </Button>
              </div>

              {embeddingTest ? (
                <AdminAlert tone={embeddingTest.ok ? "success" : "error"}>
                  <span className="flex items-center gap-2">
                    {embeddingTest.ok ? <CircleCheck className="size-4" /> : null}
                    {embeddingTest.ok
                      ? `Connected · ${embeddingTest.dimension} dimensions · ${embeddingTest.latency_ms} ms`
                      : embeddingTest.error || "Embedding connection failed"}
                  </span>
                </AdminAlert>
              ) : null}
            </div>
          ) : (
            <>
              <Field label="Provider">
                <SelectControl
                  className={inputClass}
                  value={modelForm.provider_id}
                  onValueChange={(value) => setModelForm({ ...modelForm, provider_id: value })}
                  options={[
                    { value: "", label: "Select provider" },
                    ...providers
                      .filter((provider) => {
                        if (provider.type === "openai_embeddings") return false;
                        if (!editingModel) return true;
                        const original = models.find((model) => model.id === editingModel);
                        return !original || protocolForType(provider.type) === original.protocol;
                      })
                      .map((provider) => ({
                        value: provider.id,
                        label: `${provider.name || provider.id} · ${
                          providerTypeMeta(provider.type).shortLabel
                        }`,
                      })),
                  ]}
                  contentClassName="cocola-admin-ui"
                />
              </Field>

              {selectedProvider ? (
                <div className="flex flex-wrap items-center gap-2 rounded-2xl border border-border/70 bg-surface-secondary/25 p-3">
                  <ProviderProtocolBadge type={selectedProvider.type} />
                  <span className="text-xs text-muted">
                    Compatible with {runtimeCompatibilityForType(selectedProvider.type)}
                  </span>
                </div>
              ) : null}

              <Field label="Model name">
                <Input
                  className={inputClass}
                  value={modelForm.label}
                  onChange={(event) => setModelForm({ ...modelForm, label: event.target.value })}
                  placeholder="GPT-5"
                />
              </Field>

              <Field label="Upstream model ID">
                <Input
                  className={inputClass}
                  value={modelForm.real_model}
                  onChange={(event) =>
                    setModelForm({ ...modelForm, real_model: event.target.value })
                  }
                  placeholder="gpt-5"
                />
              </Field>

              <IconPicker
                iconType={modelForm.icon_type}
                iconSlug={modelForm.icon_slug}
                imageSrc={modelIconPreview || modelForm.icon_url}
                fileName={modelIconFile?.name}
                fallbackText={(modelForm.label || modelForm.real_model || "AI")
                  .slice(0, 2)
                  .toUpperCase()}
                disabled={saving}
                onTypeChange={(iconType) => {
                  setModelIconFile(null);
                  setModelForm((current) => ({
                    ...current,
                    icon_type: iconType,
                  }));
                }}
                onSlugChange={(iconSlug) =>
                  setModelForm((current) => ({ ...current, icon_slug: iconSlug }))
                }
                onFileChange={setModelIconFile}
                onInvalid={setFormError}
              />

              <div className="grid gap-3 rounded-2xl border border-border/70 p-3 sm:grid-cols-3">
                <Toggle
                  checked={modelForm.enabled}
                  onChange={(enabled) => setModelForm({ ...modelForm, enabled })}
                  label="Enabled"
                />
                <Toggle
                  checked={modelForm.visible}
                  onChange={(visible) => setModelForm({ ...modelForm, visible })}
                  label="Visible to users"
                />
                <Toggle
                  checked={modelForm.is_default}
                  onChange={(is_default) => setModelForm({ ...modelForm, is_default })}
                  label="Protocol default"
                />
              </div>

              <details className="group rounded-2xl border border-border/70 p-3">
                <DisclosureSummary>Advanced</DisclosureSummary>
                <div className="mt-4 border-t border-border/70 pt-4">
                  <Field label="Display priority" hint="Lower numbers appear first.">
                    <Input
                      className={inputClass}
                      value={modelForm.sort_order}
                      onChange={(event) =>
                        setModelForm({ ...modelForm, sort_order: event.target.value })
                      }
                      inputMode="numeric"
                    />
                  </Field>
                </div>
              </details>
            </>
          )}
        </div>
      </AdminDrawer>

      <AdminConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={`Delete ${deleteTarget?.kind ?? "resource"}?`}
        description={
          deleteTarget?.kind === "provider"
            ? `Delete ${deleteTarget.name}? Providers with model routes cannot be deleted.`
            : `Delete ${deleteTarget?.name ?? "this model route"}? Historical run records will remain available.`
        }
        confirmLabel="Delete"
        destructive
        busy={saving}
        onConfirm={() => void deleteResource()}
      />
    </AdminPage>
  );
}

function BrandIcon({
  slug,
  fallbackText,
  imageSrc,
}: {
  slug?: string;
  fallbackText: string;
  imageSrc?: string;
}) {
  const [lobeFailed, setLobeFailed] = useState(false);
  const [localFailed, setLocalFailed] = useState(false);

  if (imageSrc) {
    return (
      <span className="admin-brand-icon">
        <Image
          src={imageSrc}
          alt=""
          width={24}
          height={24}
          className="admin-brand-img"
          unoptimized
        />
      </span>
    );
  }

  const normalized = normalizeLobeIconSlug(slug);
  const lobe = normalized && !lobeFailed ? lobeIconPath(normalized) : "";
  const local = slug ? LOCAL_SIMPLE_ICON_PATHS[slug.toLowerCase()] : "";

  if (lobe) {
    return (
      <span className="admin-brand-icon">
        <Image
          src={lobe}
          alt=""
          width={24}
          height={24}
          className="admin-brand-img"
          unoptimized
          onError={() => setLobeFailed(true)}
        />
      </span>
    );
  }
  if (local && !localFailed) {
    return (
      <span className="admin-brand-icon">
        <Image
          src={local}
          alt=""
          width={24}
          height={24}
          className="admin-brand-img"
          unoptimized
          onError={() => setLocalFailed(true)}
        />
      </span>
    );
  }
  return <span className="admin-brand-fallback">{fallbackText}</span>;
}

function BrandGlyph({ slug }: { slug: string }) {
  const [lobeFailed, setLobeFailed] = useState(false);
  const [localFailed, setLocalFailed] = useState(false);

  const normalized = normalizeLobeIconSlug(slug);
  const lobe = normalized && !lobeFailed ? lobeIconPath(normalized) : "";
  const local = slug ? LOCAL_SIMPLE_ICON_PATHS[slug.toLowerCase()] : "";
  const fallbackText =
    SIMPLE_ICON_FALLBACK_BADGES[slug.toLowerCase()] || slug.slice(0, 2).toUpperCase() || "AI";

  if (lobe) {
    return (
      <span className="admin-brand-glyph">
        <Image
          src={lobe}
          alt=""
          width={18}
          height={18}
          className="admin-brand-glyph-img"
          unoptimized
          onError={() => setLobeFailed(true)}
        />
      </span>
    );
  }
  if (local && !localFailed) {
    return (
      <span className="admin-brand-glyph">
        <Image
          src={local}
          alt=""
          width={18}
          height={18}
          className="admin-brand-glyph-img"
          unoptimized
          onError={() => setLocalFailed(true)}
        />
      </span>
    );
  }
  return <span className="admin-brand-glyph admin-brand-glyph-fallback">{fallbackText}</span>;
}

function IconPicker({
  iconType,
  iconSlug,
  imageSrc,
  fileName,
  fallbackText,
  disabled,
  onTypeChange,
  onSlugChange,
  onFileChange,
  onInvalid,
}: {
  iconType: ModelIconType;
  iconSlug: string;
  imageSrc: string;
  fileName?: string;
  fallbackText: string;
  disabled: boolean;
  onTypeChange: (type: ModelIconType) => void;
  onSlugChange: (slug: string) => void;
  onFileChange: (file: File | null) => void;
  onInvalid: (message: string) => void;
}) {
  const fileInput = useRef<HTMLInputElement>(null);
  const chooseFile = () => fileInput.current?.click();

  return (
    <FormGroup label="Icon">
      <div className="grid grid-cols-[42px_minmax(0,1fr)] items-start gap-3 rounded-2xl border border-border/70 bg-surface-secondary/15 p-3">
        <BrandIcon
          slug={iconSlug}
          imageSrc={iconType === "image" ? imageSrc : undefined}
          fallbackText={fallbackText || "AI"}
        />
        <div className="grid min-w-0 gap-3 sm:grid-cols-2">
          <div className="grid min-w-0 gap-1.5">
            <span className="text-xs font-medium text-muted">Source</span>
            <SelectControl
              className={inputClass}
              value={iconType}
              onValueChange={(value) => onTypeChange(value as ModelIconType)}
              options={[
                { value: "simple-icons", label: "Brand icon" },
                { value: "image", label: "Upload image" },
              ]}
              contentClassName="cocola-admin-ui"
            />
          </div>
          {iconType === "simple-icons" ? (
            <div className="grid min-w-0 gap-1.5">
              <span className="text-xs font-medium text-muted">Brand</span>
              <SelectControl
                className={inputClass}
                value={iconSlug}
                onValueChange={onSlugChange}
                options={SIMPLE_ICON_SLUGS.map((slug) => ({
                  value: slug,
                  label: SIMPLE_ICON_LABELS[slug] ?? slug,
                  icon: <BrandGlyph slug={slug} />,
                }))}
                contentClassName="cocola-admin-ui"
              />
            </div>
          ) : (
            <div className="grid min-w-0 gap-1.5">
              <span className="text-xs font-medium text-muted">Image</span>
              <input
                ref={fileInput}
                className="sr-only"
                type="file"
                accept="image/png,image/jpeg,image/webp,.png,.jpg,.jpeg,.webp"
                disabled={disabled}
                onChange={(event) => {
                  const file = event.target.files?.[0];
                  event.target.value = "";
                  if (!file) return;
                  const supported =
                    ["image/png", "image/jpeg", "image/webp"].includes(file.type) ||
                    /\.(png|jpe?g|webp)$/i.test(file.name);
                  if (!supported) {
                    onInvalid("Choose a PNG, JPEG, or WebP image.");
                    return;
                  }
                  if (file.size > 1024 * 1024) {
                    onInvalid("Model icons must be 1 MB or smaller.");
                    return;
                  }
                  onInvalid("");
                  onFileChange(file);
                }}
              />
              <Button
                className="h-10 min-w-0 justify-start overflow-hidden"
                variant="outline"
                isDisabled={disabled}
                onPress={chooseFile}
              >
                <Upload className="size-4 shrink-0" />
                <span className="truncate" title={fileName}>
                  {fileName || imageSrc ? "Replace image" : "Choose image"}
                </span>
              </Button>
              <span className="truncate text-xs font-normal text-muted" title={fileName}>
                {fileName || "PNG, JPEG or WebP · up to 1 MB"}
              </span>
            </div>
          )}
        </div>
      </div>
    </FormGroup>
  );
}

function protoChipLabel(type: ProviderType | undefined, protocol: ModelProtocol) {
  if (type) return providerTypeMeta(type).shortLabel;
  if (protocol === "openai-embeddings") return "Embeddings";
  return "Anthropic Messages";
}

function ModelsList({
  models,
  providerByID,
  loading,
  saving,
  onEdit,
  onDefault,
  onDelete,
}: {
  models: LLMModel[];
  providerByID: Map<string, LLMProvider>;
  loading: boolean;
  saving: boolean;
  onEdit: (model: LLMModel) => void;
  onDefault: (model: LLMModel) => void;
  onDelete: (model: LLMModel) => void;
}) {
  const columns: DataGridColumn<LLMModel>[] = [
    {
      id: "model",
      header: "Model",
      isRowHeader: true,
      minWidth: 300,
      cell: (model) => (
        <span className="flex min-w-0 items-center gap-2 py-1">
          <ModelIcon model={model} />
          <AdminTruncatedValue
            className="font-semibold"
            copyLabel="model name"
            onPress={() => onEdit(model)}
            value={model.label || model.alias}
          />
          {model.is_default ? (
            <Star className="size-3.5 shrink-0 fill-amber-400 text-amber-400" />
          ) : null}
        </span>
      ),
    },
    {
      id: "api",
      header: "Upstream API",
      minWidth: 160,
      cell: (model) => {
        const provider = providerByID.get(model.provider_id);
        return (
          <Chip size="sm" variant="soft">
            {protoChipLabel(provider?.type, model.protocol)}
          </Chip>
        );
      },
    },
    {
      id: "provider",
      header: "Provider",
      minWidth: 180,
      cell: (model) => (
        <AdminTruncatedValue
          className="text-sm"
          copyLabel="provider name"
          value={providerByID.get(model.provider_id)?.name || model.provider_id}
        />
      ),
    },
    {
      id: "availability",
      header: "Availability",
      minWidth: 190,
      cell: (model) => (
        <span className="flex gap-2">
          <Chip color={model.enabled ? "success" : "default"} size="sm" variant="soft">
            {model.enabled ? "Enabled" : "Disabled"}
          </Chip>
          <Chip color={model.visible ? "accent" : "default"} size="sm" variant="soft">
            {model.visible ? "Visible" : "Hidden"}
          </Chip>
        </span>
      ),
    },
    {
      id: "actions",
      header: "Actions",
      align: "center",
      width: 80,
      cell: (model) => (
        <ResourceMenu
          onEdit={() => onEdit(model)}
          onDefault={
            model.is_default || model.protocol === "openai-embeddings"
              ? undefined
              : () => onDefault(model)
          }
          onDelete={() => onDelete(model)}
          disabled={saving}
        />
      ),
    },
  ];
  return (
    <AdminDataGrid
      aria-label="Model routes"
      columns={columns}
      contentClassName="min-w-[880px]"
      data={models}
      getRowId={(model) => model.id}
      selectionMode="none"
      variant="primary"
      renderEmptyState={() => (
        <EmptyState>
          <EmptyState.Header>
            <EmptyState.Media variant="icon">
              <Route className="text-violet-500" />
            </EmptyState.Media>
            <EmptyState.Title>
              {loading ? "Loading model routes" : "No model routes"}
            </EmptyState.Title>
            <EmptyState.Description>
              Add a route after connecting at least one provider.
            </EmptyState.Description>
          </EmptyState.Header>
        </EmptyState>
      )}
    />
  );
}

function ProviderIcon({ provider }: { provider: LLMProvider }) {
  const guess = (provider.id || provider.name || "").toLowerCase().replace(/[^a-z0-9]/g, "");
  const fallback =
    (provider.name || provider.id || "AI").replace(/[^A-Za-z0-9一-龥]/g, "").slice(0, 2) || "AI";
  return (
    <BrandIcon
      slug={provider.icon_slug || guess}
      imageSrc={provider.icon_type === "image" ? provider.icon_url : undefined}
      fallbackText={fallback.toUpperCase()}
    />
  );
}

function ProvidersList({
  providers,
  routeCountByProvider,
  loading,
  onEdit,
  onDelete,
}: {
  providers: LLMProvider[];
  routeCountByProvider: Map<string, number>;
  loading: boolean;
  onEdit: (provider: LLMProvider) => void;
  onDelete: (provider: LLMProvider) => void;
}) {
  const columns: DataGridColumn<LLMProvider>[] = [
    {
      id: "provider",
      header: "Provider",
      isRowHeader: true,
      minWidth: 280,
      cell: (provider) => (
        <span className="flex min-w-0 items-center gap-2 py-1">
          <ProviderIcon provider={provider} />
          <span className="block min-w-0 flex-1 text-left">
            <AdminTruncatedValue
              className="font-semibold"
              copyLabel="provider name"
              onPress={() => onEdit(provider)}
              value={provider.name || provider.id}
            />
            <AdminTruncatedValue
              className="text-muted font-mono text-xs"
              copyLabel="provider ID"
              value={provider.id}
            />
          </span>
        </span>
      ),
    },
    {
      id: "api",
      header: "Upstream API",
      minWidth: 160,
      cell: (provider) => (
        <Chip size="sm" variant="soft">
          {providerTypeMeta(provider.type).shortLabel}
        </Chip>
      ),
    },
    {
      id: "endpoint",
      header: "Endpoint",
      minWidth: 260,
      cell: (provider) => (
        <AdminTruncatedValue
          className="text-muted max-w-64 font-mono text-xs"
          copyLabel="provider endpoint"
          value={provider.base_url || "—"}
        />
      ),
    },
    {
      id: "credential",
      header: "Credential",
      minWidth: 150,
      cell: (provider) => (
        <span className="text-muted font-mono text-xs">{provider.api_key_hint || "—"}</span>
      ),
    },
    {
      id: "models",
      header: "Models",
      width: 100,
      cell: (provider) => (
        <Chip size="sm" variant="soft">
          {routeCountByProvider.get(provider.id) ?? 0}
        </Chip>
      ),
    },
    {
      id: "actions",
      header: "Actions",
      align: "center",
      width: 80,
      cell: (provider) => (
        <ResourceMenu onEdit={() => onEdit(provider)} onDelete={() => onDelete(provider)} />
      ),
    },
  ];
  return (
    <AdminDataGrid
      aria-label="Model providers"
      columns={columns}
      contentClassName="min-w-[980px]"
      data={providers}
      getRowId={(provider) => provider.id}
      selectionMode="none"
      variant="primary"
      renderEmptyState={() => (
        <EmptyState>
          <EmptyState.Header>
            <EmptyState.Media variant="icon">
              <Boxes className="text-blue-500" />
            </EmptyState.Media>
            <EmptyState.Title>
              {loading ? "Loading providers" : "No providers connected"}
            </EmptyState.Title>
            <EmptyState.Description>
              Connect the API endpoint that will serve your first model.
            </EmptyState.Description>
          </EmptyState.Header>
        </EmptyState>
      )}
    />
  );
}

function ResourceMenu({
  onEdit,
  onDefault,
  onDelete,
  disabled = false,
}: {
  onEdit: () => void;
  onDefault?: () => void;
  onDelete: () => void;
  disabled?: boolean;
}) {
  return (
    <Dropdown>
      <Dropdown.Trigger
        aria-label="Open actions"
        className="text-muted hover:bg-surface-secondary mx-auto grid size-9 place-items-center rounded-xl"
        isDisabled={disabled}
      >
        <MoreHorizontal className="size-4" />
      </Dropdown.Trigger>
      <Dropdown.Popover placement="bottom end">
        <Dropdown.Menu
          aria-label="Resource actions"
          onAction={(key) => {
            if (key === "edit") onEdit();
            if (key === "default") onDefault?.();
            if (key === "delete") onDelete();
          }}
        >
          <Dropdown.Item id="edit" textValue="Edit">
            Edit
          </Dropdown.Item>
          {onDefault ? (
            <Dropdown.Item id="default" textValue="Set as protocol default">
              <Star className="size-4" />
              Set as protocol default
            </Dropdown.Item>
          ) : null}
          <Dropdown.Item id="delete" textValue="Delete">
            <Trash2 className="text-danger size-4" />
            <span className="text-danger">Delete</span>
          </Dropdown.Item>
        </Dropdown.Menu>
      </Dropdown.Popover>
    </Dropdown>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="grid gap-1.5 text-sm font-medium text-foreground">
      <span className="flex flex-wrap items-baseline justify-between gap-2">
        <span>{label}</span>
        {hint ? <span className="text-xs font-normal text-muted">{hint}</span> : null}
      </span>
      {children}
    </label>
  );
}

function FormGroup({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <div className="grid gap-1.5 text-sm font-medium text-foreground">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span>{label}</span>
        {hint ? <span className="text-xs font-normal text-muted">{hint}</span> : null}
      </div>
      {children}
    </div>
  );
}

function Toggle({
  checked,
  onChange,
  label,
  disabled = false,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: string;
  disabled?: boolean;
}) {
  return (
    <Switch isDisabled={disabled} isSelected={checked} onChange={onChange}>
      <Switch.Content>
        <Switch.Control>
          <Switch.Thumb />
        </Switch.Control>
        {label}
      </Switch.Content>
    </Switch>
  );
}

function DrawerFooter({
  saving,
  saveLabel,
  onCancel,
  onSave,
}: {
  saving: boolean;
  saveLabel: string;
  onCancel: () => void;
  onSave: () => void;
}) {
  return (
    <div className="flex items-center justify-end gap-2">
      <Button variant="outline" isDisabled={saving} onPress={onCancel}>
        Cancel
      </Button>
      <Button isDisabled={saving} onPress={onSave}>
        {saving ? "Saving…" : saveLabel}
      </Button>
    </div>
  );
}

function ProviderProtocolBadge({ type }: { type: ProviderType }) {
  const meta = providerTypeMeta(type);
  const embeddings = type === "openai_embeddings";
  return (
    <span
      className={cn(
        "inline-flex w-fit items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium",
        embeddings
          ? "border-emerald-500/25 bg-emerald-500/10 text-emerald-700"
          : "border-blue-500/25 bg-blue-500/10 text-blue-700",
      )}
      title={meta.label}
    >
      {embeddings ? <Binary className="size-3.5" /> : <Route className="size-3.5" />}
      {meta.shortLabel}
    </span>
  );
}

function providerTypeMeta(type: ProviderType) {
  if (type === "openai_embeddings") {
    return {
      value: type,
      label: "OpenAI Embeddings API",
      shortLabel: "Embeddings",
      description: "OpenAI-compatible vector embeddings.",
      defaultBaseURL: "https://api.openai.com/v1",
      defaultIconSlug: "openai",
    };
  }
  return {
    value: type,
    label: "Anthropic Messages API",
    shortLabel: "Anthropic Messages",
    description: "Native Anthropic messages and tool events for Claude Code.",
    defaultBaseURL: "https://api.anthropic.com",
    defaultIconSlug: "anthropic",
  };
}

function protocolForType(type: ProviderType): ModelProtocol {
  if (type === "openai_embeddings") return "openai-embeddings";
  return "anthropic-messages";
}

function runtimeForProtocol(protocol: ModelProtocol) {
  if (protocol === "openai-embeddings") return "Platform services";
  return "Claude Code";
}

function runtimeCompatibilityForType(type: ProviderType) {
  if (type === "openai_embeddings") return "Platform services";
  return "Claude Code";
}

function providerEndpoint(baseURL: string, type: ProviderType) {
  const base = baseURL.trim().replace(/\/$/, "") || providerTypeMeta(type).defaultBaseURL;
  if (type === "anthropic") return `${base}/v1/messages`;
  return `${base}/embeddings`;
}

function embeddingEndpoint(baseURL: string) {
  const base = baseURL
    .trim()
    .replace(/\/+$/, "")
    .replace(/\/embeddings$/, "");
  return base ? `${base}/embeddings` : "";
}

function providerIDFromName(name: string) {
  return name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function ModelIcon({ model }: { model: LLMModel }) {
  if (model.icon_type === "image" && model.icon_url) {
    return (
      <BrandIcon
        imageSrc={model.icon_url}
        fallbackText={(model.label || model.alias).slice(0, 2).toUpperCase() || "AI"}
      />
    );
  }
  return (
    <BrandIcon
      slug={model.icon_slug}
      fallbackText={
        SIMPLE_ICON_FALLBACK_BADGES[model.icon_slug.toLowerCase()] ||
        (model.label || model.alias).slice(0, 2).toUpperCase() ||
        "AI"
      }
    />
  );
}

async function errorText(response: Response) {
  const body = await response.text();
  if (!body) return `${response.status} ${response.statusText}`;
  try {
    const parsed = JSON.parse(body) as { error?: { message?: string } | string };
    if (typeof parsed.error === "string") return parsed.error;
    if (parsed.error?.message) return parsed.error.message;
  } catch {
    // Fall through to the safe response body returned by Admin API.
  }
  return body;
}

async function uploadModelIcon(file: File) {
  const form = new FormData();
  form.set("file", file);
  const response = await fetch("/api/admin/model-icons", { method: "POST", body: form });
  if (!response.ok) throw new Error(await errorText(response));
  const asset = (await response.json()) as { src?: string };
  if (!asset.src) throw new Error("The uploaded image did not return a usable icon.");
  return asset.src;
}
