"use client";

import { Settings as SettingsPageIcon } from "lucide-react";
import { Loader2, RotateCcw, Save } from "lucide-react";
import { AlertTriangle, Check } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Button, Card, Chip, Input, Label, Switch, TextField, Tooltip } from "@heroui/react";
import { AdminConfirmDialog, AdminPage, AdminPageHeader, AdminRefreshButton } from "@/components/admin/admin-ui";

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
  const [resetTarget, setResetTarget] = useState<SystemSetting | null>(null);

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

  async function reset() {
    if (!resetTarget) return;
    setSavingKey(resetTarget.key);
    setError("");
    setNotice("");
    try {
      const res = await fetch(
        `/api/admin/settings/${encodeURIComponent(resetTarget.key)}?expected_version=${resetTarget.version}`,
        { method: "DELETE" },
      );
      if (!res.ok) throw new Error(await errorText(res));
      setNotice(`${resetTarget.label} reset to startup default`);
      setResetTarget(null);
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

      {error ? (
        <div className="flex items-center gap-2 rounded-xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
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
        <div className="text-muted flex h-40 items-center justify-center text-sm">
          <Loader2 className="mr-2 size-4 animate-spin" />
          Loading settings
        </div>
      ) : (
        <section className="grid gap-5">
          {grouped.map(([group, rows]) => (
            <Card key={group} className="overflow-hidden p-0">
              <Card.Header className="bg-surface-secondary flex-row items-center justify-between px-5 py-4">
                <Card.Title>{group}</Card.Title>
                <Chip size="sm" variant="soft">{rows.length} {rows.length === 1 ? "setting" : "settings"}</Chip>
              </Card.Header>
              <Card.Content className="divide-y divide-separator p-0">
                {rows.map((setting) => {
                const draftValue = drafts[setting.key] ?? valueForDraft(setting);
                const dirty = !sameValue(valueForDraft(setting), draftValue);
                const busy = savingKey === setting.key;
                return (
                  <div key={setting.key} className="grid gap-4 px-5 py-4 lg:grid-cols-[minmax(0,1fr)_minmax(220px,320px)_180px] lg:items-center">
                    <div className="min-w-0">
                      <span className="flex flex-wrap items-center gap-2">
                        <span className="font-medium">{setting.label}</span>
                        <Chip color={setting.source === "db" ? "accent" : "default"} size="sm" variant="soft">{sourceLabel(setting.source)}</Chip>
                      </span>
                      <p className="text-muted mt-1 max-w-2xl text-xs leading-5">{setting.description}</p>
                      <div className="text-muted mt-2 flex min-w-0 flex-wrap gap-2 font-mono text-[11px]">
                        <code className="bg-surface-secondary max-w-full truncate rounded-lg px-2 py-1">{setting.key} · v{setting.version}</code>
                        {setting.env ? <code className="bg-surface-secondary max-w-full truncate rounded-lg px-2 py-1">{setting.env}</code> : null}
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
                      <div className="text-muted mt-1 text-xs">{`Default: ${formatValue(setting.default)}`}</div>
                    </div>

                    <div className="flex justify-end gap-2">
                      {setting.editable ? (
                        <>
                          <Tooltip delay={0}>
                          <Button
                            aria-label={`Save ${setting.label}`}
                            isDisabled={!dirty || busy}
                            size="sm"
                            variant={dirty ? "primary" : "outline"}
                            onPress={() => void save(setting)}
                          >
                            {busy ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}Save
                          </Button>
                          <Tooltip.Content>Save this value as a database override</Tooltip.Content>
                          </Tooltip>
                          <Tooltip delay={0}>
                          <Button
                            aria-label={`${dirty ? "Revert" : "Reset"} ${setting.label}`}
                            isDisabled={(!dirty && setting.source !== "db") || busy}
                            size="sm"
                            variant="outline"
                            onPress={() => dirty ? setDrafts((current) => ({ ...current, [setting.key]: valueForDraft(setting) })) : setResetTarget(setting)}
                          >
                            <RotateCcw className="size-4" />
                            {dirty ? "Revert" : "Reset"}
                          </Button>
                          <Tooltip.Content>{dirty ? "Discard the unsaved change" : "Remove the database override"}</Tooltip.Content>
                          </Tooltip>
                        </>
                      ) : (
                        <Chip size="sm" variant="soft">Read only</Chip>
                      )}
                    </div>
                  </div>
                );
              })}
              </Card.Content>
            </Card>
          ))}
        </section>
      )}

      <AdminConfirmDialog
        open={resetTarget !== null}
        onOpenChange={(open) => !open && setResetTarget(null)}
        title="Reset override?"
        description={`${resetTarget?.label ?? "This setting"} will return to its startup default. The expected version is checked before reset.`}
        confirmLabel="Reset override"
        busy={Boolean(resetTarget && savingKey === resetTarget.key)}
        onConfirm={() => void reset()}
      />
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
      <Switch aria-label={setting.label} isDisabled={!setting.editable} isSelected={checked} onChange={onChange}>
        <Switch.Content><Switch.Control><Switch.Thumb /></Switch.Control>{checked ? "Enabled" : "Disabled"}</Switch.Content>
      </Switch>
    );
  }

  return (
    <TextField
      isDisabled={!setting.editable}
      value={value == null ? "" : String(value)}
      variant="secondary"
      onChange={(next) => onChange(setting.kind === "int" ? (next === "" ? null : Number(next)) : next)}
    >
      <Label className="sr-only">{setting.label}</Label>
      <Input type={setting.kind === "int" ? "number" : "text"} min={setting.min} max={setting.max} />
    </TextField>
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

function sourceLabel(source: SystemSetting["source"]) {
  return source === "db" ? "DB override" : source === "env" ? "Environment" : "Default";
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
