"use client";

import {
  Check,
  Cpu,
  Eye,
  EyeOff,
  KeyRound,
  Plus,
  RefreshCw,
  Save,
  Star,
  Trash2,
} from "lucide-react";
import Image from "next/image";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import {
  LOCAL_SIMPLE_ICON_PATHS,
  SIMPLE_ICON_FALLBACK_BADGES,
  SIMPLE_ICON_LABELS,
  SIMPLE_ICON_SLUGS,
} from "@/lib/model-icons";
import { cn } from "@/lib/utils";

type LLMProvider = {
  id: string;
  name: string;
  type: "anthropic" | "openai_compat" | "fake";
  base_url: string;
  api_key_hint: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
};

type LLMModel = {
  alias: string;
  provider_id: string;
  real_model: string;
  runtime: string;
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
  type: LLMProvider["type"];
  base_url: string;
  api_key: string;
  enabled: boolean;
};

type ModelForm = {
  alias: string;
  provider_id: string;
  real_model: string;
  runtime: string;
  label: string;
  icon_type: LLMModel["icon_type"];
  icon_slug: string;
  icon_url: string;
  enabled: boolean;
  visible: boolean;
  is_default: boolean;
  sort_order: string;
};

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
  runtime: "claude-code",
  label: "",
  icon_type: "simple-icons",
  icon_slug: "anthropic",
  icon_url: "",
  enabled: true,
  visible: true,
  is_default: false,
  sort_order: "0",
};

const PROVIDER_TYPES = [
  { value: "anthropic", label: "Anthropic" },
  { value: "openai_compat", label: "OpenAI Compatible" },
  { value: "fake", label: "Fake" },
] as const;

const btn =
  "inline-flex h-9 items-center justify-center gap-2 rounded-md border border-border bg-background px-3 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-50";
const primaryBtn =
  "inline-flex h-9 items-center justify-center gap-2 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-50";
const iconBtn =
  "inline-flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-40";
const input =
  "h-9 min-w-0 rounded-md border border-input bg-background px-3 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-ring focus:ring-1 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-60";

