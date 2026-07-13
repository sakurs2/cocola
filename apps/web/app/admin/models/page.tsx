"use client";

import { Cpu as ModelsPageIcon } from "@phosphor-icons/react";
import {
  Bot,
  Boxes,
  Check,
  KeyRound,
  MoreHorizontal,
  Plus,
  Route,
  Search,
  Star,
  Trash2,
} from "lucide-react";
import Image from "next/image";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import {
  AdminAlert,
  AdminConfirmDialog,
  AdminDrawer,
  AdminEmptyState,
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
  AdminStatusBadge,
  AdminTable,
  AdminToolbar,
} from "@/components/admin/admin-ui";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  LOCAL_SIMPLE_ICON_PATHS,
  SIMPLE_ICON_FALLBACK_BADGES,
  SIMPLE_ICON_LABELS,
  SIMPLE_ICON_SLUGS,
} from "@/lib/model-icons";
import { cn } from "@/lib/utils";

type ProviderType = "anthropic" | "openai_compat" | "openai_responses";
type ModelProtocol = "anthropic-messages" | "openai-responses";
type View = "models" | "providers";

type LLMProvider = {
  id: string;
  name: string;
  type: ProviderType;
  base_url: string;
  api_key_hint: string;
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
  icon_type: "simple-icons" | "image";
  icon_slug: string;
  icon_url: string;
  enabled: boolean;
  visible: boolean;
  is_default: boolean;
  sort_order: number;
  created_at: string;
  updated_at: string;
};

