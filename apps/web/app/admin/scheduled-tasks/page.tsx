"use client";

import { Timer as ClockCountdown } from "lucide-react";
import { MoreHorizontal, Trash2 } from "lucide-react";
import { Button, Card, Dropdown, SearchField } from "@heroui/react";
import { type DataGridColumn } from "@cocola/ui-compat/data-grid";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { Segment } from "@cocola/ui-compat/segment";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useFormatter, useTranslations } from "next-intl";
import {
  AdminAlert,
  AdminDataGrid,
  AdminDrawer,
  AdminErrorDialog,
  AdminPage,
  AdminPageHeader,
  AdminRefreshButton,
  AdminStatusBadge,
  AdminTruncatedValue,
} from "@/components/admin/admin-ui";
import { TaskConfirmDialog } from "@/components/scheduled-tasks/task-drawer";
import { sortTasks, type ScheduledTask, type TaskRun } from "@/lib/scheduled-tasks";

type StatusFilter = "all" | ScheduledTask["status"] | "owner-required";

export default function ScheduledTasksPage() {
  const t = useTranslations("admin.scheduledTasksPage");
  const [tasks, setTasks] = useState<ScheduledTask[]>([]);
  const [runs, setRuns] = useState<TaskRun[]>([]);
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<StatusFilter>("all");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [selectedTask, setSelectedTask] = useState<ScheduledTask | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<ScheduledTask | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [tasksResponse, runsResponse] = await Promise.all([
        fetch("/api/admin/scheduled-tasks", { cache: "no-store" }),
        fetch("/api/admin/scheduled-task-runs?limit=200", { cache: "no-store" }),
      ]);
      if (!tasksResponse.ok) throw new Error(await responseError(tasksResponse));
      if (!runsResponse.ok) throw new Error(await responseError(runsResponse));
      const taskBody = (await tasksResponse.json()) as { tasks?: ScheduledTask[] };
      const runBody = (await runsResponse.json()) as { runs?: TaskRun[] };
      setTasks(Array.isArray(taskBody.tasks) ? taskBody.tasks : []);
      setRuns(Array.isArray(runBody.runs) ? runBody.runs : []);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const latestRun = useMemo(() => {
    const map = new Map<string, TaskRun>();
    for (const run of runs) if (!map.has(run.task_id)) map.set(run.task_id, run);
    return map;
  }, [runs]);

  const visibleTasks = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return sortTasks(tasks).filter((task) => {
      const statusMatches =
        status === "all" ||
        (status === "owner-required" ? !task.owner_user_id : task.status === status);
      if (!statusMatches) return false;
      if (!needle) return true;
      return [task.name, task.prompt, task.owner?.name, task.owner?.email]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(needle));
    });
  }, [query, status, tasks]);

  function view(task: ScheduledTask) {
    setSelectedTask(task);
    setDrawerOpen(true);
  }

  async function deleteTask(task: ScheduledTask) {
    setSaving(true);
    setError("");
    try {
      const response = await fetch(`/api/admin/scheduled-tasks/${encodeURIComponent(task.id)}`, {
        method: "DELETE",
      });
      if (!response.ok) throw new Error(await responseError(response));
      setDeleteTarget(null);
      if (selectedTask?.id === task.id) {
        setDrawerOpen(false);
        setSelectedTask(null);
      }
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  const columns: DataGridColumn<ScheduledTask>[] = [
    {
      id: "task",
      header: t("columns.task"),
      isRowHeader: true,
      minWidth: 300,
      cell: (task) => (
        <span className="block min-w-0 py-1">
          <AdminTruncatedValue
            className="font-semibold"
            copyLabel={t("copy.name")}
            onPress={() => view(task)}
            value={task.name}
          />
          <AdminTruncatedValue
            className="text-muted mt-0.5 text-xs"
            copyLabel={t("copy.prompt")}
            value={task.prompt}
          />
        </span>
      ),
    },
    {
      id: "owner",
      header: t("columns.owner"),
      minWidth: 190,
      cell: (task) =>
        task.owner_user_id ? (
          <span className="block min-w-0">
            <AdminTruncatedValue
              className="text-sm font-medium"
              copyLabel={t("copy.owner")}
              value={task.owner?.name || task.owner?.email || task.owner_user_id}
            />
            {task.owner?.name && task.owner.email ? (
              <AdminTruncatedValue
                className="text-muted text-xs"
                copyLabel={t("copy.email")}
                value={task.owner.email}
              />
            ) : null}
          </span>
        ) : (
          <AdminStatusBadge tone="amber">{t("ownerRequired")}</AdminStatusBadge>
        ),
    },
    {
      id: "schedule",
      header: t("columns.schedule"),
      minWidth: 150,
      cell: (task) => <TaskSchedule task={task} />,
    },
    {
      id: "next",
      header: t("columns.nextRun"),
      minWidth: 170,
      cell: (task) => <TaskDate value={task.next_run_at} />,
    },
    {
      id: "last",
      header: t("columns.lastResult"),
      minWidth: 170,
      cell: (task) => {
        const run = latestRun.get(task.id);
        return run ? (
          <span>
            <span className="block text-sm capitalize">{run.status}</span>
            <span className="text-muted block text-xs tabular-nums">
              <TaskDate value={run.finished_at || run.created_at} />
            </span>
          </span>
        ) : (
          <span className="text-muted">—</span>
        );
      },
    },
    {
      id: "status",
      header: t("columns.status"),
      width: 130,
      cell: (task) => <TaskStatus status={task.status} />,
    },
    {
      id: "actions",
      header: t("columns.actions"),
      align: "center",
      width: 80,
      cell: (task) => (
        <Dropdown>
          <Dropdown.Trigger
            aria-label={t("actionsFor", { name: task.name })}
            className="text-muted hover:bg-surface-secondary mx-auto grid size-9 place-items-center rounded-xl"
          >
            <MoreHorizontal className="size-4" />
          </Dropdown.Trigger>
          <Dropdown.Popover placement="bottom end">
            <Dropdown.Menu
              aria-label={t("actionsFor", { name: task.name })}
              onAction={(key) => {
                if (key === "view") view(task);
                if (key === "delete") setDeleteTarget(task);
              }}
            >
              <Dropdown.Item id="view" textValue={t("view")}>
                {t("view")}
              </Dropdown.Item>
              <Dropdown.Item id="delete" textValue={t("delete")}>
                <Trash2 className="text-danger size-4" />
                <span className="text-danger">{t("delete")}</span>
              </Dropdown.Item>
            </Dropdown.Menu>
          </Dropdown.Popover>
        </Dropdown>
      ),
    },
  ];

  return (
    <AdminPage className="admin-theme-green">
      <AdminPageHeader
        title={t("title")}
        description={t("description")}
        icon={<ClockCountdown className="size-5" />}
        actions={
          <AdminRefreshButton
            variant="outline"
            refreshing={loading}
            disabled={loading}
            onClick={() => void load()}
          >
            {t("refresh")}
          </AdminRefreshButton>
        }
      />

      <AdminErrorDialog
        error={error}
        title={t("operationFailed")}
        onDismiss={() => setError("")}
        onRetry={() => void load()}
      />

      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <SearchField
          aria-label={t("searchAria")}
          className="w-full lg:max-w-sm"
          value={query}
          onChange={setQuery}
        >
          <SearchField.Group>
            <SearchField.SearchIcon />
            <SearchField.Input placeholder={t("searchPlaceholder")} />
            <SearchField.ClearButton />
          </SearchField.Group>
        </SearchField>
        <Segment
          aria-label={t("statusFilter")}
          selectedKey={status}
          onSelectionChange={(key) => setStatus(String(key) as StatusFilter)}
        >
          <Segment.Item id="all">{t("all")}</Segment.Item>
          <Segment.Item id="active">{t("status.active")}</Segment.Item>
          <Segment.Item id="paused">{t("status.paused")}</Segment.Item>
          <Segment.Item id="completed">{t("status.completed")}</Segment.Item>
          <Segment.Item id="expired">{t("status.expired")}</Segment.Item>
          <Segment.Item id="owner-required">{t("ownerRequired")}</Segment.Item>
        </Segment>
      </div>

      <AdminDataGrid
        aria-label={t("tableAria")}
        columns={columns}
        contentClassName="min-w-[1060px]"
        data={visibleTasks}
        getRowId={(task) => task.id}
        selectionMode="none"
        variant="primary"
        renderEmptyState={() => (
          <EmptyState>
            <EmptyState.Header>
              <EmptyState.Media variant="icon">
                <ClockCountdown className="text-green-500" />
              </EmptyState.Media>
              <EmptyState.Title>
                {loading
                  ? t("empty.loading")
                  : tasks.length
                    ? t("empty.filtered")
                    : t("empty.none")}
              </EmptyState.Title>
              <EmptyState.Description>
                {loading
                  ? t("empty.loadingDescription")
                  : tasks.length
                    ? t("empty.filteredDescription")
                    : t("empty.description")}
              </EmptyState.Description>
            </EmptyState.Header>
          </EmptyState>
        )}
      />

      <AdminDrawer
        open={drawerOpen}
        onOpenChange={setDrawerOpen}
        className="admin-theme-green"
        title={selectedTask?.name || t("details.title")}
        description={t("details.description")}
        footer={
          selectedTask ? (
            <div className="flex items-center justify-between gap-2">
              <Button variant="danger-soft" onPress={() => setDeleteTarget(selectedTask)}>
                <Trash2 className="size-4" /> {t("delete")}
              </Button>
              <Button variant="outline" onPress={() => setDrawerOpen(false)}>
                {t("details.close")}
              </Button>
            </div>
          ) : null
        }
      >
        {selectedTask ? (
          <TaskDetails
            task={selectedTask}
            runs={runs.filter((run) => run.task_id === selectedTask.id)}
          />
        ) : null}
      </AdminDrawer>
      <TaskConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t("confirm.title")}
        description={t("confirm.description", {
          name: deleteTarget?.name ?? t("confirm.thisTask"),
        })}
        confirmLabel={t("confirm.action")}
        busy={saving}
        destructive
        admin
        className="admin-theme-green"
        onConfirm={() => deleteTarget && void deleteTask(deleteTarget)}
      />
    </AdminPage>
  );
}

