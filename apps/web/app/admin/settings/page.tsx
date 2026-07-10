"use client";

import { Gear as SettingsPageIcon } from "@phosphor-icons/react";
import {
  AlertTriangle,
  Check,
  Loader2,
  RefreshCw,
  RotateCcw,
  Save,
  SlidersHorizontal,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";

type SettingValue = boolean | number | string | null;

type SystemSetting = {
  key: string;
  group: string;
  label: string;
  description: string;
  kind: "bool" | "int" | "string" | "secret";
  env?: string;
  default?: SettingValue;
  value?: SettingValue;
  source: "default" | "env" | "db";
  configured: boolean;
  version: number;
  updated_at?: string;
  updated_by?: string;
  editable: boolean;
  hot_reload: boolean;
  restart_required: boolean;
  sensitive: boolean;
  min?: number;
  max?: number;
};

type Drafts = Record<string, SettingValue>;

const iconBtn =
  "inline-flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-40";
const input =
  "h-9 min-w-0 rounded-md border border-input bg-background px-3 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-ring focus:ring-1 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-60";

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
      hot: settings.filter((setting) => setting.hot_reload).length,
    }),
    [settings],
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
    <main className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border">
        <div className="mx-auto flex h-16 max-w-7xl items-center gap-3 px-6">
          <div className="admin-page-icon">
            <SettingsPageIcon className="size-[18px]" weight="duotone" />
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-base font-semibold">System Settings</h1>
            <p className="truncate text-xs text-muted-foreground">
              Startup defaults, database overrides, and runtime mutability
            </p>
          </div>
          <button className={iconBtn} title="Refresh settings" onClick={() => void load()}>
            <RefreshCw className="size-4" />
          </button>
        </div>
      </header>

      <div className="mx-auto max-w-7xl space-y-5 px-6 py-6">
        <section className="grid gap-3 md:grid-cols-3">
          <Metric label="Settings" value={String(stats.total)} />
          <Metric label="DB Overrides" value={String(stats.overrides)} />
          <Metric label="Hot Reload" value={String(stats.hot)} />
        </section>

        {error ? (
          <div className="flex items-center gap-2 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            <AlertTriangle className="size-4 shrink-0" />
            <span className="min-w-0">{error}</span>
          </div>
        ) : null}
        {notice ? (
          <div className="flex items-center gap-2 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-sm text-emerald-700 dark:text-emerald-300">
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
          <section className="space-y-4">
            {grouped.map(([group, rows]) => (
              <div key={group} className="rounded-lg border border-border bg-card">
                <div className="flex items-center gap-2 border-b border-border px-4 py-3">
                  <SlidersHorizontal className="size-4 text-muted-foreground" />
                  <h2 className="text-sm font-semibold">{group}</h2>
                </div>
                <div className="divide-y divide-border">
                  {rows.map((setting) => {
                    const draftValue = drafts[setting.key] ?? valueForDraft(setting);
                    const dirty = !sameValue(valueForDraft(setting), draftValue);
                    return (
                      <div
                        key={setting.key}
                        className="grid gap-3 px-4 py-4 xl:grid-cols-[minmax(260px,1fr)_minmax(280px,420px)_220px]"
                      >
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-2">
                            <h3 className="text-sm font-medium">{setting.label}</h3>
                            <Badge tone={sourceTone(setting.source)}>{setting.source}</Badge>
                            {setting.hot_reload ? <Badge tone="green">hot reload</Badge> : null}
                            {setting.restart_required ? <Badge tone="amber">restart</Badge> : null}
                            {setting.sensitive ? <Badge tone="red">secret</Badge> : null}
                          </div>
                          <p className="mt-1 text-sm text-muted-foreground">
                            {setting.description}
                          </p>
                          <div className="mt-2 flex flex-wrap gap-2 text-xs text-muted-foreground">
                            <span className="rounded-md bg-muted px-2 py-1 font-mono">
                              {setting.key}
                            </span>
                            {setting.env ? (
                              <span className="rounded-md bg-muted px-2 py-1 font-mono">
                                {setting.env}
                              </span>
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
                          <div className="mt-2 text-xs text-muted-foreground">
                            {setting.sensitive
                              ? setting.configured
                                ? "Configured"
                                : "Not configured"
                              : `Default: ${formatValue(setting.default)}`}
                          </div>
                        </div>

                        <div className="flex items-start justify-end gap-2">
                          {setting.editable ? (
                            <>
                              <button
                                className={iconBtn}
                                title="Save override"
                                disabled={!dirty || savingKey === setting.key}
                                onClick={() => void save(setting)}
                              >
                                {savingKey === setting.key ? (
                                  <Loader2 className="size-4 animate-spin" />
                                ) : (
                                  <Save className="size-4" />
                                )}
                              </button>
                              <button
                                className={iconBtn}
                                title="Reset override"
                                disabled={setting.source !== "db" || savingKey === setting.key}
                                onClick={() => void reset(setting)}
                              >
                                <RotateCcw className="size-4" />
                              </button>
                            </>
                          ) : (
                            <span className="rounded-md border border-border bg-muted px-2 py-1 text-xs text-muted-foreground">
                              read only
                            </span>
                          )}
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            ))}
          </section>
        )}
      </div>
    </main>
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
  if (setting.sensitive) {
    return (
      <div className="flex h-9 items-center rounded-md border border-input bg-muted px-3 text-sm text-muted-foreground">
        {setting.configured ? "Configured" : "Not configured"}
      </div>
    );
  }

  if (setting.kind === "bool") {
    return (
      <label className="flex h-9 items-center gap-3 text-sm">
        <input
          className="size-4 rounded border-border accent-primary"
          type="checkbox"
          checked={value === true}
          disabled={!setting.editable}
          onChange={(event) => onChange(event.target.checked)}
        />
        <span>{value === true ? "Enabled" : "Disabled"}</span>
      </label>
    );
  }

  if (setting.kind === "int") {
    return (
      <input
        className={input}
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
      className={input}
      value={typeof value === "string" ? value : ""}
      disabled={!setting.editable}
      onChange={(event) => onChange(event.target.value)}
    />
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-card px-4 py-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-2xl font-semibold">{value}</div>
    </div>
  );
}

function Badge({
  children,
  tone,
}: {
  children: string;
  tone: "default" | "green" | "amber" | "red";
}) {
  const cls =
    tone === "green"
      ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
      : tone === "amber"
        ? "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300"
        : tone === "red"
          ? "border-destructive/30 bg-destructive/10 text-destructive"
          : "border-border bg-muted text-muted-foreground";
  return <span className={`rounded-md border px-2 py-0.5 text-xs ${cls}`}>{children}</span>;
}

function valueForDraft(setting: SystemSetting): SettingValue {
  if (setting.sensitive) return null;
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

function sourceTone(source: SystemSetting["source"]) {
  return source === "db" ? "green" : source === "env" ? "amber" : "default";
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