export default function AdminModelsPage() {
  const [providers, setProviders] = useState<LLMProvider[]>([]);
  const [models, setModels] = useState<LLMModel[]>([]);
  const [providerForm, setProviderForm] = useState<ProviderForm>(EMPTY_PROVIDER);
  const [modelForm, setModelForm] = useState<ModelForm>(EMPTY_MODEL);
  const [editingProvider, setEditingProvider] = useState<string | null>(null);
  const [editingModel, setEditingModel] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const stats = useMemo(
    () => ({
      providers: providers.length,
      enabledProviders: providers.filter((p) => p.enabled).length,
      models: models.length,
      visibleModels: models.filter((m) => m.enabled && m.visible).length,
    }),
    [models, providers],
  );

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
      const nextProviders = providerBody.providers ?? [];
      setProviders(nextProviders);
      setModels(modelBody.models ?? []);
      setModelForm((prev) => ({
        ...prev,
        provider_id: prev.provider_id || nextProviders[0]?.id || "",
      }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function saveProvider() {
    setSaving(true);
    setError("");
    try {
      const body: Record<string, unknown> = {
        id: providerForm.id,
        name: providerForm.name,
        type: providerForm.type,
        base_url: providerForm.base_url,
        enabled: providerForm.enabled,
      };
      if (providerForm.api_key.trim()) body.api_key = providerForm.api_key.trim();
      const url = editingProvider
        ? `/api/admin/model-providers/${encodeURIComponent(editingProvider)}`
        : "/api/admin/model-providers";
      const res = await fetch(url, {
        method: editingProvider ? "PATCH" : "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!res.ok) throw new Error(await errorText(res));
      setProviderForm(EMPTY_PROVIDER);
      setEditingProvider(null);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  async function saveModel() {
    setSaving(true);
    setError("");
    try {
      const body = {
        alias: modelForm.alias,
        provider_id: modelForm.provider_id,
        real_model: modelForm.real_model,
        runtime: modelForm.runtime,
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
      const res = await fetch(url, {
        method: editingModel ? "PATCH" : "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!res.ok) throw new Error(await errorText(res));
      setModelForm({ ...EMPTY_MODEL, provider_id: providers[0]?.id || "" });
      setEditingModel(null);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  async function deleteProvider(id: string) {
    if (!confirm(`Delete provider ${id}?`)) return;
    await mutate(`/api/admin/model-providers/${encodeURIComponent(id)}`, "DELETE");
  }

  async function deleteModel(alias: string) {
    if (!confirm(`Delete model ${alias}?`)) return;
    await mutate(`/api/admin/models/${encodeURIComponent(alias)}`, "DELETE");
  }

  async function setDefault(alias: string) {
    await mutate(`/api/admin/models/${encodeURIComponent(alias)}/default`, "POST");
  }

  async function mutate(url: string, method: string) {
    setSaving(true);
    setError("");
    try {
      const res = await fetch(url, { method });
      if (!res.ok) throw new Error(await errorText(res));
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
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
  }

  function editModel(model: LLMModel) {
    setEditingModel(model.alias);
    setModelForm({
      alias: model.alias,
      provider_id: model.provider_id,
      real_model: model.real_model,
      runtime: model.runtime,
      label: model.label,
      icon_type: model.icon_type,
      icon_slug: model.icon_slug,
      icon_url: model.icon_url,
      enabled: model.enabled,
      visible: model.visible,
      is_default: model.is_default,
      sort_order: String(model.sort_order),
    });
  }

  return (
    <main className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border">
        <div className="mx-auto flex h-16 max-w-7xl items-center gap-3 px-6">
          <div className="grid size-9 place-items-center rounded-md bg-primary text-primary-foreground">
            <Cpu className="size-4" />
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-base font-semibold">Model Configuration</h1>
            <p className="truncate text-xs text-muted-foreground">
              Providers, API keys, model aliases, visibility, defaults, and logos
            </p>
          </div>
          <button className={btn} type="button" onClick={() => void load()}>
            <RefreshCw className="size-4" />
            Refresh
          </button>
        </div>
      </header>

      <div className="mx-auto max-w-7xl space-y-6 px-6 py-6">
        <section className="grid gap-3 md:grid-cols-4">
          <Metric label="Providers" value={String(stats.providers)} />
          <Metric label="Enabled Providers" value={String(stats.enabledProviders)} />
          <Metric label="Model Routes" value={String(stats.models)} />
          <Metric label="Visible Models" value={String(stats.visibleModels)} />
        </section>

        {error ? (
          <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {error}
          </div>
        ) : null}

        <section className="grid gap-4 xl:grid-cols-[0.9fr_1.4fr]">
          <div className="rounded-lg border border-border bg-card p-4">
            <div className="mb-4 flex items-center justify-between gap-3">
              <h2 className="text-sm font-semibold">Provider</h2>
              <button
                type="button"
                className={btn}
                onClick={() => {
                  setEditingProvider(null);
                  setProviderForm(EMPTY_PROVIDER);
                }}
              >
                <Plus className="size-4" />
                New
              </button>
            </div>
            <div className="grid gap-3">
              <Field label="Provider ID">
                <input
                  className={input}
                  value={providerForm.id}
                  disabled={Boolean(editingProvider)}
                  onChange={(e) => setProviderForm({ ...providerForm, id: e.target.value })}
                  placeholder="anthropic"
                />
              </Field>
              <Field label="Name">
                <input
                  className={input}
                  value={providerForm.name}
                  onChange={(e) => setProviderForm({ ...providerForm, name: e.target.value })}
                  placeholder="Anthropic"
                />
              </Field>
              <Field label="Type">
                <select
                  className={input}
                  value={providerForm.type}
                  onChange={(e) =>
                    setProviderForm({
                      ...providerForm,
                      type: e.target.value as ProviderForm["type"],
                    })
                  }
                >
                  {PROVIDER_TYPES.map((type) => (
                    <option key={type.value} value={type.value}>
                      {type.label}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Base URL">
                <input
                  className={input}
                  value={providerForm.base_url}
                  onChange={(e) => setProviderForm({ ...providerForm, base_url: e.target.value })}
                  placeholder="https://api.anthropic.com"
                />
              </Field>
              <Field label="API Key">
                <div className="flex min-w-0 items-center gap-2">
                  <KeyRound className="size-4 shrink-0 text-muted-foreground" />
                  <input
                    className={input}
                    value={providerForm.api_key}
                    onChange={(e) => setProviderForm({ ...providerForm, api_key: e.target.value })}
                    placeholder={editingProvider ? "Leave blank to keep current key" : "sk-..."}
                    type="password"
                  />
                </div>
              </Field>
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={providerForm.enabled}
                  onChange={(e) => setProviderForm({ ...providerForm, enabled: e.target.checked })}
                />
                Enabled
              </label>
              <button
                type="button"
                className={primaryBtn}
                disabled={saving}
                onClick={() => void saveProvider()}
              >
                <Save className="size-4" />
                {editingProvider ? "Save Provider" : "Create Provider"}
              </button>
            </div>
          </div>

          <div className="rounded-lg border border-border bg-card p-4">
            <div className="mb-4 flex items-center justify-between gap-3">
              <h2 className="text-sm font-semibold">Model Route</h2>
              <button
                type="button"
                className={btn}
                onClick={() => {
                  setEditingModel(null);
                  setModelForm({ ...EMPTY_MODEL, provider_id: providers[0]?.id || "" });
                }}
              >
                <Plus className="size-4" />
                New
              </button>
            </div>
            <div className="grid gap-3 md:grid-cols-2">
              <Field label="Alias">
                <input
                  className={input}
                  value={modelForm.alias}
                  disabled={Boolean(editingModel)}
                  onChange={(e) => setModelForm({ ...modelForm, alias: e.target.value })}
                  placeholder="claude-sonnet"
                />
              </Field>
              <Field label="Provider">
                <select
                  className={input}
                  value={modelForm.provider_id}
                  onChange={(e) => setModelForm({ ...modelForm, provider_id: e.target.value })}
                >
                  <option value="">Select provider</option>
                  {providers.map((provider) => (
                    <option key={provider.id} value={provider.id}>
                      {provider.name || provider.id}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Real Model">
                <input
                  className={input}
                  value={modelForm.real_model}
                  onChange={(e) => setModelForm({ ...modelForm, real_model: e.target.value })}
                  placeholder="claude-3-5-sonnet-20241022"
                />
              </Field>
              <Field label="Label">
                <input
                  className={input}
                  value={modelForm.label}
                  onChange={(e) => setModelForm({ ...modelForm, label: e.target.value })}
                  placeholder="Claude Sonnet"
                />
              </Field>
              <Field label="Icon">
                <div className="grid gap-2 sm:grid-cols-[9rem_1fr]">
                  <select
                    className={input}
                    value={modelForm.icon_type}
                    onChange={(e) =>
                      setModelForm({
                        ...modelForm,
                        icon_type: e.target.value as ModelForm["icon_type"],
                      })
                    }
                  >
                    <option value="simple-icons">Local icon</option>
                    <option value="image">Image URL</option>
                  </select>
                  {modelForm.icon_type === "image" ? (
                    <input
                      className={input}
                      value={modelForm.icon_url}
                      onChange={(e) => setModelForm({ ...modelForm, icon_url: e.target.value })}
                      placeholder="https://..."
                    />
                  ) : (
                    <select
                      className={input}
                      value={modelForm.icon_slug}
                      onChange={(e) => setModelForm({ ...modelForm, icon_slug: e.target.value })}
                    >
                      {SIMPLE_ICON_SLUGS.map((slug) => (
                        <option key={slug} value={slug}>
                          {SIMPLE_ICON_LABELS[slug] ?? slug}
                        </option>
                      ))}
                    </select>
                  )}
                </div>
              </Field>
              <Field label="Sort">
                <input
                  className={input}
                  value={modelForm.sort_order}
                  onChange={(e) => setModelForm({ ...modelForm, sort_order: e.target.value })}
                  inputMode="numeric"
                />
              </Field>
              <div className="flex flex-wrap items-center gap-4 md:col-span-2">
                <CheckBox
                  label="Enabled"
                  checked={modelForm.enabled}
                  onChange={(enabled) => setModelForm({ ...modelForm, enabled })}
                />
                <CheckBox
                  label="Visible"
                  checked={modelForm.visible}
                  onChange={(visible) => setModelForm({ ...modelForm, visible })}
                />
                <CheckBox
                  label="Default"
                  checked={modelForm.is_default}
                  onChange={(is_default) => setModelForm({ ...modelForm, is_default })}
                />
              </div>
              <button
                type="button"
                className={cn(primaryBtn, "md:col-span-2")}
                disabled={saving || providers.length === 0}
                onClick={() => void saveModel()}
              >
                <Save className="size-4" />
                {editingModel ? "Save Model" : "Create Model"}
              </button>
            </div>
          </div>
        </section>

        <section className="rounded-lg border border-border bg-card">
          <SectionHeader title="Providers" loading={loading} />
          <div className="overflow-x-auto">
            <table className="min-w-[920px] text-sm">
              <thead className="border-y border-border bg-muted/40 text-xs text-muted-foreground">
                <tr>
                  <Th>Provider ID</Th>
                  <Th>Name</Th>
                  <Th>Type</Th>
                  <Th>Base URL</Th>
                  <Th>API Key</Th>
                  <Th>Status</Th>
                  <Th>Actions</Th>
                </tr>
              </thead>
              <tbody>
                {providers.length === 0 ? (
                  <EmptyRow colSpan={7} text="No providers configured" />
                ) : (
                  providers.map((provider) => (
                    <tr key={provider.id} className="border-b border-border last:border-0">
                      <Td title={provider.id}>{provider.id}</Td>
                      <Td title={provider.name}>{provider.name}</Td>
                      <Td>{provider.type}</Td>
                      <Td title={provider.base_url}>{provider.base_url || "-"}</Td>
                      <Td>{provider.api_key_hint || "-"}</Td>
                      <Td>
                        <Status on={provider.enabled} />
                      </Td>
                      <Td>
                        <RowActions
                          onEdit={() => editProvider(provider)}
                          onDelete={() => void deleteProvider(provider.id)}
                        />
                      </Td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </section>

        <section className="rounded-lg border border-border bg-card">
          <SectionHeader title="Model Routes" loading={loading} />
          <div className="overflow-x-auto">
            <table className="min-w-[1120px] text-sm">
              <thead className="border-y border-border bg-muted/40 text-xs text-muted-foreground">
                <tr>
                  <Th>Alias</Th>
                  <Th>Label</Th>
                  <Th>Provider ID</Th>
                  <Th>Real Model</Th>
                  <Th>Icon</Th>
                  <Th>Status</Th>
                  <Th>Visible</Th>
                  <Th>Default</Th>
                  <Th>Actions</Th>
                </tr>
              </thead>
              <tbody>
                {models.length === 0 ? (
                  <EmptyRow colSpan={9} text="No model routes configured" />
                ) : (
                  models.map((model) => (
                    <tr key={model.alias} className="border-b border-border last:border-0">
                      <Td title={model.alias}>{model.alias}</Td>
                      <Td title={model.label}>{model.label}</Td>
                      <Td title={model.provider_id}>{model.provider_id}</Td>
                      <Td title={model.real_model}>{model.real_model}</Td>
                      <Td title={model.icon_type === "image" ? model.icon_url : model.icon_slug}>
                        <span className="inline-flex items-center gap-2">
                          <ModelIcon model={model} />
                          <span className="truncate">
                            {model.icon_type === "image"
                              ? "image"
                              : SIMPLE_ICON_LABELS[model.icon_slug.toLowerCase()] ||
                                model.icon_slug}
                          </span>
                        </span>
                      </Td>
                      <Td>
                        <Status on={model.enabled} />
                      </Td>
                      <Td>
                        {model.visible ? (
                          <Eye className="size-4" />
                        ) : (
                          <EyeOff className="size-4 text-muted-foreground" />
                        )}
                      </Td>
                      <Td>
                        {model.is_default ? (
                          <Star className="size-4 fill-primary text-primary" />
                        ) : (
                          "-"
                        )}
                      </Td>
                      <Td>
                        <div className="flex items-center gap-1">
                          <button
                            className={iconBtn}
                            type="button"
                            onClick={() => editModel(model)}
                            title="Edit"
                          >
                            <Save className="size-4" />
                          </button>
                          <button
                            className={iconBtn}
                            type="button"
                            onClick={() => void setDefault(model.alias)}
                            title="Set default"
                            disabled={model.is_default}
                          >
                            <Star className="size-4" />
                          </button>
                          <button
                            className={iconBtn}
                            type="button"
                            onClick={() => void deleteModel(model.alias)}
                            title="Delete"
                          >
                            <Trash2 className="size-4" />
                          </button>
                        </div>
                      </Td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </section>
      </div>
    </main>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <div className="text-xs font-medium text-muted-foreground">{label}</div>
      <div className="mt-2 text-2xl font-semibold tabular-nums">{value}</div>
    </div>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="grid gap-1.5 text-xs font-medium text-muted-foreground">
      {label}
      {children}
    </label>
  );
}

function CheckBox({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="flex items-center gap-2 text-sm">
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      {label}
    </label>
  );
}

function SectionHeader({ title, loading }: { title: string; loading: boolean }) {
  return (
    <div className="flex h-12 items-center justify-between px-4">
      <h2 className="text-sm font-semibold">{title}</h2>
      {loading ? <span className="text-xs text-muted-foreground">Loading...</span> : null}
    </div>
  );
}

function Th({ children }: { children: ReactNode }) {
  return <th className="px-4 py-2 text-left font-medium">{children}</th>;
}

function Td({ children, title }: { children: ReactNode; title?: string }) {
  return (
    <td className="max-w-[16rem] px-4 py-3 align-middle">
      <div className="truncate" title={title}>
        {children}
      </div>
    </td>
  );
}

function EmptyRow({ colSpan, text }: { colSpan: number; text: string }) {
  return (
    <tr>
      <td colSpan={colSpan} className="px-4 py-8 text-center text-sm text-muted-foreground">
        {text}
      </td>
    </tr>
  );
}

function Status({ on }: { on: boolean }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium",
        on
          ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-700"
          : "border-border bg-muted text-muted-foreground",
      )}
    >
      {on ? <Check className="size-3" /> : null}
      {on ? "Enabled" : "Disabled"}
    </span>
  );
}

function RowActions({ onEdit, onDelete }: { onEdit: () => void; onDelete: () => void }) {
  return (
    <div className="flex items-center gap-1">
      <button className={iconBtn} type="button" onClick={onEdit} title="Edit">
        <Save className="size-4" />
      </button>
      <button className={iconBtn} type="button" onClick={onDelete} title="Delete">
        <Trash2 className="size-4" />
      </button>
    </div>
  );
}

function ModelIcon({ model }: { model: LLMModel }) {
  const simpleIconPath =
    model.icon_type === "simple-icons" && model.icon_slug
      ? LOCAL_SIMPLE_ICON_PATHS[model.icon_slug.toLowerCase()]
      : "";

  if (model.icon_type === "image" && model.icon_url) {
    return (
      <span className="inline-flex size-6 shrink-0 overflow-hidden rounded-full border border-border bg-background">
        <Image
          src={model.icon_url}
          alt=""
          width={96}
          height={96}
          unoptimized
          className="size-full object-contain"
        />
      </span>
    );
  }
  if (simpleIconPath) {
    return (
      <span className="inline-flex size-6 shrink-0 items-center justify-center overflow-hidden rounded-full border border-border bg-white">
        <Image
          src={simpleIconPath}
          alt=""
          width={96}
          height={96}
          unoptimized
          className="size-[72%] object-contain"
        />
      </span>
    );
  }
  return (
    <span className="inline-flex size-6 shrink-0 items-center justify-center rounded-full border border-border bg-muted text-[10px] font-semibold">
      {SIMPLE_ICON_FALLBACK_BADGES[model.icon_slug.toLowerCase()] ||
        (model.icon_slug || "AI").slice(0, 2).toUpperCase()}
    </span>
  );
}

async function errorText(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: { message?: string }; error_description?: string };
    return body.error?.message || body.error_description || `request failed: ${res.status}`;
  } catch {
    return `request failed: ${res.status}`;
  }
}
