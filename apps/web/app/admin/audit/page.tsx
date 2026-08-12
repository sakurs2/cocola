"use client";

import { FileText } from "lucide-react";
import { Clock3 } from "lucide-react";
import { Button, Input, Label, SearchField, TextField } from "@heroui/react";
import { type DataGridColumn } from "@cocola/ui-compat/data-grid";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { Segment } from "@cocola/ui-compat/segment";
import { useCallback, useEffect, useState } from "react";
import { useFormatter, useTranslations } from "next-intl";
import {
  AdminDataGrid,
  AdminErrorDialog,
  AdminPage,
  AdminPageHeader,
  AdminPagination,
  AdminRefreshButton,
  AdminStatusBadge,
  AdminTruncatedValue,
} from "@/components/admin/admin-ui";

type ConversationRun = {
  trace_id: string;
  conversation_id: string;
  conversation_title?: string;
  user_id: string;
  user_email: string;
  source: string;
  model_alias: string;
  status: string;
  started_at: string;
  duration_ms: number;
  ttft_ms: number;
  error_code?: string;
};

const PAGE_SIZE = 50;

export default function AdminAuditPage() {
  const t = useTranslations("admin.runs");
  const format = useFormatter();
  const [runs, setRuns] = useState<ConversationRun[]>([]);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [source, setSource] = useState("");
  const [from, setFrom] = useState("");
  const [until, setUntil] = useState("");
  const [page, setPage] = useState(0);
  const [hasNext, setHasNext] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    const params = new URLSearchParams({
      limit: String(PAGE_SIZE + 1),
      offset: String(page * PAGE_SIZE),
    });
    if (search.trim()) params.set("search", search.trim());
    if (status) params.set("status", status);
    if (source) params.set("source", source);
    if (from) params.set("from", new Date(`${from}T00:00:00`).toISOString());
    if (until) params.set("until", new Date(`${until}T23:59:59.999`).toISOString());
    try {
      const response = await fetch(`/api/admin/conversation-runs?${params}`, {
        cache: "no-store",
      });
      if (!response.ok) throw new Error(await errorText(response));
      const body = (await response.json()) as { runs?: ConversationRun[] };
      const loadedRuns = Array.isArray(body.runs) ? body.runs : [];
      setHasNext(loadedRuns.length > PAGE_SIZE);
      setRuns(loadedRuns.slice(0, PAGE_SIZE));
    } catch (loadError) {
      setHasNext(false);
      setError(loadError instanceof Error ? loadError.message : String(loadError));
    } finally {
      setLoading(false);
    }
  }, [from, page, search, source, status, until]);

  useEffect(() => {
    void load();
  }, [load]);

  const columns: DataGridColumn<ConversationRun>[] = [
    {
      id: "started",
      header: t("columns.started"),
      minWidth: 170,
      cell: (run) => (
        <span className="text-muted text-xs tabular-nums">
          {format.dateTime(new Date(run.started_at), {
            month: "short",
            day: "2-digit",
            hour: "2-digit",
            minute: "2-digit",
            second: "2-digit",
          })}
        </span>
      ),
    },
    {
      id: "user",
      header: t("columns.user"),
      minWidth: 210,
      cell: (run) => (
        <AdminTruncatedValue
          className="text-sm font-medium"
          copyLabel={t("copy.user")}
          value={run.user_email || run.user_id || "—"}
        />
      ),
    },
    {
      id: "conversation",
      header: t("columns.conversation"),
      isRowHeader: true,
      minWidth: 280,
      cell: (run) => (
        <span className="block min-w-0">
          <AdminTruncatedValue
            className="text-sm font-semibold"
            copyLabel={t("copy.conversationTitle")}
            value={run.conversation_title || t("untitled")}
          />
          {run.conversation_id ? (
            <AdminTruncatedValue
              className="text-accent font-mono text-xs"
              copyLabel={t("copy.conversationId")}
              href={`/conversations/${encodeURIComponent(run.conversation_id)}`}
              value={run.conversation_id}
            />
          ) : null}
        </span>
      ),
    },
    {
      id: "source",
      header: t("columns.source"),
      minWidth: 140,
      cell: (run) => (
        <span className="text-muted text-sm">
          {run.source === "scheduled_task" ? t("sources.scheduled") : t("sources.interactive")}
        </span>
      ),
    },
    {
      id: "model",
      header: t("columns.model"),
      minWidth: 160,
      cell: (run) => (
        <AdminTruncatedValue
          className="text-sm"
          copyLabel={t("copy.model")}
          value={run.model_alias || t("defaultModel")}
        />
      ),
    },
    {
      id: "latency",
      header: t("columns.latency"),
      minWidth: 150,
      cell: (run) => (
        <span className="font-mono text-xs">
          <span className="block">
            {t("latency.total", { duration: formatDuration(run.duration_ms) })}
          </span>
          <span className="text-muted block">TTFT {formatDuration(run.ttft_ms)}</span>
        </span>
      ),
    },
    {
      id: "result",
      header: t("columns.result"),
      minWidth: 150,
      cell: (run) => (
        <span>
          <RunStatus status={run.status} />
          {run.error_code ? (
            <AdminTruncatedValue
              className="text-muted mt-1 font-mono text-xs"
              copyLabel={t("copy.errorCode")}
              value={run.error_code}
            />
          ) : null}
        </span>
      ),
    },
    {
      id: "trace",
      header: t("columns.traceId"),
      minWidth: 280,
      cell: (run) => (
        <span className="flex min-w-0 items-center gap-2">
          <span className="min-w-0 flex-1 font-mono text-xs">
            <AdminTruncatedValue copyLabel={t("copy.traceId")} value={run.trace_id || "—"} />
          </span>
          <Button
            size="sm"
            variant="outline"
            onPress={() => {
              window.location.href = `/admin/traces/${encodeURIComponent(run.trace_id)}`;
            }}
          >
            {t("open")}
          </Button>
        </span>
      ),
    },
  ];

  return (
    <AdminPage className="admin-theme-indigo">
      <AdminPageHeader
        icon={<FileText className="size-5" />}
        title={t("title")}
        description={t("description")}
        actions={
          <AdminRefreshButton onClick={() => void load()} refreshing={loading} disabled={loading}>
            {t("refresh")}
          </AdminRefreshButton>
        }
      />

      <div className="grid gap-3 xl:grid-cols-[minmax(260px,1fr)_auto_auto] xl:items-end">
        <SearchField
          aria-label={t("searchAria")}
          className="w-full"
          value={search}
          onChange={(value) => {
            setPage(0);
            setSearch(value);
          }}
        >
          <SearchField.Group>
            <SearchField.SearchIcon />
            <SearchField.Input placeholder={t("searchPlaceholder")} />
            <SearchField.ClearButton />
          </SearchField.Group>
        </SearchField>
        <Segment
          aria-label={t("resultFilter")}
          selectedKey={status || "all"}
          onSelectionChange={(key) => {
            setPage(0);
            setStatus(String(key) === "all" ? "" : String(key));
          }}
        >
          <Segment.Item id="all">{t("statuses.all")}</Segment.Item>
          <Segment.Item id="running">{t("statuses.running")}</Segment.Item>
          <Segment.Item id="success">{t("statuses.success")}</Segment.Item>
          <Segment.Item id="error">{t("statuses.error")}</Segment.Item>
          <Segment.Item id="cancelled">{t("statuses.cancelled")}</Segment.Item>
        </Segment>
        <Segment
          aria-label={t("sourceFilter")}
          selectedKey={source || "all"}
          onSelectionChange={(key) => {
            setPage(0);
            setSource(String(key) === "all" ? "" : String(key));
          }}
        >
          <Segment.Item id="all">{t("sources.all")}</Segment.Item>
          <Segment.Item id="interactive">{t("sources.interactive")}</Segment.Item>
          <Segment.Item id="scheduled_task">{t("sources.scheduledShort")}</Segment.Item>
        </Segment>
      </div>
      <div className="grid max-w-xl gap-3 sm:grid-cols-2">
        <TextField
          value={from}
          variant="secondary"
          onChange={(value) => {
            setPage(0);
            setFrom(value);
          }}
        >
          <Label>{t("from")}</Label>
          <Input type="date" />
        </TextField>
        <TextField
          value={until}
          variant="secondary"
          onChange={(value) => {
            setPage(0);
            setUntil(value);
          }}
        >
          <Label>{t("until")}</Label>
          <Input type="date" />
        </TextField>
      </div>

      <AdminErrorDialog
        error={error}
        title={t("loadFailed")}
        onDismiss={() => setError("")}
        onRetry={() => void load()}
      />

      <AdminDataGrid
        aria-label={t("tableAria")}
        columns={columns}
        contentClassName="min-w-[1420px]"
        data={runs}
        getRowId={(run) => run.trace_id}
        selectionMode="none"
        variant="primary"
        renderEmptyState={() => (
          <EmptyState>
            <EmptyState.Header>
              <EmptyState.Media variant="icon">
                <Clock3 className="text-indigo-500" />
              </EmptyState.Media>
              <EmptyState.Title>
                {loading ? t("empty.loadingTitle") : t("empty.title")}
              </EmptyState.Title>
              <EmptyState.Description>
                {loading ? t("empty.loadingDescription") : t("empty.description")}
              </EmptyState.Description>
            </EmptyState.Header>
          </EmptyState>
        )}
      />
      <AdminPagination
        page={page}
        pageSize={PAGE_SIZE}
        count={runs.length}
        hasNext={hasNext}
        loading={loading}
        label={t("paginationLabel")}
        onPageChange={setPage}
      />
    </AdminPage>
  );
}

function RunStatus({ status }: { status: string }) {
  const t = useTranslations("admin.runs.statuses");
  const tone =
    status === "success"
      ? "green"
      : status === "running"
        ? "sky"
        : status === "cancelled" || status === "interrupted"
          ? "amber"
          : "red";
  return (
    <AdminStatusBadge tone={tone} dot>
      {status === "success" || status === "running" || status === "error" || status === "cancelled"
        ? t(status)
        : status === "interrupted"
          ? t("interrupted")
          : status || t("unknown")}
    </AdminStatusBadge>
  );
}

function formatDuration(ms: number) {
  if (!ms) return "—";
  if (ms < 1000) return `${ms} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 2 : 1)} s`;
  return `${(ms / 60_000).toFixed(1)} min`;
}

async function errorText(response: Response) {
  try {
    const body = (await response.json()) as { error?: string | { message?: string } };
    if (typeof body.error === "string") return body.error;
    if (body.error?.message) return body.error.message;
  } catch {
    // Fall through to the status line.
  }
  return `${response.status} ${response.statusText}`;
}