type ProviderForm = {
  id: string;
  name: string;
  type: ProviderType;
  base_url: string;
  api_key: string;
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

type DeleteTarget =
  | { kind: "model"; id: string; name: string }
  | { kind: "provider"; id: string; name: string };

const EMPTY_PROVIDER: ProviderForm = {
  id: "",
  name: "",
  type: "anthropic",
  base_url: "https://api.anthropic.com",
  api_key: "",
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

const PROVIDER_TYPES: Array<{
  value: ProviderType;
  label: string;
  shortLabel: string;
  description: string;
  defaultBaseURL: string;
}> = [
  {
    value: "anthropic",
    label: "Anthropic Messages API",
    shortLabel: "Anthropic Messages",
    description: "Native Anthropic messages and tool events for Claude Code.",
    defaultBaseURL: "https://api.anthropic.com",
  },
  {
    value: "openai_compat",
    label: "OpenAI Chat Completions",
    shortLabel: "Chat Completions",
    description:
      "OpenAI-compatible /chat/completions adapter. Responses API compatibility is separate.",
    defaultBaseURL: "https://api.openai.com/v1",
  },
  {
    value: "openai_responses",
    label: "OpenAI Responses API",
    shortLabel: "Responses API",
    description: "Structured /responses requests and events required by Codex.",
    defaultBaseURL: "https://api.openai.com/v1",
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
  const [editingProvider, setEditingProvider] = useState<string | null>(null);
  const [editingModel, setEditingModel] = useState<string | null>(null);
  const [providerDrawerOpen, setProviderDrawerOpen] = useState(false);
  const [modelDrawerOpen, setModelDrawerOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [formError, setFormError] = useState("");

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
    if (!needle) return providers;
    return providers.filter((provider) =>
      [provider.name, provider.id, provider.base_url, providerTypeMeta(provider.type).label]
        .join(" ")
        .toLowerCase()
        .includes(needle),
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
    setEditingProvider(provider.id);
    setProviderForm({
      id: provider.id,
      name: provider.name,
      type: provider.type,
      base_url: provider.base_url,
      api_key: "",
      enabled: provider.enabled,
    });
    setFormError("");
    setProviderDrawerOpen(true);
  }

  function createModel() {
    if (providers.length === 0) {
      createProvider();
      return;
    }
    setEditingModel(null);
    setModelForm({ ...EMPTY_MODEL, provider_id: providers[0]?.id ?? "" });
    setFormError("");
    setModelDrawerOpen(true);
  }

  function editModel(model: LLMModel) {
    setEditingModel(model.id);
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
  const editingProviderHasRoutes = editingProvider
    ? (routeCountByProvider.get(editingProvider) ?? 0) > 0
    : false;

  return (
    <AdminPage>
      <AdminPageHeader
        eyebrow="Intelligence"
        title="Models"
        description="Connect model providers and decide which routes are available to each Agent Runtime."
        icon={<ModelsPageIcon className="size-5" weight="duotone" />}
        actions={
          <AdminRefreshButton
            variant="outline"
            refreshing={loading}
            disabled={loading}
            onClick={() => void load()}
          >
            Refresh
          </AdminRefreshButton>
        }
      />

      {error ? <AdminAlert tone="error">{error}</AdminAlert> : null}

      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border/70">
        <div className="flex items-center gap-1" role="tablist" aria-label="Model configuration">
          <ViewTab
            active={view === "models"}
            onClick={() => {
              setView("models");
              setQuery("");
            }}
          >
            Model routes <Count>{models.length}</Count>
          </ViewTab>
          <ViewTab
            active={view === "providers"}
            onClick={() => {
              setView("providers");
              setQuery("");
            }}
          >
            Providers <Count>{providers.length}</Count>
          </ViewTab>
        </div>
        <Button className="mb-2 gap-2" onClick={view === "models" ? createModel : createProvider}>
          <Plus className="size-4" />
          {view === "models" ? "Add model" : "Add provider"}
        </Button>
      </div>

      <AdminToolbar>
        <label className="flex min-w-0 flex-1 items-center gap-2 rounded-xl border border-input bg-background px-3 sm:max-w-md">
          <Search className="size-4 shrink-0 text-muted-foreground" />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={view === "models" ? "Find a model route" : "Find a provider"}
            className="h-10 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
        </label>
      </AdminToolbar>

      {view === "models" ? (
        <ModelsTable
          models={visibleModels}
          providerByID={providerByID}
          loading={loading}
          saving={saving}
          onAdd={createModel}
          onEdit={editModel}
          onDefault={(model) => void setDefault(model)}
          onDelete={(model) =>
            setDeleteTarget({ kind: "model", id: model.id, name: model.label || model.alias })
          }
        />
      ) : (
        <ProvidersTable
          providers={visibleProviders}
          routeCountByProvider={routeCountByProvider}
          loading={loading}
          onAdd={createProvider}
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
                onChange={(event) => setProviderForm({ ...providerForm, name: event.target.value })}
                placeholder="OpenAI production"
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

          <div className="rounded-2xl border border-blue-500/20 bg-blue-500/[0.06] p-3">
            <div className="text-[11px] font-semibold uppercase tracking-[0.12em] text-blue-700/75">
              Request path
            </div>
            <code className="mt-1 block break-all text-xs text-foreground">
              {providerEndpoint(providerForm.base_url, providerForm.type)}
            </code>
            <p className="mt-2 text-xs leading-5 text-muted-foreground">
              {providerForm.type === "openai_responses"
                ? "The upstream must implement POST /responses. Chat Completions compatibility is not sufficient."
                : providerForm.type === "openai_compat"
                  ? "This route uses /chat/completions; it does not imply Responses API support."
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
        title={editingModel ? "Edit model route" : "Add model route"}
        description="Connect a user-visible model to one provider and upstream model ID."
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
          <Field label="Provider">
            <select
              className={inputClass}
              value={modelForm.provider_id}
              onChange={(event) => setModelForm({ ...modelForm, provider_id: event.target.value })}
            >
              <option value="">Select provider</option>
              {providers
                .filter((provider) => {
                  if (!editingModel) return true;
                  const original = models.find((model) => model.id === editingModel);
                  return !original || protocolForType(provider.type) === original.protocol;
                })
                .map((provider) => (
                  <option key={provider.id} value={provider.id}>
                    {provider.name || provider.id} · {providerTypeMeta(provider.type).shortLabel}
                  </option>
                ))}
            </select>
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
                onChange={(event) => setModelForm({ ...modelForm, label: event.target.value })}
                placeholder="GPT-5"
              />
            </Field>
            <Field label="Alias" hint="Unique only inside the selected provider.">
              <input
                className={cn(inputClass, "font-mono text-xs")}
                value={modelForm.alias}
                disabled={Boolean(editingModel)}
                onChange={(event) => setModelForm({ ...modelForm, alias: event.target.value })}
                placeholder="gpt-5"
              />
            </Field>
          </div>

          <Field label="Upstream model ID">
            <input
              className={cn(inputClass, "font-mono text-xs")}
              value={modelForm.real_model}
              onChange={(event) => setModelForm({ ...modelForm, real_model: event.target.value })}
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
                <select
                  className={inputClass}
                  value={modelForm.icon_type}
                  onChange={(event) =>
                    setModelForm({
                      ...modelForm,
                      icon_type: event.target.value as ModelForm["icon_type"],
                    })
                  }
                >
                  <option value="simple-icons">Brand icon</option>
                  <option value="image">Image URL</option>
                </select>
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
                  <select
                    className={inputClass}
                    value={modelForm.icon_slug}
                    onChange={(event) =>
                      setModelForm({ ...modelForm, icon_slug: event.target.value })
                    }
                  >
                    {SIMPLE_ICON_SLUGS.map((slug) => (
                      <option key={slug} value={slug}>
                        {SIMPLE_ICON_LABELS[slug] ?? slug}
                      </option>
                    ))}
                  </select>
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

function ModelsTable({
  models,
  providerByID,
  loading,
  saving,
  onAdd,
  onEdit,
  onDefault,
  onDelete,
}: {
  models: LLMModel[];
  providerByID: Map<string, LLMProvider>;
  loading: boolean;
  saving: boolean;
  onAdd: () => void;
  onEdit: (model: LLMModel) => void;
  onDefault: (model: LLMModel) => void;
  onDelete: (model: LLMModel) => void;
}) {
  if (!loading && models.length === 0) {
    return (
      <AdminTable>
        <AdminEmptyState
          icon={<Route className="size-5" />}
          title="No model routes"
          description="Add a model route after connecting at least one provider."
          action={<Button onClick={onAdd}>Add model</Button>}
        />
      </AdminTable>
    );
  }
  return (
    <AdminTable>
      <table className="w-full min-w-[880px] text-sm">
        <thead className="border-b border-border/70 bg-muted/35 text-left text-xs text-muted-foreground">
          <tr>
            <Th>Model</Th>
            <Th>Upstream API</Th>
            <Th>Provider</Th>
            <Th>Upstream model</Th>
            <Th>Availability</Th>
            <Th>
              <span className="sr-only">Actions</span>
            </Th>
          </tr>
        </thead>
        <tbody>
          {models.map((model) => {
            const provider = providerByID.get(model.provider_id);
            return (
              <tr
                key={model.id}
                className="border-b border-border/60 last:border-0 hover:bg-muted/20"
              >
                <Td>
                  <button
                    type="button"
                    onClick={() => onEdit(model)}
                    className="flex max-w-xs items-center gap-3 text-left"
                  >
                    <ModelIcon model={model} />
                    <span className="min-w-0">
                      <span className="flex items-center gap-1.5 font-medium text-foreground">
                        <span className="truncate">{model.label || model.alias}</span>
                        {model.is_default ? (
                          <Star className="size-3.5 fill-primary text-primary" />
                        ) : null}
                      </span>
                      <code className="block truncate text-[11px] text-muted-foreground">
                        {model.alias}
                      </code>
                    </span>
                  </button>
                </Td>
                <Td>
                  <div className="grid gap-1">
                    {provider ? (
                      <ProviderProtocolBadge type={provider.type} />
                    ) : (
                      <RuntimeProtocolBadge protocol={model.protocol} />
                    )}
                    <span className="text-[11px] text-muted-foreground">
                      {provider
                        ? runtimeCompatibilityForType(provider.type)
                        : runtimeForProtocol(model.protocol)}
                    </span>
                  </div>
                </Td>
                <Td>
                  <div className="font-medium">{provider?.name || model.provider_id}</div>
                  <code className="text-[11px] text-muted-foreground">{model.provider_id}</code>
                </Td>
                <Td>
                  <code className="text-xs text-muted-foreground">{model.real_model}</code>
                </Td>
                <Td>
                  <div className="flex flex-wrap gap-1.5">
                    <AdminStatusBadge tone={model.enabled ? "green" : "neutral"} dot>
                      {model.enabled ? "Enabled" : "Disabled"}
                    </AdminStatusBadge>
                    <AdminStatusBadge tone={model.visible ? "sky" : "neutral"}>
                      {model.visible ? "Visible" : "Hidden"}
                    </AdminStatusBadge>
                  </div>
                </Td>
                <Td>
                  <ResourceMenu
                    onEdit={() => onEdit(model)}
                    onDefault={model.is_default ? undefined : () => onDefault(model)}
                    onDelete={() => onDelete(model)}
                    disabled={saving}
                  />
                </Td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </AdminTable>
  );
}

function ProvidersTable({
  providers,
  routeCountByProvider,
  loading,
  onAdd,
  onEdit,
  onDelete,
}: {
  providers: LLMProvider[];
  routeCountByProvider: Map<string, number>;
  loading: boolean;
  onAdd: () => void;
  onEdit: (provider: LLMProvider) => void;
  onDelete: (provider: LLMProvider) => void;
}) {
  if (!loading && providers.length === 0) {
    return (
      <AdminTable>
        <AdminEmptyState
          icon={<Boxes className="size-5" />}
          title="No providers connected"
          description="Connect the API endpoint that will serve your first model."
          action={<Button onClick={onAdd}>Add provider</Button>}
        />
      </AdminTable>
    );
  }
  return (
    <AdminTable>
      <table className="w-full min-w-[820px] text-sm">
        <thead className="border-b border-border/70 bg-muted/35 text-left text-xs text-muted-foreground">
          <tr>
            <Th>Provider</Th>
            <Th>Upstream API</Th>
            <Th>Endpoint</Th>
            <Th>Credential</Th>
            <Th>Models</Th>
            <Th>Status</Th>
            <Th>
              <span className="sr-only">Actions</span>
            </Th>
          </tr>
        </thead>
        <tbody>
          {providers.map((provider) => (
            <tr
              key={provider.id}
              className="border-b border-border/60 last:border-0 hover:bg-muted/20"
            >
              <Td>
                <button type="button" onClick={() => onEdit(provider)} className="text-left">
                  <span className="block font-medium text-foreground">
                    {provider.name || provider.id}
                  </span>
                  <code className="text-[11px] text-muted-foreground">{provider.id}</code>
                </button>
              </Td>
              <Td>
                <ProviderProtocolBadge type={provider.type} />
              </Td>
              <Td>
                <code
                  className="block max-w-xs truncate text-xs text-muted-foreground"
                  title={provider.base_url}
                >
                  {provider.base_url || "—"}
                </code>
              </Td>
              <Td>
                {provider.api_key_hint ? (
                  <span className="font-mono text-xs">{provider.api_key_hint}</span>
                ) : (
                  "—"
                )}
              </Td>
              <Td>
                <span className="tabular-nums">{routeCountByProvider.get(provider.id) ?? 0}</span>
              </Td>
              <Td>
                <AdminStatusBadge tone={provider.enabled ? "green" : "neutral"} dot>
                  {provider.enabled ? "Enabled" : "Disabled"}
                </AdminStatusBadge>
              </Td>
              <Td>
                <ResourceMenu onEdit={() => onEdit(provider)} onDelete={() => onDelete(provider)} />
              </Td>
            </tr>
          ))}
        </tbody>
      </table>
    </AdminTable>
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
      <DropdownMenuContent align="end">
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

function ViewTab({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "relative flex h-11 items-center gap-2 px-3 text-sm font-medium text-muted-foreground transition hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30",
        active &&
          "text-foreground after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded-full after:bg-primary",
      )}
    >
      {children}
    </button>
  );
}

function Count({ children }: { children: ReactNode }) {
  return (
    <span className="rounded-full bg-muted px-1.5 py-0.5 text-[11px] tabular-nums">{children}</span>
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
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: string;
}) {
  return (
    <label className="flex min-h-10 cursor-pointer items-center gap-2 rounded-xl border border-border/70 px-3 text-sm font-medium">
      <input
        type="checkbox"
        checked={checked}
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

function RuntimeProtocolBadge({ protocol }: { protocol: ModelProtocol }) {
  const responses = protocol === "openai-responses";
  return (
    <span
      className={cn(
        "inline-flex w-fit items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium",
        responses
          ? "border-violet-500/25 bg-violet-500/10 text-violet-700"
          : "border-blue-500/25 bg-blue-500/10 text-blue-700",
      )}
    >
      {responses ? <Bot className="size-3.5" /> : <Route className="size-3.5" />}
      {responses ? "Responses" : "Messages"}
    </span>
  );
}

function ProviderProtocolBadge({ type }: { type: ProviderType }) {
  const meta = providerTypeMeta(type);
  const responses = type === "openai_responses";
  const chatCompletions = type === "openai_compat";
  return (
    <span
      className={cn(
        "inline-flex w-fit items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium",
        responses
          ? "border-violet-500/25 bg-violet-500/10 text-violet-700"
          : chatCompletions
            ? "border-cyan-500/25 bg-cyan-500/10 text-cyan-700"
            : "border-blue-500/25 bg-blue-500/10 text-blue-700",
      )}
      title={meta.label}
    >
      {responses ? <Bot className="size-3.5" /> : <Route className="size-3.5" />}
      {meta.shortLabel}
    </span>
  );
}

function Th({ children }: { children: ReactNode }) {
  return <th className="px-4 py-3 font-medium">{children}</th>;
}

function Td({ children }: { children: ReactNode }) {
  return <td className="px-4 py-3 align-middle">{children}</td>;
}

function providerTypeMeta(type: ProviderType) {
  return PROVIDER_TYPES.find((item) => item.value === type) ?? PROVIDER_TYPES[0]!;
}

function protocolForType(type: ProviderType): ModelProtocol {
  return type === "openai_responses" ? "openai-responses" : "anthropic-messages";
}

function runtimeForProtocol(protocol: ModelProtocol) {
  return protocol === "openai-responses" ? "Codex" : "Claude Code";
}

function runtimeCompatibilityForType(type: ProviderType) {
  if (type === "openai_responses") return "Codex";
  if (type === "openai_compat") return "Claude Code via Cocola adapter";
  return "Claude Code";
}

function providerEndpoint(baseURL: string, type: ProviderType) {
  const base = baseURL.trim().replace(/\/$/, "") || providerTypeMeta(type).defaultBaseURL;
  if (type === "anthropic") return `${base}/v1/messages`;
  if (type === "openai_responses") return `${base}/responses`;
  return `${base}/chat/completions`;
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
      <span className="relative size-9 shrink-0 overflow-hidden rounded-xl border border-border bg-background">
        <Image src={model.icon_url} alt="" fill sizes="36px" className="object-cover" unoptimized />
      </span>
    );
  }
  const slug = model.icon_slug.toLowerCase();
  const localPath = LOCAL_SIMPLE_ICON_PATHS[slug];
  if (localPath) {
    return (
      <span className="relative size-9 shrink-0 overflow-hidden rounded-xl border border-border bg-background p-2">
        <Image
          src={localPath}
          alt=""
          fill
          sizes="36px"
          className="object-contain p-2"
          unoptimized
        />
      </span>
    );
  }
  return (
    <span className="grid size-9 shrink-0 place-items-center rounded-xl border border-border bg-muted text-xs font-semibold">
      {SIMPLE_ICON_FALLBACK_BADGES[slug] || model.label.slice(0, 2).toUpperCase() || "AI"}
    </span>
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
