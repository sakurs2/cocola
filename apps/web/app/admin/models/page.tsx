"use client";

import { Cpu as ModelsPageIcon } from "lucide-react";
import {
  Binary,
  Bot,
  Boxes,
  Check,
  CircleCheck,
  CircleCheckBig,
  KeyRound,
  LoaderCircle,
  MoreHorizontal,
  PlugZap,
  Plus,
  RefreshCw,
  Route,
  Search,
  Star,
  Trash2,
} from "lucide-react";
import Image from "next/image";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { AdminAlert, AdminConfirmDialog, AdminDrawer } from "@/components/admin/admin-ui";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
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

type ProviderType = "anthropic" | "openai_responses" | "openai_embeddings";
type ConfigurableProviderType = Exclude<ProviderType, "openai_embeddings">;
type ModelProtocol = "anthropic-messages" | "openai-responses" | "openai-embeddings";
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
  type: ConfigurableProviderType;
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

const PROVIDER_TYPES: Array<{
  value: ConfigurableProviderType;
  label: string;
  shortLabel: string;
  description: string;
  defaultBaseURL: string;
  defaultIconSlug: string;
}> = [
  {
    value: "anthropic",
    label: "Anthropic Messages API",
    shortLabel: "Anthropic Messages",
    description: "Native Anthropic messages and tool events for Claude Code.",
    defaultBaseURL: "https://api.anthropic.com",
    defaultIconSlug: "anthropic",
  },
  {
    value: "openai_responses",
    label: "OpenAI Responses API",
    shortLabel: "Responses API",
    description: "Structured /responses requests and events required by Codex.",
    defaultBaseURL: "https://api.openai.com/v1",
    defaultIconSlug: "openai",
  },
];

const inputClass =
  "h-10 w-full min-w-0 rounded-xl border border-input bg-background px-3 text-sm text-foreground outline-none transition disabled:cursor-not-allowed disabled:bg-muted/50 disabled:text-muted-foreground";

