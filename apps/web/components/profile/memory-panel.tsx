"use client";

import {
  BrainCircuit,
  ChevronDown,
  ChevronRight,
  LoaderCircle,
  RefreshCw,
  Trash2,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";

type MemorySettings = {
  global_enabled: boolean;
  use_enabled: boolean;
  learn_enabled: boolean;
};

type MemoryItem = {
  id: string;
  category: string;
  title: string;
  abstract?: string;
  content?: string;
};

export function MemoryPanel() {
  const [settings, setSettings] = useState<MemorySettings | null>(null);
  const [items, setItems] = useState<MemoryItem[]>([]);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const settingsResponse = await fetch("/api/memory/settings", { cache: "no-store" });
      if (!settingsResponse.ok) throw new Error(await responseError(settingsResponse));
      setSettings((await settingsResponse.json()) as MemorySettings);

      const itemsResponse = await fetch("/api/memory/items?limit=50", { cache: "no-store" });
      if (!itemsResponse.ok) throw new Error(await responseError(itemsResponse));
      const body = (await itemsResponse.json()) as { items?: MemoryItem[] };
      setItems(body.items ?? []);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function updateSettings(next: MemorySettings) {
    if (!settings?.global_enabled) return;
    setSaving(true);
    setError("");
    try {
      const response = await fetch("/api/memory/settings", {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          use_enabled: next.use_enabled,
          learn_enabled: next.learn_enabled,
        }),
      });
      if (!response.ok) throw new Error(await responseError(response));
      setSettings((await response.json()) as MemorySettings);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  async function toggleItem(item: MemoryItem) {
    if (expanded === item.id) {
      setExpanded(null);
      return;
    }
    setExpanded(item.id);
    if (item.content) return;
    const response = await fetch(`/api/memory/items/${encodeURIComponent(item.id)}`, {
      cache: "no-store",
    });
    if (!response.ok) {
      setError(await responseError(response));
      return;
    }
    const detail = (await response.json()) as MemoryItem;
    setItems((current) => current.map((value) => (value.id === item.id ? detail : value)));
  }

  async function deleteItem(id: string) {
    setSaving(true);
    const response = await fetch(`/api/memory/items/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
    setSaving(false);
    if (!response.ok) {
      setError(await responseError(response));
      return;
    }
    setItems((current) => current.filter((item) => item.id !== id));
    if (expanded === id) setExpanded(null);
  }

  async function clearAll() {
    if (!window.confirm("Delete all your saved memories? This cannot be undone.")) return;
    setSaving(true);
    const response = await fetch("/api/memory/items", { method: "DELETE" });
    setSaving(false);
    if (!response.ok) {
      setError(await responseError(response));
      return;
    }
    setItems([]);
    setExpanded(null);
  }

  return (
    <section className="rounded-2xl border border-border bg-card shadow-card">
      <div className="flex items-center gap-3 border-b border-border px-4 py-3">
        <div className="grid size-8 place-items-center rounded-xl bg-emerald-500/10">
          <BrainCircuit className="size-4 text-emerald-600" />
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold">Memory</h2>
          <p className="text-xs text-muted-foreground">
            Control how Cocola uses and learns your long-term preferences.
          </p>
        </div>
        <Button size="icon" variant="ghost" disabled={loading} onClick={() => void load()}>
          <RefreshCw className={`size-4 ${loading ? "animate-spin" : ""}`} />
          <span className="sr-only">Refresh memory</span>
        </Button>
      </div>

      <div className="space-y-4 p-4">
        {loading ? (
          <div className="flex items-center justify-center py-8 text-sm text-muted-foreground">
            <LoaderCircle className="mr-2 size-4 animate-spin" /> Loading memory
          </div>
        ) : settings ? (
          <>
            {!settings.global_enabled ? (
              <div className="rounded-xl border border-amber-500/25 bg-amber-500/[0.06] px-3 py-2 text-sm">
                Disabled by administrator. You can still view and delete existing memories.
              </div>
            ) : null}
            <div className="grid gap-3 sm:grid-cols-2">
              <MemoryToggle
                label="Use memories"
                description="Recall relevant memories before answering."
                checked={settings.use_enabled}
                disabled={saving || !settings.global_enabled}
                onChange={(use_enabled) => void updateSettings({ ...settings, use_enabled })}
              />
              <MemoryToggle
                label="Learn from conversations"
                description="Capture useful facts from new conversations."
                checked={settings.learn_enabled}
                disabled={saving || !settings.global_enabled}
                onChange={(learn_enabled) => void updateSettings({ ...settings, learn_enabled })}
              />
            </div>

            <div className="flex items-center justify-between gap-3 border-t border-border pt-4">
              <div>
                <div className="text-sm font-semibold">Saved memories</div>
                <div className="text-xs text-muted-foreground">{items.length} visible items</div>
              </div>
              <Button
                size="sm"
                variant="outline"
                className="gap-2 text-destructive"
                disabled={saving || items.length === 0}
                onClick={() => void clearAll()}
              >
                <Trash2 className="size-3.5" /> Clear all
              </Button>
            </div>

            {items.length === 0 ? (
              <div className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-sm text-muted-foreground">
                No saved memories yet.
              </div>
            ) : (
              <div className="divide-y divide-border overflow-hidden rounded-xl border border-border">
                {items.map((item) => (
                  <div key={item.id} className="bg-background">
                    <div className="flex items-center gap-2 px-3 py-2.5">
                      <button
                        type="button"
                        className="flex min-w-0 flex-1 items-center gap-2 text-left"
                        onClick={() => void toggleItem(item)}
                      >
                        {expanded === item.id ? (
                          <ChevronDown className="size-4 shrink-0 text-muted-foreground" />
                        ) : (
                          <ChevronRight className="size-4 shrink-0 text-muted-foreground" />
                        )}
                        <span className="min-w-0">
                          <span className="block truncate text-sm font-medium">{item.title}</span>
                          <span className="block text-xs capitalize text-muted-foreground">
                            {item.category}
                          </span>
                        </span>
                      </button>
                      <Button
                        size="icon"
                        variant="ghost"
                        disabled={saving}
                        onClick={() => void deleteItem(item.id)}
                      >
                        <Trash2 className="size-4 text-destructive" />
                        <span className="sr-only">Delete memory</span>
                      </Button>
                    </div>
                    {expanded === item.id ? (
                      <div className="border-t border-border bg-muted/25 px-9 py-3 text-sm leading-6 text-muted-foreground whitespace-pre-wrap">
                        {item.content || item.abstract || "Loading…"}
                      </div>
                    ) : null}
                  </div>
                ))}
              </div>
            )}
          </>
        ) : null}
        {error ? <div className="text-sm text-destructive">{error}</div> : null}
      </div>
    </section>
  );
}

function MemoryToggle({
  label,
  description,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  description: string;
  checked: boolean;
  disabled: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="flex items-start gap-3 rounded-xl border border-border bg-background p-3">
      <input
        type="checkbox"
        className="mt-1"
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span>
        <span className="block text-sm font-medium">{label}</span>
        <span className="mt-0.5 block text-xs leading-5 text-muted-foreground">{description}</span>
      </span>
    </label>
  );
}

async function responseError(response: Response) {
  try {
    const body = await response.json();
    if (typeof body?.error === "string") return body.error;
    if (typeof body?.error?.message === "string") return body.error.message;
  } catch {
    // Fall back to status text.
  }
  return `${response.status} ${response.statusText}`;
}
