"use client";

import { AlertCircle, CheckCircle2, FileCode2, RotateCcw, Save } from "lucide-react";
import { FormEvent, useEffect, useMemo, useState } from "react";

const MAX_CONTENT_BYTES = 32 * 1024;

type AgentInstructionsRecord = {
  content: string;
  version: number;
};

type Notice = { tone: "success" | "error"; message: string } | null;

export function AgentInstructionsPanel() {
  const [content, setContent] = useState("");
  const [savedContent, setSavedContent] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState<Notice>(null);
  const contentBytes = useMemo(() => new TextEncoder().encode(content).byteLength, [content]);
  const tooLarge = contentBytes > MAX_CONTENT_BYTES;
  const changed = content !== savedContent;

  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      try {
        const response = await fetch("/api/account/agent-instructions", {
          cache: "no-store",
          signal: controller.signal,
        });
        const body = await response.json();
        if (!response.ok) {
          throw new Error(instructionsError(body, "Could not load AGENTS.md."));
        }
        const record = body as AgentInstructionsRecord;
        setContent(record.content || "");
        setSavedContent(record.content || "");
      } catch (error) {
        if (controller.signal.aborted) return;
        setNotice({
          tone: "error",
          message: error instanceof Error ? error.message : "Could not load AGENTS.md.",
        });
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    }
    void load();
    return () => controller.abort();
  }, []);

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!changed || tooLarge || saving) return;
    setNotice(null);
    setSaving(true);
    try {
      const response = await fetch("/api/account/agent-instructions", {
        method: "PUT",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ content }),
      });
      const body = await response.json();
      if (!response.ok) {
        throw new Error(instructionsError(body, "Could not save AGENTS.md."));
      }
      const record = body as AgentInstructionsRecord;
      setContent(record.content || "");
      setSavedContent(record.content || "");
      setNotice({ tone: "success", message: "AGENTS.md saved." });
    } catch (error) {
      setNotice({
        tone: "error",
        message: error instanceof Error ? error.message : "Could not save AGENTS.md.",
      });
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="overflow-hidden rounded-2xl border border-border bg-card shadow-card">
      <div className="flex items-center gap-3 border-b border-border px-4 py-3">
        <div className="grid size-8 place-items-center rounded-xl bg-violet-500/10">
          <FileCode2 className="size-4 text-violet-600" />
        </div>
        <div className="min-w-0">
          <h2 className="text-sm font-semibold">Agent instructions</h2>
          <p className="text-xs text-muted-foreground">
            Persistent preferences applied to each agent turn.
          </p>
        </div>
      </div>

      <form className="space-y-4 p-4" onSubmit={save}>
        <div className="overflow-hidden rounded-xl border border-input bg-background">
          <div className="flex items-center justify-between border-b border-input bg-muted/50 px-3 py-2">
            <div className="flex items-center gap-2 font-mono text-xs font-medium">
              <span className="size-2 rounded-full bg-violet-500" aria-hidden />
              AGENTS.md
            </div>
            <span
              className={
                tooLarge
                  ? "font-mono text-[11px] text-destructive"
                  : "font-mono text-[11px] text-muted-foreground"
              }
            >
              {formatBytes(contentBytes)} / 32 KB
            </span>
          </div>
          <textarea
            aria-label="AGENTS.md content"
            value={content}
            onChange={(event) => {
              setContent(event.target.value);
              setNotice(null);
            }}
            disabled={loading || saving}
            spellCheck={false}
            placeholder={
              loading
                ? "Loading AGENTS.md…"
                : "# Working preferences\n- Answer in Chinese.\n- Keep code changes small and reviewable."
            }
            className="min-h-56 w-full resize-y bg-transparent px-4 py-3 font-mono text-[13px] leading-6 outline-none placeholder:text-muted-foreground/70 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring disabled:cursor-wait disabled:opacity-60"
          />
        </div>

        <p className="text-xs leading-5 text-muted-foreground">
          Your current request and repository-specific instructions can override these preferences.
          Administrator and safety policies always take priority.
        </p>

        {tooLarge ? (
          <div role="alert" className="flex items-start gap-2 text-sm text-destructive">
            <AlertCircle className="mt-0.5 size-4 shrink-0" />
            <span>AGENTS.md must be 32 KB or smaller.</span>
          </div>
        ) : null}
        {notice ? <NoticeLine notice={notice} /> : null}

        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
          <button
            type="button"
            onClick={() => {
              setContent("");
              setNotice(null);
            }}
            disabled={loading || saving || content.length === 0}
            className="inline-flex h-9 items-center gap-2 rounded-xl border border-input bg-background px-3 text-sm font-medium hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
          >
            <RotateCcw className="size-4" />
            Clear editor
          </button>
          <button
            type="submit"
            disabled={loading || saving || !changed || tooLarge}
            className="inline-flex h-9 items-center gap-2 rounded-xl bg-primary px-3 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <Save className="size-4" />
            {saving ? "Saving…" : "Save AGENTS.md"}
          </button>
        </div>
      </form>
    </section>
  );
}

function NoticeLine({ notice }: { notice: Exclude<Notice, null> }) {
  const Icon = notice.tone === "success" ? CheckCircle2 : AlertCircle;
  return (
    <div
      role={notice.tone === "error" ? "alert" : "status"}
      className={
        notice.tone === "success"
          ? "flex items-start gap-2 text-sm text-emerald-600"
          : "flex items-start gap-2 text-sm text-destructive"
      }
    >
      <Icon className="mt-0.5 size-4 shrink-0" />
      <span>{notice.message}</span>
    </div>
  );
}

function instructionsError(body: unknown, fallback: string): string {
  const envelope = body as { error?: { message?: string } };
  return envelope?.error?.message || fallback;
}

function formatBytes(value: number): string {
  if (value < 1024) return `${value} B`;
  return `${(value / 1024).toFixed(value < 10 * 1024 ? 1 : 0)} KB`;
}