export default function AdminModelsPage() {
  const [providers, setProviders] = useState<LLMProvider[]>([]);
  const [models, setModels] = useState<LLMModel[]>([]);
  const [view, setView] = useState<View>("models");
  const [query, setQuery] = useState("");
  const [providerForm, setProviderForm] = useState<ProviderForm>(EMPTY_PROVIDER);
  const [modelForm, setModelForm] = useState<ModelForm>(EMPTY_MODEL);
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
    setFormError("");
    setProviderDrawerOpen(true);
  }

  function createModel() {
    const firstChatProvider = providers.find((provider) => provider.type !== "openai_embeddings");
    setEditingModel(null);
    setModelKind("chat");
    setModelForm({ ...EMPTY_MODEL, provider_id: firstChatProvider?.id ?? "" });
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
    setFormError("");
    setModelDrawerOpen(true);
  }

  async function saveProvider() {
    setSaving(true);
    setFormError("");
    try {
      const body: Record<string, unknown> = {
        id: providerForm.id.trim() || providerIDFromName(providerForm.name),
        name: providerForm.name,
        type: providerForm.type,
        base_url: providerForm.base_url,
        icon_type: providerForm.icon_type,
        icon_slug: providerForm.icon_slug,
        icon_url: providerForm.icon_url,
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
      const body = {
        alias: modelForm.alias,
        provider_id: modelForm.provider_id,
        real_model: modelForm.real_model,
        label: modelForm.label,
        icon_type: modelForm.icon_type,
        icon_slug: modelForm.icon_slug,
        icon_url: modelForm.icon_url,
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
  const editingProviderHasRoutes = editingProvider
    ? (routeCountByProvider.get(editingProvider) ?? 0) > 0
    : false;

  return (
    <main className="admin-theme-violet min-h-screen bg-background text-foreground">
      <div className="mx-auto w-full max-w-[100rem] space-y-6 px-4 py-5 sm:px-6 sm:py-6">
        <div className="admin-models-head">
          <span className="admin-models-icon">
            <ModelsPageIcon className="size-[22px]" />
          </span>
          <div>
            <div className="admin-models-title">Models</div>
            <div className="admin-models-sub">
              Connect model providers and decide which routes are available to each Agent Runtime.
            </div>
          </div>
          <div className="flex-1" />
          <button
            type="button"
            className="admin-user-refresh"
            disabled={loading}
            onClick={() => {
              setRefreshTick((tick) => tick + 1);
              void load();
            }}
          >
            <RefreshCw key={refreshTick} className="admin-refresh-icon size-4" />
            Refresh
          </button>
        </div>

        {error ? <AdminAlert tone="error">{error}</AdminAlert> : null}

        <section className="grid gap-4 md:grid-cols-4">
          <MetricCard
            tone="violet"
            label="Model routes"
            value={models.length}
            icon={<Route className="size-[22px]" />}
          />
          <MetricCard
            tone="blue"
            label="Providers"
            value={providers.filter((provider) => provider.type !== "openai_embeddings").length}
            icon={<Boxes className="ic-boxes size-[22px]" />}
          />
          <MetricCard
            tone="green"
            label="Enabled"
            value={models.filter((model) => model.enabled).length}
            icon={<CircleCheckBig className="size-[22px]" />}
          />
          <MetricCard
            tone="amber"
            label="Default set"
            value={models.filter((model) => model.is_default).length}
            icon={<Star className="size-[22px]" />}
          />
        </section>

        <div className="admin-models-tabbar">
          <div className="admin-models-tabs" role="tablist" aria-label="Model configuration">
            <button
              type="button"
              role="tab"
              aria-selected={view === "models"}
              className={cn("admin-models-tab", view === "models" && "is-active")}
              onClick={() => {
                setView("models");
                setQuery("");
              }}
            >
              <Route className="size-[15px]" />
              <span>Model routes</span>
              <span className="admin-models-count">{models.length}</span>
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={view === "providers"}
              className={cn("admin-models-tab", view === "providers" && "is-active")}
              onClick={() => {
                setView("providers");
                setQuery("");
              }}
            >
              <Boxes className="ic-boxes size-[15px]" />
              <span>Providers</span>
              <span className="admin-models-count">{visibleProviders.length}</span>
            </button>
          </div>
          <Button
            className="admin-primary-btn gap-2"
            onClick={view === "models" ? createModel : createProvider}
          >
            <Plus className="size-4" />
            {view === "models" ? "Add model" : "Add provider"}
          </Button>
        </div>

        <div className="admin-user-toolbar">
          <label className="admin-models-search">
            <Search className="size-4" />
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={view === "models" ? "Find a model route" : "Find a provider"}
            />
          </label>
        </div>

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
          description="Choose the wire protocol the upstream actually implements."
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
            <FormGroup
              label="Protocol"
              hint={
                editingProviderHasRoutes
                  ? "Remove its model routes before changing protocol."
                  : undefined
              }
            >
              <div className="grid gap-2">
                {PROVIDER_TYPES.map((type) => (
                  <button
                    key={type.value}
                    type="button"
                    disabled={editingProviderHasRoutes}
                    onClick={() =>
                      setProviderForm((current) => ({
                        ...current,
                        type: type.value,
                        base_url:
                          !current.base_url ||
                          PROVIDER_TYPES.some((item) => item.defaultBaseURL === current.base_url)
                            ? type.defaultBaseURL
                            : current.base_url,
                        icon_slug:
                          current.icon_type === "simple-icons" &&
                          (!current.icon_slug ||
                            PROVIDER_TYPES.some(
                              (item) => item.defaultIconSlug === current.icon_slug,
                            ))
                            ? type.defaultIconSlug
                            : current.icon_slug,
                      }))
                    }
                    className={cn(
                      "rounded-2xl border p-3 text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30 disabled:cursor-not-allowed disabled:opacity-65",
                      providerForm.type === type.value
                        ? "border-primary/45 bg-primary/5 shadow-sm"
                        : "border-border bg-background hover:border-primary/25 hover:bg-muted/25",
                    )}
                  >
                    <span className="flex items-center justify-between gap-3">
                      <span className="text-sm font-semibold">{type.label}</span>
                      {providerForm.type === type.value ? (
                        <Check className="size-4 text-primary" />
                      ) : null}
                    </span>
                    <span className="mt-1 block text-xs leading-5 text-muted-foreground">
                      {type.description}
                    </span>
                  </button>
                ))}
              </div>
            </FormGroup>

            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Name">
                <input
                  className={inputClass}
                  value={providerForm.name}
                  onChange={(event) =>
                    setProviderForm({ ...providerForm, name: event.target.value })
                  }
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

            <Field label="Base URL">
              <input
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
              <p className="mt-2 text-xs leading-5 text-muted-foreground">
                {providerForm.type === "openai_responses"
                  ? "The upstream must implement POST /responses with Codex-compatible tool events."
                  : "This route uses the native Anthropic Messages API."}
              </p>
            </div>

            <Field
              label="API key"
              hint={editingProvider ? "Leave blank to keep the current key." : undefined}
            >
              <div className="flex items-center gap-2 rounded-xl border border-input bg-background px-3">
                <KeyRound className="size-4 shrink-0 text-muted-foreground" />
                <input
                  className="h-10 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
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

            <details
              className="group rounded-2xl border border-border/70 p-3"
              open={!editingProvider}
            >
              <summary className="cursor-pointer list-none text-sm font-medium [&::-webkit-details-marker]:hidden">
                Appearance
              </summary>
              <div className="mt-4 grid gap-4 border-t border-border/70 pt-4 sm:grid-cols-2">
                <Field label="Icon source">
                  <SelectControl
                    className={inputClass}
                    value={providerForm.icon_type}
                    onValueChange={(value) =>
                      setProviderForm({
                        ...providerForm,
                        icon_type: value as ProviderForm["icon_type"],
                      })
                    }
                    options={[
                      { value: "simple-icons", label: "Brand icon" },
                      { value: "image", label: "Image URL" },
                    ]}
                    contentClassName="cocola-admin-ui"
                  />
                </Field>
                {providerForm.icon_type === "image" ? (
                  <Field label="Image URL">
                    <input
                      className={inputClass}
                      value={providerForm.icon_url}
                      onChange={(event) =>
                        setProviderForm({ ...providerForm, icon_url: event.target.value })
                      }
                      placeholder="https://..."
                    />
                  </Field>
                ) : (
                  <Field label="Brand">
                    <SelectControl
                      className={inputClass}
                      value={providerForm.icon_slug}
                      onValueChange={(value) =>
                        setProviderForm({ ...providerForm, icon_slug: value })
                      }
                      options={SIMPLE_ICON_SLUGS.map((slug) => ({
                        value: slug,
                        label: SIMPLE_ICON_LABELS[slug] ?? slug,
                        icon: <BrandGlyph slug={slug} />,
                      }))}
                      contentClassName="cocola-admin-ui"
                    />
                  </Field>
                )}
              </div>
            </details>

            <details className="group rounded-2xl border border-border/70 p-3">
              <summary className="cursor-pointer list-none text-sm font-medium [&::-webkit-details-marker]:hidden">
                Advanced
              </summary>
              <div className="mt-4 border-t border-border/70 pt-4">
                <Field
                  label="Provider ID"
                  hint="Generated from the provider name when left blank; it cannot be changed later."
                >
                  <input
                    className={cn(inputClass, "font-mono text-xs")}
                    value={providerForm.id}
                    disabled={Boolean(editingProvider)}
                    onChange={(event) =>
                      setProviderForm({ ...providerForm, id: event.target.value })
                    }
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
                <div className="grid gap-2 sm:grid-cols-2">
                  <button
                    type="button"
                    onClick={() => setModelKind("chat")}
                    className={cn(
                      "rounded-2xl border p-3 text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30",
                      modelKind === "chat"
                        ? "border-primary/45 bg-primary/5 shadow-sm"
                        : "border-border bg-background hover:border-primary/25 hover:bg-muted/25",
                    )}
                  >
                    <span className="flex items-center justify-between gap-3 text-sm font-semibold">
                      Chat model{" "}
                      {modelKind === "chat" ? <Check className="size-4 text-primary" /> : null}
                    </span>
                    <span className="mt-1 block text-xs leading-5 text-muted-foreground">
                      Used directly by Agent Runtimes.
                    </span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setModelKind("embedding")}
                    className={cn(
                      "rounded-2xl border p-3 text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30",
                      modelKind === "embedding"
                        ? "border-primary/45 bg-primary/5 shadow-sm"
                        : "border-border bg-background hover:border-primary/25 hover:bg-muted/25",
                    )}
                  >
                    <span className="flex items-center justify-between gap-3 text-sm font-semibold">
                      Embedding model
                      {modelKind === "embedding" ? <Check className="size-4 text-primary" /> : null}
                    </span>
                    <span className="mt-1 block text-xs leading-5 text-muted-foreground">
                      Shared by Memory and knowledge features; never shown to users.
                    </span>
                  </button>
                </div>
              </FormGroup>
            ) : null}

            {modelKind === "embedding" ? (
              <div className="grid gap-5">
                <Field label="Model name">
                  <input
                    className={cn(inputClass, "font-mono text-xs")}
                    value={embeddingForm.model}
                    onChange={(event) => {
                      setEmbeddingForm({ ...embeddingForm, model: event.target.value });
                      setEmbeddingTest(null);
                    }}
                    placeholder="text-embedding-3-large"
                  />
                </Field>

                <Field label="Base URL">
                  <input
                    className={cn(inputClass, "font-mono text-xs")}
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
                  <div className="flex items-center gap-2 rounded-xl border border-input bg-background px-3">
                    <KeyRound className="size-4 shrink-0 text-muted-foreground" />
                    <input
                      className="h-10 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
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

                <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-border/70 bg-muted/20 p-3">
                  <div className="min-w-0">
                    <div className="text-xs font-medium text-foreground">OpenAI Embeddings</div>
                    <code className="mt-1 block break-all text-[11px] text-muted-foreground">
                      {embeddingEndpoint(embeddingForm.base_url)}
                    </code>
                  </div>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={!canTestEmbedding || testingEmbedding || saving}
                    onClick={() => void testEmbeddingConnection()}
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
                    onValueChange={(value) => {
                      setModelForm({
                        ...modelForm,
                        provider_id: value,
                      });
                    }}
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
                  <div className="flex flex-wrap items-center gap-2 rounded-2xl border border-border/70 bg-muted/25 p-3">
                    <ProviderProtocolBadge type={selectedProvider.type} />
                    <span className="text-xs text-muted-foreground">
                      Compatible with {runtimeCompatibilityForType(selectedProvider.type)}
                    </span>
                  </div>
                ) : null}

                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="Display name">
                    <input
                      className={inputClass}
                      value={modelForm.label}
                      onChange={(event) =>
                        setModelForm({ ...modelForm, label: event.target.value })
                      }
                      placeholder="GPT-5"
                    />
                  </Field>
                  <Field label="Alias" hint="Unique only inside the selected provider.">
                    <input
                      className={cn(inputClass, "font-mono text-xs")}
                      value={modelForm.alias}
                      disabled={Boolean(editingModel)}
                      onChange={(event) =>
                        setModelForm({ ...modelForm, alias: event.target.value })
                      }
                      placeholder="gpt-5"
                    />
                  </Field>
                </div>

                <Field label="Upstream model ID">
                  <input
                    className={cn(inputClass, "font-mono text-xs")}
                    value={modelForm.real_model}
                    onChange={(event) =>
                      setModelForm({ ...modelForm, real_model: event.target.value })
                    }
                    placeholder="gpt-5"
                  />
                </Field>

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
                  <summary className="cursor-pointer list-none text-sm font-medium [&::-webkit-details-marker]:hidden">
                    Appearance and order
                  </summary>
                  <div className="mt-4 grid gap-4 border-t border-border/70 pt-4 sm:grid-cols-2">
                    <Field label="Icon source">
                      <SelectControl
                        className={inputClass}
                        value={modelForm.icon_type}
                        onValueChange={(value) =>
                          setModelForm({
                            ...modelForm,
                            icon_type: value as ModelForm["icon_type"],
                          })
                        }
                        options={[
                          { value: "simple-icons", label: "Brand icon" },
                          { value: "image", label: "Image URL" },
                        ]}
                        contentClassName="cocola-admin-ui"
                      />
                    </Field>
                    {modelForm.icon_type === "image" ? (
                      <Field label="Image URL">
                        <input
                          className={inputClass}
                          value={modelForm.icon_url}
                          onChange={(event) =>
                            setModelForm({ ...modelForm, icon_url: event.target.value })
                          }
                          placeholder="https://..."
                        />
                      </Field>
                    ) : (
                      <Field label="Brand">
                        <SelectControl
                          className={inputClass}
                          value={modelForm.icon_slug}
                          onValueChange={(value) =>
                            setModelForm({ ...modelForm, icon_slug: value })
                          }
                          options={SIMPLE_ICON_SLUGS.map((slug) => ({
                            value: slug,
                            label: SIMPLE_ICON_LABELS[slug] ?? slug,
                            icon: <BrandGlyph slug={slug} />,
                          }))}
                          contentClassName="cocola-admin-ui"
                        />
                      </Field>
                    )}
                    <Field label="Display priority" hint="Lower numbers appear first.">
                      <input
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
      </div>
    </main>
  );
}

