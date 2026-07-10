"use client";

import { TerminalWindow as ComponentLogsPageIcon } from "@phosphor-icons/react";
import { AlertTriangle, CheckCircle2, Loader2, RefreshCw, ScrollText } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";

type LogFile = {
  name: string;
  size: number;
  updated_at: string;
};

type LogResponse = {
  files?: LogFile[];
  selected?: string;
  lines?: string[];
  log_dir?: string;
};

const input =
  "h-9 min-w-0 rounded-md border border-input bg-background px-3 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-ring focus:ring-1 focus:ring-ring";
const iconBtn =
  "inline-flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-40";

export default function ComponentLogsPage() {
  const [files, setFiles] = useState<LogFile[]>([]);
  const [selected, setSelected] = useState("");
  const [lines, setLines] = useState<string[]>([]);
  const [lineCount, setLineCount] = useState(500);
  const [logDir, setLogDir] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(
    async (nextSelected = selected) => {
      setLoading(true);
      setError("");
      const params = new URLSearchParams({ lines: String(lineCount) });
      if (nextSelected) params.set("file", nextSelected);
      try {
        const res = await fetch(`/api/admin/component-logs?${params.toString()}`, {
          cache: "no-store",
        });
        if (!res.ok) throw new Error(await errorText(res));
        const body = (await res.json()) as LogResponse;
        const nextFiles = body.files ?? [];
        const nextSelectedFile = body.selected ?? "";
        setFiles(nextFiles);
        setSelected(nextSelectedFile);
        setLines(body.lines ?? []);
        setLogDir(body.log_dir ?? "");
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setLoading(false);
      }
    },
    [lineCount, selected],
  );

  useEffect(() => {
    void load();
  }, [load]);

  const selectedFile = useMemo(
    () => files.find((file) => file.name === selected),
    [files, selected],
  );

  return (
    <main className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border">
        <div className="mx-auto flex h-16 max-w-7xl items-center gap-3 px-6">
          <div className="admin-page-icon">
            <ComponentLogsPageIcon className="size-[18px]" weight="duotone" />
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-base font-semibold">Component Logs</h1>
            <p className="truncate text-xs text-muted-foreground">
              Structured stdout logs captured by the local runtime log directory
            </p>
          </div>
          <button className={iconBtn} title="Refresh component logs" onClick={() => void load()}>
            <RefreshCw className="size-4" />
          </button>
        </div>
      </header>

      <div className="mx-auto max-w-7xl space-y-5 px-6 py-6">
        <section className="grid gap-3 md:grid-cols-3">
          <Metric label="Log Files" value={String(files.length)} />
          <Metric label="Loaded Lines" value={String(lines.length)} />
          <Metric label="Selected Size" value={formatBytes(selectedFile?.size ?? 0)} />
        </section>

        <section className="rounded-lg border border-border bg-card">
          <div className="flex items-center gap-2 border-b border-border px-4 py-3">
            <ScrollText className="size-4 text-muted-foreground" />
            <h2 className="text-sm font-semibold">Source</h2>
          </div>
          <div className="grid gap-3 p-4 md:grid-cols-[minmax(240px,1fr)_160px_120px]">
            <select
              className={input}
              value={selected}
              onChange={(event) => {
                const value = event.target.value;
                setSelected(value);
                void load(value);
              }}
            >
              {files.map((file) => (
                <option key={file.name} value={file.name}>
                  {file.name}
                </option>
              ))}
              {files.length === 0 ? <option value="">No log files</option> : null}
            </select>
            <input
              className={input}
              type="number"
              min={1}
              max={2000}
              value={lineCount}
              onChange={(event) => setLineCount(Number(event.target.value))}
            />
            <button
              className="inline-flex h-9 items-center justify-center gap-2 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
              disabled={loading}
              onClick={() => void load()}
            >
              {loading ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <RefreshCw className="size-4" />
              )}
              Load
            </button>
          </div>
          <div className="border-t border-border px-4 py-2 font-mono text-xs text-muted-foreground">
            {logDir || "-"}
          </div>
        </section>

        {error ? (
          <div className="flex items-center gap-2 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            <AlertTriangle className="size-4 shrink-0" />
            <span className="min-w-0">{error}</span>
          </div>
        ) : null}

        <section className="overflow-hidden rounded-lg border border-border bg-card">
          <div className="flex items-center justify-between border-b border-border px-4 py-3">
            <h2 className="text-sm font-semibold">{selected || "Logs"}</h2>
            {loading ? (
              <span className="inline-flex items-center text-xs text-muted-foreground">
                <Loader2 className="mr-2 size-3 animate-spin" />
                Loading
              </span>
            ) : (
              <span className="inline-flex items-center text-xs text-muted-foreground">
                <CheckCircle2 className="mr-2 size-3" />
                Updated
              </span>
            )}
          </div>
          <pre className="h-[560px] overflow-auto bg-zinc-950 p-4 font-mono text-xs leading-5 text-zinc-100">
            {lines.length > 0 ? lines.join("\n") : "No component log lines"}
          </pre>
        </section>
      </div>
    </main>
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

function formatBytes(value: number) {
  if (!value) return "-";
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / 1024 / 1024).toFixed(1)} MB`;
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