function TaskStatus({ status }: { status: ScheduledTask["status"] }) {
  const t = useTranslations("admin.scheduledTasksPage.status");
  const tone =
    status === "active"
      ? "green"
      : status === "paused"
        ? "amber"
        : status === "completed"
          ? "green"
          : status === "expired"
            ? "red"
            : "neutral";
  return (
    <AdminStatusBadge tone={tone} dot>
      {t(status)}
    </AdminStatusBadge>
  );
}

function TaskDetails({ task, runs }: { task: ScheduledTask; runs: TaskRun[] }) {
  const t = useTranslations("admin.scheduledTasksPage.details");
  return (
    <div className="space-y-5">
      <Card className="p-4">
        <Card.Content className="grid gap-3 p-0 sm:grid-cols-2">
          <Detail label={t("owner")}>
            {task.owner?.name || task.owner?.email || task.owner_user_id || t("owner")}
            {task.owner?.name && task.owner.email ? (
              <span className="block text-xs text-muted">{task.owner.email}</span>
            ) : null}
          </Detail>
          <Detail label={t("status")}>
            <TaskStatus status={task.status} />
          </Detail>
          <Detail label={t("schedule")}>
            <TaskSchedule task={task} />
          </Detail>
          <Detail label={t("timezone")}>{task.timezone || "—"}</Detail>
          <Detail label={t("nextRun")}>
            <TaskDate value={task.next_run_at} />
          </Detail>
          <Detail label={t("lastRun")}>
            <TaskDate value={task.last_run_at} />
          </Detail>
          <Detail label={t("lastResult")}>
            <span className="capitalize">{task.last_status || "—"}</span>
          </Detail>
          <Detail label={t("ends")}>
            <TaskDate value={task.expires_at} />
          </Detail>
          <Detail label={t("model")}>
            <span className="font-mono text-xs">{task.model_alias || "—"}</span>
          </Detail>
          <Detail label={t("runs")}>
            <span className="tabular-nums">{task.run_count}</span>
          </Detail>
        </Card.Content>
      </Card>

      <section>
        <h3 className="text-xs font-medium text-muted">{t("prompt")}</h3>
        <p className="bg-surface-secondary mt-2 whitespace-pre-wrap rounded-2xl p-4 text-sm leading-6">
          {task.prompt}
        </p>
      </section>

      {task.last_error ? (
        <AdminAlert tone="error">
          <span className="font-medium">{t("lastError")}</span> {task.last_error}
        </AdminAlert>
      ) : null}

      {task.attachments?.length ? (
        <section>
          <h3 className="text-xs font-medium text-muted">{t("attachments")}</h3>
          <div className="mt-2 divide-y divide-border/60 rounded-2xl border border-border/70 bg-surface/60 px-4">
            {task.attachments.map((attachment) => (
              <div key={attachment.id || attachment.filename} className="py-2.5 text-sm">
                {attachment.filename}
              </div>
            ))}
          </div>
        </section>
      ) : null}

      {runs.length ? (
        <section>
          <h3 className="text-xs font-medium text-muted">{t("recentRuns")}</h3>
          <div className="mt-2 divide-y divide-border/60 rounded-2xl border border-border/70 bg-surface/60 px-4">
            {runs.slice(0, 8).map((run) => (
              <div key={run.id} className="py-3 text-xs">
                <div className="flex items-center justify-between gap-3">
                  <span className="font-medium capitalize">{run.status}</span>
                  <span className="text-muted">
                    <TaskDate value={run.finished_at || run.started_at || run.created_at} />
                  </span>
                </div>
                {run.error ? <p className="mt-1 text-danger">{run.error}</p> : null}
              </div>
            ))}
          </div>
        </section>
      ) : null}
    </div>
  );
}