function MetricCard({
  tone,
  label,
  value,
  icon,
}: {
  tone: "blue" | "green" | "violet" | "amber";
  label: string;
  value: number;
  icon: ReactNode;
}) {
  return (
    <div className="admin-metric-card" data-tone={tone}>
      <div className="admin-metric-head">
        <span className="admin-metric-glyph">{icon}</span>
        <span className="admin-metric-key">{label}</span>
      </div>
      <div className="admin-metric-val">{value}</div>
    </div>
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

function protoChipLabel(type: ProviderType | undefined, protocol: ModelProtocol) {
  if (type) return providerTypeMeta(type).shortLabel;
  if (protocol === "openai-embeddings") return "Embeddings";
  return protocol === "openai-responses" ? "Responses API" : "Anthropic Messages";
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
  if (!loading && models.length === 0) {
    return (
      <div className="admin-user-list">
        <div className="admin-user-state">
          No model routes — add one after connecting at least one provider.
        </div>
      </div>
    );
  }
  return (
    <div className="admin-user-list">
      <div className="admin-model-cols">
        <div>Model</div>
        <div>Upstream API</div>
        <div>Provider</div>
        <div>Availability</div>
        <div>Actions</div>
      </div>
      {models.map((model) => {
        const provider = providerByID.get(model.provider_id);
        return (
          <div className="admin-model-row" key={model.id}>
            <button
              type="button"
              onClick={() => onEdit(model)}
              className="admin-user-cell admin-model-namebtn"
            >
              <ModelIcon model={model} />
              <span className="min-w-0">
                <span className="admin-user-name flex items-center gap-1.5">
                  <span className="admin-model-cell-text" title={model.label || model.alias}>
                    {model.label || model.alias}
                  </span>
                  {model.is_default ? (
                    <Star className="size-3.5 shrink-0 fill-[#f5a623] text-[#f5a623]" />
                  ) : null}
                </span>
              </span>
            </button>
            <div className="admin-model-apicell">
              <span
                className="admin-chip admin-chip--proto admin-model-cell-text"
                title={protoChipLabel(provider?.type, model.protocol)}
              >
                {protoChipLabel(provider?.type, model.protocol)}
              </span>
            </div>
            <div>
              <div
                className="admin-model-provname admin-model-cell-text"
                title={provider?.name || model.provider_id}
              >
                {provider?.name || model.provider_id}
              </div>
            </div>
            <div className="admin-model-chipstack">
              <span
                className={cn("admin-chip", model.enabled ? "admin-chip--ok" : "admin-chip--off")}
              >
                {model.enabled ? <span className="admin-chip-dot" /> : null}
                {model.enabled ? "Enabled" : "Disabled"}
              </span>
              <span
                className={cn(
                  "admin-chip",
                  model.visible ? "admin-chip--visible" : "admin-chip--off",
                )}
              >
                {model.visible ? "Visible" : "Hidden"}
              </span>
            </div>
            <div className="admin-user-actions">
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
            </div>
          </div>
        );
      })}
    </div>
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
  if (!loading && providers.length === 0) {
    return (
      <div className="admin-user-list">
        <div className="admin-user-state">
          No providers connected — connect the API endpoint that will serve your first model.
        </div>
      </div>
    );
  }
  return (
    <div className="admin-user-list">
      <div className="admin-prov-cols">
        <div>Provider</div>
        <div>Upstream API</div>
        <div>Endpoint</div>
        <div>Credential</div>
        <div>Models</div>
        <div>Actions</div>
      </div>
      {providers.map((provider) => (
        <div className="admin-prov-row" key={provider.id}>
          <button
            type="button"
            onClick={() => onEdit(provider)}
            className="admin-user-cell admin-model-namebtn"
          >
            <ProviderIcon provider={provider} />
            <span className="min-w-0">
              <span className="admin-user-name block truncate">{provider.name || provider.id}</span>
              <span className="admin-user-sub block font-mono">{provider.id}</span>
            </span>
          </button>
          <div>
            <span className="admin-chip admin-chip--proto">
              {providerTypeMeta(provider.type).shortLabel}
            </span>
          </div>
          <div className="admin-model-endpoint font-mono" title={provider.base_url}>
            {provider.base_url || "—"}
          </div>
          <div className="admin-model-cred font-mono">{provider.api_key_hint || "—"}</div>
          <div className="admin-model-num">{routeCountByProvider.get(provider.id) ?? 0}</div>
          <div className="admin-user-actions">
            <ResourceMenu onEdit={() => onEdit(provider)} onDelete={() => onDelete(provider)} />
          </div>
        </div>
      ))}
    </div>
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
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" disabled={disabled} aria-label="Open actions">
          <MoreHorizontal className="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="cocola-admin-ui admin-actions-menu">
        <DropdownMenuItem onSelect={onEdit}>Edit</DropdownMenuItem>
        {onDefault ? (
          <DropdownMenuItem onSelect={onDefault}>
            <Star className="mr-2 size-4" /> Set as protocol default
          </DropdownMenuItem>
        ) : null}
        <DropdownMenuSeparator />
        <DropdownMenuItem className="text-destructive focus:text-destructive" onSelect={onDelete}>
          <Trash2 className="mr-2 size-4" /> Delete
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="grid gap-1.5 text-sm font-medium text-foreground">
      <span className="flex flex-wrap items-baseline justify-between gap-2">
        <span>{label}</span>
        {hint ? <span className="text-xs font-normal text-muted-foreground">{hint}</span> : null}
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
        {hint ? <span className="text-xs font-normal text-muted-foreground">{hint}</span> : null}
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
    <label
      className={cn(
        "flex min-h-10 items-center gap-2 rounded-xl border border-border/70 px-3 text-sm font-medium",
        disabled ? "cursor-not-allowed opacity-60" : "cursor-pointer",
      )}
    >
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.target.checked)}
      />
      {label}
    </label>
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
      <Button variant="outline" disabled={saving} onClick={onCancel}>
        Cancel
      </Button>
      <Button disabled={saving} onClick={onSave}>
        {saving ? "Saving…" : saveLabel}
      </Button>
    </div>
  );
}

function ProviderProtocolBadge({ type }: { type: ProviderType }) {
  const meta = providerTypeMeta(type);
  const responses = type === "openai_responses";
  const embeddings = type === "openai_embeddings";
  return (
    <span
      className={cn(
        "inline-flex w-fit items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium",
        embeddings
          ? "border-emerald-500/25 bg-emerald-500/10 text-emerald-700"
          : responses
            ? "border-violet-500/25 bg-violet-500/10 text-violet-700"
            : "border-blue-500/25 bg-blue-500/10 text-blue-700",
      )}
      title={meta.label}
    >
      {embeddings ? (
        <Binary className="size-3.5" />
      ) : responses ? (
        <Bot className="size-3.5" />
      ) : (
        <Route className="size-3.5" />
      )}
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
  return PROVIDER_TYPES.find((item) => item.value === type) ?? PROVIDER_TYPES[0]!;
}

function protocolForType(type: ProviderType): ModelProtocol {
  if (type === "openai_responses") return "openai-responses";
  if (type === "openai_embeddings") return "openai-embeddings";
  return "anthropic-messages";
}

function runtimeForProtocol(protocol: ModelProtocol) {
  if (protocol === "openai-embeddings") return "Platform services";
  return protocol === "openai-responses" ? "Codex" : "Claude Code";
}

function runtimeCompatibilityForType(type: ProviderType) {
  if (type === "openai_responses") return "Codex";
  if (type === "openai_embeddings") return "Platform services";
  return "Claude Code";
}

function providerEndpoint(baseURL: string, type: ProviderType) {
  const base = baseURL.trim().replace(/\/$/, "") || providerTypeMeta(type).defaultBaseURL;
  if (type === "anthropic") return `${base}/v1/messages`;
  if (type === "openai_embeddings") return `${base}/embeddings`;
  return `${base}/responses`;
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
