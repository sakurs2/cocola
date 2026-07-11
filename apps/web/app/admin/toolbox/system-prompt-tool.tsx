"use client";

import { FileText } from "@phosphor-icons/react";
import { ArrowRight, CircleAlert, LoaderCircle, Save, ToggleLeft, ToggleRight } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { AdminAlert, AdminDrawer, AdminStatusBadge } from "@/components/admin/admin-ui";
import { Button } from "@/components/ui/button";

type AgentPrompt = {
  content: string;
  enabled: boolean;
  version: number;
};

const EMPTY_PROMPT: AgentPrompt = {
  content: "",
  enabled: false,
  version: 0,
};

const textarea =
  "min-h-80 w-full resize-y rounded-xl border border-input bg-background px-3 py-2.5 text-sm leading-6 text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-ring focus:ring-1 focus:ring-ring";

export function SystemPromptTool({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [prompt, setPrompt] = useState<AgentPrompt>(EMPTY_PROMPT);
  const [draft, setDraft] = useState("");
  const [enabled, setEnabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [saveError, setSaveError] = useState("");

  const dirty = useMemo(
    () => draft !== prompt.content || enabled !== prompt.enabled,
    [draft, enabled, prompt.content, prompt.enabled],
  );

  const applyPrompt = useCallback((nextPrompt: AgentPrompt) => {
    setPrompt(nextPrompt);
    setDraft(nextPrompt.content ?? "");
    setEnabled(Boolean(nextPrompt.enabled));
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError("");
    try {
      const response = await fetch("/api/admin/agent-prompts/global", { cache: "no-store" });
      if (!response.ok) throw new Error(await readError(response));
      applyPrompt((await response.json()) as AgentPrompt);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : String(error));
    } finally {
      setLoading(false);
    }
  }, [applyPrompt]);

  useEffect(() => {
    void load();
  }, [load]);

  const setOpen = (nextOpen: boolean) => {
    if (saving) return;
    setDraft(prompt.content);
    setEnabled(prompt.enabled);
    setSaveError("");
    onOpenChange(nextOpen);
  };

  const save = async () => {
    if (!dirty || loading) return;
    setSaving(true);
    setSaveError("");
    try {
      const response = await fetch("/api/admin/agent-prompts/global", {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ content: draft, enabled }),
      });
      if (!response.ok) throw new Error(await readError(response));
      applyPrompt((await response.json()) as AgentPrompt);
      onOpenChange(false);
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : String(error));
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <button
        type="button"
        className="admin-module-card group w-full text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
        onClick={() => setOpen(true)}
      >
        <span className="admin-module-icon bg-sky-500/10 text-sky-700">
          <FileText className="size-[18px]" weight="duotone" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-sm font-semibold text-foreground">System Prompt</span>
          <span className="mt-1 block text-xs leading-5 text-muted-foreground">
            Set the global behavior policy applied to new agent turns.
          </span>
          <span className="mt-4 flex flex-wrap items-center gap-2">
            <PromptStatus loading={loading} error={Boolean(loadError)} enabled={prompt.enabled} />
            {!loading && !loadError ? (
              <span className="font-mono text-[10px] text-muted-foreground">
                v{prompt.version || 0}
              </span>
            ) : null}
          </span>
        </span>
        <ArrowRight className="mt-1 size-4 shrink-0 -translate-x-1 text-muted-foreground opacity-0 transition-all duration-200 group-hover:translate-x-0 group-hover:text-primary group-hover:opacity-100" />
      </button>

      <AdminDrawer
        open={open}
        onOpenChange={setOpen}
        title="System Prompt"
        description="Set the global behavior policy applied to new agent turns."
        size="lg"
        footer={
          <div className="flex items-center justify-end gap-2">
            <Button variant="outline" disabled={saving} onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button
              className="min-w-32 gap-2"
              disabled={saving || loading || !dirty}
              onClick={() => void save()}
            >
              {saving ? (
                <LoaderCircle className="size-4 animate-spin" />
              ) : (
                <Save className="size-4" />
              )}
              {saving ? "Saving…" : "Save changes"}
            </Button>
          </div>
        }
      >
        <div className="space-y-5">
          {loading ? (
            <div className="flex min-h-48 items-center justify-center text-sm text-muted-foreground">
              <LoaderCircle className="mr-2 size-4 animate-spin" />
              Loading system prompt
            </div>
          ) : (
            <>
              {loadError ? (
                <AdminAlert tone="error" icon={<CircleAlert className="size-4" />}>
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <span>{loadError}</span>
                    <Button variant="outline" size="sm" onClick={() => void load()}>
                      Retry
                    </Button>
                  </div>
                </AdminAlert>
              ) : null}
              {saveError ? (
                <AdminAlert tone="error" icon={<CircleAlert className="size-4" />}>
                  {saveError}
                </AdminAlert>
              ) : null}

              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <div className="text-sm font-semibold text-foreground">Global system prompt</div>
                  <p className="mt-1 max-w-xl text-xs leading-5 text-muted-foreground">
                    Saved content is injected server-side and is never copied into trace metadata.
                  </p>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  className="gap-2"
                  onClick={() => setEnabled((value) => !value)}
                  disabled={saving || Boolean(loadError)}
                >
                  {enabled ? <ToggleRight className="size-4" /> : <ToggleLeft className="size-4" />}
                  {enabled ? "Enabled" : "Disabled"}
                </Button>
              </div>

              <textarea
                className={textarea}
                value={draft}
                onChange={(event) => setDraft(event.target.value)}
                placeholder="Write the global behavior policy for agents..."
                spellCheck={false}
                disabled={saving || Boolean(loadError)}
              />

              <div className="grid gap-3 sm:grid-cols-2">
                <PromptMeta label="Version" value={prompt.version || 0} />
                <PromptMeta label="Characters" value={draft.length} />
              </div>
              <div className="text-xs text-muted-foreground">
                {dirty ? "Unsaved changes" : "Up to date"}
              </div>
            </>
          )}
        </div>
      </AdminDrawer>
    </>
  );
}

function PromptStatus({
  loading,
  error,
  enabled,
}: {
  loading: boolean;
  error: boolean;
  enabled: boolean;
}) {
  if (loading) return <AdminStatusBadge>Loading</AdminStatusBadge>;
  if (error) return <AdminStatusBadge tone="red">Unavailable</AdminStatusBadge>;
  return (
    <AdminStatusBadge tone={enabled ? "green" : "neutral"} dot={enabled}>
      {enabled ? "Enabled" : "Disabled"}
    </AdminStatusBadge>
  );
}

function PromptMeta({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="rounded-xl border border-border/70 bg-muted/35 px-3 py-2.5">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 font-mono text-sm font-semibold tabular-nums text-foreground">
        {value}
      </div>
    </div>
  );
}

async function readError(response: Response) {
  try {
    const data = await response.json();
    if (typeof data?.error === "string") return data.error;
    if (typeof data?.message === "string") return data.message;
  } catch {
    // fall through
  }
  return `${response.status} ${response.statusText}`;
}
