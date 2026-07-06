"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { LoaderCircle, RotateCw, Save, ToggleLeft, ToggleRight } from "lucide-react";

type AgentPrompt = {
  id: string;
  name: string;
  content: string;
  enabled: boolean;
  scope: string;
  priority: number;
  version: number;
  updated_at?: string;
  updated_by?: string;
};

const textarea =
  "min-h-80 w-full resize-y rounded-md border border-input bg-background px-3 py-2 text-sm leading-6 text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-ring focus:ring-1 focus:ring-ring";
const btn =
  "inline-flex h-9 items-center justify-center gap-2 rounded-md border border-border bg-background px-3 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-50";
const primaryBtn =
  "inline-flex h-9 items-center justify-center gap-2 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-50";

const EMPTY_PROMPT: AgentPrompt = {
  id: "global",
  name: "Global Agent Prompt",
  content: "",
  enabled: false,
  scope: "global",
  priority: 100,
  version: 0,
};

export default function AdminAgentPromptPage() {
  const [prompt, setPrompt] = useState<AgentPrompt>(EMPTY_PROMPT);
  const [draft, setDraft] = useState("");
  const [enabled, setEnabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [savedAt, setSavedAt] = useState("");

  const dirty = useMemo(
    () => draft !== prompt.content || enabled !== prompt.enabled,
    [draft, enabled, prompt.content, prompt.enabled],
  );
  const charCount = draft.length;

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await fetch("/api/admin/agent-prompts/global", { cache: "no-store" });
      if (!res.ok) throw new Error(await readError(res));
      const data = (await res.json()) as AgentPrompt;
      setPrompt(data);
      setDraft(data.content ?? "");
      setEnabled(Boolean(data.enabled));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError("");
    try {
      const res = await fetch("/api/admin/agent-prompts/global", {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ content: draft, enabled }),
      });
      if (!res.ok) throw new Error(await readError(res));
      const data = (await res.json()) as AgentPrompt;
      setPrompt(data);
      setDraft(data.content ?? "");
      setEnabled(Boolean(data.enabled));
      setSavedAt(new Date().toLocaleTimeString());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const toggleEnabled = () => setEnabled((value) => !value);

  return (
    <main className="mx-auto max-w-5xl space-y-6 px-6 py-6">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold">Agent Prompt</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Configure the global system prompt applied to new agent turns.
          </p>
        </div>
        <div className="grid grid-cols-3 overflow-hidden rounded-md border border-border text-center text-xs">
          <Stat label="Status" value={enabled ? "On" : "Off"} />
          <Stat label="Version" value={prompt.version || 0} />
          <Stat label="Chars" value={charCount} />
        </div>
      </header>

      {error ? (
        <div className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600">
          {error}
        </div>
      ) : null}

      <section className="rounded-lg border border-border bg-card p-4">
        <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="text-sm font-semibold">Global system prompt</h2>
            <p className="mt-1 text-xs text-muted-foreground">
              Saved prompt content is injected server-side and is not shown in trace metadata.
            </p>
          </div>
          <div className="flex items-center gap-2">
            <button className={btn} type="button" onClick={toggleEnabled} disabled={saving}>
              {enabled ? <ToggleRight className="size-4" /> : <ToggleLeft className="size-4" />}
              {enabled ? "Enabled" : "Disabled"}
            </button>
            <button
              className={btn}
              type="button"
              onClick={() => void load()}
              disabled={loading || saving}
            >
              <RotateCw className={`size-4 ${loading ? "animate-spin" : ""}`} />
              Refresh
            </button>
            <button
              className={primaryBtn}
              type="button"
              onClick={() => void save()}
              disabled={saving || !dirty}
            >
              {saving ? (
                <LoaderCircle className="size-4 animate-spin" />
              ) : (
                <Save className="size-4" />
              )}
              Save
            </button>
          </div>
        </div>

        <textarea
          className={textarea}
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          placeholder="Write the global behavior policy for agents..."
          spellCheck={false}
        />

        <div className="mt-3 flex flex-wrap items-center justify-between gap-3 text-xs text-muted-foreground">
          <span>{dirty ? "Unsaved changes" : savedAt ? `Saved at ${savedAt}` : "Up to date"}</span>
          <span>
            Scope {prompt.scope || "global"} · Priority {prompt.priority || 100}
          </span>
        </div>
      </section>
    </main>
  );
}

function Stat({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="min-w-24 border-r border-border px-3 py-2 last:border-r-0">
      <div className="text-muted-foreground">{label}</div>
      <div className="mt-1 truncate text-sm font-semibold text-foreground">{value}</div>
    </div>
  );
}

async function readError(res: Response) {
  try {
    const data = await res.json();
    if (typeof data?.error === "string") return data.error;
    if (typeof data?.message === "string") return data.message;
  } catch {
    // fall through
  }
  return `${res.status} ${res.statusText}`;
}
