"use client";

import { Settings as SettingsPageIcon } from "lucide-react";
import { Database, Layers, Loader2, RotateCcw, Save, SlidersHorizontal } from "lucide-react";
import { AlertTriangle, Check } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { AdminPage, AdminPageHeader, AdminRefreshButton } from "@/components/admin/admin-ui";

type SettingValue = boolean | number | string | null;

type SystemSetting = {
  key: string;
  group: string;
  label: string;
  description: string;
  kind: "bool" | "int" | "string" | "quantity";
  env?: string;
  default?: SettingValue;
  value?: SettingValue;
  source: "default" | "env" | "db";
  configured: boolean;
  version: number;
  updated_at?: string;
  updated_by?: string;
  editable: boolean;
  min?: number;
  max?: number;
};

type Drafts = Record<string, SettingValue>;

export default function AdminSettingsPage() {
  const [settings, setSettings] = useState<SystemSetting[]>([]);
  const [drafts, setDrafts] = useState<Drafts>({});
  const [loading, setLoading] = useState(true);
  const [savingKey, setSavingKey] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await fetch("/api/admin/settings", { cache: "no-store" });
      if (!res.ok) throw new Error(await errorText(res));
      const body = (await res.json()) as { settings?: SystemSetting[] };
      const next = body.settings ?? [];
      setSettings(next);
      setDrafts(Object.fromEntries(next.map((setting) => [setting.key, valueForDraft(setting)])));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const grouped = useMemo(() => {
    const groups = new Map<string, SystemSetting[]>();
    for (const setting of settings) {
      const rows = groups.get(setting.group) ?? [];
      rows.push(setting);
      groups.set(setting.group, rows);
    }
    return Array.from(groups.entries());
  }, [settings]);

  const stats = useMemo(
    () => ({
      total: settings.length,
      overrides: settings.filter((setting) => setting.source === "db").length,
      groups: grouped.length,
    }),
    [grouped.length, settings],
  );

  async function save(setting: SystemSetting) {
    setSavingKey(setting.key);
    setError("");
    setNotice("");
    try {
      const res = await fetch(`/api/admin/settings/${encodeURIComponent(setting.key)}`, {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          value: serializeDraft(setting, drafts[setting.key] ?? valueForDraft(setting)),
          expected_version: setting.version,
        }),
      });
      if (!res.ok) throw new Error(await errorText(res));
      setNotice(`${setting.label} saved`);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSavingKey("");
    }
  }

  async function reset(setting: SystemSetting) {
    setSavingKey(setting.key);
    setError("");
    setNotice("");
    try {
      const res = await fetch(
        `/api/admin/settings/${encodeURIComponent(setting.key)}?expected_version=${setting.version}`,
        { method: "DELETE" },
      );
      if (!res.ok) throw new Error(await errorText(res));
      setNotice(`${setting.label} reset to startup default`);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSavingKey("");
    }
  }

  return (
    <AdminPage className="admin-theme-slate">
      <AdminPageHeader
        icon={<SettingsPageIcon className="size-5" />}
        title="Settings"
        description="Runtime settings with environment defaults and database overrides"
        actions={
          <AdminRefreshButton
            variant="outline"
            title="Refresh settings"
            aria-label="Refresh settings"
            onClick={() => void load()}
            disabled={loading}
            refreshing={loading}
          >
            Refresh
          </AdminRefreshButton>
        }
      />

      <section className="grid gap-4 md:grid-cols-3">
        <Metric
          label="Settings"
          value={String(stats.total)}
          detail="Across all runtime groups"
          tone="slate"
          icon={<SlidersHorizontal />}
        />
        <Metric
          label="DB Overrides"
          value={String(stats.overrides)}
          detail="Persisted to database"
          tone="green"
          icon={<Database />}
        />
        <Metric
          label="Groups"
          value={String(stats.groups)}
          detail="Logical config sections"
          tone="indigo"
          icon={<Layers />}
        />
      </section>

      {error ? (
        <div className="flex items-center gap-2 rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
          <AlertTriangle className="size-4 shrink-0" />
          <span className="min-w-0">{error}</span>
        </div>
      ) : null}
      {notice ? (
        <div className="flex items-center gap-2 rounded-xl border border-emerald-500/30 bg-emerald-500/10 px-4 py-3 text-sm text-emerald-700 dark:text-emerald-300">
          <Check className="size-4 shrink-0" />
          <span className="min-w-0">{notice}</span>
        </div>
      ) : null}

      {loading ? (
        <div className="flex h-40 items-center justify-center text-sm text-muted-foreground">
          <Loader2 className="mr-2 size-4 animate-spin" />
          Loading settings
        </div>
      ) : (
        <section className="space-y-5">
          {grouped.map(([group, rows]) => (
            <div key={group} className="admin-set-group">
              <div className="admin-set-group-head">
                <span className="admin-set-glyph">
                  <SlidersHorizontal className="size-[19px]" />
                </span>
                <span className="admin-set-title">{group}</span>
                <span className="admin-set-count">
                  {rows.length} {rows.length === 1 ? "setting" : "settings"}
                </span>
              </div>
              <div className="admin-set-cols">
                <div>Setting</div>
                <div>Value</div>
                <div className="r">Actions</div>
              </div>

              {rows.map((setting) => {
                const draftValue = drafts[setting.key] ?? valueForDraft(setting);
                const dirty = !sameValue(valueForDraft(setting), draftValue);
                const busy = savingKey === setting.key;
                return (
                  <div key={setting.key} className="admin-set-row">
                    <div className="admin-set-meta">
                      <div className="admin-set-name">
                        <span className="admin-set-label">{setting.label}</span>
                        <span className="admin-set-src" data-src={setting.source}>
                          <span className="admin-set-sdot" />
                          {setting.source}
                        </span>
                      </div>
                      <div className="admin-set-desc">{setting.description}</div>
                      <div className="admin-set-keys">
                        <span className="admin-set-keychip">{setting.key}</span>
                        {setting.env ? (
                          <span className="admin-set-keychip">{setting.env}</span>
                        ) : null}
                      </div>
                    </div>

                    <div className="min-w-0">
                      <SettingControl
                        setting={setting}
                        value={draftValue}
                        onChange={(value) =>
                          setDrafts((prev) => ({ ...prev, [setting.key]: value }))
                        }
                      />
                      <div className="admin-set-ctrl-def">{`Default: ${formatValue(setting.default)}`}</div>
                    </div>

                    <div className="admin-set-actions">
                      {setting.editable ? (
                        <>
                          <button
                            type="button"
                            className="admin-set-iconbtn admin-set-iconbtn--primary"
                            title="Save override"
                            aria-label="Save override"
                            disabled={!dirty || busy}
                            onClick={() => void save(setting)}
                          >
                            {busy ? (
                              <Loader2 className="size-4 animate-spin" />
                            ) : (
                              <Save className="size-4" />
                            )}
                          </button>
                          <button
                            type="button"
                            className="admin-set-iconbtn"
                            title="Reset override"
                            aria-label="Reset override"
                            disabled={setting.source !== "db" || busy}
                            onClick={() => void reset(setting)}
                          >
                            <RotateCcw className="size-4" />
                          </button>
                        </>
                      ) : (
                        <span className="admin-set-ro">read only</span>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          ))}
        </section>
      )}
    </AdminPage>
  );
}

function SettingControl({
  setting,
  value,
  onChange,
}: {
  setting: SystemSetting;
  value: SettingValue;
  onChange: (value: SettingValue) => void;
}) {
  if (setting.kind === "bool") {
    const checked = value === true;
    return (
      <label className="admin-set-toggle">
        <input
          aria-label={setting.label}
          type="checkbox"
          checked={checked}
          disabled={!setting.editable}
          onChange={(event) => onChange(event.target.checked)}
        />
        <span className="admin-set-track" />
        <span className="admin-set-toggle-text">{checked ? "Enabled" : "Disabled"}</span>
      </label>
    );
  }

  if (setting.kind === "int") {
    return (
      <input
        aria-label={setting.label}
        className="admin-set-ctrl-input"
        type="number"
        min={setting.min}
        max={setting.max}
        value={typeof value === "number" ? String(value) : ""}
        disabled={!setting.editable}
        onChange={(event) =>
          onChange(event.target.value === "" ? null : Number(event.target.value))
        }
      />
    );
  }

  return (
    <input
      aria-label={setting.label}
      className="admin-set-ctrl-input"
      value={typeof value === "string" ? value : ""}
      disabled={!setting.editable}
      onChange={(event) => onChange(event.target.value)}
    />
  );
}

function Metric({
  label,
  value,
  detail,
  tone,
  icon,
}: {
  label: string;
  value: string;
  detail: string;
  tone: string;
  icon: React.ReactNode;
}) {
  return (
    <div className="admin-metric-card" data-tone={tone}>
      <div className="admin-metric-head">
        <span className="admin-metric-glyph">{icon}</span>
        <span className="admin-metric-key">{label}</span>
      </div>
      <div className="admin-metric-val truncate">{value}</div>
      <div className="admin-metric-detail">{detail}</div>
    </div>
  );
}

function valueForDraft(setting: SystemSetting): SettingValue {
  if (setting.value !== undefined) return setting.value;
  return setting.default ?? null;
}

function serializeDraft(setting: SystemSetting, value: SettingValue) {
  if (setting.kind === "int") return Number(value);
  return value;
}

function sameValue(a: SettingValue, b: SettingValue) {
  return String(a ?? "") === String(b ?? "");
}

function formatValue(value: SettingValue | undefined) {
  if (value === undefined || value === null || value === "") return "-";
  return String(value);
}

async function errorText(res: Response) {
  try {
    const body = (await res.json()) as { error?: string | { message?: string } };
    if (typeof body.error === "string") return body.error;
    if (body.error?.message) return body.error.message;
  } catch {
    // fall through
  }
  return `${res.status} ${res.statusText}`;
}
