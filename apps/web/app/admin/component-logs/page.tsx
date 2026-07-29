"use client";

import { SquareTerminal as ComponentLogsPageIcon } from "lucide-react";
import { AlertTriangle, AlignLeft, CheckCircle2, HardDrive, Loader2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
} from "@/components/admin/admin-ui";
import { SelectControl } from "@/components/ui/select-control";

type LogFile = {
  name: string;
  label: string;
  size: number;
};

type LogResponse = {
  files?: LogFile[];
  selected?: string;
  lines?: string[];
};

const input =
  "h-9 min-w-0 rounded-md border border-input bg-background px-3 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-ring focus:ring-1 focus:ring-ring";

export default function ComponentLogsPage() {
  const [files, setFiles] = useState<LogFile[]>([]);
  const [selected, setSelected] = useState("");
  const [lines, setLines] = useState<string[]>([]);
  const [lineCount, setLineCount] = useState(500);
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
    <AdminPage className="admin-theme-slate">
      <AdminPageHeader
        icon={<ComponentLogsPageIcon className="size-5" />}
        title="Service Logs"
        description="Recent output from Cocola's core runtime services"
        actions={
          <AdminRefreshButton
            variant="outline"
            title="Refresh component logs"
            aria-label="Refresh component logs"
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
          label="Components"
          value={String(files.length)}
          tone="slate"
          icon={<ComponentLogsPageIcon />}
        />
        <Metric
          label="Loaded Lines"
          value={String(lines.length)}
          tone="sky"
          icon={<AlignLeft />}
        />
        <Metric
          label="Selected Size"
          value={formatBytes(selectedFile?.size ?? 0)}
          tone="violet"
          icon={<HardDrive />}
        />
      </section>

      <section className="admin-surface">
        <div className="admin-surface-head">
          <div>
            <div className="admin-surface-title">Source</div>
            <div className="admin-surface-sub">
              Pick a component and how many recent lines to load
            </div>
          </div>
        </div>
        <div className="grid gap-3 p-4 md:grid-cols-[minmax(240px,1fr)_160px_120px]">
          <SelectControl
            className={input}
            value={selected}
            onValueChange={(value) => {
              setSelected(value);
              void load(value);
            }}
            options={
              files.length
                ? files.map((file) => ({ value: file.name, label: file.label }))
                : [{ value: "", label: "No component logs", disabled: true }]
            }
            contentClassName="cocola-admin-ui"
          />
          <input
            className={input}
            type="number"
            min={1}
            max={2000}
            value={lineCount}
            onChange={(event) => setLineCount(Number(event.target.value))}
          />
          <button
            type="button"
            className="admin-card-btn admin-card-btn--accent justify-center"
            disabled={loading}
            onClick={() => void load()}
          >
            Load
          </button>
        </div>
      </section>

      {error ? (
        <div className="flex items-center gap-2 rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
          <AlertTriangle className="size-4 shrink-0" />
          <span className="min-w-0">{error}</span>
        </div>
      ) : null}

      <section className="admin-surface overflow-hidden">
        <div className="admin-surface-head">
          <div className="admin-surface-title">{selectedFile?.label ?? "Logs"}</div>
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
    </AdminPage>
  );
}

function Metric({
  label,
  value,
  tone,
  icon,
}: {
  label: string;
  value: string;
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