function TaskDate({ value }: { value?: string }) {
  const format = useFormatter();
  if (!value) return <>—</>;
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return <>{value}</>;
  return (
    <span className="text-muted text-sm tabular-nums">
      {format.dateTime(new Date(timestamp), {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      })}
    </span>
  );
}

function TaskSchedule({ task }: { task: ScheduledTask }) {
  const t = useTranslations("tasks.schedule");
  const weekdayT = useTranslations("tasks.drawer.weekdays");
  const format = useFormatter();
  const spec = task.schedule_spec ?? {};
  const minute = String(Number(spec.minute ?? 0)).padStart(2, "0");
  const hour = String(Number(spec.hour ?? 0)).padStart(2, "0");
  if (task.schedule_kind === "once") {
    const timestamp = Date.parse(String(spec.run_at ?? ""));
    const date = Number.isFinite(timestamp)
      ? format.dateTime(new Date(timestamp), {
          month: "short",
          day: "numeric",
          hour: "2-digit",
          minute: "2-digit",
        })
      : "—";
    return <span className="text-muted text-sm">{t("once", { date })}</span>;
  }
  if (task.schedule_kind === "hourly")
    return <span className="text-muted text-sm">{t("hourly", { minute })}</span>;
  if (task.schedule_kind === "daily")
    return <span className="text-muted text-sm">{t("daily", { time: `${hour}:${minute}` })}</span>;
  if (task.schedule_kind === "weekly") {
    const weekdays = [
      "monday",
      "tuesday",
      "wednesday",
      "thursday",
      "friday",
      "saturday",
      "sunday",
    ] as const;
    const weekday = weekdays[Math.max(0, Math.min(6, Number(spec.weekday ?? 1) - 1))] ?? "monday";
    return (
      <span className="text-muted text-sm">
        {t("weekly", { weekday: weekdayT(weekday), time: `${hour}:${minute}` })}
      </span>
    );
  }
  return (
    <span className="text-muted text-sm">
      {t("monthly", { day: Number(spec.day ?? 1), time: `${hour}:${minute}` })}
    </span>
  );
}

function Detail({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted">{label}</dt>
      <dd className="mt-1 break-words text-sm font-medium text-foreground">{children}</dd>
    </div>
  );
}

async function responseError(response: Response): Promise<string> {
  const text = await response.text();
  if (!text) return `${response.status} ${response.statusText}`;
  try {
    const body = JSON.parse(text) as {
      error?: string | { code?: string; message?: string };
      message?: string;
    };
    if (typeof body.error === "object") return body.error.message || body.error.code || text;
    return body.message || body.error || text;
  } catch {
    return text;
  }
}
