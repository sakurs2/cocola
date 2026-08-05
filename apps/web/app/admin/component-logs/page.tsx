"use client";

import { SquareTerminal as ComponentLogsPageIcon } from "lucide-react";
import { AlertTriangle, CheckCircle2, Copy, Loader2 } from "lucide-react";
import { Button, Card, Input, Label, TextField, Tooltip } from "@heroui/react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { AdminPage, AdminPageHeader, AdminRefreshButton } from "@/components/admin/admin-ui";
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

export default function ComponentLogsPage() {
  const [files, setFiles] = useState<LogFile[]>([]);
  const [selected, setSelected] = useState("");
  const [lines, setLines] = useState<string[]>([]);
  const [lineCount, setLineCount] = useState(500);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);

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

      <Card className="p-4">
        <Card.Content className="grid gap-3 p-0 md:grid-cols-[minmax(240px,1fr)_160px_120px] md:items-end">
          <SelectControl
            className="w-full"
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
          <TextField value={String(lineCount)} variant="secondary" onChange={(value) => setLineCount(Math.max(1, Math.min(2000, Number(value) || 1)))}><Label>Lines</Label><Input type="number" min={1} max={2000} /></TextField>
          <Button isDisabled={loading} isPending={loading} onPress={() => void load()}>Load</Button>
        </Card.Content>
      </Card>

      {error ? (
        <div className="flex items-center gap-2 rounded-xl border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
          <AlertTriangle className="size-4 shrink-0" />
          <span className="min-w-0">{error}</span>
        </div>
      ) : null}

      <Card className="overflow-hidden p-0">
        <Card.Header className="flex-row items-center justify-between p-4">
          <span><Card.Title>{selectedFile?.label ?? "Logs"}</Card.Title><Card.Description>{lines.length} lines · {formatBytes(selectedFile?.size ?? 0)}</Card.Description></span>
          {loading ? (
            <span className="text-muted inline-flex items-center text-xs">
              <Loader2 className="mr-2 size-3 animate-spin" />
              Loading
            </span>
          ) : (
            <Tooltip delay={0}><Button isIconOnly aria-label="Copy loaded logs" variant="ghost" onPress={async () => { await navigator.clipboard.writeText(lines.join("\n")); setCopied(true); window.setTimeout(() => setCopied(false), 1600); }}>{copied ? <CheckCircle2 className="text-success size-4" /> : <Copy className="size-4" />}</Button><Tooltip.Content>{copied ? "Copied" : "Copy logs"}</Tooltip.Content></Tooltip>
          )}
        </Card.Header>
        <pre className="h-[560px] overflow-auto bg-zinc-950 p-4 font-mono text-xs leading-5 text-zinc-100">
          {lines.length > 0 ? lines.join("\n") : "No component log lines"}
        </pre>
      </Card>
    </AdminPage>
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
